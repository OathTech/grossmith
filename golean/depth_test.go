package golean

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/harness"
)

// This witness needs a GoLean checkout with the strict-lane depth guard.
// It is opt-in because deps/golean may predate that protocol. An explicitly
// selected checkout must work; missing tools or protocol drift fail the test.
// Run with GOLEAN_CHECKOUT=/path/to/checkout go test -run TestGoLeanChoiceDepth ./golean.
func TestGoLeanChoiceDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes GoLean's differential harness")
	}
	checkout := os.Getenv("GOLEAN_CHECKOUT")
	if checkout == "" {
		t.Skip("set GOLEAN_CHECKOUT to a GoLean checkout with the depth guard")
	}
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module depth-witness\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	goBin := realGo(t)
	ref := &harness.GcAdapter{GoBin: goBin, Timeout: 20 * time.Second}
	// Repeated map folds exercise choices without changing the sum.
	// Twenty iterations outrun the old fixed streams. The second witness
	// exposes map iteration order in its result and must remain a mismatch.
	sources := map[string]string{
		"deep": `package main
func fuzzSubject() int {
	m := map[int]int{1: 10, 2: 20, 3: 30}
	sum := 0
	for i := 0; i < 20; i++ {
		for k, v := range m { sum += k + v }
	}
	return sum
}
`,
		"order_dependent": `package main
func fuzzSubject() int {
	m := map[int]int{1: 10, 2: 20, 3: 30}
	for k := range m { return k }
	return 0
}
`,
	}
	var cases []Case
	for _, id := range []string{"deep", "order_dependent"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), []byte(sources[id]), 0o644); err != nil {
			t.Fatal(err)
		}
		// Both fixtures return one int, so this driver's observation tuple
		// matches them without borrowing the generator's random subject.
		driver := `package main
import "fmt"
func main() {
	fmt.Printf("{\"schema\":\"grossmith-observation-v2\",\"status\":\"ok\",\"values\":[{\"kind\":\"int\",\"goType\":\"int\",\"int\":%d}]}", fuzzSubject())
}
`
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), []byte(driver), 0o644); err != nil {
			t.Fatal(err)
		}
		out := ref.Run(ctx, dir)
		if out.Status != harness.StatusRan {
			t.Fatalf("%s reference: %+v", id, out)
		}
		cases = append(cases, Case{ID: id, Dir: dir, Reference: out})
	}
	cfg := Config{Checkout: checkout, GoBin: goBin, Jobs: 2}
	work := filepath.Join(root, "with-depth")
	results, err := Run(ctx, work, cases, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := results["deep"]; got.Verdict != harness.VerdictMatch {
		t.Fatalf("deterministic deep row: %+v", got)
	}
	if got := results["order_dependent"]; got.Verdict != harness.VerdictMismatch || strings.Contains(got.Detail, "exhausted") {
		t.Fatalf("order-dependent row was not rejected: %+v", got)
	}
	// Re-run exactly the deterministic subject with no depth and with an
	// insufficient declared depth. Both controls must refuse coverage;
	// otherwise a passing positive case could be a disabled depth guard.
	manifest, err := os.ReadFile(filepath.Join(work, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var deepRow []string
	for _, line := range strings.Split(string(manifest), "\n") {
		if strings.HasPrefix(line, "deep\t") {
			deepRow = strings.Split(line, "\t")
		}
	}
	if len(deepRow) != 10 || deepRow[7] != "strict" || deepRow[9] != "depth=1024" {
		t.Fatalf("unexpected strict manifest row: %q", deepRow)
	}
	for _, params := range []string{"-", "depth=1"} {
		t.Run(params, func(t *testing.T) {
			control := t.TempDir()
			deepRow[9] = params
			body := []byte(strings.Join(deepRow, "\t") + "\n")
			manifestPath := filepath.Join(control, "manifest.tsv")
			if err := os.WriteFile(manifestPath, body, 0o644); err != nil {
				t.Fatal(err)
			}
			resultsPath := filepath.Join(control, "results.tsv")
			metaPath := resultsPath + ".meta"
			if err := invoke(ctx, filepath.Join(checkout, "scripts", "diff-coverage"), cfg, control, manifestPath, resultsPath, metaPath, 1); err != nil {
				t.Fatal(err)
			}
			if err := checkMeta(metaPath, body); err != nil {
				t.Fatal(err)
			}
			rows, err := parseResults(resultsPath, map[string]bool{"deep": true})
			if err != nil {
				t.Fatal(err)
			}
			got := rows["deep"]
			if got.Verdict != harness.VerdictMismatch || got.Stage != "nondet" || !strings.Contains(got.Detail, "exhausted") {
				t.Fatalf("insufficient depth did not refuse coverage: %+v", got)
			}
		})
	}
}
