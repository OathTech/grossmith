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
	// A VALID ran document: status ok with at least one observed value.
	// (These fixtures used to carry no values, which Document.Validate
	// rejects — prevalidate refuses that before the run now, so a fixture
	// has to be a document a real driver could emit.)
	ranOK := harness.Outcome{Status: harness.StatusRan,
		Document: observe.Document{Schema: observe.Schema, Status: observe.StatusOK,
			Values: []observe.Value{{Kind: "int", GoType: "int", Int: 1}}}}

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
	// lean-observation reads its STRUCTURED document (see
	// TestLeanObservationClassification), so it needs a realistic detail;
	// every other stage classifies on the stage alone.
	detailFor := func(stage string) string {
		if stage == "lean-observation" {
			return `expected status ok, got {"schema":"golean-observation-v1","status":"panic","values":[]}`
		}
		return "d"
	}
	for stage, want := range cases {
		res, err := judge("FAIL", stage, detailFor(stage))
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != want {
			t.Fatalf("stage %s: got %s (%s), want %s", stage, res.Verdict, res.Detail, want)
		}
	}
}

// TestLeanObservationClassification (2026-08-10 audit, P1): the
// semantic/infra boundary was decided by a SUFFIX test against two
// literal JSON tails, so whitespace, field order, an added field, or
// encoder evolution would have turned the same structured refusal into a
// semantic divergence. It reads a typed status now, and anything it
// cannot classify is a harness error — never a mismatch.
func TestLeanObservationClassification(t *testing.T) {
	got := func(doc string) harness.Verdict {
		t.Helper()
		res, err := judge("FAIL", "lean-observation", "expected status ok, got "+doc)
		if err != nil {
			t.Fatal(err)
		}
		return res.Verdict
	}
	// Statuses meaning their machine produced NO observation: infra.
	for _, doc := range []string{
		`{"message":"m","schema":"golean-observation-v1","status":"stuck"}`,
		`{"message":"m","schema":"golean-observation-v1","status":"unsupported"}`,
		// The shapes the old suffix test would have MISSED, each
		// downgrading a refusal into a fabricated semantic divergence.
		`{"status":"unsupported","schema":"golean-observation-v1","message":"m"}`,
		`{ "schema": "golean-observation-v1", "status": "stuck", "message": "m" }`,
		`{"message":"m","schema":"golean-observation-v1","status":"unsupported","newField":1}`,
		`{"message":"m","schema":"golean-observation-v1","status":"stuck"}` + "\n",
	} {
		if v := got(doc); v != harness.VerdictCloneInfra {
			t.Fatalf("a refusal classified %s, want clone-infra: %s", v, doc)
		}
	}
	// Statuses meaning their machine DID produce an outcome: the semantic
	// signal the campaign exists to find.
	for _, status := range []string{"ok", "panic", "deadlock", "race", "error"} {
		doc := `{"message":"m","schema":"golean-observation-v1","status":"` + status + `"}`
		if v := got(doc); v != harness.VerdictMismatch {
			t.Fatalf("status %q classified %s, want mismatch", status, v)
		}
	}
	// Unclassifiable: a grown vocabulary, another schema, no document at
	// all (their machine's error text, captured with 2>&1), or a detail
	// with no document separator. Never a mismatch.
	for _, tc := range []struct{ name, detail string }{
		{"unknown status", `expected status ok, got {"schema":"golean-observation-v1","status":"quantum"}`},
		{"foreign schema", `expected status ok, got {"schema":"golean-observation-v2","status":"stuck"}`},
		{"machine error text", "expected status ok, got lean: internal error: uncaught exception"},
		{"no separator", "something else entirely"},
	} {
		res, err := judge("FAIL", "lean-observation", tc.detail)
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != harness.VerdictHarnessError {
			t.Fatalf("%s classified %s, want harness-error", tc.name, res.Verdict)
		}
		// The raw detail survives into the record, or triage is blind.
		if !strings.Contains(res.Detail, tc.detail) {
			t.Fatalf("%s: detail %q lost the original", tc.name, res.Detail)
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
	// A VALID ran document: status ok with at least one observed value.
	// (These fixtures used to carry no values, which Document.Validate
	// rejects — prevalidate refuses that before the run now, so a fixture
	// has to be a document a real driver could emit.)
	ranOK := harness.Outcome{Status: harness.StatusRan,
		Document: observe.Document{Schema: observe.Schema, Status: observe.StatusOK,
			Values: []observe.Value{{Kind: "int", GoType: "int", Int: 1}}}}
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
	unsupported, err := judge("FAIL", "lean-observation",
		`expected status ok, got {"message":"interface satisfaction for $runtime.Error: ...","schema":"golean-observation-v1","status":"unsupported"}`)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.Verdict != harness.VerdictCloneInfra {
		t.Fatalf("machine-level unsupported judged %s", unsupported.Verdict)
	}
	wrong, err := judge("FAIL", "lean-observation",
		`expected status ok, got {"schema":"golean-observation-v1","status":"panic","message":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Verdict != harness.VerdictMismatch {
		t.Fatalf("non-refusal lean-observation judged %s", wrong.Verdict)
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
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

// TestRunFailsClosedOnCaseSlice (2026-08-10 audit, P1 — both halves
// reproduced against the exported boundary before the fix, each
// returning err=nil and a `match` verdict). `golean.Run` is a product
// API: the default CLI does not construct these inputs, but the contract
// has to hold for programmatic callers.
func TestRunFailsClosedOnCaseSlice(t *testing.T) {
	caseDir := t.TempDir()
	src := "package main\n\nfunc fuzzSubject() int {\n\treturn 1\n}\n"
	if err := os.WriteFile(filepath.Join(caseDir, "subject.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	valid := observe.Document{Schema: observe.Schema, Status: observe.StatusOK,
		Values: []observe.Value{{Kind: "int", GoType: "int", Int: 1}}}
	// A stub that would PASS every row, so a leaked case reaches `match`.
	checkout := passingStub(t)

	t.Run("duplicate case ID fails the run", func(t *testing.T) {
		// The reproduction: the duplicate was recorded as a harness-error
		// and then OVERWRITTEN by the translated row's verdict, because
		// both entries share the map key. A whole-run precondition cannot
		// be a per-case verdict.
		dup := Case{ID: "case_00000", Dir: caseDir, Features: []string{"ints"},
			Reference: harness.Outcome{Status: harness.StatusRan, Document: valid}}
		res, err := Run(context.Background(), t.TempDir(), []Case{dup, dup}, Config{Checkout: checkout})
		if err == nil {
			t.Fatalf("duplicate IDs accepted: %v", res)
		}
		if !strings.Contains(err.Error(), "duplicate case ID") {
			t.Fatalf("refusal does not name the cause: %v", err)
		}
	})
	t.Run("invalid exported document fails the run", func(t *testing.T) {
		// The reproduction: translate switched on Document.Status alone,
		// so an impossible document — status ok carrying a panic payload,
		// empty schema — was translated and judged `match`.
		bad := Case{ID: "case_00000", Dir: caseDir, Features: []string{"ints"},
			Reference: harness.Outcome{Status: harness.StatusRan,
				Document: observe.Document{Status: observe.StatusOK,
					Panic: &observe.PanicInfo{Kind: "runtime", Message: "boom"}}}}
		res, err := Run(context.Background(), t.TempDir(), []Case{bad}, Config{Checkout: checkout})
		if err == nil {
			t.Fatalf("an invalid exported document was judged: %v", res)
		}
		if !strings.Contains(err.Error(), "reference document is invalid") {
			t.Fatalf("refusal does not name the cause: %v", err)
		}
	})
	t.Run("unsafe feature tag fails the run before any write", func(t *testing.T) {
		work := t.TempDir()
		bad := Case{ID: "case_00000", Dir: caseDir, Features: []string{"a\tb"},
			Reference: harness.Outcome{Status: harness.StatusRan, Document: valid}}
		if _, err := Run(context.Background(), work, []Case{bad}, Config{Checkout: checkout}); err == nil {
			t.Fatal("unsafe feature tag accepted")
		}
		if _, err := os.Stat(filepath.Join(work, "cases")); !os.IsNotExist(err) {
			t.Fatal("a case tree was written before the metadata was validated")
		}
	})
	t.Run("a clean slice still runs", func(t *testing.T) {
		ok := Case{ID: "case_00000", Dir: caseDir, Features: []string{"ints"},
			Reference: harness.Outcome{Status: harness.StatusRan, Document: valid}}
		res, err := Run(context.Background(), t.TempDir(), []Case{ok}, Config{Checkout: checkout})
		if err != nil {
			t.Fatalf("clean slice refused: %v", err)
		}
		if res["case_00000"].Verdict != harness.VerdictMatch {
			t.Fatalf("clean slice verdict %s, want match", res["case_00000"].Verdict)
		}
	})
}

// TestClearCaseRootRefusesForeignTrees (2026-08-10 audit, P1/P2: cases
// were written into whatever was already in the work tree, so a previous
// run's case directories survived beside this run's). The tree starts
// empty, and a leftover must prove it is OURS — by a marker naming this
// work directory — before it is removed. A name test cannot do this job:
// safe case IDs are permissive, so `vendor/main.go` looks exactly like a
// translated case.
func TestClearCaseRootRefusesForeignTrees(t *testing.T) {
	seed := func(t *testing.T, withMarker bool, markerFor string) (work, victim string) {
		t.Helper()
		work = t.TempDir()
		dir := filepath.Join(work, "cases", "case_00007")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		victim = filepath.Join(dir, "main.go")
		if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
			t.Fatal(err)
		}
		if withMarker {
			target := work
			if markerFor != "" {
				target = markerFor
			}
			if err := os.WriteFile(filepath.Join(work, workMarker), []byte(workMarkerContent(target)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return work, victim
	}
	t.Run("our own marked leftover is cleared", func(t *testing.T) {
		work, _ := seed(t, true, "")
		if err := clearCaseRoot(work); err != nil {
			t.Fatalf("our own leftover was refused: %v", err)
		}
		if _, err := os.Stat(filepath.Join(work, "cases")); !os.IsNotExist(err) {
			t.Fatal("leftover survived")
		}
	})
	t.Run("unmarked tree refuses untouched", func(t *testing.T) {
		work, victim := seed(t, false, "")
		if err := clearCaseRoot(work); err == nil {
			t.Fatal("an unmarked work tree was cleared")
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "precious" {
			t.Fatalf("the unmarked tree was damaged: %v %q", err, b)
		}
	})
	t.Run("marker for another directory refuses", func(t *testing.T) {
		work, victim := seed(t, true, filepath.Join(t.TempDir(), "elsewhere"))
		if err := clearCaseRoot(work); err == nil {
			t.Fatal("a marker naming another work dir was accepted")
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "precious" {
			t.Fatalf("the tree was damaged: %v %q", err, b)
		}
	})
	t.Run("case root is a symlink", func(t *testing.T) {
		work := t.TempDir()
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, filepath.Join(work, "cases")); err != nil {
			t.Fatal(err)
		}
		if err := clearCaseRoot(work); err == nil {
			t.Fatal("a symlinked case root was accepted")
		}
	})
	// End to end: a second Run over the same work dir clears only its own
	// previous cases, and a foreign directory there stops the run.
	t.Run("second run clears its own tree", func(t *testing.T) {
		caseDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(caseDir, "subject.go"),
			[]byte("package main\n\nfunc fuzzSubject() int { return 1 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		valid := observe.Document{Schema: observe.Schema, Status: observe.StatusOK,
			Values: []observe.Value{{Kind: "int", GoType: "int", Int: 1}}}
		checkout := passingStub(t)
		work := t.TempDir()
		run := func(id string) error {
			_, err := Run(context.Background(), work, []Case{{ID: id, Dir: caseDir,
				Features:  []string{"ints"},
				Reference: harness.Outcome{Status: harness.StatusRan, Document: valid}}},
				Config{Checkout: checkout})
			return err
		}
		if err := run("case_00000"); err != nil {
			t.Fatal(err)
		}
		if err := run("case_00001"); err != nil {
			t.Fatalf("second run into the same work dir: %v", err)
		}
		// The first run's case must be gone, not sitting beside the second's.
		if _, err := os.Stat(filepath.Join(work, "cases", "case_00000")); !os.IsNotExist(err) {
			t.Fatal("the previous run's translated case survived into this run's tree")
		}
	})
}

// TestProfileMergesCallerPolicy (2026-08-10 audit, P1/P2: Profile
// ASSIGNED both slices, so a caller's own masks and exclusions were
// silently discarded — and a profile whose job is to remove capability
// must never be able to restore any).
func TestProfileMergesCallerPolicy(t *testing.T) {
	cfg := gen.DefaultConfig(1)
	cfg.NoObserve = []gen.Shape{gen.ShapeString}
	cfg.Exclude = []string{"linearize"}
	got := Profile(cfg)
	shapes := map[gen.Shape]bool{}
	for _, s := range got.NoObserve {
		shapes[s] = true
	}
	for _, want := range []gen.Shape{gen.ShapeString, gen.ShapeSlice, gen.ShapeMap} {
		if !shapes[want] {
			t.Fatalf("profile dropped mask %v: %v", want, got.NoObserve)
		}
	}
	tags := map[string]bool{}
	for _, tag := range got.Exclude {
		tags[tag] = true
	}
	for _, want := range []string{"linearize", "observe_point", "defer", "recover"} {
		if !tags[want] {
			t.Fatalf("profile dropped exclusion %q: %v", want, got.Exclude)
		}
	}
	// Idempotent and order-independent: applying it twice, or to a config
	// that already carries the profile, changes nothing.
	twice := Profile(got)
	if len(twice.NoObserve) != len(got.NoObserve) || len(twice.Exclude) != len(got.Exclude) {
		t.Fatalf("profile is not idempotent: %v / %v", twice.NoObserve, twice.Exclude)
	}
	if err := twice.Validate(); err != nil {
		t.Fatalf("merged profile does not validate: %v", err)
	}
}

// passingStub is a diff-coverage stand-in that PASSES every manifest row
// and publishes meta for the manifest it was handed — so anything that
// reaches it lands on `match`, which is what makes the fail-closed
// witnesses above meaningful.
func passingStub(t *testing.T) string {
	t.Helper()
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/usr/bin/env bash\n" +
		"printf 'result\\tid\\tfeatures\\tstage\\tdetail\\n' > \"$GOLEAN_COVERAGE_RESULTS\"\n" +
		"while IFS=$'\\t' read -r id rest; do [ \"${id#\\#}\" = \"$id\" ] || continue; " +
		"printf 'PASS\\t%s\\tints\\t-\\t-\\n' \"$id\" >> \"$GOLEAN_COVERAGE_RESULTS\"; done < \"$1\"\n" +
		"printf 'key\\tvalue\\nmanifest_sha256\\t%s\\n' \"$(sha256sum \"$1\" | cut -d' ' -f1)\" > \"$GOLEAN_COVERAGE_META\"\n"
	if err := os.WriteFile(filepath.Join(checkout, "scripts", "diff-coverage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return checkout
}
