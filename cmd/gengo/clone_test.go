package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"grossmith/harness"
)

func TestCloneOptionsRequireGc(t *testing.T) {
	for _, clone := range []string{"", "golean"} {
		for _, option := range []string{"clone-go", "clone-gcflags"} {
			t.Run(clone+"/"+option, func(t *testing.T) {
				cfg := base(filepath.Join(t.TempDir(), "batch"))
				cfg.clone = clone
				// Explicit empty values are still inapplicable flags.
				cfg.explicit = map[string]bool{option: true}
				if _, _, err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "require -clone gc") {
					t.Fatalf("inapplicable clone option accepted: %v", err)
				}
			})
		}
	}
}

func TestCloneToolchainPreflightBeforeWrites(t *testing.T) {
	cfg := base(filepath.Join(t.TempDir(), "batch"))
	cfg.clone, cfg.cloneGo = "gc", filepath.Join(t.TempDir(), "missing-go")
	cfg.allowDirty = true
	if err := run(cfg); err == nil || !strings.Contains(err.Error(), "clone toolchain preflight") {
		t.Fatalf("missing clone compiler accepted: %v", err)
	}
	for _, path := range []string{cfg.out, cfg.out + ".staging", cfg.out + ".prev"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("clone preflight wrote %s: %v", path, err)
		}
	}
}

func TestGcCloneCampaign(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	// A distinct path (including spaces) catches accidental reuse of -go
	// and checks that a tool path is never interpreted as shell words.
	cloneGo := filepath.Join(t.TempDir(), "clone go")
	if err := os.Symlink(goBin, cloneGo); err != nil {
		t.Skipf("cannot create a toolchain symlink: %v", err)
	}
	for _, clone := range []string{"gc", "gc-386"} {
		t.Run(clone, func(t *testing.T) {
			if clone == "gc-386" && (runtime.GOOS == "darwin" || runtime.GOARCH != "amd64" && runtime.GOARCH != "386") {
				t.Skip("host cannot execute 386 binaries")
			}
			cfg := base(filepath.Join(t.TempDir(), "batch"))
			cfg.clone, cfg.cloneGo, cfg.cloneGCFlags = clone, cloneGo, "-N -l"
			cfg.allowDirty = true
			cfg.timeout = 20 * time.Second
			if err := run(cfg); err != nil {
				// The preflight runs a trivial 386 binary before any write;
				// a host that builds but cannot execute 32-bit code refuses
				// the campaign there rather than producing all clone-infra.
				if clone == "gc-386" && strings.Contains(err.Error(), "clone toolchain preflight") &&
					strings.Contains(err.Error(), "("+string(harness.StatusRunFailed)+")") {
					t.Skipf("this host cannot execute 32-bit binaries: %v", err)
				}
				t.Fatal(err)
			}
			rep, err := harness.ReadBatchReport(cfg.out)
			if err != nil {
				t.Fatal(err)
			}
			if rep.CloneName != clone || rep.CloneOracle == nil {
				t.Fatalf("clone identity not recorded: %+v", rep)
			}
			if rep.CloneOracle.Path != cloneGo || rep.CloneOracle.GCFlags != cfg.cloneGCFlags {
				t.Fatalf("wrong clone configuration: %+v", rep.CloneOracle)
			}
			if rep.ReferenceOracle.Path == cloneGo || rep.ReferenceOracle.GCFlags != "" {
				t.Fatalf("clone configuration leaked into reference: %+v", rep.ReferenceOracle)
			}
			if clone == "gc" && rep.Verdicts[harness.VerdictMatch] != cfg.n {
				t.Fatalf("optimization comparison: %v", rep.Verdicts)
			}
			if err := run(config{verify: cfg.out}); err != nil {
				t.Fatalf("gc campaign cannot verify offline: %v", err)
			}
			if clone == "gc-386" && rep.Verdicts[harness.VerdictCloneInfra] == cfg.n {
				t.Skip("386 report verified, but this host cannot execute 32-bit binaries")
			}
			if rep.Verdicts[harness.VerdictMatch]+rep.Verdicts[harness.VerdictMismatch] != cfg.n {
				t.Fatalf("comparison did not produce semantic verdicts: %v", rep.Verdicts)
			}
		})
	}
}

// `go version` alone admitted bad -clone-gcflags; every case then failed
// to build as clone-infra and the run exited 0. The preflight builds a
// trivial program with the clone's flags before anything is written.
func TestClonePreflightBuildsWithCloneFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	cfg := base(filepath.Join(t.TempDir(), "batch"))
	cfg.clone, cfg.cloneGCFlags, cfg.allowDirty = "gc", "-not-a-compiler-flag", true
	if err := run(cfg); err == nil || !strings.Contains(err.Error(), "clone toolchain preflight") {
		t.Fatalf("unusable clone flags accepted: %v", err)
	}
	for _, path := range []string{cfg.out, cfg.out + ".staging", cfg.out + ".prev"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("clone preflight wrote %s: %v", path, err)
		}
	}
}

// A clone campaign in which no case reached a semantic verdict now fails
// with a distinct message, for every clone kind, while the batch stays
// published and verifiable.
func TestZeroJudgedCampaignFails(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	input := filepath.Join(t.TempDir(), "subject.go")
	// Compiles for neither side: no case can reach a semantic verdict.
	if err := os.WriteFile(input, []byte("package main; func fuzzSubject() int { return notDefined }"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := checkConfig(t, input)
	err := run(cfg)
	if err == nil || !strings.Contains(err.Error(), "INCOMPLETE CAMPAIGN") {
		t.Fatalf("zero-judged campaign exited cleanly: %v", err)
	}
	if err := run(config{verify: cfg.out}); err != nil {
		t.Fatalf("incomplete campaign not published for inspection: %v", err)
	}
}
