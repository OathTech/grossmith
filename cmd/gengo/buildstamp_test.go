package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The go command stamps a build with the revision of the nearest enclosing
// .git directory, which for a nested worktree or an exported tree unpacked
// inside a checkout is another checkout's revision; generatorRev must not
// record that stamp as this tree's identity.
func TestStampDescribesSource(t *testing.T) {
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	tracked := filepath.Join(root, "cmd", "gengo", "main.go")
	write(tracked, "package main\n")
	write(filepath.Join(root, ".gitignore"), "/scratch/\n")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", ".gitignore", "cmd/gengo/main.go"},
		{"-c", "user.name=t", "-c", "user.email=t@local", "commit", "-q", "-m", "fixture"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	exported := filepath.Join(root, "scratch", "export", "cmd", "gengo", "main.go")
	write(exported, "package main\n")
	nested := filepath.Join(root, "scratch", "wt", "cmd", "gengo", "main.go")
	write(nested, "package main\n")
	write(filepath.Join(root, "scratch", "wt", ".git"), "gitdir: /elsewhere\n")
	untracked := filepath.Join(root, "cmd", "gengo", "extra.go")
	write(untracked, "package main\n")

	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"file tracked by the stamping repository", tracked, true},
		{"worktree nested under another checkout", nested, false},
		{"exported tree unpacked inside a checkout", exported, false},
		{"untracked file in the stamping repository", untracked, false},
		{"trimmed source path", "grossmith/cmd/gengo/main.go", true},
		{"source no longer present", filepath.Join(root, "gone", "main.go"), true},
	} {
		if got := stampDescribesSource(tc.src); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A judged campaign must name its generator: an identity git could not
// state, or whose cleanliness git could not check, is not a revision.
func TestRevisionUnstated(t *testing.T) {
	for rev, want := range map[string]bool{
		"unknown":                      true,
		"cwd-git:abc123-dirty-unknown": true,
		"abc123":                       false,
		"abc123-dirty":                 false,
		"cwd-git:abc123":               false,
		"cwd-git:abc123-dirty":         false,
	} {
		if got := revisionUnstated(rev); got != want {
			t.Errorf("%q: got %v, want %v", rev, got, want)
		}
	}
}
