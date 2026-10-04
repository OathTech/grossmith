package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/harness"
)

// A trailing slash on -out made `<out>/.staging` — a staging tree inside
// the batch — so a second run published over a tree holding its own
// staging and left the batch unverifiable.
func TestOutTrailingSlashStagesBesideTheBatch(t *testing.T) {
	out := filepath.Join(t.TempDir(), "batch")
	for i := 0; i < 2; i++ {
		if err := run(base(out + string(filepath.Separator))); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if _, err := os.Lstat(filepath.Join(out, ".staging")); !os.IsNotExist(err) {
			t.Fatalf("run %d left staging inside the batch: %v", i, err)
		}
		if _, err := harness.VerifyBatch(out); err != nil {
			t.Fatalf("run %d: batch does not verify: %v", i, err)
		}
	}
}

func TestOutRootsRefused(t *testing.T) {
	sep := string(filepath.Separator)
	for _, out := range []string{"", ".", "./", "..", "../..", "a/..", sep, sep + sep} {
		t.Run(out, func(t *testing.T) {
			cfg := base(out)
			if err := run(cfg); err == nil {
				t.Fatalf("-out %q accepted", out)
			}
		})
	}
	if got, err := cleanOutDir("x/batch//"); err != nil || got != filepath.Join("x", "batch") {
		t.Fatalf("cleanOutDir: %q, %v", got, err)
	}
}

// writeCheckSource writes a trivial subject for a -check run.
func writeCheckSource(t *testing.T) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "subject.go")
	if err := os.WriteFile(input, []byte("package main; func fuzzSubject() int { return 3 }"), 0o644); err != nil {
		t.Fatal(err)
	}
	return input
}

// -check verified -out empty only at start; publication then renamed
// whatever a second writer had put at -out aside and deleted it, and the
// run exited 0. Publication must refuse instead, touching nothing there
// and leaving the finished result in staging.
func TestCheckPublishLeavesSecondWriterFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	for _, preexisting := range []bool{false, true} {
		name := "absent"
		if preexisting {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			cfg := checkConfig(t, writeCheckSource(t))
			if preexisting {
				if err := os.Mkdir(cfg.out, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			other := filepath.Join(cfg.out, "other-run.txt")
			beforePublish = func(out string) {
				if err := os.MkdirAll(out, 0o755); err != nil {
					t.Error(err)
				}
				if err := os.WriteFile(other, []byte("written by someone else\n"), 0o644); err != nil {
					t.Error(err)
				}
			}
			defer func() { beforePublish = func(string) {} }()
			err := run(cfg)
			if err == nil || !strings.Contains(err.Error(), "NOT published") || !strings.Contains(err.Error(), cfg.out+".staging") {
				t.Fatalf("check replaced a changed -out: %v", err)
			}
			if b, err := os.ReadFile(other); err != nil || string(b) != "written by someone else\n" {
				t.Fatalf("second writer's file lost: %q %v", b, err)
			}
			if _, err := os.Lstat(cfg.out + ".prev"); !os.IsNotExist(err) {
				t.Fatalf("check moved -out aside: %v", err)
			}
			if _, err := os.Stat(filepath.Join(cfg.out+".staging", "complete.json")); err != nil {
				t.Fatalf("finished result not left in staging: %v", err)
			}
		})
	}
}

// Two -check runs to the same new -out each treated the other's staging
// as an interrupted leftover and removed it. A check now claims staging
// exclusively and refuses when it is already there.
func TestCheckRefusesExistingStaging(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	cfg := checkConfig(t, writeCheckSource(t))
	// The first run's staging, as it exists mid-run.
	first, err := stageBatchDir(cfg.out, true)
	if err != nil {
		t.Fatal(err)
	}
	inProgress := filepath.Join(first, "case_00000")
	if err := os.Mkdir(inProgress, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(cfg); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second check took over the first one's staging: %v", err)
	}
	if _, err := os.Stat(inProgress); err != nil {
		t.Fatalf("first run's staging removed: %v", err)
	}
	if _, err := os.Lstat(cfg.out); !os.IsNotExist(err) {
		t.Fatalf("refused check published: %v", err)
	}
}

func TestPublishCheckRefusals(t *testing.T) {
	root := t.TempDir()
	mkWork := func() string {
		w, err := os.MkdirTemp(root, "work")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w, "complete.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return w
	}
	// Absent and empty destinations publish.
	for _, setup := range []func(string){func(string) {}, func(p string) { os.Mkdir(p, 0o755) }} {
		out := filepath.Join(t.TempDir(), "out")
		setup(out)
		if err := publishCheck(out, mkWork()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, "complete.json")); err != nil {
			t.Fatal(err)
		}
	}
	// Non-empty dir, file, and symlink are refused untouched.
	target := t.TempDir()
	for name, setup := range map[string]func(string) error{
		"nonempty": func(p string) error {
			if err := os.Mkdir(p, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(p, "keep"), nil, 0o644)
		},
		"file":    func(p string) error { return os.WriteFile(p, []byte("keep"), 0o644) },
		"symlink": func(p string) error { return os.Symlink(target, p) },
	} {
		out := filepath.Join(t.TempDir(), "out")
		if err := setup(out); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Lstat(out)
		work := mkWork()
		if err := publishCheck(out, work); err == nil || !strings.Contains(err.Error(), work) {
			t.Fatalf("%s: publish replaced -out: %v", name, err)
		}
		after, err := os.Lstat(out)
		if err != nil || after.Mode() != before.Mode() {
			t.Fatalf("%s: -out changed", name)
		}
		if _, err := os.Stat(filepath.Join(work, "complete.json")); err != nil {
			t.Fatalf("%s: staging lost: %v", name, err)
		}
	}
}

// A symlinked -out to an empty directory was replaced by a real
// directory; check mode refuses the link up front.
func TestCheckRefusesSymlinkOut(t *testing.T) {
	cfg := checkConfig(t, writeCheckSource(t))
	target := t.TempDir()
	if err := os.Symlink(target, cfg.out); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := run(cfg); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked -out accepted: %v", err)
	}
	if fi, err := os.Lstat(cfg.out); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v", err)
	}
	if _, err := os.Lstat(cfg.out + ".staging"); !os.IsNotExist(err) {
		t.Fatalf("refused check staged: %v", err)
	}
}
