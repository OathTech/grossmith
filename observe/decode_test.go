package observe

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func okDoc(value string) []byte {
	return []byte(`{"schema":"grossmith-observation-v2","status":"ok","values":[` + value + `]}`)
}

// refuseBoth requires both entry points to refuse: Parse, and plain
// json.Unmarshal (the path report decoding reaches through Outcome).
func refuseBoth(t *testing.T, raw []byte) {
	t.Helper()
	if _, err := Parse(raw); err == nil {
		t.Errorf("Parse accepted %s", raw)
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err == nil {
		t.Errorf("json.Unmarshal accepted %s", raw)
	}
}

// base64.StdEncoding accepts non-zero padding bits and skips embedded
// newlines, so "wh==" and "w\ng==" both decoded to the byte c2 that
// "wg==" denotes. Evidence keeps one spelling per byte string.
func TestBase64PayloadsMustBeCanonical(t *testing.T) {
	for _, text := range []string{`wh==`, `w\ng==`, `w\r\ng==`, `wg`, `wg=`, ` wg==`} {
		refuseBoth(t, okDoc(`{"kind":"string","goType":"string","strBytes":"`+text+`"}`))
		refuseBoth(t, okDoc(`{"kind":"interface","goType":"any","dynType":"string","payload":{"kind":"string","goType":"string","strBytes":"`+text+`"}}`))
		refuseBoth(t, []byte(`{"schema":"grossmith-observation-v2","status":"panic","panic":{"kind":"other","messageBytes":"`+text+`"}}`))
		refuseBoth(t, []byte(`{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int"}],"events":[{"at":"recovered","panic":{"kind":"other","messageBytes":"`+text+`"}}]}`))
	}
	doc, err := Parse(okDoc(`{"kind":"string","goType":"string","strBytes":"wg=="}`))
	if err != nil || doc.Values[0].StringData() != "\xc2" {
		t.Fatalf("canonical strBytes refused: %v", err)
	}
	doc, err = Parse([]byte(`{"schema":"grossmith-observation-v2","status":"panic","panic":{"kind":"other","messageBytes":"wg=="}}`))
	if err != nil || doc.Panic.MessageData() != "\xc2" {
		t.Fatalf("canonical messageBytes refused: %v", err)
	}
}

// An explicit null decoded to the field's zero, so {"int":null} matched a
// real {"int":0}. Every Value field now refuses null at every nesting level;
// omission (the documented encoding of a zero payload) stays valid.
func TestValueFieldsRefuseNull(t *testing.T) {
	base := map[string]string{
		"kind": `"kind":null,"goType":"int"`, "goType": `"kind":"int","goType":null`,
		"bool": `"kind":"bool","goType":"bool","bool":null`, "int": `"kind":"int","goType":"int","int":null`,
		"uint": `"kind":"uint","goType":"uint","uint":null`, "str": `"kind":"string","goType":"string","str":null`,
		"strBytes": `"kind":"string","goType":"string","strBytes":null`,
		"len":      `"kind":"slice","goType":"[]int","len":null`, "elems": `"kind":"slice","goType":"[]int","elems":null`,
		"fields":  `"kind":"struct","goType":"S","fields":null`,
		"entries": `"kind":"map","goType":"map[int]int","entries":null`,
		"dynType": `"kind":"int","goType":"int","dynType":null`, "payload": `"kind":"int","goType":"int","payload":null`,
	}
	if len(base) != len(valueFieldNames) {
		t.Fatalf("test covers %d fields, Value has %d", len(base), len(valueFieldNames))
	}
	for field, body := range base {
		v := `{` + body + `}`
		for _, ctx := range []string{
			`%s`,
			`{"kind":"interface","goType":"any","dynType":"T","payload":%s}`,
			`{"kind":"slice","goType":"[]T","len":1,"elems":[%s]}`,
			`{"kind":"struct","goType":"S","fields":[{"name":"f","value":%s}]}`,
			`{"kind":"map","goType":"map[int]T","len":1,"entries":[{"key":{"kind":"int","goType":"int"},"value":%s}]}`,
		} {
			raw := okDoc(fmt.Sprintf(ctx, v))
			if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "null") {
				t.Errorf("%s: explicit null accepted or refused for another reason: %v", field, err)
			}
			var doc Document
			if err := json.Unmarshal(raw, &doc); err == nil {
				t.Errorf("%s: json.Unmarshal accepted explicit null", field)
			}
		}
	}
	for _, raw := range []string{
		`{"kind":"struct","goType":"S","fields":[{"name":null,"value":{"kind":"int","goType":"int"}}]}`,
		`{"kind":"struct","goType":"S","fields":[null]}`,
		`{"kind":"slice","goType":"[]int","len":1,"elems":[null]}`,
		`{"kind":"map","goType":"map[int]int","len":1,"entries":[{"key":null,"value":{"kind":"int","goType":"int"}}]}`,
		`{"kind":"map","goType":"map[int]int","len":1,"entries":[null]}`,
		`null`,
	} {
		refuseBoth(t, okDoc(raw))
	}
	// Omitted zero payloads remain the canonical encoding and must match
	// the explicit zero spelling encoding/json would also accept.
	for _, raw := range []string{
		`{"kind":"int","goType":"int"}`, `{"kind":"int","goType":"int","int":0}`,
		`{"kind":"bool","goType":"bool"}`, `{"kind":"string","goType":"string"}`,
		`{"kind":"slice","goType":"[]int"}`, `{"kind":"slice","goType":"[]int","len":0,"elems":[]}`,
		`{"kind":"struct","goType":"S"}`, `{"kind":"map","goType":"map[int]int"}`,
	} {
		if _, err := Parse(okDoc(raw)); err != nil {
			t.Errorf("omitted or zero payload refused: %s: %v", raw, err)
		}
	}
}

// Field types and spellings stay exact inside the single-pass decoder.
func TestValueDecoderTypesAndNames(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"int","goType":"int","int":1.0}`,
		`{"kind":"int","goType":"int","int":1e2}`,
		`{"kind":"int","goType":"int","int":"1"}`,
		`{"kind":"int","goType":"int","int":9223372036854775808}`,
		`{"kind":"uint","goType":"uint","uint":-1}`,
		`{"kind":"bool","goType":"bool","bool":1}`,
		`{"kind":"slice","goType":"[]int","len":1,"elems":{"kind":"int","goType":"int"}}`,
		`{"kind":"int","goType":"int","Int":1}`,
		`{"kind":"int","goType":"int","extra":1}`,
		`{"kind":"interface","goType":"any","dynType":"int","payload":{"kind":"int","goType":"int","ſtr":"a"}}`,
		`{"kind":"interface","goType":"any","dynType":"int","payload":{"kind":"int","goType":"int","int":1,"int":2}}`,
		`{"kind":"struct","goType":"S","fields":[{"name":"a","Name":"b","value":{"kind":"int","goType":"int"}}]}`,
		`{"kind":"struct","goType":"S","fields":[{"name":"a","value":{"kind":"int","goType":"int"},"extra":1}]}`,
		`{"kind":"map","goType":"map[int]int","len":1,"entries":[{"key":{"kind":"int","goType":"int"},"key":{"kind":"int","goType":"int"},"value":{"kind":"int","goType":"int"}}]}`,
	} {
		refuseBoth(t, okDoc(raw))
	}
	want := Value{Kind: "interface", GoType: "any", DynType: "S", Payload: &Value{
		Kind: "struct", GoType: "S", Fields: []Field{{Name: "m", Value: Value{
			Kind: "map", GoType: "map[string][]uint8", Len: 1, Entries: []Entry{{
				Key:   StringValue("string", "\xc2"),
				Value: Value{Kind: "slice", GoType: "[]uint8", Len: 2, Elems: []Value{{Kind: "uint", GoType: "uint8", Uint: 255}, {Kind: "bool", GoType: "bool", Bool: true}}},
			}},
		}}},
	}}
	wire, err := OK(nil, []Value{want, {Kind: "int", GoType: "int64", Int: -9223372036854775808}}).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(wire)
	if err != nil {
		t.Fatalf("round trip refused: %v", err)
	}
	back, err := doc.Canonical()
	if err != nil || string(back) != string(wire) {
		t.Fatalf("round trip changed the document:\n%s\n%s", wire, back)
	}
}

func nestedInterfaces(depth int) []byte {
	inner := `{"kind":"int","goType":"int","int":1}`
	return okDoc(strings.Repeat(`{"kind":"interface","goType":"any","dynType":"int","payload":`, depth) + inner + strings.Repeat("}", depth))
}

// Each level used to validate and decode its whole subtree again, so a
// 2000-deep interface chain (124 KB) took 4.6 s. One subtree scan per
// outermost value keeps decoding linear in the input.
func TestDeepNestingDecodesInOnePass(t *testing.T) {
	const depth = 3000
	raw := nestedInterfaces(depth)
	before := subtreeScans.Load()
	start := time.Now()
	doc, err := Parse(raw)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if scans := subtreeScans.Load() - before; scans != 1 {
		t.Fatalf("a single top-level value was scanned %d times; nested values must not rescan", scans)
	}
	got, v := 0, doc.Values[0]
	for v.Payload != nil {
		got, v = got+1, *v.Payload
	}
	if got != depth || v.Int != 1 {
		t.Fatalf("decoded depth %d, innermost %+v", got, v)
	}
	// Generous: the quadratic decoder needed roughly 10 s at this depth;
	// the linear one takes milliseconds, even under the race detector.
	if elapsed > 3*time.Second {
		t.Fatalf("depth %d took %v", depth, elapsed)
	}
}
