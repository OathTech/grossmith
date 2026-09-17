package harness

import (
	"testing"

	"grossmith/observe"
)

// Cross both ways of reporting infrastructure failure: an adapter that
// never ran and an adapter that ran but produced an error document.
func TestJudgeOutcomeMatrix(t *testing.T) {
	type specimen struct {
		name    string
		outcome Outcome
		invalid bool
		infra   bool
	}
	cases := []specimen{
		{"ok", Outcome{Status: StatusRan, Document: observe.OK(nil, []observe.Value{{Kind: "int", GoType: "int", Int: 7}})}, false, false},
		{"document error", Outcome{Status: StatusRan, Document: observe.Errored(observe.ErrTimeout, "deadline")}, false, true},
		{"build failure", Outcome{Status: StatusBuildFailed, Detail: "compiler failed"}, false, true},
		{"run failure", Outcome{Status: StatusRunFailed, Detail: "process failed"}, false, true},
		{"timeout", Outcome{Status: StatusTimeout, Detail: "deadline"}, false, true},
		{"adapter failure", Outcome{Status: StatusAdapterErr, Detail: "unavailable"}, false, true},
		{"invalid document", Outcome{Status: StatusRan}, true, false},
		{"unknown status", Outcome{Status: "unknown", Detail: "unknown"}, true, false},
		{"missing failure reason", Outcome{Status: StatusTimeout}, true, false},
		{"non-ran with a document", Outcome{Status: StatusBuildFailed, Detail: "compiler failed", Document: observe.Errored(observe.ErrCompile, "duplicate payload")}, true, false},
	}
	for _, ref := range cases {
		for _, clone := range cases {
			t.Run(ref.name+"/"+clone.name, func(t *testing.T) {
				want := VerdictMatch
				switch {
				case ref.invalid || clone.invalid:
					want = VerdictHarnessError
				case ref.infra && clone.infra:
					want = VerdictBothInfra
				case ref.infra:
					want = VerdictRefInfra
				case clone.infra:
					want = VerdictCloneInfra
				}
				if got, detail := Judge(ref.outcome, clone.outcome, observe.PanicExact); got != want {
					t.Fatalf("got %s (%s), want %s", got, detail, want)
				}
				if got, _ := Judge(ref.outcome, clone.outcome, "unknown-policy"); got != VerdictHarnessError {
					t.Fatalf("unknown policy accepted as %s", got)
				}
			})
		}
	}
}
