package golean

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/harness"
)

// The E2 witness: a campaign must use the toolchain its report NAMES,
// whatever an ordinary PATH lookup would find first.
//
// This matters because build environments routinely carry several Go
// installations — a version manager's shim, a vendored toolchain, a
// distro package — and the audit found the clone's script resolving
// `go` from ambient PATH while batch.json named the pinned reference.
// A campaign judged by a different compiler than the one recorded is
// unreproducible, and a version difference alone is enough to move
// verdicts.
//
// The witness puts a STAND-IN `go` earlier in PATH than the real one.
// The stand-in identifies itself distinctly and counts its calls, so
// the test can prove two things: that ordinary lookup really would
// have taken it (otherwise the witness proves nothing), and that
// resolution through the pin does not.
func TestPinnedToolchainIgnoresPathOrder(t *testing.T) {
	work := t.TempDir()

	standInDir := t.TempDir()
	callLog := filepath.Join(standInDir, "calls.log")
	standIn := "#!/bin/sh\necho called >> " + callLog + "\necho go version go0.0.0-standin linux/amd64\n"
	if err := os.WriteFile(filepath.Join(standInDir, "go"), []byte(standIn), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", standInDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Control: ordinary lookup must reach the stand-in, or the rest of
	// this test is vacuous.
	if found, err := exec.LookPath("go"); err != nil || !strings.HasPrefix(found, standInDir) {
		t.Fatalf("ordinary lookup resolved %q, not the stand-in — vacuous witness", found)
	}
	real := realGo(t)

	shimDir, err := goShim(work, real)
	if err != nil {
		t.Fatal(err)
	}
	// Resolution exactly as the clone's script does it: `command -v go`
	// under the PATH invoke() constructs (pin first, ambient tail).
	out, err := exec.Command("bash", "-c", "command -v go && go version").CombinedOutput()
	if err != nil {
		t.Fatalf("control probe: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "standin") {
		t.Fatalf("control probe did not reach the stand-in — vacuous witness:\n%s", out)
	}
	shimPath := shimDir + string(os.PathListSeparator) + os.Getenv("PATH")
	cmd := exec.Command("bash", "-c", "command -v go && go version")
	cmd.Env = append(os.Environ(), "PATH="+shimPath)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned probe: %v: %s", err, out)
	}
	if !strings.HasPrefix(string(out), shimDir+"/go\n") {
		t.Fatalf("pinned `go` did not resolve through the pin directory:\n%s", out)
	}
	if strings.Contains(string(out), "standin") {
		t.Fatalf("pinned resolution still reached the stand-in:\n%s", out)
	}

	// The stand-in's call count: exactly the one deliberate control
	// probe above, and nothing from the pinned path.
	b, _ := os.ReadFile(callLog)
	if got := strings.Count(string(b), "called"); got != 1 {
		t.Fatalf("stand-in called %d times, want exactly the control probe", got)
	}

	// The GcAdapter side of the same property.
	ad := &harness.GcAdapter{GoBin: real}
	oid, err := ad.Oracle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(oid.Version, "standin") || oid.Path != real {
		t.Fatalf("pinned adapter resolved the wrong toolchain: %+v", oid)
	}
	if oid.SHA256 == "" {
		t.Fatal("oracle identity missing the binary hash")
	}
}

// realGo resolves the actual toolchain without consulting PATH (which
// this test deliberately reorders).
func realGo(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(filepath.Join(goroot(t), "bin", "go"), "version").Output()
	if err != nil || !strings.Contains(string(out), "go version go1") {
		t.Fatalf("real toolchain probe: %v: %s", err, out)
	}
	return filepath.Join(goroot(t), "bin", "go")
}

func goroot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(filepath.Join("/usr", "local", "go", "bin", "go"), "env", "GOROOT").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	t.Skip("no toolchain at /usr/local/go — this witness needs a known-good real go")
	return ""
}

// A relative pin would re-resolve differently per working directory,
// which is exactly the ambiguity E2 removes.
func TestGoShimRejectsRelativePins(t *testing.T) {
	if _, err := goShim(t.TempDir(), "go"); err == nil {
		t.Fatal("relative GoBin accepted")
	}
}
