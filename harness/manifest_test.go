package harness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The E3 witnesses: the audit's reproductions, made permanent. A batch
// is exactly its descriptor — anything else on disk, anything missing,
// anything changed refuses BEFORE execution, because each of those
// means the programs judged are not the programs recorded.

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

	// The audit's reproduction: an unlisted .go file beside the inputs
	// compiles into the case — its probe used an extra.go whose init
	// panicked, which changed the outcome while the recorded hash
	// stayed clean. Refused before anything executes.
	t.Run("extra file in case dir", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "case_00000", "extra.go"), []byte("package main\n\nfunc init() { panic(\"from an unlisted file\") }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "not in the manifest")
	})
	t.Run("subject edited after generation", func(t *testing.T) {
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
	t.Run("input file is a symlink", func(t *testing.T) {
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
	t.Run("root go.mod edited", func(t *testing.T) {
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

// Mid-arc review findings 3, 4, 6: symlinked case DIRECTORIES, unlisted
// root entries, and a completion descriptor that must bind the manifest
// it was written for.
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
	t.Run("unlisted root file", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "neither a declared case nor a batch artifact")
	})
	t.Run("root symlink under an allowlisted name", func(t *testing.T) {
		// A symlink NAMED like a batch artifact (batch.json here, but
		// manifest.json is the same rule) would point the artifact
		// somewhere the batch tree cannot vouch for (E5 re-review nit:
		// the refusal landed without this witness).
		root := manifestFixture(t)
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "batch.json")); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "symlink")
	})
	t.Run("unlisted root directory", func(t *testing.T) {
		root := manifestFixture(t)
		if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "neither a declared case nor a batch artifact")
	})
	t.Run("completion descriptor names another manifest", func(t *testing.T) {
		root := manifestFixture(t)
		wrong := strings.Repeat("0", 64)
		if err := os.WriteFile(filepath.Join(root, "complete.json"), []byte(`{"schema":"grossmith-complete-v1","manifestSha256":"`+wrong+`"}`), 0o644); err != nil {
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

// The E5 completion-descriptor witnesses (arc-end review B1/B3/B4): the
// descriptor now binds the report artifacts, and its checker refuses
// what it cannot read or parse instead of skipping or crashing.
func TestCompletionDescriptorFailClosed(t *testing.T) {
	writeCompletion := func(t *testing.T, root, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "complete.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("empty object refuses instead of crashing", func(t *testing.T) {
		// The review's reproduction: `{}` panicked the verifier on an
		// unguarded digest truncation. The shipped witness had used a
		// 16-character stand-in — exactly long enough to step over the
		// bug — so this one uses the shortest possible descriptor.
		root := manifestFixture(t)
		writeCompletion(t, root, `{}`)
		wantRefusal(t, root, "schema")
	})
	t.Run("short digest refuses before comparing", func(t *testing.T) {
		root := manifestFixture(t)
		writeCompletion(t, root, `{"schema":"grossmith-complete-v1","manifestSha256":"0000000000000000"}`)
		wantRefusal(t, root, "not a sha256 digest")
	})
	t.Run("truncated json refuses", func(t *testing.T) {
		root := manifestFixture(t)
		writeCompletion(t, root, `{"schema":"grossmith-comp`)
		wantRefusal(t, root, "complete.json")
	})
	t.Run("unknown field refuses", func(t *testing.T) {
		// A field the checker does not understand must refuse rather
		// than silently not bind (B4: schema was decoded, never compared,
		// and unknown fields passed).
		root := manifestFixture(t)
		if err := WriteComplete(root); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, "complete.json"))
		if err != nil {
			t.Fatal(err)
		}
		writeCompletion(t, root, strings.Replace(string(b), `"schema"`, `"extra": 1, "schema"`, 1))
		wantRefusal(t, root, "unknown field")
	})
	t.Run("wrong schema refuses", func(t *testing.T) {
		root := manifestFixture(t)
		writeCompletion(t, root, `{"schema":"grossmith-complete-v9","manifestSha256":"`+strings.Repeat("0", 64)+`"}`)
		wantRefusal(t, root, "schema")
	})
	t.Run("unreadable descriptor refuses, never skips", func(t *testing.T) {
		// B4's reproduction: the old check sat inside `if err == nil`,
		// so an unreadable complete.json skipped the binding entirely
		// and the batch still validated.
		if os.Geteuid() == 0 {
			t.Skip("file modes do not restrict root")
		}
		root := manifestFixture(t)
		if err := WriteComplete(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, "complete.json"), 0o000); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "unreadable")
	})
}

// The E5 report-binding witnesses (arc-end review B1): the reviewer
// rewrote totals, verdicts, and the reference identity in a published
// batch.json and -verify returned success — verdicts live there, so an
// unbound report was an unbound conclusion.
func TestReportBinding(t *testing.T) {
	reportFixture := func(t *testing.T) string {
		t.Helper()
		root := manifestFixture(t)
		for name, content := range map[string]string{
			"batch.json":   `{"schema":"grossmith-batch-v1","total":1}`,
			"manifest.tsv": "case_00000\t1\n",
		} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := WriteComplete(root); err != nil {
			t.Fatal(err)
		}
		return root
	}
	t.Run("clean batch of record verifies with reports bound", func(t *testing.T) {
		root := reportFixture(t)
		info, err := VerifyBatch(root)
		if err != nil {
			t.Fatalf("clean batch refused: %v", err)
		}
		if !info.ReportsBound {
			t.Fatal("reportFiles written but VerifyBatch reports them unbound")
		}
	})
	t.Run("edited report refuses", func(t *testing.T) {
		root := reportFixture(t)
		if err := os.WriteFile(filepath.Join(root, "batch.json"), []byte(`{"schema":"grossmith-batch-v1","total":99999}`), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "the report changed after the batch finished")
	})
	t.Run("edited tsv refuses", func(t *testing.T) {
		root := reportFixture(t)
		if err := os.WriteFile(filepath.Join(root, "manifest.tsv"), []byte("case_00000\t2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "the report changed after the batch finished")
	})
	t.Run("missing recorded report refuses", func(t *testing.T) {
		root := reportFixture(t)
		if err := os.Remove(filepath.Join(root, "batch.json")); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "cannot be read")
	})
	t.Run("unrecorded report on disk refuses", func(t *testing.T) {
		// The converse: complete.json was written for an unjudged batch
		// (no batch.json), then a report appeared beside it — a report
		// nobody vouched for.
		root := manifestFixture(t)
		if err := os.WriteFile(filepath.Join(root, "manifest.tsv"), []byte("case_00000\t1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteComplete(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "batch.json"), []byte(`{"schema":"grossmith-batch-v1"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, root, "does not name it")
	})
	t.Run("legacy completion verifies with reports stated unbound", func(t *testing.T) {
		root := manifestFixture(t)
		sum, err := FileSHA256(filepath.Join(root, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "complete.json"),
			[]byte(`{"schema":"grossmith-complete-v1","manifestSha256":"`+sum+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := VerifyBatch(root)
		if err != nil {
			t.Fatalf("legacy batch refused: %v", err)
		}
		if info.ReportsBound {
			t.Fatal("legacy completion has no reportFiles but VerifyBatch claims the report is bound")
		}
	})
}

// The E4 harness witnesses: a compiler that never returns is bounded by
// the build budget and does not leave background work running, and a
// subject that writes without bound is classified by the output cap
// rather than as a parse failure.
//
// Both are ordinary hang/runaway modes for a tool that compiles and
// runs thousands of generated programs unattended — a toolchain that
// wedges on a pathological input, a generated program that loops on
// output. The harness has to survive them and say which phase failed.
func TestStalledCompilerIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("starts subprocesses")
	}
	// A stub toolchain that starts background work and then never
	// returns. Before E4 the build phase had no deadline at all, so
	// this hung the campaign, and cancellation reached only the direct
	// child — background work outlived it.
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	stub := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + "\nsleep 300\n"
	bin := filepath.Join(dir, "go")
	if err := os.WriteFile(bin, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	caseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(caseDir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The BUILD BUDGET path, not a caller cancellation: BuildBudget makes
	// the const-sized deadline exercisable (arc-end review: BuildTimeout
	// was never reached by any test, and a caller-canceled build printed
	// "timeout after 2m0s" — a duration that was not the cause).
	ad := &GcAdapter{GoBin: bin, AdapterName: "stalled", BuildBudget: 2 * time.Second}
	start := time.Now()
	out := ad.Run(context.Background(), caseDir)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("stalled compiler not bounded by the build budget: %s", elapsed)
	}
	if out.Status != StatusBuildFailed || !strings.Contains(out.Detail, "build timeout after 2s") {
		t.Fatalf("stalled compiler classified %s (%s), want the named build budget", out.Status, out.Detail)
	}
	// A caller deadline tighter than the budget must be reported as the
	// caller's, never as the budget's duration.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out = (&GcAdapter{GoBin: bin, AdapterName: "stalled2"}).Run(ctx, caseDir)
	if out.Status != StatusBuildFailed || !strings.Contains(out.Detail, "caller's deadline") {
		t.Fatalf("caller-canceled build classified %s (%s), want the caller's deadline named", out.Status, out.Detail)
	}
	// The background work must be gone too: cancellation covers the
	// process group, so nothing the toolchain started keeps running
	// after the case is done with. Signal 0 is a liveness probe. The pid
	// file is MANDATORY — without it this assertion is vacuous (arc-end
	// review: it sat inside `if err == nil` and skipped silently, which
	// was the one check distinguishing group kill from a plain
	// CommandContext).
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("stub never wrote its pid file — the group-kill assertion would be vacuous: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("stub pid file: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL) // clean up the leak
		t.Fatalf("background work (pid %d) outlived the build deadline", pid)
	}
}

// TestIdentityProbesBounded (E5, review C1): the identity probe gates
// every batch and had no budget at all (measured still running at 45s),
// and Oracle's deadline was defeated by a child holding the pipe open
// (measured blocked at 50s with the child alive). Two stub behaviors,
// both against Identity AND Oracle:
//   - the probe never returns: bounded by the caller's (tighter)
//     deadline via group kill;
//   - the probe exits but leaves a child holding stdout: WaitDelay
//     bounds the wait and the probe refuses with a typed error naming
//     it (a toolchain that leaves background work holding its stdout is
//     not one an identity should quietly vouch for).
func TestIdentityProbesBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("starts subprocesses")
	}
	writeStub := func(t *testing.T, script string) string {
		t.Helper()
		bin := filepath.Join(t.TempDir(), "go")
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	probes := map[string]func(*GcAdapter, context.Context) error{
		"identity": func(a *GcAdapter, ctx context.Context) error { _, err := a.Identity(ctx); return err },
		"oracle":   func(a *GcAdapter, ctx context.Context) error { _, err := a.Oracle(ctx); return err },
	}
	t.Run("probe that never returns", func(t *testing.T) {
		bin := writeStub(t, "#!/bin/sh\nsleep 300\n")
		for name, probe := range probes {
			ad := &GcAdapter{GoBin: bin, AdapterName: "stalled-" + name}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			start := time.Now()
			err := probe(ad, ctx)
			cancel()
			if elapsed := time.Since(start); elapsed > 15*time.Second {
				t.Fatalf("%s probe not bounded: %s", name, elapsed)
			}
			if err == nil {
				t.Fatalf("%s probe returned success from a toolchain that never answered", name)
			}
		}
	})
	t.Run("probe exits but a child holds the pipe", func(t *testing.T) {
		bin := writeStub(t, "#!/bin/sh\necho go version go1.26 linux/amd64\nsleep 300 &\nexit 0\n")
		for name, probe := range probes {
			ad := &GcAdapter{GoBin: bin, AdapterName: "pipeheld-" + name}
			start := time.Now()
			err := probe(ad, context.Background())
			if elapsed := time.Since(start); elapsed > 15*time.Second {
				t.Fatalf("%s probe waited out a pipe-holding child: %s", name, elapsed)
			}
			if !errors.Is(err, exec.ErrWaitDelay) {
				t.Fatalf("%s probe: want the WaitDelay refusal, got %v", name, err)
			}
		}
	})
}

func TestUnboundedSubjectOutputHitsTheCap(t *testing.T) {
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
	runaway := `package main

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
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(runaway), 0o644); err != nil {
		t.Fatal(err)
	}
	ad := &GcAdapter{AdapterName: "runaway", Timeout: 30 * time.Second}
	out := ad.Run(context.Background(), dir)
	// The FULL flood phrase, not a substring a coincidental panic detail
	// could satisfy (arc-end review: `Contains(Detail, "cap")` passed on
	// a slice-capacity panic without a byte having flooded). The subject
	// writes 32MB by construction against the 8MB cap, so a cap refusal
	// here means bytes really flowed past it.
	if out.Status != StatusRunFailed || !strings.Contains(out.Detail, "subject output exceeded the 8MB cap") {
		t.Fatalf("runaway output classified %s (%s), want the output-cap refusal", out.Status, out.Detail)
	}
}
