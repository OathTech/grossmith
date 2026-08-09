package golean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/harness"
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
		wantWorkRefusal(t, work, rep, "does not record it")
	})
}

func wantWorkRefusal(t *testing.T, work string, rep harness.BatchReport, fragment string) {
	t.Helper()
	err := VerifyWork(work, rep)
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("want refusal containing %q, got: %v", fragment, err)
	}
}
