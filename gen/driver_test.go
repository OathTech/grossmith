package gen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDriverFailsClosedOnDriverPanic (audit H1): a DRIVER defect — here a
// subject returning a shape _gReflect cannot serialize — must exit nonzero
// with no observation document, never unwind into main's recover and emit
// a fabricated status:"panic" document at exit 0.
func TestDriverFailsClosedOnDriverPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	dir := t.TempDir()
	subject := "package main\n\nfunc " + Subject + "() float64 { return 1.5 }\n"
	var g Generator
	driver := g.driverSource(make([]binding, 1))
	files := map[string]string{
		"subject.go": subject,
		"driver.go":  driver,
		"go.mod":     "module grossmith-h1-witness\n\ngo 1.26\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-o", "case.bin", ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	run := exec.Command(filepath.Join(dir, "case.bin"))
	run.Env = []string{}
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	err := run.Run()
	if err == nil {
		t.Fatalf("driver defect exited 0 with stdout: %s", stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("driver defect emitted a document: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "grossmith driver") {
		t.Fatalf("stderr does not identify the driver defect: %s", stderr.String())
	}
}
