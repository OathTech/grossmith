package harness

import (
	"time"
	"os/exec"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The E3 witnesses: the audit's replicated probes, permanent. A batch is
// exactly its descriptor — anything else on disk, anything missing,
// anything changed, refuses BEFORE execution.

func manifestFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "case_00000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"subject.go": "package main\n\nfunc fuzzSubject() int { return 1 }\n",
		"driver.go":  "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := WriteManifest(root, "test", "go 1.26", []string{"case_00000"}, map[string]int64{"case_00000": 1}); err != nil {
		t.Fatal(err)
	}
	return root
}

func wantRefusal(t *testing.T, root, fragment string) {
	t.Helper()
	if _, err := ValidateBatch(root); err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("want refusal containing %q, got: %v", fragment, err)
	}
}

func TestBatchValidation(t *testing.T) {
	// The clean fixture validates.
	root := manifestFixture(t)
	if _, err := ValidateBatch(root); err != nil {
		t.Fatalf("clean batch refused: %v", err)
	}

	// The audit's replication: an injected file beside the inputs (its
	// probe used an extra.go with an init panic that was built and
	// judged) refuses the batch before anything executes.
	t.Run("injected file", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "case_00000", "extra.go"), []byte("package main\n\nfunc init() { panic(\"injected\") }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "not in the manifest")
	})
	t.Run("tampered subject", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "case_00000", "subject.go"), []byte("package main\n\nfunc fuzzSubject() int { return 2 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "does not match the manifest")
	})
	t.Run("missing declared file", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.Remove(filepath.Join(root, "case_00000", "driver.go")); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "missing")
	})
	t.Run("undeclared case dir", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.MkdirAll(filepath.Join(root, "case_00099"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "neither a declared case nor a batch artifact")
	})
	t.Run("symlinked input", func(t *testing.T) {
		root := manifestFixture(t)
		target := filepath.Join(t.TempDir(), "elsewhere.go")
		if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "case_00000", "subject.go")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "not a regular file")
	})
	t.Run("tampered root go.mod", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.26\n\nreplace a => ../b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "does not match the manifest")
	})
	t.Run("unknown manifest field", func(t *testing.T) {
		root := manifestFixture(t)
		b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		mutated := strings.Replace(string(b), `"schema"`, `"extra": 1, "schema"`, 1)
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(mutated), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "unknown field")
	})
}

// Mid-arc review findings 3, 4, 6: symlinked case DIRECTORIES, foreign
// root entries, and a completion descriptor that must bind the manifest.
func TestBatchValidationHardening(t *testing.T) {
	t.Run("symlinked case dir", func(t *testing.T) {
		root := manifestFixture(t)
		real := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Rename(filepath.Join(root, "case_00000"), real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(root, "case_00000")); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "symlink")
	})
	t.Run("foreign root file", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "neither a declared case nor a batch artifact")
	})
	t.Run("foreign root dir", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "neither a declared case nor a batch artifact")
	})
	t.Run("stale completion descriptor", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "complete.json"), []byte(`{"schema":"grossmith-complete-v1","manifestSha256":"0000000000000000"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "complete.json binds manifest")
	})
	t.Run("verify requires completion", func(t *testing.T) {
		root := manifestFixture(t)
		if _, err := VerifyBatch(root); err == nil {
			t.Fatal("VerifyBatch accepted a batch without complete.json")
		}
	})
}

// The E4 harness witnesses: a wedged compiler is bounded by the build
// budget with its process TREE killed, and a flooding subject is
// classified as the output cap, not a parse mystery.
func TestWedgedCompilerIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	// A fake "go" that spawns a child and sleeps forever: the old code
	// hung on it and leaked the child past cancellation.
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	fake := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + "\nsleep 300\n"
	bin := filepath.Join(dir, "go")
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	caseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(caseDir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ad := &GcAdapter{GoBin: bin, AdapterName: "wedged"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	out := ad.Run(ctx, caseDir)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("wedged compiler not bounded: %s", elapsed)
	}
	if out.Status != StatusBuildFailed {
		t.Fatalf("wedged compiler classified %s, want build-failed", out.Status)
	}
	// The spawned CHILD must be dead too (process-group kill).
	if b, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(b))
		time.Sleep(200 * time.Millisecond)
		if err := exec.Command("kill", "-0", pid).Run(); err == nil {
			exec.Command("kill", "-9", pid).Run()
			t.Fatalf("compiler's child %s survived cancellation — group kill leaked", pid)
		}
	}
}

func TestFloodingSubjectHitsTheCap(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "case_00000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	flood := `package main

import "os"

func main() {
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = 'x'
	}
	for i := 0; i < 32; i++ {
		os.Stdout.Write(chunk)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(flood), 0o644); err != nil {
		t.Fatal(err)
	}
	ad := &GcAdapter{AdapterName: "flood", Timeout: 30 * time.Second}
	out := ad.Run(context.Background(), dir)
	if out.Status != StatusRunFailed || !strings.Contains(out.Detail, "cap") {
		t.Fatalf("flooding subject classified %s (%s), want the output-cap refusal", out.Status, out.Detail)
	}
}
