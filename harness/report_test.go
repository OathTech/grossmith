package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/observe"
)

// The 2026-08-10 audit's P0/P1 on integrity-vs-meaning: `batch.json`
// could be byte-authentic yet internally false, and `-verify` called it
// verified. These witnesses are the report mutator the audit asked for,
// as a table: each row changes ONE semantic invariant while leaving the
// digests honest (the fixture re-binds complete.json after every
// mutation), so nothing here is caught by integrity checking.

// reportFixture builds a small batch with a manifest, case records, and
// a self-consistent report, then returns the root.
func reportFixture(t *testing.T) (string, BatchReport, Manifest) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subjects := map[string][]byte{
		"case_00000": []byte("package main\n\nfunc fuzzSubject() int { return 1 }\n"),
		"case_00001": []byte("package main\n\nfunc fuzzSubject() int { return 2 }\n"),
	}
	var ids []string
	for id, src := range subjects {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), src, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := map[string]any{
			"schema": CaseSchema, "id": id, "seed": 1, "generatorRev": "t",
			"subjectSha256": SubjectHash(src),
			"features":      map[string]int{"ints": 1},
			"drawTrace":     []int{1},
		}
		b, err := json.MarshalIndent(rec, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "case.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	sortStrings(ids)
	m, err := WriteManifest(root, "t", "go 1.26", ids, map[string]int64{"case_00000": 1, "case_00001": 2})
	if err != nil {
		t.Fatal(err)
	}
	ran := func(v int64) Outcome {
		return Outcome{Status: StatusRan,
			Document: observe.OK(nil, []observe.Value{{Kind: "int", GoType: "int", Int: v}})}
	}
	rep := BatchReport{
		Schema: BatchSchema, GeneratorRev: "t", Seeds: [2]int64{1, 2},
		ReferenceName: "gc", ReferenceIdentity: "go version go1.26 (pinned)",
		PanicPolicy: string(observe.PanicExact), Started: "2026-08-10T00:00:00Z",
		Cases: []CaseResult{
			{ID: "case_00000", SubjectSHA256: SubjectHash(subjects["case_00000"]), Reference: ran(1)},
			{ID: "case_00001", SubjectSHA256: SubjectHash(subjects["case_00001"]), Reference: ran(2)},
		},
		Total: 2, RefRan: 2, Composition: map[string]int{"ints": 2},
	}
	if err := WriteBatch(root, rep); err != nil {
		t.Fatal(err)
	}
	if err := WriteComplete(root); err != nil {
		t.Fatal(err)
	}
	return root, rep, m
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestValidateBatchReportAcceptsAConsistentReport(t *testing.T) {
	root, rep, m := reportFixture(t)
	if err := ValidateBatchReport(root, rep, m); err != nil {
		t.Fatalf("a self-consistent report was refused: %v", err)
	}
	// And through the strict decoder, which is how -verify reads it.
	decoded, err := ReadBatchReport(root)
	if err != nil {
		t.Fatalf("strict decode of our own report failed: %v", err)
	}
	if err := ValidateBatchReport(root, decoded, m); err != nil {
		t.Fatalf("decoded report refused: %v", err)
	}
}

// TestReportMutationsRefuse: one semantic invariant broken per row, with
// the byte bindings left honest.
func TestReportMutationsRefuse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*BatchReport)
		fragment string
	}{
		{"rewritten total", func(r *BatchReport) { r.Total = 99999 }, "total 99999"},
		{"rewritten refRan", func(r *BatchReport) { r.RefRan = 0 }, "refRan 0"},
		{"fabricated panic paths", func(r *BatchReport) { r.PanicPaths = 3 }, "panicPaths 3"},
		{"fabricated recovered events", func(r *BatchReport) { r.Recovered = 7 }, "recovered 7"},
		{"dropped case", func(r *BatchReport) { r.Cases = r.Cases[:1] }, "has no result"},
		{"duplicated case", func(r *BatchReport) { r.Cases = append(r.Cases, r.Cases[0]) }, "duplicate case"},
		{"undeclared case", func(r *BatchReport) {
			r.Cases = append(r.Cases, CaseResult{ID: "case_09999", Reference: r.Cases[0].Reference})
		}, "not in the batch descriptor"},
		{"wrong subject digest", func(r *BatchReport) {
			r.Cases[0].SubjectSHA256 = strings.Repeat("a", 64)
		}, "the descriptor digests"},
		{"malformed subject digest", func(r *BatchReport) { r.Cases[0].SubjectSHA256 = "abc" }, "not a sha256 digest"},
		{"invalid case document", func(r *BatchReport) {
			r.Cases[0].Reference.Document = observe.Document{Schema: observe.Schema, Status: observe.StatusOK}
		}, "document is invalid"},
		{"non-ran outcome with no reason", func(r *BatchReport) {
			r.Cases[0].Reference = Outcome{Status: StatusTimeout}
		}, "with no detail"},
		{"unknown outcome status", func(r *BatchReport) {
			r.Cases[0].Reference = Outcome{Status: OutcomeStatus("teleported"), Detail: "x"}
		}, "unknown status"},
		{"unknown verdict", func(r *BatchReport) { r.Cases[0].Verdict = Verdict("probably-fine") }, "unknown verdict"},
		{"histogram disagrees", func(r *BatchReport) {
			r.Cases[0].Verdict, r.Cases[1].Verdict = VerdictMatch, VerdictMatch
			r.Verdicts = map[Verdict]int{VerdictMatch: 99}
		}, "histogram disagrees"},
		{"wrapper caught inflated", func(r *BatchReport) { r.WrapperCaught = 3 }, "wrapperCaught 3"},
		{"wrapper legs without a clone", func(r *BatchReport) {
			j := 1
			r.WrapperJudged = &j
		}, "wrapperJudged"},
		{"clone judged without wrapper legs", func(r *BatchReport) { r.CloneName = "gc-386" }, "wrapperJudged is absent"},
		{"composition inflated", func(r *BatchReport) { r.Composition["ints"] = 99 }, "composition[ints]"},
		{"composition tag dropped", func(r *BatchReport) { delete(r.Composition, "ints") }, "composition omits"},
		{"inverted seed range", func(r *BatchReport) { r.Seeds = [2]int64{9, 1} }, "inverted"},
		{"no reference identity", func(r *BatchReport) { r.ReferenceIdentity = "" }, "no reference identity"},
		{"malformed oracle digest", func(r *BatchReport) {
			r.ReferenceOracle = &OracleIdentity{Path: "/go", Version: "go1.26", SHA256: "nope"}
		}, "not a sha256 digest"},
		{"oracle without a path", func(r *BatchReport) {
			r.ReferenceOracle = &OracleIdentity{Version: "go1.26", SHA256: strings.Repeat("a", 64)}
		}, "names no path or version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, m := reportFixture(t)
			tc.mutate(&rep)
			// Write the mutated report and RE-BIND the digests, so the
			// integrity layer is satisfied and only meaning is at stake.
			if err := WriteBatch(root, rep); err != nil {
				t.Fatal(err)
			}
			if err := WriteComplete(root); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBatch(root); err != nil {
				t.Fatalf("the mutation broke the INTEGRITY layer, so this row proves nothing about meaning: %v", err)
			}
			decoded, err := ReadBatchReport(root)
			if err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			err = ValidateBatchReport(root, decoded, m)
			if err == nil {
				t.Fatal("an internally false report validated")
			}
			if !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("refusal %q does not name the broken invariant %q", err, tc.fragment)
			}
		})
	}
}

// TestReportVerdictsAreRecomputed: a recorded verdict that its own
// documents do not support is refused — the check that makes a verdict a
// claim rather than an assertion. Only possible when the comparison was
// OURS; a clone whose harness applied its own equivalence records a
// conclusion this validator cannot recompute, which is stated in
// report.go rather than silently skipped.
func TestReportVerdictsAreRecomputed(t *testing.T) {
	root, rep, m := reportFixture(t)
	clone := rep.Cases[0].Reference // identical documents => match
	rep.Cases[0].Clone = &clone
	rep.Cases[0].Verdict = VerdictMismatch // the lie
	rep.Cases[1].Verdict = VerdictMatch
	rep.CloneName = "gc-386"
	rep.CloneIdentity = "gc-386"
	zero := 0
	rep.WrapperJudged, rep.WrapperCloneInfra = &zero, &zero
	rep.Verdicts = map[Verdict]int{VerdictMismatch: 1, VerdictMatch: 1}
	if err := WriteBatch(root, rep); err != nil {
		t.Fatal(err)
	}
	err := ValidateBatchReport(root, rep, m)
	if err == nil {
		t.Fatal("a verdict its own documents contradict was accepted")
	}
	if !strings.Contains(err.Error(), "its own documents judge") {
		t.Fatalf("refusal does not name the recomputation: %v", err)
	}
	// The honest version of the same report validates.
	rep.Cases[0].Verdict = VerdictMatch
	rep.Verdicts = map[Verdict]int{VerdictMatch: 2}
	if err := ValidateBatchReport(root, rep, m); err != nil {
		t.Fatalf("a truthful verdict was refused: %v", err)
	}
}

// TestReadBatchReportIsStrict: an evidence artifact the checker cannot
// fully understand is a refusal, not a partial read.
func TestReadBatchReportIsStrict(t *testing.T) {
	for _, tc := range []struct{ name, mutate, fragment string }{
		{"unknown field", `"extra": 1, "schema"`, "unknown field"},
		{"wrong schema", `"schema-was-here"`, "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := reportFixture(t)
			path := filepath.Join(root, "batch.json")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mutated := strings.Replace(string(b), `"schema"`, tc.mutate, 1)
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadBatchReport(root); err == nil {
				t.Fatal("accepted")
			} else if !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("refusal %q does not name %q", err, tc.fragment)
			}
		})
	}
}
