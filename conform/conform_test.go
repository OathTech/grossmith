package conform

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"grossmith/gen"
)

// TestBatchConformsAndIsDeterministic is the end-to-end witness: a small
// batch builds, runs, and produces byte-identical observations on a second
// run (the repeat is a backstop against generator bugs, not the mechanism —
// determinism comes from construction).
func TestBatchConformsAndIsDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	root := t.TempDir()
	const n = 12
	for i := 0; i < n; i++ {
		c, err := gen.New(gen.DefaultConfig(int64(1000 + i))).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", 1000+i, err)
		}
		dir := filepath.Join(root, filepath.Base(t.Name())+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), c.Source, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), c.Driver, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	host := Runtime{Name: "host"}
	rep, err := Run(root, host, 20*time.Second, 4)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ran != n {
		for _, f := range rep.Failures {
			t.Errorf("FAIL %s: %s", f.Dir, f.Detail)
		}
		t.Fatalf("conformance %d/%d", rep.Ran, rep.Total)
	}
	rep2, err := Run(root, host, 20*time.Second, 4)
	if err != nil {
		t.Fatal(err)
	}
	if divs := Diff(rep, rep2); len(divs) != 0 {
		t.Fatalf("nondeterministic observations: %+v", divs)
	}
}
