package golean

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grossmith/harness"
	"grossmith/observe"
)

// GoLean observes string bytes through its own byte-array channel. Check
// the actual frontend, Go harness and Lean evaluator before allowing the
// new generator surface through this profile.
func TestGoLeanByteStrings(t *testing.T) {
	checkout := integrationCheckout(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-strings\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "input")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := `package main
func fuzzSubject() (string, string, int) {
    s := "µ"
    a, b := s[:1], s[1:]
    sum := 0
    for i, r := range a { sum += i + int(r) }
    return a, b, sum
}
`
	if err := os.WriteFile(filepath.Join(dir, "subject.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	// Run consumes reference status; its external harness computes the
	// actual Go value tuple from source before comparing it with Lean.
	ref := observe.OK(nil, []observe.Value{
		observe.StringValue("string", "\xc2"), observe.StringValue("string", "\xb5"),
		{Kind: "int", GoType: "int", Int: 65533},
	})
	results, err := Run(context.Background(), filepath.Join(root, "work"), []Case{{
		ID: "byte_strings", Dir: dir, Features: []string{"string_bytes", "string_slice", "string_range"},
		Reference: harness.Outcome{Status: harness.StatusRan, Document: ref},
	}}, Config{Checkout: checkout})
	if err != nil {
		t.Fatal(err)
	}
	if got := results["byte_strings"]; got.Verdict != harness.VerdictMatch {
		t.Fatalf("byte strings did not reach a semantic match: %+v", got)
	}
}
