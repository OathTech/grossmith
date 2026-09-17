package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactJSONIsUnambiguous(t *testing.T) {
	for _, file := range []string{"manifest.json", "complete.json", "batch.json", "case_00000/case.json"} {
		for _, mutation := range []string{"duplicate schema", "schema alias", "trailing bracket", "second document"} {
			t.Run(file+"/"+mutation, func(t *testing.T) {
				root, _, m := reportFixture(t)
				path := filepath.Join(root, filepath.FromSlash(file))
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				raw := string(b)
				switch mutation {
				case "duplicate schema":
					raw = strings.Replace(raw, `"schema":`, `"schema":"ignored","schema":`, 1)
				case "schema alias":
					raw = strings.Replace(raw, `"schema":`, `"schema":"ignored","Schema":`, 1)
				case "trailing bracket":
					raw += "]"
				case "second document":
					raw += "{}"
				}
				if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
					t.Fatal(err)
				}
				switch file {
				case "manifest.json":
					_, err = ReadManifest(root)
				case "complete.json":
					_, err = checkComplete(root)
				case "batch.json":
					_, err = ReadBatchReport(root)
				default:
					_, err = readCaseFeatures(root, m)
				}
				if err == nil {
					t.Fatal("ambiguous or malformed artifact accepted")
				}
			})
		}
	}
}

func TestReportAliasesRefuseAfterDigestRebinding(t *testing.T) {
	for _, needle := range []string{`"total":2`, `"status":"ran"`} {
		root, rep, _ := comparisonReportFixture(t)
		b, err := json.Marshal(rep)
		if err != nil {
			t.Fatal(err)
		}
		replacement := `"total":999999,"Total":2`
		if needle == `"status":"ran"` {
			replacement = `"status":"build-failed","Status":"ran"`
		}
		raw := strings.Replace(string(b), needle, replacement, 1)
		if raw == string(b) {
			t.Fatal("alias did not apply")
		}
		if err := os.WriteFile(filepath.Join(root, "batch.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteComplete(root); err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyBatch(root); err != nil {
			t.Fatalf("honestly rebound report failed integrity: %v", err)
		}
		if _, err := ReadBatchReport(root); err == nil || !strings.Contains(err.Error(), "exact spelling") {
			t.Fatalf("report field alias accepted: %v", err)
		}
	}
}
