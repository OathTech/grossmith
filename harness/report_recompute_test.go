package harness

import (
	"context"
	"strings"
	"testing"

	"grossmith/observe"
)

const goleanPolicy = "golean-harness (expected status + exact panic message)"

// goleanReportFixture is a GoLean (external policy) report over the
// two-case fixture: every case translated and matched.
func goleanReportFixture(t *testing.T) (string, BatchReport, Manifest) {
	t.Helper()
	root, rep, m := reportFixture(t)
	rep.CloneName, rep.CloneIdentity, rep.PanicPolicy = "golean", "golean checkout abc", goleanPolicy
	for i := range rep.Cases {
		rep.Cases[i].Verdict = VerdictMatch
		rep.Cases[i].CloneSourceSHA256 = rep.Cases[i].SubjectSHA256
	}
	zero := 0
	rep.WrapperJudged, rep.WrapperCloneInfra = &zero, &zero
	recount(&rep)
	return root, rep, m
}

// recount rederives the aggregates a mutation of one case changes, so
// each row below differs from a consistent report only in the verdict
// rule under test.
func recount(rep *BatchReport) {
	rep.Verdicts = map[Verdict]int{}
	rep.CompositionJudged = map[string]int{}
	rep.RefRan, rep.PanicPaths = 0, 0
	for _, cr := range rep.Cases {
		rep.Verdicts[cr.Verdict]++
		if cr.Verdict == VerdictMatch || cr.Verdict == VerdictMismatch {
			rep.CompositionJudged["ints"]++
		}
		if cr.Reference.Status == StatusRan {
			rep.RefRan++
			if cr.Reference.Document.Status == observe.StatusPanic {
				rep.PanicPaths++
			}
		}
	}
}

// Under the external GoLean policy the clone's verdict is an attestation,
// but its reference-side part is golean.translate's rule over the recorded
// reference outcome. Reports contradicting that rule used to verify.
func TestExternalVerdictReferenceSideIsRecomputed(t *testing.T) {
	buildFailed := Outcome{Status: StatusBuildFailed, Detail: "build: exit 1"}
	errored := Outcome{Status: StatusRan, Document: observe.Errored(observe.ErrRun, "exit 2")}
	panicked := func(msg string) Outcome {
		return Outcome{Status: StatusRan, Document: observe.Panicked(nil, observe.PanicOther, msg)}
	}
	for _, tc := range []struct {
		name     string
		ref      *Outcome
		verdict  Verdict
		accepted bool
	}{
		{"reference build failed, ref-infra", &buildFailed, VerdictRefInfra, true},
		{"reference build failed, match", &buildFailed, VerdictMatch, false},
		{"reference build failed, clone-infra", &buildFailed, VerdictCloneInfra, false},
		{"reference error document, ref-infra", &errored, VerdictRefInfra, true},
		{"reference error document, mismatch", &errored, VerdictMismatch, false},
		{"reference ok, ref-infra", nil, VerdictRefInfra, false},
		{"reference ok, both-infra", nil, VerdictBothInfra, false},
		{"reference ok, clone-infra from GoLean results", nil, VerdictCloneInfra, true},
		{"reference ok, harness-error", nil, VerdictHarnessError, true},
		{"representable panic, match", ptr(panicked("boom")), VerdictMatch, true},
		{"representable panic, clone-infra from GoLean results", ptr(panicked("boom")), VerdictCloneInfra, true},
		{"empty panic message, clone-infra", ptr(panicked("")), VerdictCloneInfra, true},
		{"empty panic message, match", ptr(panicked("")), VerdictMatch, false},
		{"dash panic message, mismatch", ptr(panicked("-")), VerdictMismatch, false},
		{"control character in panic message, match", ptr(panicked("a\x01b")), VerdictMatch, false},
		{"invalid UTF-8 panic message, match", ptr(panicked("\xc2")), VerdictMatch, false},
		{"invalid UTF-8 panic message, ref-infra", ptr(panicked("\xc2")), VerdictRefInfra, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, m := goleanReportFixture(t)
			if err := ValidateBatchReport(root, rep, m); err != nil {
				t.Fatalf("baseline refused: %v", err)
			}
			if tc.ref != nil {
				rep.Cases[0].Reference = *tc.ref
			}
			rep.Cases[0].Verdict = tc.verdict
			recount(&rep)
			err := ValidateBatchReport(root, rep, m)
			if tc.accepted && err != nil {
				t.Fatalf("producer-consistent verdict refused: %v", err)
			}
			if !tc.accepted && err == nil {
				t.Fatal("verdict contradicting the recorded reference outcome accepted")
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// Histograms list exactly the keys that occur. A zero-count entry used to
// compare equal to an absent one and was accepted.
func TestHistogramZeroEntriesRefuse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BatchReport)
	}{
		{"composition", func(r *BatchReport) { r.Composition["never_present"] = 0 }},
		{"judged composition", func(r *BatchReport) { r.CompositionJudged["never_present"] = 0 }},
		{"verdicts", func(r *BatchReport) { r.Verdicts[VerdictCloneInfra] = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, m := comparisonReportFixture(t)
			if err := ValidateBatchReport(root, rep, m); err != nil {
				t.Fatalf("baseline refused: %v", err)
			}
			tc.mutate(&rep)
			err := ValidateBatchReport(root, rep, m)
			if err == nil || !strings.Contains(err.Error(), "computed absent") {
				t.Fatalf("zero-count histogram entry accepted or refused for another reason: %v", err)
			}
		})
	}
}

// A gc clone's identity string is derivable from its structured oracle.
// The oracle used to be optional and never compared with the string, so a
// report could name one toolchain or flag set and record another.
func TestGcCloneOracleMatchesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*BatchReport)
		accepted bool
	}{
		{"oracle omitted", func(r *BatchReport) { r.CloneOracle = nil }, false},
		{"gcflags differ", func(r *BatchReport) { r.CloneOracle.GCFlags = "-N" }, false},
		{"gcflags dropped from the oracle", func(r *BatchReport) { r.CloneOracle.GCFlags = "" }, false},
		{"path differs", func(r *BatchReport) { r.CloneOracle.Path = "/usr/local/go/bin/go" }, false},
		{"version differs", func(r *BatchReport) { r.CloneOracle.Version = "go version go1.25.0 linux/amd64" }, false},
		{"GOARCH differs", func(r *BatchReport) { r.CloneOracle.GOARCH = "arm64" }, false},
		{"GOARCH empty in both", func(r *BatchReport) {
			r.CloneOracle.GOARCH = ""
			r.CloneIdentity = strings.Replace(r.CloneIdentity, "GOARCH=amd64", "GOARCH=", 1)
		}, false},
		{"nested oracle on a gc clone", func(r *BatchReport) {
			o := *r.CloneOracle
			r.CloneNestedOracle = &o
		}, false},
		{"gc-386 built for amd64", func(r *BatchReport) { r.CloneName = "gc-386" }, false},
		{"gc-386 built for 386", func(r *BatchReport) {
			r.CloneName, r.CloneOracle.GOARCH = "gc-386", "386"
			r.CloneIdentity = strings.Replace(r.CloneIdentity, "GOARCH=amd64", "GOARCH=386", 1)
		}, true},
		{"no gcflags", func(r *BatchReport) {
			r.CloneOracle.GCFlags = ""
			r.CloneIdentity = "go version go1.26.0 linux/amd64 (/toolchains/clone/bin/go, GOARCH=amd64)"
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, rep, m := comparisonReportFixture(t)
			if err := ValidateBatchReport(root, rep, m); err != nil {
				t.Fatalf("baseline refused: %v", err)
			}
			tc.mutate(&rep)
			err := ValidateBatchReport(root, rep, m)
			if tc.accepted && err != nil {
				t.Fatalf("consistent clone identity refused: %v", err)
			}
			if !tc.accepted && err == nil {
				t.Fatal("clone identity contradicting or lacking its oracle accepted")
			}
		})
	}
	t.Run("cloneOracle on a GoLean clone", func(t *testing.T) {
		root, rep, m := goleanReportFixture(t)
		rep.CloneOracle = &OracleIdentity{Path: "/go", SHA256: strings.Repeat("a", 64), Version: "go1.26", GOARCH: "amd64"}
		if err := ValidateBatchReport(root, rep, m); err == nil {
			t.Fatal("gc clone oracle accepted for a GoLean clone")
		}
	})
}

// The validator recomputes Identity from Oracle, so a real adapter's two
// outputs must agree for every configuration the CLI can request.
func TestGcAdapterIdentityRecomputesFromOracle(t *testing.T) {
	ctx := context.Background()
	for _, a := range []*GcAdapter{{}, {GCFlags: "-N -l"}, {GOARCH: "386", AdapterName: "gc-386", GCFlags: `-d="x y"`}} {
		id, err := a.Identity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		o, err := a.Oracle(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := gcIdentity(o.Version, o.Path, o.GOARCH, o.GCFlags); id != want {
			t.Fatalf("Identity %q, recomputed from Oracle %q", id, want)
		}
	}
}
