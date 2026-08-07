package observe

import (
	"strings"
	"testing"
)

// TestWidthIsObservable: two values with identical numeric content but
// different GoType are unequal — the reason this package exists.
func TestWidthIsObservable(t *testing.T) {
	a := OK(nil, []Value{{Kind: "int", GoType: "int8", Int: 5}})
	b := OK(nil, []Value{{Kind: "int", GoType: "int32", Int: 5}})
	eq, err := Equal(a, b, PanicExact)
	if err != nil {
		t.Fatal(err)
	}
	if eq {
		t.Fatal("int8(5) compared equal to int32(5) — width erased")
	}
}

// TestInterfacePayloadIsObservable: same dynamic type, different payload —
// must be unequal (audit C4's injectivity violation, now a schema witness).
func TestInterfacePayloadIsObservable(t *testing.T) {
	box := func(n int64) Value {
		return Value{Kind: "interface", GoType: "I0", DynType: "T0",
			Payload: &Value{Kind: "int", GoType: "T0", Int: n}}
	}
	a := OK(nil, []Value{box(1)})
	b := OK(nil, []Value{box(2)})
	eq, err := Equal(a, b, PanicExact)
	if err != nil {
		t.Fatal(err)
	}
	if eq {
		t.Fatal("T0(1) and T0(2) boxed in I0 compared equal — payload dropped")
	}
}

// TestPanicPolicies: kind-only ignores message prose; exact does not.
func TestPanicPolicies(t *testing.T) {
	a := Panicked(nil, PanicIndexRange, "index out of range [44] with length 3")
	b := Panicked(nil, PanicIndexRange, "index out of range [7] with length 2")
	if eq, _ := Equal(a, b, PanicExact); eq {
		t.Fatal("exact policy ignored differing messages")
	}
	if eq, _ := Equal(a, b, PanicKindOnly); !eq {
		t.Fatal("kind policy compared messages")
	}
	c := Panicked(nil, PanicDivide, "x")
	if eq, _ := Equal(a, c, PanicKindOnly); eq {
		t.Fatal("kind policy ignored differing kinds")
	}
}

// TestParseFailsClosed: unknown schema, status, kind, or field is an error.
func TestParseFailsClosed(t *testing.T) {
	good, err := OK(nil, []Value{{Kind: "bool", GoType: "bool", Bool: true}}).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(good); err != nil {
		t.Fatalf("canonical round-trip failed: %v", err)
	}
	bad := []string{
		`{"schema":"grossmith-observation-v1","status":"ok"}`,
		`{"schema":"grossmith-observation-v2","status":"weird"}`,
		`{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"quaternion","goType":"q"}]}`,
		`{"schema":"grossmith-observation-v2","status":"ok","bonus":1}`,
		`not json`,
		// Audit findings: the vocabularies BEYOND the three the driver
		// exercises must be closed too, and trailing data is a protocol
		// violation, not ignorable.
		`{"schema":"grossmith-observation-v2","status":"panic","panic":{"kind":"divide-by-zero","message":"m"}}`,
		`{"schema":"grossmith-observation-v2","status":"ok","events":[{"at":"recovered","panic":{"kind":"KABOOM","message":"m"}}]}`,
		`{"schema":"grossmith-observation-v2","status":"ok","events":[{"at":"teleport","value":{"kind":"bool","goType":"bool"}}]}`,
		`{"schema":"grossmith-observation-v2","status":"error","error":{"kind":"gremlins","detail":"d"}}`,
		`{"schema":"grossmith-observation-v2","status":"ok"} trailing`,
		`{"schema":"grossmith-observation-v2","status":"ok"}{"schema":"grossmith-observation-v2","status":"ok"}`,
	}
	for _, s := range bad {
		if _, err := Parse([]byte(s)); err == nil {
			t.Fatalf("parsed without error: %s", s)
		}
	}
}

// TestKindMapping covers the gc message prose mapping.
func TestKindMapping(t *testing.T) {
	cases := map[string]PanicKind{
		"runtime error: integer divide by zero":               PanicDivide,
		"runtime error: index out of range [44] with length 3": PanicIndexRange,
		"runtime error: slice bounds out of range [:9]":        PanicSliceBounds,
		"interface conversion: main.I0 is main.T0, not main.T1": PanicIfaceConv,
		"runtime error: invalid memory address or nil pointer dereference": PanicNilDeref,
		"boom": PanicOther,
	}
	for msg, want := range cases {
		if got := KindFromMessage(msg); got != want {
			t.Fatalf("KindFromMessage(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestEventsAreOrdered: event order participates in equality.
func TestEventsAreOrdered(t *testing.T) {
	v := func(n int64) Event {
		val := Value{Kind: "int", GoType: "int", Int: n}
		return Event{At: "point", Value: &val}
	}
	a := OK([]Event{v(1), v(2)}, nil)
	b := OK([]Event{v(2), v(1)}, nil)
	if eq, _ := Equal(a, b, PanicExact); eq {
		t.Fatal("event order erased")
	}
	if !strings.Contains(string(mustCanon(t, a)), `"at":"point"`) {
		t.Fatal("events missing from canonical form")
	}
}

func mustCanon(t *testing.T, d Document) []byte {
	t.Helper()
	b, err := d.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
