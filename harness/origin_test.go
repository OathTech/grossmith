package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceCheckOriginVerification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CaseRecord)
		want   string
	}{
		{"valid current", func(r *CaseRecord) {}, ""},
		{"valid copied", func(r *CaseRecord) { r.Origin.Driver = "copied" }, ""},
		{"kind", func(r *CaseRecord) { r.Origin.Kind = "generated" }, "origin kind"},
		{"path", func(r *CaseRecord) { r.Origin.Path = "" }, "origin path"},
		{"path NUL", func(r *CaseRecord) { r.Origin.Path = "source\x00.go" }, "origin path"},
		{"driver", func(r *CaseRecord) { r.Origin.Driver = "unknown" }, "origin driver"},
		{"config", func(r *CaseRecord) { r.Config = map[string]any{"Seed": 0} }, "config"},
		{"tape", func(r *CaseRecord) { r.DrawTrace = []int{1, 2, 3} }, "draw trace"},
		{"empty tape", func(r *CaseRecord) { r.DrawTrace = []int{} }, "draw trace"},
		{"seed", func(r *CaseRecord) { r.Seed = 17 }, "seed"},
		{"features", func(r *CaseRecord) { r.Features = map[string]int{"ints": 1} }, "features"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, _ := reportFixture(t)
			seeds := map[string]int64{}
			ids := []string{"case_00000", "case_00001"}
			for _, id := range ids {
				path := filepath.Join(root, id, "case.json")
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var rec CaseRecord
				if err := json.Unmarshal(b, &rec); err != nil {
					t.Fatal(err)
				}
				rec.Origin = &CaseOrigin{Kind: "source-check", Path: "/moved-away/subject.go", Driver: "current"}
				rec.Seed, rec.Config, rec.DrawTrace, rec.Features = 0, nil, nil, map[string]int{}
				if id == ids[0] {
					tc.mutate(&rec)
				}
				seeds[id] = rec.Seed
				if err := WriteCaseRecord(filepath.Dir(path), rec); err != nil {
					t.Fatal(err)
				}
			}
			rep.Seeds, rep.Composition = [2]int64{0, seeds[ids[0]]}, map[string]int{}
			m, err := WriteManifest(root, "t", "go 1.26", ids, seeds)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteBatch(root, rep); err != nil {
				t.Fatal(err)
			}
			if err := WriteComplete(root); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBatch(root); err != nil {
				t.Fatalf("input integrity failed: %v", err)
			}
			err = ValidateBatchReport(root, rep, m)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want origin contradiction naming %q, got %v", tc.want, err)
			}
		})
	}
}
