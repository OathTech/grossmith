package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"grossmith/gen"
	"grossmith/harness"
)

// Battery functions in the CLI (gc-verified arc G1): sources are written
// with each case, then every battery runs under the pinned reference and
// its functions.json is written before the manifest digests the tree.

type batteryJob struct {
	id, dir string
	specs   []harness.BatterySpec
}

func writeBatterySources(caseDir string, c gen.Case) error {
	bdir := filepath.Join(caseDir, harness.BatteryDir)
	if err := os.Mkdir(bdir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bdir, harness.FunctionsGo), c.FunctionsSource, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bdir, harness.BatteryDriverGo), c.BatteryDriver, 0o644)
}

func batterySpecs(c gen.Case) []harness.BatterySpec {
	specs := make([]harness.BatterySpec, len(c.Functions))
	for i, f := range c.Functions {
		specs[i] = harness.BatterySpec{Name: f.Name, Params: f.Params, Results: f.Results,
			Inputs: f.Inputs, Features: f.Features}
	}
	return specs
}

// runBatteries runs every job on a worker pool. The first failure fails
// the batch: a case without expected outcomes is not a battery case.
func runBatteries(ref *harness.GcAdapter, oracle harness.OracleIdentity, jobs []batteryJob, workers int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	work := make(chan batteryJob)
	var mu sync.Mutex
	var first error
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if first == nil {
			first = err
			cancel()
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				if ctx.Err() != nil {
					continue
				}
				b, err := ref.RunBattery(ctx, j.dir, j.id, j.specs, oracle)
				if err == nil {
					err = os.WriteFile(filepath.Join(j.dir, harness.FunctionsJSON), b, 0o644)
				}
				if err != nil {
					fail(err)
				}
			}
		}()
	}
	for _, j := range jobs {
		work <- j
	}
	close(work)
	wg.Wait()
	return first
}

// replayBattery checks a replayed case's battery against the record: the
// regenerated sources must equal the case's battery files byte for byte,
// and a fresh reference run must reproduce functions.json's functions,
// inputs and outcomes.
func replayBattery(caseDir, caseID string, c gen.Case, goBin string, timeout time.Duration) error {
	recorded, err := harness.ReadFunctionsDoc(filepath.Join(caseDir, harness.FunctionsJSON))
	if err != nil {
		return err
	}
	for name, want := range map[string][]byte{harness.FunctionsGo: c.FunctionsSource, harness.BatteryDriverGo: c.BatteryDriver} {
		got, err := os.ReadFile(filepath.Join(caseDir, harness.BatteryDir, name))
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("replayed %s/%s differs from the case's file", harness.BatteryDir, name)
		}
	}
	ref := &harness.GcAdapter{GoBin: goBin, Timeout: timeout, AdapterName: "gc"}
	oracle, err := ref.Oracle(context.Background())
	if err != nil {
		return err
	}
	b, err := ref.RunBattery(context.Background(), caseDir, caseID, batterySpecs(c), oracle)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "grossmith-functions-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	fresh, err := harness.ReadFunctionsDoc(tmp.Name())
	if err != nil {
		return err
	}
	if !harness.SameBattery(recorded, fresh) {
		return fmt.Errorf("replayed battery outcomes differ from %s (recorded under %s, replayed under %s)", harness.FunctionsJSON, recorded.Oracle.Version, fresh.Oracle.Version)
	}
	fmt.Printf("replay %s: battery sources byte-identical, %d functions' outcomes reproduced\n", caseID, len(fresh.Functions))
	return nil
}
