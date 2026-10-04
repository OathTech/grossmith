package golean

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/harness"
	"grossmith/observe"
)

// The E5 clone-coverage witnesses (arc-end review B2): golean-work/
// holds the main.go the CLONE actually compiled, and it was outside
// every descriptor — the reference inputs were digested while the clone
// side was not, so the exit condition's "both adapters" was false.

func workFixture(t *testing.T) (string, harness.BatchReport) {
	t.Helper()
	work := t.TempDir()
	dir := filepath.Join(work, "cases", "case_00000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	subject := []byte("package main\n\nfunc fuzzSubject() int { return 1 }\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), subject, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.tsv", "results.tsv", "results.tsv.meta"} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(name+" content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	perCase, workFiles, err := WorkDigests(work)
	if err != nil {
		t.Fatal(err)
	}
	rep := harness.BatchReport{
		Cases: []harness.CaseResult{{
			ID:                "case_00000",
			SubjectSHA256:     harness.SubjectHash(subject),
			CloneSourceSHA256: perCase["case_00000"],
		}},
		CloneWorkFiles: workFiles,
	}
	return work, rep
}

func TestVerifyWork(t *testing.T) {
	t.Run("clean tree verifies", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := VerifyWork(work, rep); err != nil {
			t.Fatalf("clean tree refused: %v", err)
		}
	})
	t.Run("clone source edited after the batch refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.WriteFile(filepath.Join(work, "cases", "case_00000", "main.go"),
			[]byte("package main\n\nfunc fuzzSubject() int { return 2 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "changed after the batch finished")
	})
	t.Run("recorded digest differing from the subject refuses", func(t *testing.T) {
		// The byte-copy claim: main.go IS subject.go. A report recording
		// anything else describes a clone run over different bytes.
		work, rep := workFixture(t)
		rep.Cases[0].SubjectSHA256 = strings.Repeat("a", 64)
		wantWorkRefusal(t, work, rep, "compiled different bytes")
	})
	t.Run("extra file in a case dir refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.WriteFile(filepath.Join(work, "cases", "case_00000", "extra.go"),
			[]byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "unexpected entry")
	})
	t.Run("unrecorded case dir refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		dir := filepath.Join(work, "cases", "case_00099")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "does not record")
	})
	t.Run("recorded case missing from the tree refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.RemoveAll(filepath.Join(work, "cases", "case_00000")); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "missing from the work tree")
	})
	t.Run("edited results refuse", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.WriteFile(filepath.Join(work, "results.tsv"), []byte("rewritten\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "does not match the recorded")
	})
	t.Run("unrecorded work file refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		delete(rep.CloneWorkFiles, "results.tsv")
		wantWorkRefusal(t, work, rep, "require recorded work file")
	})
	// The work root used to be read only for cases/ and the three named
	// files, so anything else placed beside them was never noticed.
	t.Run("run diagnostics at the work root verify", func(t *testing.T) {
		work, rep := workFixture(t)
		for _, dir := range []string{"artifacts/go-run/case_00000", "goshim"} {
			if err := os.MkdirAll(filepath.Join(work, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{workMarker, "diff-coverage.log"} {
			if err := os.WriteFile(filepath.Join(work, name), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := VerifyWork(work, rep); err != nil {
			t.Fatalf("tree with Run's diagnostics refused: %v", err)
		}
	})
	t.Run("leftover file at the work root refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.WriteFile(filepath.Join(work, "results.old.tsv"), []byte("previous run\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "unexpected entry results.old.tsv at the work root")
	})
	t.Run("leftover directory at the work root refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.MkdirAll(filepath.Join(work, "cases.bak", "case_00000"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "unexpected entry cases.bak at the work root")
	})
	t.Run("known name with the wrong type refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		if err := os.MkdirAll(filepath.Join(work, "diff-coverage.log"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "want a regular file")
	})
	t.Run("symlinked results file refuses", func(t *testing.T) {
		work, rep := workFixture(t)
		path := filepath.Join(work, "results.tsv")
		target := filepath.Join(t.TempDir(), "results.tsv")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		wantWorkRefusal(t, work, rep, "want a regular file")
	})
}

// Report validation recomputes golean.translate's panic-message refusal
// with harness.GoLeanRefusesPanicMessage; the two must not drift apart.
func TestPanicMessageRefusalMatchesTranslate(t *testing.T) {
	for _, msg := range []string{
		"", "-", "boom", "a\x01b", "\x1f", "tab\there", "line\nbreak", "\xc2", "µ", "\u007f", "--", " -",
		"runtime error: integer divide by zero",
	} {
		c := Case{ID: "case_00000", Dir: t.TempDir(), Reference: harness.Outcome{
			Status: harness.StatusRan, Document: observe.Panicked(nil, observe.PanicOther, msg)}}
		_, res, _ := translate(t.TempDir(), c)
		refused := res.Verdict == harness.VerdictCloneInfra
		if refused != harness.GoLeanRefusesPanicMessage(msg) {
			t.Errorf("%q: translate refusal %v (verdict %s), harness predicate %v",
				msg, refused, res.Verdict, harness.GoLeanRefusesPanicMessage(msg))
		}
	}
}

func TestVerifyEmptyCloneWork(t *testing.T) {
	rep := harness.BatchReport{
		CloneWorkFiles: map[string]string{},
		Cases:          []harness.CaseResult{{ID: "refused", Verdict: harness.VerdictCloneInfra}},
	}
	wire, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded harness.BatchReport
	if err := json.Unmarshal(wire, &decoded); err != nil || decoded.CloneWorkFiles == nil {
		t.Fatalf("explicit empty work evidence lost on serialization: %s: %v", wire, err)
	}
	work := t.TempDir()
	if err := VerifyWork(work, decoded); err != nil {
		t.Fatalf("no translated cases should need no clone files: %v", err)
	}
	// Empty evidence cannot excuse a claimed semantic comparison.
	decoded.Cases[0].Verdict = harness.VerdictMatch
	wantWorkRefusal(t, work, decoded, "no clone source digest")
	decoded.Cases[0].Verdict = harness.VerdictCloneInfra
	// Even without a cases/ directory, unexpected result files must be seen.
	if err := os.WriteFile(filepath.Join(work, "results.tsv"), []byte("leftover results"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantWorkRefusal(t, work, decoded, "does not record it")
}

func wantWorkRefusal(t *testing.T, work string, rep harness.BatchReport, fragment string) {
	t.Helper()
	err := VerifyWork(work, rep)
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("want refusal containing %q, got: %v", fragment, err)
	}
}
