package observe

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestStringObservationPreservesEveryByte(t *testing.T) {
	seen := map[string]string{}
	inputs := []string{"", "µ", "�", "a\x00b", "\xc2\xb5", "\xc2", "\xb5", "\xc0\x80"}
	for b := 0; b < 256; b++ {
		inputs = append(inputs, string([]byte{byte(b)}))
	}
	for _, s := range inputs {
		doc := OK(nil, []Value{StringValue("string", s)})
		if err := doc.Validate(); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		wire, err := doc.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if old, ok := seen[string(wire)]; ok && old != s {
			t.Fatalf("%q and %q have identical observations", old, s)
		}
		seen[string(wire)] = s
		back, err := Parse(wire)
		if err != nil {
			t.Fatalf("%q cannot round-trip: %v", s, err)
		}
		if got := back.Values[0].StringData(); got != s {
			t.Fatalf("string bytes changed: %x -> %x", s, got)
		}
	}
	left := OK(nil, []Value{StringValue("string", "\xc2")})
	right := OK(nil, []Value{StringValue("string", "\xb5")})
	if equal, err := Equal(left, right, PanicExact); err != nil || equal {
		t.Fatalf("different malformed UTF-8 sequences collapsed: equal=%v err=%v", equal, err)
	}
}

func TestStringPayloadMustBeCanonical(t *testing.T) {
	for _, v := range []Value{
		{Kind: "string", GoType: "string", Str: "\xff"},
		{Kind: "string", GoType: "string", Str: "x", StrBytes: []byte{0xff}},
		{Kind: "string", GoType: "string", StrBytes: []byte("valid UTF-8")},
		{Kind: "string", GoType: "string", StrBytes: []byte{}},
		{Kind: "int", GoType: "int", StrBytes: []byte{0xff}},
		{Kind: "array", GoType: "[0]int", StrBytes: []byte{0xff}},
	} {
		doc := OK(nil, []Value{v})
		if _, err := Equal(doc, doc, PanicExact); err == nil {
			t.Errorf("non-canonical string payload accepted: %+v", v)
		}
	}
	if _, err := Parse([]byte(`{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"string","goType":"string","strBytes":"invalid base64!"}]}`)); err == nil {
		t.Fatal("malformed base64 accepted")
	}
}

func TestStringWirePayloads(t *testing.T) {
	// Exercise every nesting route, including report decoding's json.Unmarshal
	// path, so a guard solely in Parse cannot hide malformed stored evidence.
	contexts := map[string]string{
		"result":    `{"schema":"grossmith-observation-v2","status":"ok","values":[%s]}`,
		"array":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"array","goType":"[1]string","len":1,"elems":[%s]}]}`,
		"slice":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"slice","goType":"[]string","len":1,"elems":[%s]}]}`,
		"struct":    `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"struct","goType":"S","fields":[{"name":"s","value":%s}]}]}`,
		"map key":   `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"map","goType":"map[string]int","len":1,"entries":[{"key":%s,"value":{"kind":"int","goType":"int"}}]}]}`,
		"map value": `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"map","goType":"map[int]string","len":1,"entries":[{"key":{"kind":"int","goType":"int"},"value":%s}]}]}`,
		"interface": `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"interface","goType":"interface{}","dynType":"string","payload":%s}]}`,
		"point":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"point","value":%s}]}`,
		"defer":     `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"defer","value":%s}]}`,
	}
	for name, context := range contexts {
		t.Run(name, func(t *testing.T) {
			for _, payload := range []string{
				`"strBytes":"wg==","str":""`,
				`"strBytes":"wg==","str":null`,
				`"strBytes":[194,null]`, `"strBytes":[194]`,
				`"strBytes":null`, `"str":null`,
				`"strBytes":""`, `"strBytes":"YQ=="`,
			} {
				v := `{"kind":"string","goType":"string",` + payload + `}`
				raw := []byte(fmt.Sprintf(context, v))
				if _, err := Parse(raw); err == nil {
					t.Errorf("Parse accepted %s", payload)
				}
				var doc Document
				if err := json.Unmarshal(raw, &doc); err == nil {
					t.Errorf("JSON decoding accepted %s", payload)
				}
			}
			for _, payload := range []string{"", `,"str":""`, `,"str":"µ"`, `,"strBytes":"wg=="`} {
				v := `{"kind":"string","goType":"string"` + payload + `}`
				if _, err := Parse([]byte(fmt.Sprintf(context, v))); err != nil {
					t.Errorf("valid payload %q refused: %v", payload, err)
				}
			}
		})
	}
}
