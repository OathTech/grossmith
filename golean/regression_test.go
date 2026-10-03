package golean

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/harness"
	"grossmith/observe"
)

func TestGoLeanAssignmentBeforePanic(t *testing.T) {
	if os.Getenv("GOLEAN_CHECKOUT") == "" {
		t.Skip("set GOLEAN_CHECKOUT to check the saved conformance regression against a current clone")
	}
	checkout := integrationCheckout(t)
	source, err := os.ReadFile(filepath.Join("testdata", "assignment-before-panic", "subject.go"))
	if err != nil {
		t.Fatal(err)
	}
	driver, err := gen.DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "assignment")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		"go.mod":                []byte("module assignment-witness\n\ngo 1.26\n"),
		"assignment/subject.go": source,
		"assignment/driver.go":  driver,
	} {
		if err := os.WriteFile(filepath.Join(root, path), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ref := &harness.GcAdapter{GoBin: realGo(t), Timeout: 20 * time.Second}
	out := ref.Run(t.Context(), dir)
	if out.Status != harness.StatusRan || out.Document.Status != observe.StatusOK || len(out.Document.Values) != 19 {
		t.Fatalf("reference did not recover the partial assignment: %+v", out)
	}
	values := out.Document.Values
	if values[11].Kind != "string" || values[11].StringData() != "" || values[18].Kind != "int" || values[18].Int != 3 {
		t.Fatalf("unexpected reference partial state: %+v", values)
	}
	results, err := Run(t.Context(), filepath.Join(root, "work"), []Case{{
		ID: "assignment", Dir: dir, Reference: out,
	}}, Config{Checkout: checkout, GoBin: ref.GoBin})
	if err != nil {
		t.Fatal(err)
	}
	if got := results["assignment"]; got.Verdict != harness.VerdictMatch {
		t.Fatalf("assignment before panic regressed: %+v", got)
	}
}
