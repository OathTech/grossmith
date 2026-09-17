package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func comparisonReportFixture(t *testing.T) (string, BatchReport, Manifest) {
	t.Helper()
	root, rep, m := reportFixture(t)
	rep.CloneName, rep.CloneIdentity = "gc", "go clone"
	for i := range rep.Cases {
		clone := rep.Cases[i].Reference
		rep.Cases[i].Clone = &clone
		rep.Cases[i].Verdict = VerdictMatch
	}
	rep.Verdicts = map[Verdict]int{VerdictMatch: rep.Total}
	rep.CompositionJudged = map[string]int{"ints": rep.Total}
	zero := 0
	rep.WrapperJudged, rep.WrapperCloneInfra = &zero, &zero
	if err := WriteBatch(root, rep); err != nil {
		t.Fatal(err)
	}
	if err := WriteComplete(root); err != nil {
		t.Fatal(err)
	}
	return root, rep, m
}

func TestReportCaseRecordConsistency(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CaseRecord)
	}{
		{"schema", func(r *CaseRecord) { r.Schema = "other" }},
		{"seed", func(r *CaseRecord) { r.Seed++ }},
		{"generator", func(r *CaseRecord) { r.GeneratorRev = "other" }},
		{"subject digest", func(r *CaseRecord) { r.SubjectSHA256 = strings.Repeat("a", 64) }},
		{"driver digest", func(r *CaseRecord) { r.DriverSHA256 = strings.Repeat("a", 64) }},
		{"zero feature count", func(r *CaseRecord) { r.Features["ints"] = 0 }},
		{"negative feature count", func(r *CaseRecord) { r.Features["ints"] = -1 }},
		{"missing record", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, _ := comparisonReportFixture(t)
			path := filepath.Join(root, "case_00000", "case.json")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var rec CaseRecord
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			if tc.mutate == nil {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				tc.mutate(&rec)
				b, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m, err := WriteManifest(root, "t", "go 1.26", []string{"case_00000", "case_00001"},
				map[string]int64{"case_00000": 1, "case_00001": 2})
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteComplete(root); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBatch(root); err != nil {
				t.Fatalf("mutation affected input integrity: %v", err)
			}
			if err := ValidateBatchReport(root, rep, m); err == nil {
				t.Fatal("report with contradictory or absent case metadata accepted")
			}
		})
	}
}

func TestWrapperLegsMustMatchIndividualVerdicts(t *testing.T) {
	root, rep, _ := comparisonReportFixture(t)
	dir := filepath.Join(root, "case_00000")
	b, err := os.ReadFile(filepath.Join(dir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec CaseRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Features["recover_wrapper"] = 1
	if err := WriteCaseRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	m, err := WriteManifest(root, "t", "go 1.26", []string{"case_00000", "case_00001"},
		map[string]int64{"case_00000": 1, "case_00001": 2})
	if err != nil {
		t.Fatal(err)
	}
	rep.Composition["recover_wrapper"], rep.CompositionJudged["recover_wrapper"] = 1, 1
	rep.WrapperCaught = 1
	judged, infra := 1, 0
	rep.WrapperJudged, rep.WrapperCloneInfra = &judged, &infra
	if err := ValidateBatchReport(root, rep, m); err != nil {
		t.Fatalf("consistent wrapper report refused: %v", err)
	}
	// Both counters stay nonnegative and their sum still equals caught.
	// Only checking against each case's verdict reveals the contradiction.
	judged, infra = 0, 1
	if err := ValidateBatchReport(root, rep, m); err == nil {
		t.Fatal("wrapper legs exchanged while preserving their sum were accepted")
	}
}

// Rebind each incomplete report so its bytes are intact. Verification
// must reject the missing evidence instead of interpreting its absence
// as a request to skip the corresponding consistency check.
func TestIncompleteComparisonReportsRefuse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BatchReport)
	}{
		{"schema omitted", func(r *BatchReport) { r.Schema = "" }},
		{"subject digest omitted", func(r *BatchReport) { r.Cases[0].SubjectSHA256 = "" }},
		{"clone outcome omitted", func(r *BatchReport) { r.Cases[0].Clone = nil }},
		{"case verdict omitted", func(r *BatchReport) { r.Cases[0].Verdict = ""; r.Verdicts[VerdictMatch]-- }},
		{"histogram omitted", func(r *BatchReport) { r.Verdicts = nil }},
		{"histogram emptied", func(r *BatchReport) { r.Verdicts = map[Verdict]int{} }},
		{"unknown histogram key", func(r *BatchReport) { r.Verdicts["unknown"] = 0 }},
		{"unknown policy", func(r *BatchReport) { r.PanicPolicy = "ignore-values" }},
		{"external policy on direct clone", func(r *BatchReport) { r.PanicPolicy = "golean-harness (expected status + exact panic message)" }},
		{"clone identity omitted", func(r *BatchReport) { r.CloneIdentity = "" }},
		{"reference name omitted", func(r *BatchReport) { r.ReferenceName = "" }},
		{"different generator", func(r *BatchReport) { r.GeneratorRev = "another-revision" }},
		{"different seed range", func(r *BatchReport) { r.Seeds = [2]int64{3, 4} }},
		{"judged composition omitted", func(r *BatchReport) { r.CompositionJudged = nil }},
		{"judged composition inflated", func(r *BatchReport) { r.CompositionJudged["ints"]++ }},
		{"wrapper legs cancel", func(r *BatchReport) {
			judged, infra := 1, -1
			r.WrapperJudged, r.WrapperCloneInfra = &judged, &infra
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, m := comparisonReportFixture(t)
			if err := ValidateBatchReport(root, rep, m); err != nil {
				t.Fatalf("initial report is invalid: %v", err)
			}
			tc.mutate(&rep)
			if err := WriteBatch(root, rep); err != nil {
				t.Fatal(err)
			}
			if err := WriteComplete(root); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBatch(root); err != nil {
				t.Fatalf("mutation affected input integrity: %v", err)
			}
			if err := ValidateBatchReport(root, rep, m); err == nil {
				t.Fatal("incomplete or contradictory report accepted")
			}
		})
	}
}
