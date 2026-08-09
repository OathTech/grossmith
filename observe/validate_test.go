package observe

import (
	"strings"
	"testing"
)

// The E1 adversarial table: every status/payload and kind/field
// combination the protocol cannot produce must be rejected by Validate
// (and therefore by Parse and Equal). The audit's replications lead the
// table — `status:"panic"` with no payload and an empty goType both
// parsed and judged as matches before E1.
func TestValidateRejectsForbiddenCombinations(t *testing.T) {
	bad := map[string]string{
		"panic without payload":  `{"schema":"grossmith-observation-v2","status":"panic"}`,
		"error without payload":  `{"schema":"grossmith-observation-v2","status":"error"}`,
		"ok without values":      `{"schema":"grossmith-observation-v2","status":"ok"}`,
		"ok with panic payload":  `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"panic":{"kind":"divide","message":"m"}}`,
		"ok with error payload":  `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"error":{"kind":"run","detail":"d"}}`,
		"panic with values":      `{"schema":"grossmith-observation-v2","status":"panic","panic":{"kind":"divide","message":"m"},"values":[{"kind":"int","goType":"int"}]}`,
		"panic with empty msg":   `{"schema":"grossmith-observation-v2","status":"panic","panic":{"kind":"divide","message":""}}`,
		"error with values":      `{"schema":"grossmith-observation-v2","status":"error","error":{"kind":"run","detail":"d"},"values":[{"kind":"int","goType":"int"}]}`,
		"error with events":      `{"schema":"grossmith-observation-v2","status":"error","error":{"kind":"run","detail":"d"},"events":[{"at":"point","value":{"kind":"int","goType":"int"}}]}`,
		"empty goType":           `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":""}]}`,
		"scalar with elems":      `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int","elems":[{"kind":"int","goType":"int"}]}]}`,
		"slice len mismatch":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"slice","goType":"[]int","len":2,"elems":[{"kind":"int","goType":"int"}]}]}`,
		"array len mismatch":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"array","goType":"[2]int","len":1,"elems":[{"kind":"int","goType":"int"},{"kind":"int","goType":"int"}]}]}`,
		"map len mismatch":       `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"map","goType":"map[int]int","len":2,"entries":[{"key":{"kind":"int","goType":"int","int":1},"value":{"kind":"int","goType":"int"}}]}]}`,
		"map keys unsorted":      `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"map","goType":"map[int]int","len":2,"entries":[{"key":{"kind":"int","goType":"int","int":5},"value":{"kind":"int","goType":"int"}},{"key":{"kind":"int","goType":"int","int":3},"value":{"kind":"int","goType":"int"}}]}]}`,
		"map keys duplicate":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"map","goType":"map[int]int","len":2,"entries":[{"key":{"kind":"int","goType":"int","int":3},"value":{"kind":"int","goType":"int"}},{"key":{"kind":"int","goType":"int","int":3},"value":{"kind":"int","goType":"int"}}]}]}`,
		"struct field unnamed":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"struct","goType":"S0","fields":[{"name":"","value":{"kind":"int","goType":"int"}}]}]}`,
		"interface no dynType":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"interface","goType":"I0","payload":{"kind":"int","goType":"T0"}}]}`,
		"interface no payload":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"interface","goType":"I0","dynType":"T0"}]}`,
		"point event no value":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"point"}]}`,
		"point event with panic": `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"point","value":{"kind":"int","goType":"int"},"panic":{"kind":"divide","message":"m"}}]}`,
		"recovered no panic":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"recovered"}]}`,
		"recovered with value":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"recovered","panic":{"kind":"divide","message":"m"},"value":{"kind":"int","goType":"int"}}]}`,
	}
	for name, raw := range bad {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: Parse accepted a structurally impossible document", name)
		}
	}
	// The judge's own gate: an invalid exported Document (no Parse
	// round-trip) must error out of Equal, not compare.
	invalid := Document{Schema: Schema, Status: StatusPanic}
	if _, err := Equal(invalid, invalid, PanicExact); err == nil {
		t.Error("Equal compared an invalid document instead of failing closed")
	}
	// And well-formed documents still flow.
	good := OK(nil, []Value{{Kind: "int", GoType: "int", Int: 7}})
	if err := good.Validate(); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	if eq, err := Equal(good, good, PanicExact); err != nil || !eq {
		t.Fatalf("valid self-comparison: eq=%v err=%v", eq, err)
	}
}

// Unknown policies are errors, never silent exact comparison (audit
// P2/P3: an unrecognized PanicPolicy value fell through to exact).
func TestUnknownPanicPolicyErrors(t *testing.T) {
	good := OK(nil, []Value{{Kind: "int", GoType: "int", Int: 7}})
	if _, err := Equal(good, good, PanicPolicy("lenient")); err == nil || !strings.Contains(err.Error(), "unknown panic policy") {
		t.Fatalf("unknown policy not rejected: %v", err)
	}
}
