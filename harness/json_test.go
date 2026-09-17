package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactJSONIsUnambiguous(t *testing.T) {
	for _, file := range []string{"manifest.json", "complete.json", "batch.json", "case_00000/case.json"} {
		for _, mutation := range []string{"duplicate schema", "trailing bracket", "second document"} {
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
