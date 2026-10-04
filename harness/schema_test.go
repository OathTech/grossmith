package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A report written under the previous batch schema is refused by schema
// name, before any field-level complaint, so the message points at the
// revision that wrote it.
func TestOlderBatchSchemaRefusedByName(t *testing.T) {
	root := t.TempDir()
	old := `{"schema":"grossmith-batch-v1","generatorRev":"x","total":1,"unknownLegacyField":true}`
	if err := os.WriteFile(filepath.Join(root, "batch.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadBatchReport(root)
	if err == nil || !strings.Contains(err.Error(), `schema "grossmith-batch-v1", want "grossmith-batch-v2"`) {
		t.Fatalf("want a schema refusal, got %v", err)
	}
}
