package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/harness"
	"grossmith/observe"
)

func batteryBase(out string) config {
	cfg := base(out)
	cfg.functions, cfg.batteryInputs, cfg.functionPanics = 2, 8, "biased"
	cfg.explicit = map[string]bool{}
	return cfg
}

// TestBatteryFlagsRefuseBeforeWrites: battery flags that cannot take
// effect, or that the generator would refuse, fail before anything is
// written.
func TestBatteryFlagsRefuseBeforeWrites(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")
	fake := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fake, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fake, "scripts", "diff-coverage"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mutate := map[string]func(*config){
		"negative functions":           func(c *config) { c.functions = -1 },
		"too many functions":           func(c *config) { c.functions = 65 },
		"too many inputs":              func(c *config) { c.batteryInputs = 5000 },
		"zero inputs":                  func(c *config) { c.batteryInputs = 0 },
		"unknown panic density":        func(c *config) { c.functionPanics = "often" },
		"inputs without functions":     func(c *config) { c.functions = 0; c.explicit["battery-inputs"] = true },
		"density without functions":    func(c *config) { c.functions = 0; c.explicit["function-panics"] = true },
		"int params without functions": func(c *config) { c.functions = 0; c.functionInts = true },
		"functions with golean":        func(c *config) { c.clone = "golean:" + fake },
		"functions with source check":  func(c *config) { c.check = filepath.Join(fake, "subject.go") },
	}
	for name, m := range mutate {
		cfg := batteryBase(out)
		m(&cfg)
		if err := run(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%s: wrote to %s before refusing", name, out)
		}
	}
}

// TestBatteryBatchEndToEnd: a battery batch publishes, verifies, and
// replays (sources byte-identical, outcomes reproduced by a fresh run).
func TestBatteryBatchEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs batteries")
	}
	out := filepath.Join(t.TempDir(), "batch")
	cfg := batteryBase(out)
	cfg.timeout = 10 * time.Second
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	info, err := harness.VerifyBatch(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.IDs) != 2 {
		t.Fatalf("verified %d cases", len(info.IDs))
	}
	for _, id := range info.IDs {
		d, err := harness.ReadFunctionsDoc(filepath.Join(out, id, harness.FunctionsJSON))
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Functions) != 2 || len(d.Functions[0].Inputs) != 8 {
			t.Fatalf("%s: %d functions, %d inputs", id, len(d.Functions), len(d.Functions[0].Inputs))
		}
	}
	if err := runReplay(config{replay: filepath.Join(out, "case_00001"), timeout: 10 * time.Second}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// A battery whose recorded outcomes no longer match a fresh run is
	// refused by replay. The file is edited into another internally valid
	// document; replay reads the case directly, not the descriptors.
	if err := flipFirstOutcome(filepath.Join(out, "case_00001", harness.FunctionsJSON)); err != nil {
		t.Fatal(err)
	}
	err = runReplay(config{replay: filepath.Join(out, "case_00001"), timeout: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "replayed battery outcomes differ") {
		t.Fatalf("replay of edited battery outcomes: %v", err)
	}
}

// flipFirstOutcome turns the first expectation from ok into a divide
// panic, or from a panic into zero results.
func flipFirstOutcome(path string) error {
	d, err := harness.ReadFunctionsDoc(path)
	if err != nil {
		return err
	}
	f := &d.Functions[0]
	e := &f.Expected[0]
	if e.OK != nil {
		p := observe.NewPanicInfo(observe.PanicDivide, "runtime error: integer divide by zero")
		e.OK, e.Panic = nil, &p
	} else {
		e.Panic = nil
		for _, r := range f.Results {
			v := "0"
			if r == "bool" {
				v = "false"
			}
			e.OK = append(e.OK, json.RawMessage(v))
		}
	}
	b, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// TestBatteryReviewRefusals pins the G1 pre-merge review's findings:
// replay builds the regenerated battery (a leftover file in battery/ is
// not compiled), replay checks functions.json's source digests, and a
// judged batch's -verify holds functions.json to the report's reference
// oracle even when every digest has been rebound.
func TestBatteryReviewRefusals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs batteries")
	}
	out := filepath.Join(t.TempDir(), "batch")
	cfg := batteryBase(out)
	cfg.timeout, cfg.judge, cfg.allowDirty = 10*time.Second, true, true
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	caseDir := filepath.Join(out, "case_00001")
	replay := func() error {
		return runReplay(config{replay: caseDir, timeout: 10 * time.Second})
	}

	// A leftover file that would not compile: replay must not build it.
	leftover := filepath.Join(caseDir, harness.BatteryDir, "zz.go")
	if err := os.WriteFile(leftover, []byte("package main\n\nthis does not compile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replay(); err != nil {
		t.Fatalf("replay compiled a file that is not part of the regenerated battery: %v", err)
	}
	os.Remove(leftover)

	// rebind re-digests the tree after an edit, so only content checks
	// can refuse it.
	rebind := func() {
		t.Helper()
		m, err := harness.ReadManifest(out)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		seeds := map[string]int64{}
		for _, mc := range m.Cases {
			ids = append(ids, mc.ID)
			seeds[mc.ID] = mc.Seed
		}
		if _, err := harness.WriteManifest(out, m.GeneratorRev, m.GoVersion, ids, seeds); err != nil {
			t.Fatal(err)
		}
		if err := harness.WriteComplete(out); err != nil {
			t.Fatal(err)
		}
	}
	editDoc := func(edit func(*harness.FunctionsDoc)) {
		t.Helper()
		path := filepath.Join(caseDir, harness.FunctionsJSON)
		d, err := harness.ReadFunctionsDoc(path)
		if err != nil {
			t.Fatal(err)
		}
		edit(&d)
		b, _ := json.MarshalIndent(d, "", " ")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig, err := os.ReadFile(filepath.Join(caseDir, harness.FunctionsJSON))
	if err != nil {
		t.Fatal(err)
	}

	editDoc(func(d *harness.FunctionsDoc) { d.FunctionsSHA256 = strings.Repeat("0", 64) })
	if err := replay(); err == nil || !strings.Contains(err.Error(), "source digests") {
		t.Fatalf("replay with a wrong functionsSha256: %v", err)
	}

	os.WriteFile(filepath.Join(caseDir, harness.FunctionsJSON), orig, 0o644)
	editDoc(func(d *harness.FunctionsDoc) { d.Oracle.Version = "go version go1.18 linux/386" })
	rebind()
	if _, err := harness.VerifyBatch(out); err != nil {
		t.Fatalf("rebound tree should pass the digest checks: %v", err)
	}
	rep, err := harness.ReadBatchReport(out)
	if err != nil {
		t.Fatal(err)
	}
	m, err := harness.ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.ValidateBatchReport(out, rep, m); err == nil || !strings.Contains(err.Error(), "produced by oracle") {
		t.Fatalf("report validation with a different battery oracle: %v", err)
	}
}
