package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/observe"
)

func writeCases(t *testing.T, root string, n int, seedBase int64) {
	t.Helper()
	// The throwaway module: without it these dirs build only when TMPDIR
	// happens to sit inside a Go module (GoLean's bug report: the suite
	// was red on stock machines, masked here by the sandbox's in-repo
	// TMPDIR).
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		c, err := gen.New(gen.DefaultConfig(seedBase + int64(i))).Generate()
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "case_"+strings.Repeat("0", 3)+string(rune('a'+i)))
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
}

// TestVerdictTaxonomy is the audit's Phase 1 done-when in miniature: a
// deliberately altered clone is OBSERVATION-MISMATCH, an unavailable clone
// is CLONE-INFRA-FAILURE, and neither is ever confused with the other.
func TestVerdictTaxonomy(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	root := t.TempDir()
	writeCases(t, root, 6, 424200)
	ctx := context.Background()
	ref := &GcAdapter{AdapterName: "ref", Timeout: 20 * time.Second}

	// 1. Identical clone (same toolchain): every judged case must match.
	same := &GcAdapter{AdapterName: "same", Timeout: 20 * time.Second}
	rep, err := RunBatch(ctx, root, ref, same, observe.PanicExact, 4)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdicts[VerdictMatch] != rep.Total {
		t.Fatalf("identical clone: %v", rep.Verdicts)
	}
	if rep.ReferenceIdentity == "" || rep.CloneIdentity == "" {
		t.Fatal("identities not recorded")
	}

	// 2. Altered clone: corrupt one case's driver for the clone by giving
	// the clone a doctored copy of the tree.
	altRoot := t.TempDir()
	if err := copyTree(root, altRoot); err != nil {
		t.Fatal(err)
	}
	// Flip every observed int's sign in EVERY altered driver: doctoring
	// only one case made the witness hostage to that case's draw path (a
	// panicking case observes no values and the flip is invisible).
	dirs, _ := filepath.Glob(filepath.Join(altRoot, "*", "driver.go"))
	applied := 0
	for _, doctored := range dirs {
		b, _ := os.ReadFile(doctored)
		nb := strings.Replace(string(b), `"int": v.Int()`, `"int": -(v.Int() + 1)`, 1)
		if nb != string(b) {
			applied++
		}
		if err := os.WriteFile(doctored, []byte(nb), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if applied == 0 {
		t.Fatal("driver corruption did not apply")
	}
	altered := &treeAdapter{inner: &GcAdapter{AdapterName: "altered", Timeout: 20 * time.Second}, root: root, altRoot: altRoot}
	rep2, err := RunBatch(ctx, root, ref, altered, observe.PanicExact, 4)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Verdicts[VerdictMismatch] < 1 {
		t.Fatalf("altered clone produced no observation-mismatch: %v", rep2.Verdicts)
	}
	if rep2.Verdicts[VerdictMismatch]+rep2.Verdicts[VerdictMatch] != rep2.Total {
		t.Fatalf("altered clone leaked non-semantic verdicts: %v", rep2.Verdicts)
	}

	// 3. Unavailable clone: a nonexistent toolchain is infra failure on
	// every case, never a mismatch.
	broken := &GcAdapter{AdapterName: "broken", GoBin: "/nonexistent/go"}
	if _, err := RunBatch(ctx, root, ref, broken, observe.PanicExact, 2); err == nil {
		t.Fatal("unavailable clone should fail at identity")
	}
	// And a clone that dies per-case (bad GOARCH for this host binary
	// path) — simulate by running with an adapter whose binary breaks at
	// run time via doctored empty driver dir.
	emptyRoot := t.TempDir()
	missing := &treeAdapter{inner: &GcAdapter{AdapterName: "missing", Timeout: 5 * time.Second}, root: root, altRoot: emptyRoot}
	rep3, err := RunBatch(ctx, root, ref, missing, observe.PanicExact, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Verdicts[VerdictCloneInfra] != rep3.Total {
		t.Fatalf("per-case unavailable clone: %v", rep3.Verdicts)
	}
	if rep3.Verdicts[VerdictMismatch] != 0 {
		t.Fatalf("infra failure leaked into mismatch: %v", rep3.Verdicts)
	}

	// The report is durable.
	if err := WriteBatch(root, rep2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "batch.json")); err != nil {
		t.Fatal(err)
	}
}

// TestBuildSurvivesHostileGit (2026-08-08 review G2 + tight audit F1):
// a corrupt repository above a case dir must not break the reference
// build — case binaries carry no VCS stamp by policy.
func TestBuildSurvivesHostileGit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	root := t.TempDir()
	writeCases(t, root, 1, 717000)
	dir := filepath.Join(root, "case_000a")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A .git dir whose HEAD is garbage: `git status` exits 128, which
	// Go 1.26's auto VCS stamping turns into a build failure without
	// -buildvcs=false.
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := &GcAdapter{AdapterName: "ref", Timeout: 20 * time.Second}
	out := ref.Run(context.Background(), dir)
	if out.Status != StatusRan {
		t.Fatalf("hostile git broke the build: %s: %s", out.Status, out.Detail)
	}
}

// TestJudgeDocumentStatusAxis (audit H2): StatusRan with an error document
// is infrastructure, never semantic — on either side, or both.
func TestJudgeDocumentStatusAxis(t *testing.T) {
	ran := func(d observe.Document) Outcome { return Outcome{Status: StatusRan, Document: d} }
	real := ran(observe.OK(nil, []observe.Value{{Kind: "int", GoType: "int", Int: 7}}))
	errDoc := ran(observe.Errored(observe.ErrTimeout, "deadline"))

	if v, _ := Judge(errDoc, errDoc, observe.PanicExact); v != VerdictBothInfra {
		t.Fatalf("two error documents judged %s, want both-infra", v)
	}
	if v, d := Judge(real, errDoc, observe.PanicExact); v != VerdictCloneInfra {
		t.Fatalf("clone error document judged %s (%s)", v, d)
	}
	if v, _ := Judge(errDoc, real, observe.PanicExact); v != VerdictRefInfra {
		t.Fatalf("reference error document judged %s, want ref-infra", v)
	}
	if v, _ := Judge(real, real, observe.PanicExact); v != VerdictMatch {
		t.Fatalf("real documents judged %s, want match", v)
	}
}

// TestRunBatchAbortsOnCancel (audit L5): a cancelled batch is an error,
// never a conformance statement full of infra verdicts.
func TestRunBatchAbortsOnCancel(t *testing.T) {
	root := t.TempDir()
	writeCases(t, root, 2, 616100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ref := &GcAdapter{AdapterName: "ref", Timeout: 5 * time.Second}
	if _, err := RunBatch(ctx, root, ref, nil, observe.PanicExact, 2); err == nil {
		t.Fatal("cancelled batch returned a report instead of an error")
	}
}

// treeAdapter runs its inner adapter against a parallel tree — the harness
// for doctored-clone tests.
type treeAdapter struct {
	inner   *GcAdapter
	root    string
	altRoot string
}

func (a *treeAdapter) Name() string { return a.inner.Name() }
func (a *treeAdapter) Identity(ctx context.Context) (string, error) {
	return a.inner.Identity(ctx)
}
func (a *treeAdapter) Run(ctx context.Context, caseDir string) Outcome {
	rel, err := filepath.Rel(a.root, caseDir)
	if err != nil {
		return Outcome{Status: StatusAdapterErr, Detail: err.Error()}
	}
	return a.inner.Run(ctx, filepath.Join(a.altRoot, rel))
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// TestPanicPolicyEndToEnd: a cross-arch clone diverges under exact policy
// only where width matters; the report's verdict split stays semantic.
func TestGcCrossArchStillJudges(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	root := t.TempDir()
	writeCases(t, root, 6, 515150)
	ref := &GcAdapter{AdapterName: "amd64", Timeout: 20 * time.Second}
	alt := &GcAdapter{AdapterName: "386", GOARCH: "386", Timeout: 20 * time.Second}
	rep, err := RunBatch(context.Background(), root, ref, alt, observe.PanicKindOnly, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Vacuity guard: a host that cannot execute 32-bit binaries (seen
	// live 2026-08-08 — a sandbox seccomp policy SIGTRAPs every 386 ELF)
	// yields 100% clone-infra, and a witness that passes while proving
	// nothing is a defect of its own. Skip honestly; the gc-386
	// discrimination proof runs in CI where ia32 executes.
	if rep.Verdicts[VerdictCloneInfra] == rep.Total {
		t.Skipf("every 386 outcome was infrastructure (%v) — this host cannot execute 32-bit binaries; witness vacuous here", rep.Verdicts)
	}
	for v := range rep.Verdicts {
		switch v {
		case VerdictMatch, VerdictMismatch, VerdictCloneInfra:
		default:
			t.Fatalf("unexpected verdict %s: %v", v, rep.Verdicts)
		}
	}
}
