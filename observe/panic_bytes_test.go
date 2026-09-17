package observe

import (
	"encoding/json"
	"testing"
)

func TestPanicMessageBytes(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		document := func(message string) Document {
			d := Panicked(nil, PanicOther, message)
			if recovered {
				return OK([]Event{{At: "recovered", Panic: d.Panic}}, []Value{{Kind: "int", GoType: "int"}})
			}
			return d
		}
		for _, message := range []string{"", "hello", "µ", "\x00", "\xc2", "\xb5", "\xff\xfe"} {
			doc := document(message)
			if err := doc.Validate(); err != nil {
				t.Fatalf("recovered=%v message=%q: %v", recovered, message, err)
			}
			wire, err := doc.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			back, err := Parse(wire)
			if err != nil {
				t.Fatal(err)
			}
			if equal, err := Equal(doc, back, PanicExact); err != nil || !equal {
				t.Fatalf("message did not round-trip: equal=%v err=%v", equal, err)
			}
		}
		a, b := document("\xc2"), document("\xb5")
		if equal, err := Equal(a, b, PanicExact); err != nil || equal {
			t.Fatalf("different panic bytes collapsed: equal=%v err=%v", equal, err)
		}
		if equal, err := Equal(a, b, PanicKindOnly); err != nil || !equal {
			t.Fatalf("kind policy retained messages: equal=%v err=%v", equal, err)
		}
	}
}

func TestPanicMessagePresence(t *testing.T) {
	for _, payload := range []string{
		`{"kind":"other"}`, `{"kind":"other","message":null}`,
		`{"kind":"other","messageBytes":null}`,
		`{"kind":"other","messageBytes":""}`,
		`{"kind":"other","messageBytes":"eA=="}`,
		`{"kind":"other","message":"","messageBytes":"wg=="}`,
	} {
		if _, err := Parse([]byte(`{"schema":"grossmith-observation-v2","status":"panic","panic":` + payload + `}`)); err == nil {
			t.Errorf("accepted missing or contradictory panic message: %s", payload)
		}
	}
	for _, p := range []PanicInfo{{Kind: PanicOther}, {Kind: PanicOther, Message: "\xff"}} {
		doc := Document{Schema: Schema, Status: StatusPanic, Panic: &p}
		if _, err := Equal(doc, doc, PanicExact); err == nil {
			t.Errorf("accepted malformed in-memory panic: %+v", p)
		}
		if _, err := json.Marshal(doc); err == nil {
			t.Errorf("serialized malformed in-memory panic: %+v", p)
		}
	}
}
