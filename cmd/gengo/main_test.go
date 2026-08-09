package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/harness"
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
	cfg.allowDirty = true // dev-tree tests are not campaigns of record
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
	// Config-absent simulation: REMOVE the field (E3's strict decode
	// rejects unknown fields, so the old rename-to-configGone trick now
	// fails earlier, on the strictness itself — also worth witnessing).
	var recMap map[string]json.RawMessage
	if err := json.Unmarshal(orig, &recMap); err != nil {
		t.Fatal(err)
	}
	delete(recMap, "config")
	noConfig, err := json.Marshal(recMap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, noConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(rcfg)
	if err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("config-absent record error does not name the cause: %v", err)
	}
	// And the strict decode itself: an unknown field is a refusal.
	tampered := strings.Replace(string(orig), `"config"`, `"configGone"`, 1)
	if err := os.WriteFile(recPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(rcfg)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown record field accepted: %v", err)
	}
}

// TestPairsMode (Phase 4 rung 5, GoLean R4): -pairs generates the full
// optional-construct pair matrix with each pair force-included, records
// the pair in the manifest and the include in case.json, and reports
// realized co-emission honestly.
func TestPairsMode(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the full pair matrix")
	}
	out := filepath.Join(t.TempDir(), "pairs")
	cfg := base(out)
	cfg.n = 0
	cfg.pairs = 1
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	tags := gen.Optional()
	wantCases := len(tags) * (len(tags) - 1) / 2
	dirs, err := filepath.Glob(filepath.Join(out, "case_*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != wantCases {
		t.Fatalf("pairs mode generated %d cases, want %d (one per pair)", len(dirs), wantCases)
	}
	// Sampled record honesty: each case.json's config carries its pair.
	realized := 0
	for i := 0; i < wantCases; i += 37 {
		b, err := os.ReadFile(filepath.Join(out, fmt.Sprintf("case_%05d", i), "case.json"))
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			Config struct{ Include []string }
		}
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatal(err)
		}
		if len(rec.Config.Include) != 2 {
			t.Fatalf("case %d: include not recorded: %v", i, rec.Config.Include)
		}
		var full struct{ Features map[string]int }
		if err := json.Unmarshal(b, &full); err != nil {
			t.Fatal(err)
		}
		if full.Features[rec.Config.Include[0]] > 0 && full.Features[rec.Config.Include[1]] > 0 {
			realized++
		}
	}
	if realized == 0 {
		t.Fatal("no sampled pair realized co-emission — forcing is inert")
	}
	// -pairs with explicit -n is refused before writes.
	confl := base(filepath.Join(t.TempDir(), "x"))
	confl.pairs = 1
	confl.explicit = map[string]bool{"n": true}
	if err := run(confl); err == nil || !strings.Contains(err.Error(), "replaces -n") {
		t.Fatalf("-pairs with -n accepted: %v", err)
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
	cfg.allowDirty = true // dev-tree tests are not campaigns of record
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

// TestBatchPublishAtomicity (E3; audit P1: in-place regeneration left
// interrupted hybrids that passed the ownership check). The batch
// lifecycle is staging -> complete.json -> atomic swap; every crash
// window either preserves the previous batch or is recovered by the
// next run's stageBatchDir.
func TestBatchPublishAtomicity(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")

	// A published "previous" batch.
	work1, err := stageBatchDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work1, "marker"), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishBatch(out, work1); err != nil {
		t.Fatal(err)
	}

	// Crash window 1: a half-built staging dir left behind. Real staging
	// writes its ownership marker FIRST, so the simulation includes it
	// (mid-arc finding 2: an unmarked dir is FOREIGN and refused). The
	// next stage removes ours; the published batch is untouched.
	if err := os.MkdirAll(out+".staging", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out+".staging", harness.StagingMarker()), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out+".staging", "half"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	work2, err := stageBatchDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work2, "half")); !os.IsNotExist(err) {
		t.Fatal("leftover staging content survived restaging")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "marker")); string(b) != "previous" {
		t.Fatal("published batch disturbed by restaging")
	}

	// Crash window 2: interrupted publish — out renamed to .prev, new
	// batch never moved in. The next stage ROLLS BACK.
	if err := os.Rename(out, out+".prev"); err != nil {
		t.Fatal(err)
	}
	if _, err := stageBatchDir(out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "marker")); string(b) != "previous" {
		t.Fatal("interrupted publish not rolled back — the previous batch is gone")
	}

	// A full successful replacement: the new content swaps in, the
	// previous is cleaned up.
	work3, err := stageBatchDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work3, "marker"), []byte("next"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishBatch(out, work3); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "marker")); string(b) != "next" {
		t.Fatal("publish did not swap the new batch in")
	}
	if _, err := os.Stat(out + ".prev"); !os.IsNotExist(err) {
		t.Fatal("previous batch not cleaned after successful publish")
	}

	// Finding 2's refusals: FOREIGN prev and staging (no gengo marker or
	// manifest) are never touched — no rename, no delete.
	foreign := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(foreign+".prev", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign+".prev", "user-data"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := stageBatchDir(foreign); err == nil {
		t.Fatal("foreign .prev accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(foreign+".prev", "user-data")); string(b) != "precious" {
		t.Fatal("foreign .prev was touched")
	}
	foreign2 := filepath.Join(t.TempDir(), "data2")
	if err := os.MkdirAll(foreign2+".staging", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign2+".staging", "user-data"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := stageBatchDir(foreign2); err == nil {
		t.Fatal("foreign .staging accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(foreign2+".staging", "user-data")); string(b) != "precious" {
		t.Fatal("foreign .staging was touched")
	}
}
