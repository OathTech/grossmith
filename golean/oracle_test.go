package golean

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/harness"
)

// The E2 witness (audit P0: the script oracle resolved `go` from ambient
// PATH, so a PATH-shadowing binary — malicious or just a different
// version — could do the value comparison while the report named the
// pinned reference). A DECOY `go` sits FIRST in the ambient PATH and
// records every invocation; with GoBin pinned, resolution through the
// shim must reach the pinned binary and the decoy's log must stay empty.
func TestGoShimDefeatsPathShadowing(t *testing.T) {
	work := t.TempDir()

	// The decoy: first in ambient PATH, logs and lies.
	decoyDir := t.TempDir()
	decoyLog := filepath.Join(decoyDir, "invocations.log")
	decoy := "#!/bin/sh\necho invoked >> " + decoyLog + "\necho go version go0.0.0-decoy linux/amd64\n"
	if err := os.WriteFile(filepath.Join(decoyDir, "go"), []byte(decoy), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", decoyDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The pinned binary: the real toolchain, resolved outside the decoyed
	// PATH would be wrong — resolve it via the harness (which the CLI
	// preflight uses) and confirm the decoy DID shadow ambient lookup, or
	// the witness proves nothing.
	if shadow, err := exec.LookPath("go"); err != nil || !strings.HasPrefix(shadow, decoyDir) {
		t.Fatalf("decoy not shadowing ambient PATH (resolved %q) — vacuous witness", shadow)
	}
	real := realGo(t)

	shimDir, err := goShim(work, real)
	if err != nil {
		t.Fatal(err)
	}
	// Resolution exactly as the script does it: `command -v go` under the
	// invoke-constructed PATH (shim first, ambient tail).
	shimPath := shimDir + string(os.PathListSeparator) + os.Getenv("PATH")
	out, err := exec.Command("bash", "-c", "command -v go && go version").CombinedOutput()
	if err != nil {
		t.Fatalf("ambient probe: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "decoy") {
		t.Fatalf("ambient resolution did not hit the decoy — vacuous witness:\n%s", out)
	}
	cmd := exec.Command("bash", "-c", "command -v go && go version")
	cmd.Env = append(os.Environ(), "PATH="+shimPath)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shim probe: %v: %s", err, out)
	}
	if !strings.HasPrefix(string(out), shimDir+"/go\n") {
		t.Fatalf("shimmed `go` did not resolve to the shim:\n%s", out)
	}
	if strings.Contains(string(out), "decoy") {
		t.Fatalf("shimmed resolution still reached the decoy:\n%s", out)
	}

	// The decoy log: exactly one hit — the deliberate ambient probe.
	b, _ := os.ReadFile(decoyLog)
	if got := strings.Count(string(b), "invoked"); got != 1 {
		t.Fatalf("decoy invoked %d times, want exactly the ambient control probe", got)
	}

	// And the GcAdapter side: pinned GoBin must ignore the decoyed PATH.
	ad := &harness.GcAdapter{GoBin: real}
	oid, err := ad.Oracle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(oid.Version, "decoy") || oid.Path != real {
		t.Fatalf("pinned adapter reached the decoy: %+v", oid)
	}
	if oid.SHA256 == "" {
		t.Fatal("oracle identity missing the binary hash")
	}
}

// realGo resolves the actual toolchain without consulting the (decoyed)
// PATH: GOROOT/bin/go from the build's own runtime metadata.
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
	// The test binary was built by a real toolchain; its GOROOT is
	// baked into runtime metadata independent of PATH.
	out, err := exec.Command(filepath.Join("/usr", "local", "go", "bin", "go"), "env", "GOROOT").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	t.Skip("no toolchain at /usr/local/go — shim witness needs a known real go")
	return ""
}

// GoBin must be absolute — a relative pin would re-resolve differently
// per working directory, which is the ambiguity E2 exists to kill.
func TestGoShimRejectsRelativePins(t *testing.T) {
	if _, err := goShim(t.TempDir(), "go"); err == nil {
		t.Fatal("relative GoBin accepted")
	}
}
