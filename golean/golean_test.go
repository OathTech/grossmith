package golean

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/harness"
	"grossmith/observe"
)

// TestProfileMasksEvents: subjects generated under the GoLean profile never
// call the obs* event API — the property the translate guard enforces.
func TestProfileMasksEvents(t *testing.T) {
	for seed := int64(9000); seed < 9040; seed++ {
		c, err := gen.New(Profile(gen.DefaultConfig(seed))).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if m := obsCallRe.Find(c.Source); m != nil {
			t.Fatalf("seed %d: profile subject calls %s", seed, m)
		}
	}
}

// TestTranslateFailsClosed: cases that cannot reach GoLean's harness get an
// explicit non-semantic verdict, never a manifest row.
func TestTranslateFailsClosed(t *testing.T) {
	root := t.TempDir()
	ranOK := harness.Outcome{Status: harness.StatusRan,
		Document: observe.Document{Schema: observe.Schema, Status: observe.StatusOK}}

	// Reference infra failure propagates as reference-infra.
	_, res, ok := translate(root, Case{ID: "a", Dir: root,
		Reference: harness.Outcome{Status: harness.StatusBuildFailed, Detail: "boom"}})
	if ok || res.Verdict != harness.VerdictRefInfra {
		t.Fatalf("build-failed reference: ok=%v verdict=%s", ok, res.Verdict)
	}

	// A subject calling obs* is a campaign misconfiguration.
	dir := filepath.Join(root, "obscase")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package main\n\nfunc fuzzSubject() int {\n\tobsInt(\"p\", \"int\", 1)\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "subject.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, res, ok = translate(root, Case{ID: "b", Dir: dir, Reference: ranOK})
	if ok || res.Verdict != harness.VerdictHarnessError || !strings.Contains(res.Detail, "obsInt") {
		t.Fatalf("obs-calling subject: ok=%v res=%+v", ok, res)
	}

	// A panic without a pinnable message cannot become a manifest row.
	_, res, ok = translate(root, Case{ID: "c", Dir: dir, Reference: harness.Outcome{
		Status:   harness.StatusRan,
		Document: observe.Document{Schema: observe.Schema, Status: observe.StatusPanic},
	}})
	if ok || res.Verdict != harness.VerdictHarnessError {
		t.Fatalf("messageless panic: ok=%v res=%+v", ok, res)
	}
}

// TestParseResultsFailsClosed: unknown vocabulary anywhere is an error, not
// a guessed verdict.
func TestParseResultsFailsClosed(t *testing.T) {
	known := map[string]bool{"case_a": true}
	write := func(content string) string {
		p := filepath.Join(t.TempDir(), "results.tsv")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	header := "result\tid\tfeatures\tstage\tdetail\n"
	bad := []string{
		"res\tid\tfeatures\tstage\tdetail\nPASS\tcase_a\tf\t-\t-\n", // header
		header + "PASS\tcase_a\tf\t-\n",                             // field count
		header + "PASS\tcase_b\tf\t-\t-\n",                          // unknown id
		header + "PASS\tcase_a\tf\t-\t-\nFAIL\tcase_a\tf\tx\ty\n",   // duplicate
		header + "MAYBE\tcase_a\tf\t-\t-\n",                         // result vocab
	}
	for _, content := range bad {
		if _, err := parseResults(write(content), known); err == nil {
			t.Fatalf("parsed without error: %q", content)
		}
	}
	good, err := parseResults(write(header+"PASS\tcase_a\tf\t-\t-\n"), known)
	if err != nil {
		t.Fatal(err)
	}
	if good["case_a"].Verdict != harness.VerdictMatch {
		t.Fatalf("PASS row: %+v", good["case_a"])
	}
}

// TestJudgeMapping pins the stage->verdict taxonomy.
func TestJudgeMapping(t *testing.T) {
	cases := map[string]harness.Verdict{
		"differential":     harness.VerdictMismatch,
		"lean-observation": harness.VerdictMismatch,
		"nondet":           harness.VerdictMismatch,
		"frontend-export":  harness.VerdictCloneInfra,
		"lean-run":         harness.VerdictCloneInfra,
		"harness":          harness.VerdictCloneInfra,
		"go-run":           harness.VerdictHarnessError,
		"go-observation":   harness.VerdictHarnessError,
		"brand-new-stage":  harness.VerdictHarnessError,
	}
	for stage, want := range cases {
		res, err := judge("FAIL", stage, "d")
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != want {
			t.Fatalf("stage %s: got %s, want %s", stage, res.Verdict, want)
		}
	}
}

// TestStaleResultsNeverRepublished (audit F1, demonstrated live before the
// fix): diff-coverage exits 1 both for legitimate FAIL rows and for a
// lake-build failure that publishes nothing. With index-based case IDs a
// previous campaign's results.tsv passed every id check, and its verdicts
// — including a fabricated mismatch — were republished as this run's
// conformance statement. Run must instead error on a no-publish run, and
// on results published for a different manifest.
func TestStaleResultsNeverRepublished(t *testing.T) {
	ranOK := harness.Outcome{Status: harness.StatusRan,
		Document: observe.Document{Schema: observe.Schema, Status: observe.StatusOK}}
	caseDir := t.TempDir()
	src := "package main\n\nfunc fuzzSubject() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(caseDir, "subject.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []Case{{ID: "case_00000", Dir: caseDir, Features: []string{"ints"}, Reference: ranOK}}

	stubCheckout := func(script string) string {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "scripts", "diff-coverage"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return root
	}
	staleWork := func() string {
		work := t.TempDir()
		if err := os.MkdirAll(work, 0o755); err != nil {
			t.Fatal(err)
		}
		stale := "result\tid\tfeatures\tstage\tdetail\nFAIL\tcase_00000\tints\tdifferential\tfabricated\n"
		if err := os.WriteFile(filepath.Join(work, "results.tsv"), []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}
		return work
	}

	// A no-publish exit 1 (the lake-build failure shape) with a stale
	// results.tsv already in place: must error, never read the stale file.
	noPublish := stubCheckout("#!/usr/bin/env bash\necho 'lake build failed' >&2\nexit 1\n")
	_, err := Run(context.Background(), staleWork(), cases, Config{Checkout: noPublish})
	if err == nil || !strings.Contains(err.Error(), "published no results") {
		t.Fatalf("no-publish run: %v", err)
	}

	// Results published for a DIFFERENT manifest: must error on the sha.
	wrongManifest := stubCheckout("#!/usr/bin/env bash\n" +
		"printf 'result\\tid\\tfeatures\\tstage\\tdetail\\nPASS\\tcase_00000\\tints\\t-\\t-\\n' > \"$GOLEAN_COVERAGE_RESULTS\"\n" +
		"printf 'key\\tvalue\\nmanifest_sha256\\tdeadbeef\\n' > \"$GOLEAN_COVERAGE_META\"\n")
	_, err = Run(context.Background(), staleWork(), cases, Config{Checkout: wrongManifest})
	if err == nil || !strings.Contains(err.Error(), "not ours") {
		t.Fatalf("wrong-manifest run: %v", err)
	}

	// Both failure modes leave the script log behind for diagnosis.
	work := staleWork()
	if _, err := Run(context.Background(), work, cases, Config{Checkout: noPublish}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(work, "diff-coverage.log")); err != nil {
		t.Fatalf("diff-coverage.log not persisted: %v", err)
	}
}

// TestStuckIsInfraNotMismatch (audit F6): a machine that cannot evaluate
// the case produced no observation — the interpreter analogue of a
// frontend gap, never a semantic divergence.
func TestStuckIsInfraNotMismatch(t *testing.T) {
	stuck, err := judge("FAIL", "lean-observation",
		`expected status ok, got {"message":"mismatched + integer kinds","schema":"golean-observation-v1","status":"stuck"}`)
	if err != nil {
		t.Fatal(err)
	}
	if stuck.Verdict != harness.VerdictCloneInfra {
		t.Fatalf("stuck judged %s", stuck.Verdict)
	}
	wrong, err := judge("FAIL", "lean-observation",
		`expected status ok, got {"schema":"golean-observation-v1","status":"panic","message":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Verdict != harness.VerdictMismatch {
		t.Fatalf("non-stuck lean-observation judged %s", wrong.Verdict)
	}
}

// TestGoLeanEndToEnd is the Phase 1 vertical slice: profile-generated
// cases, gc reference pass, GoLean campaign, verdicts. Requires the
// deps/golean checkout (skipped elsewhere) and builds real binaries.
func TestGoLeanEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes GoLean's differential harness")
	}
	checkout, err := filepath.Abs(filepath.Join("..", "deps", "golean"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "scripts", "diff-coverage")); err != nil {
		t.Skip("no GoLean checkout at deps/golean")
	}

	root := t.TempDir()
	ctx := context.Background()
	ref := &harness.GcAdapter{AdapterName: "ref", Timeout: 20 * time.Second}
	var cases []Case
	for seed := int64(31337); seed < 31343; seed++ {
		c, err := gen.New(Profile(gen.DefaultConfig(seed))).Generate()
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("case_%02d", seed-31337)
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), c.Source, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), c.Driver, 0o644); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, Case{ID: id, Dir: dir, Features: c.Features,
			Reference: ref.Run(ctx, dir)})
	}

	results, err := Run(ctx, filepath.Join(root, "golean-work"), cases, Config{Checkout: checkout})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[harness.Verdict]int{}
	for _, c := range cases {
		res, ok := results[c.ID]
		if !ok {
			t.Fatalf("case %s has no verdict", c.ID)
		}
		counts[res.Verdict]++
		// Translation soundness: their gc oracle and ours are the same
		// toolchain, so a go-side disagreement (harness-error) means the
		// adapter corrupted the case on the way through.
		if res.Verdict == harness.VerdictHarnessError || res.Verdict == harness.VerdictRefInfra {
			t.Errorf("case %s: %s (stage %s): %s", c.ID, res.Verdict, res.Stage, res.Detail)
		}
	}
	t.Logf("golean verdicts: %v", counts)
}
