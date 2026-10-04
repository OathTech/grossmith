package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/harness"
	"grossmith/internal/strictjson"
	"grossmith/observe"
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

func TestCheckPreservesInterruptedPublish(t *testing.T) {
	for _, emptyOut := range []bool{false, true} {
		t.Run(fmt.Sprintf("emptyOut=%v", emptyOut), func(t *testing.T) {
			root := t.TempDir()
			out := filepath.Join(root, "result")
			if err := run(base(out)); err != nil {
				t.Fatal(err)
			}
			complete, err := os.ReadFile(filepath.Join(out, "complete.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(out, out+".prev"); err != nil {
				t.Fatal(err)
			}
			if emptyOut {
				if err := os.Mkdir(out, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			input := filepath.Join(root, "edited.go")
			if err := os.WriteFile(input, []byte("package main; func fuzzSubject() int { return 202 }"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := checkConfig(t, input)
			cfg.out = out
			if err := run(cfg); err == nil || !strings.Contains(err.Error(), "previous batch") {
				t.Fatalf("check consumed an interrupted publish: %v", err)
			}
			if _, err := harness.VerifyBatch(out + ".prev"); err != nil {
				t.Fatalf("previous batch damaged: %v", err)
			}
			after, err := os.ReadFile(filepath.Join(out+".prev", "complete.json"))
			if err != nil || !bytes.Equal(after, complete) {
				t.Fatalf("previous batch replaced: %v", err)
			}
			if _, err := os.Lstat(out + ".staging"); !os.IsNotExist(err) {
				t.Fatalf("refused check started staging: %v", err)
			}
			entries, err := os.ReadDir(out)
			if emptyOut && (err != nil || len(entries) != 0) || !emptyOut && !os.IsNotExist(err) {
				t.Fatalf("refused check changed output: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestCheckGoLeanRequiresCurrentDriver(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package main; func fuzzSubject() int { return 1 }")
	driver, err := gen.DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subject.go"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, edited := range []bool{false, true} {
		data := driver
		if edited {
			data = bytes.Replace(driver, []byte("r0 := fuzzSubject()"), []byte("r0 := fuzzSubject() + 1"), 1)
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := checkConfig(t, dir)
		cfg.clone, cfg.cloneGCFlags = "golean", ""
		delete(cfg.explicit, "clone-gcflags")
		_, _, err := loadCheck(cfg)
		if edited {
			err = run(cfg)
		}
		if edited && (err == nil || !strings.Contains(err.Error(), "current observation driver")) {
			t.Fatalf("GoLean accepted an edited reference driver: %v", err)
		}
		if !edited && err != nil {
			t.Fatalf("unchanged driver refused: %v", err)
		}
		if _, err := os.Stat(cfg.out); !os.IsNotExist(err) {
			t.Fatal("driver validation wrote output")
		}
	}
}

func TestCheckPanicMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	var docs []observe.Document
	for _, message := range []string{"", "\xc2", "\xb5"} {
		input := filepath.Join(t.TempDir(), "subject.go")
		source := fmt.Sprintf("package main; func fuzzSubject() int { panic(%q) }", message)
		if err := os.WriteFile(input, []byte(source), 0o644); err != nil {
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
		if rep.Verdicts[harness.VerdictMatch] != 1 {
			t.Fatalf("gc disagreed with itself: %v", rep.Verdicts)
		}
		doc := rep.Cases[0].Reference.Document
		if doc.Status != observe.StatusPanic || doc.Panic.MessageData() != message {
			t.Fatalf("panic %x was changed: %+v", message, doc)
		}
		if err := run(config{verify: cfg.out}); err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	if equal, err := observe.Equal(docs[1], docs[2], observe.PanicExact); err != nil || equal {
		t.Fatalf("checked panic bytes collapsed: equal=%v err=%v", equal, err)
	}
}

func TestReplayChecksSourceOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*harness.CaseRecord)
		want   string
	}{
		{"valid", func(r *harness.CaseRecord) {}, "existing-source check"},
		{"kind", func(r *harness.CaseRecord) { r.Origin.Kind = "generated" }, "origin kind"},
		{"driver", func(r *harness.CaseRecord) { r.Origin.Driver = "unknown" }, "origin driver"},
		{"path", func(r *harness.CaseRecord) { r.Origin.Path = "" }, "origin path"},
		{"config", func(r *harness.CaseRecord) { r.Config = gen.DefaultConfig(0) }, "generated config"},
		{"trace", func(r *harness.CaseRecord) { r.DrawTrace = []int{1} }, "draw trace"},
		{"seed", func(r *harness.CaseRecord) { r.Seed = 1 }, "zero placeholder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "case_00000")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			rec := harness.CaseRecord{Schema: harness.CaseSchema, ID: "case_00000",
				Origin: &harness.CaseOrigin{Kind: "source-check", Path: "/moved/source.go", Driver: "current"}}
			tc.mutate(&rec)
			b, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "case.json"), b, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := run(config{replay: dir, timeout: time.Second}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func checkGoLeanCheckout(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("invokes GoLean's differential harness")
	}
	checkout := os.Getenv("GOLEAN_CHECKOUT")
	explicit := checkout != ""
	if !explicit {
		checkout = filepath.Join("..", "..", "deps", "golean")
	}
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "scripts", "diff-coverage")); err != nil {
		if explicit {
			t.Fatal(err)
		}
		t.Skip("GoLean checkout unavailable")
	}
	// Identity records the repository root; a test dependency may be a
	// symlink to a shared checkout in an isolated grossmith worktree.
	checkout, err = filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}
	return checkout
}

func TestCheckGoLeanCurrentDriver(t *testing.T) {
	checkout := checkGoLeanCheckout(t)
	dir := t.TempDir()
	source := []byte("package main; func fuzzSubject() int { return 1 }")
	driver, err := gen.DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"subject.go": source, "driver.go": driver} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := checkConfig(t, dir)
	cfg.clone, cfg.cloneGCFlags = "golean:"+checkout, ""
	delete(cfg.explicit, "clone-gcflags")
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	rep, err := harness.ReadBatchReport(cfg.out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdicts[harness.VerdictMatch] != 1 || rep.Cases[0].Reference.Document.Values[0].Int != 1 {
		t.Fatalf("unchanged-driver comparison failed: %+v", rep.Cases)
	}
	if err := run(config{verify: cfg.out}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGoLeanPanicMessages(t *testing.T) {
	checkout := checkGoLeanCheckout(t)
	for _, message := range []string{"\x01", "\a", "a\x1fb", "ordinary", "quoted \"µ\" \\"} {
		t.Run(fmt.Sprintf("%q", message), func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "subject.go")
			source := fmt.Sprintf("package main; func fuzzSubject() int { panic(%q) }", message)
			if err := os.WriteFile(input, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := checkConfig(t, input)
			cfg.clone, cfg.cloneGCFlags = "golean:"+checkout, ""
			delete(cfg.explicit, "clone-gcflags")
			if err := run(cfg); err != nil {
				t.Fatal(err)
			}
			rep, err := harness.ReadBatchReport(cfg.out)
			if err != nil {
				t.Fatal(err)
			}
			want := harness.VerdictMatch
			if strings.ContainsAny(message, "\x01\a\x1f") {
				want = harness.VerdictCloneInfra
			}
			if rep.Total != 1 || rep.Verdicts[want] != 1 {
				t.Fatalf("panic comparison: want %s, got %+v", want, rep.Cases)
			}
			p := rep.Cases[0].Reference.Document.Panic
			if p == nil || p.MessageData() != message {
				t.Fatalf("reference panic changed: %+v", p)
			}
			if err := run(config{verify: cfg.out}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Directory-mode -check copies only subject.go and driver.go; any other
// Go file in the directory was dropped from the build without a word.
func TestCheckDirectoryRefusesOtherGoFiles(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package main\nfunc fuzzSubject() int { return helper() }\n")
	driver, err := gen.DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"subject.go": source, "driver.go": driver,
		"helper.go": []byte("package main\nfunc helper() int { return 4 }\n"),
		"notes.txt": []byte("not Go"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := checkConfig(t, dir)
	if _, _, err := loadCheck(cfg); err == nil || !strings.Contains(err.Error(), "helper.go") || strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("extra Go file not named in a refusal: %v", err)
	}
}
