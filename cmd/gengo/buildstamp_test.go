package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The go command stamps a build inside a nested git worktree with the
// outer checkout's revision; generatorRev must not record that stamp as
// this tree's identity.
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
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "cmd", "gengo", "main.go")
	write(plain, "package main\n")
	nested := filepath.Join(root, "wt", "agent", "cmd", "gengo", "main.go")
	write(nested, "package main\n")
	write(filepath.Join(root, "wt", "agent", ".git"), "gitdir: /elsewhere\n")

	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"repository with a .git directory", plain, true},
		{"worktree nested under another checkout", nested, false},
		{"trimmed source path", "grossmith/cmd/gengo/main.go", true},
		{"source no longer present", filepath.Join(root, "gone", "main.go"), true},
	} {
		if got := stampDescribesSource(tc.src); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
