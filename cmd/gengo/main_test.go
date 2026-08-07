package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func base(out string) config {
	return config{n: 2, seed: 1, out: out, swarm: true,
		policy: "exact", timeout: time.Second, workers: 1}
}

// TestValidationRefusesBeforeWrites (audit H5): every rejected invocation
// leaves the filesystem untouched.
func TestValidationRefusesBeforeWrites(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")
	// A structurally valid checkout, for mutations that must fail on OTHER
	// grounds than the checkout stat.
	fake := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fake, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fake, "scripts", "diff-coverage"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mutate := []func(*config){
		func(c *config) { c.n = 0 },
		func(c *config) { c.n = -5 },
		func(c *config) { c.out = "" },
		func(c *config) { c.timeout = 0 },
		func(c *config) { c.workers = 0 },
		func(c *config) { c.policy = "fuzzy" },
		func(c *config) { c.clone = "gcc" },
		func(c *config) { c.clone = "golean:" },
		// A missing checkout refuses BEFORE generation (audit F9)...
		func(c *config) { c.clone = "golean:/nonexistent/checkout" },
		// ...and an explicit -panic-policy with golean is refused rather
		// than recorded-but-unapplied (audit F5).
		func(c *config) { c.clone = "golean:" + fake; c.policySet = true },
	}
	for i, m := range mutate {
		cfg := base(out)
		m(&cfg)
		if err := run(cfg); err == nil {
			t.Fatalf("mutation %d accepted", i)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("mutation %d wrote to %s before validation", i, out)
		}
	}
}

// TestForeignOutDirRefused: a non-empty directory that is not a gengo
// batch is never touched — not even by the stale-case cleanup.
func TestForeignOutDirRefused(t *testing.T) {
	out := t.TempDir()
	precious := filepath.Join(out, "case_notes.txt")
	if err := os.WriteFile(precious, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(base(out))
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("foreign dir accepted: %v", err)
	}
	if b, err := os.ReadFile(precious); err != nil || string(b) != "keep" {
		t.Fatalf("foreign file damaged: %v %q", err, b)
	}
}

// TestStaleBatchShrinks: regenerating a smaller batch into the same out
// dir removes the earlier batch's extra case dirs.
func TestStaleBatchShrinks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")
	cfg := base(out)
	cfg.n = 3
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.n = 1
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	dirs, err := filepath.Glob(filepath.Join(out, "case_*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "case_00000" {
		t.Fatalf("stale cases survived: %v", dirs)
	}
	// Each surviving case carries its replay record.
	if _, err := os.Stat(filepath.Join(dirs[0], "case.json")); err != nil {
		t.Fatal(err)
	}
}

// TestStaleBatchReportRemoved (audit F4): a previous run's batch.json must
// not survive next to regenerated cases — with index-based IDs it is
// structurally consistent with the new dirs and nothing rechecks hashes.
func TestStaleBatchReportRemoved(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")
	if err := run(base(out)); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "batch.json")
	if err := os.WriteFile(stale, []byte(`{"schema":"grossmith-batch-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(base(out)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale batch.json survived regeneration")
	}
}

// TestReplayFromArtifacts (Phase 3 done-when): a case regenerates from
// its case.json record alone — byte-identical subject, observation equal
// to the batch record — and a tampered record is refused, never
// silently regenerated differently.
func TestReplayFromArtifacts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	out := filepath.Join(t.TempDir(), "batch")
	cfg := base(out)
	cfg.judge = true
	cfg.timeout = 20 * time.Second
	cfg.workers = 2
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	rcfg := base("")
	rcfg.replay = filepath.Join(out, "case_00000")
	rcfg.timeout = 20 * time.Second
	if err := run(rcfg); err != nil {
		t.Fatalf("replay of a fresh artifact failed: %v", err)
	}

	// Conflicting flags are refused, never silently ignored (audit F5).
	confl := rcfg
	confl.explicit = map[string]bool{"replay": true, "n": true}
	if err := run(confl); err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Fatalf("conflicting -n accepted with -replay: %v", err)
	}

	recPath := filepath.Join(out, "case_00000", "case.json")
	orig, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatal(err)
	}
	tamper := func(name, old, new string) {
		t.Helper()
		tampered := strings.Replace(string(orig), old, new, 1)
		if tampered == string(orig) {
			t.Fatalf("%s tamper did not apply", name)
		}
		if err := os.WriteFile(recPath, []byte(tampered), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := run(rcfg); err == nil {
			t.Fatalf("%s-tampered record accepted", name)
		}
	}
	// Subject hash, the draw trace, and an absent config must each be
	// refused (audit F8: only the hash was tampered before; F6: the
	// config-absent error must name the real cause).
	tamper("hash", `"subjectSha256": "`, `"subjectSha256": "0000`)
	tamper("trace", `"drawTrace": [`, `"drawTrace": [999999999, `)
	tampered := strings.Replace(string(orig), `"config"`, `"configGone"`, 1)
	if err := os.WriteFile(recPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(rcfg)
	if err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("config-absent record error does not name the cause: %v", err)
	}
}

// TestJudgeWritesBatchReport: the reference pass produces the durable
// artifact of record.
func TestJudgeWritesBatchReport(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	out := filepath.Join(t.TempDir(), "batch")
	cfg := base(out)
	cfg.judge = true
	cfg.timeout = 20 * time.Second
	cfg.workers = 2
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "batch.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"grossmith-batch-v1", "referenceIdentity", "\"refRan\": 2"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("batch.json missing %q:\n%s", want, b)
		}
	}
}
