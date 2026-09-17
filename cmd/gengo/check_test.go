package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/harness"
	"grossmith/internal/strictjson"
)

func checkConfig(t *testing.T, input string) config {
	t.Helper()
	cfg := base(filepath.Join(t.TempDir(), "result"))
	cfg.check, cfg.clone, cfg.allowDirty = input, "gc", true
	cfg.cloneGCFlags, cfg.timeout = "-N -l", 20*time.Second
	cfg.explicit = map[string]bool{"check": true, "out": true, "clone": true, "clone-gcflags": true}
	return cfg
}

func TestCheckInputsAndOptions(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package main\nfunc fuzzSubject() int { return 17 }\n")
	driver, err := gen.DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	// A directory check preserves a saved driver, including its comments.
	driver = append(driver, []byte("\n// original saved driver\n")...)
	for name, data := range map[string][]byte{"subject.go": source, "driver.go": driver} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := checkConfig(t, dir)
	c, origin, err := loadCheck(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.Source, source) || !bytes.Equal(c.Driver, driver) || origin.Driver != "copied" {
		t.Fatal("directory check changed saved inputs")
	}
	for _, option := range []string{"n", "seed", "swarm", "stats", "pairs", "replay", "verify", "allow-legacy-verify"} {
		cfg.explicit[option] = true
		if _, _, err := loadCheck(cfg); err == nil || !strings.Contains(err.Error(), "does not apply") {
			t.Fatalf("ignored %s: %v", option, err)
		}
		delete(cfg.explicit, option)
	}
	delete(cfg.explicit, "out")
	if _, _, err := loadCheck(cfg); err == nil {
		t.Fatal("check accepted an implicit destination")
	}
	cfg.explicit["out"] = true
	cfg.out = dir
	if _, _, err := loadCheck(cfg); err == nil {
		t.Fatal("check accepted the input directory as its output")
	}
	if after, err := os.ReadFile(filepath.Join(dir, "subject.go")); err != nil || !bytes.Equal(after, source) {
		t.Fatal("input changed during option validation")
	}
}

func TestCheckKeepsInputsOutsideStaging(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "result")
	for _, suffix := range []string{".staging", ".prev"} {
		dir := out + suffix
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(dir, "subject.go")
		if err := os.WriteFile(source, []byte("package main; func fuzzSubject() int { return 1 }"), 0o644); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, "alias"+suffix)
		if err := os.Symlink(dir, alias); err != nil {
			t.Skipf("cannot create path alias: %v", err)
		}
		cfg := checkConfig(t, filepath.Join(alias, "subject.go"))
		cfg.out = out
		if err := run(cfg); err == nil || !strings.Contains(err.Error(), "inside output tree") {
			t.Fatalf("input/output overlap accepted: %v", err)
		}
		if _, err := os.Stat(source); err != nil {
			t.Fatal("input was removed")
		}
	}
}

func TestCheckExistingSource(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	input := filepath.Join(t.TempDir(), "subject.go")
	source := []byte("package main\nfunc fuzzSubject() (int, string) { return 7, \"µ\"[:1] }\n")
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := checkConfig(t, input)
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	rep, err := harness.ReadBatchReport(cfg.out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 1 || rep.Verdicts[harness.VerdictMatch] != 1 || len(rep.Composition) != 0 {
		t.Fatalf("unexpected check report: %+v", rep)
	}
	values := rep.Cases[0].Reference.Document.Values
	if len(values) != 2 || values[0].Int != 7 || values[1].StringData() != "\xc2" {
		t.Fatalf("checked a different program: %+v", values)
	}
	caseDir := filepath.Join(cfg.out, "case_00000")
	b, err := os.ReadFile(filepath.Join(caseDir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec caseRecordIn
	if err := strictjson.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Origin == nil || rec.Origin.Kind != "source-check" || rec.Origin.Path != input ||
		rec.Origin.Driver != "current" || rec.Config != nil || rec.DrawTrace != nil || rec.Seed != 0 {
		t.Fatalf("source check claimed generation metadata: %+v", rec)
	}
	if err := run(config{verify: cfg.out}); err != nil {
		t.Fatalf("check report cannot verify offline: %v", err)
	}
	if err := run(config{replay: caseDir, timeout: time.Second}); err == nil || !strings.Contains(err.Error(), "existing-source check") {
		t.Fatalf("check record claims to be replayable: %v", err)
	}
	// A subsequent directory check copies the saved driver, and also
	// leaves the earlier published report independently verifiable.
	cfg2 := checkConfig(t, caseDir)
	if err := run(cfg2); err != nil {
		t.Fatal(err)
	}
	if err := run(config{verify: cfg2.out}); err != nil {
		t.Fatal(err)
	}
	if err := run(config{verify: cfg.out}); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(input); err != nil || !bytes.Equal(after, source) {
		t.Fatal("check modified original source")
	}
}
