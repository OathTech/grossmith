package strictjson

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, raw := range []string{
		`{"status":"error","status":"ok"}`,
		`{"nested":[{"value":1,"value":2}]}`,
		`{"value":1,"v\u0061lue":2}`,
		`{"nested":{"x":1,"x":1}}`,
		`{} {}`,
		`{}]`,
		`{}}`,
		`{} trailing`,
		`{"incomplete":`,
		`"\ud800"`,
		`"\udc00"`,
		`"\ud800\u0041"`,
		`"\ud800x"`,
		"\"\xff\"",
		``,
		strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	} {
		if err := Validate([]byte(raw)); err == nil {
			t.Errorf("accepted invalid/ambiguous JSON: %.100s", raw)
		}
	}
	for _, raw := range []string{
		`{"a":{"value":1},"b":{"value":2}}`,
		`[{"value":1},{"value":2}]`,
		`{"text":"escaped \"value\":1,\"value\":2"}`,
		`{"min":-9223372036854775808,"max":18446744073709551615}`,
		`{"huge":1e1000}`,
		"\n {} \r\n\t",
		`null`,
		`[true, false, null, 1, "x"]`,
		`"\ud83d\ude00"`,
		`"\\ud800"`,
		`"\"escaped quote\" and \/"`,
		strings.Repeat("[", 10000) + "0" + strings.Repeat("]", 10000),
	} {
		if err := Validate([]byte(raw)); err != nil {
			t.Errorf("valid JSON refused: %s: %v", raw, err)
		}
	}
}

func TestUnmarshalRejectsUnknownFieldsAndPreservesWidths(t *testing.T) {
	var dst struct {
		Value uint64 `json:"value"`
	}
	if err := Unmarshal([]byte(`{"extra":1}`), &dst); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field accepted: %v", err)
	}
	if err := Unmarshal([]byte(`{"value":18446744073709551615}`), &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Value != ^uint64(0) {
		t.Fatalf("integer rounded: %d", dst.Value)
	}
}

func FuzzValidate(f *testing.F) {
	for _, raw := range []string{`{}`, `{"x":1,"x":2}`, `{"a":[{"b":null}]}`, `[]]`, `"x"`, `"\ud800"`, `"\ud83d\ude00"`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_ = Validate([]byte(raw)) // arbitrary input must never panic.
		var dst struct {
			Status string `json:"status"`
			Items  []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		}
		_ = Unmarshal([]byte(raw), &dst)
		_ = UnmarshalExtensible([]byte(raw), &dst)
	})
}

func TestSchemaFieldSpelling(t *testing.T) {
	type item struct {
		Kind string `json:"kind"`
	}
	type record struct {
		Status string          `json:"status"`
		Items  []item          `json:"items"`
		ByName map[string]item `json:"byName"`
		Open   map[string]int  `json:"open"`
	}
	for _, decode := range []func([]byte, any) error{Unmarshal, UnmarshalExtensible} {
		for _, raw := range []string{
			`{"status":"error","Status":"ok"}`,
			`{"Status":"ok"}`,
			`{"ſtatus":"ok"}`,
			`{"items":[{"kind":"int","Kind":"bool"}]}`,
			`{"items":[{"Kind":"int"}]}`,
			`{"byName":{"A":{"Kind":"int"}}}`,
		} {
			var dst record
			if err := decode([]byte(raw), &dst); err == nil || !strings.Contains(err.Error(), "exact spelling") {
				t.Errorf("accepted schema alias: %s: %v", raw, err)
			}
		}
		var dst record
		if err := decode([]byte(`{"open":{"status":1,"Status":2,"ſtatus":3},"byName":{"A":{"kind":"int"},"a":{"kind":"bool"}}}`), &dst); err != nil {
			t.Fatalf("case-sensitive map keys refused: %v", err)
		}
		if dst.Open["status"] != 1 || dst.Open["Status"] != 2 || dst.Open["ſtatus"] != 3 || len(dst.ByName) != 2 {
			t.Fatal("map keys folded together")
		}
	}
	var dst record
	if err := UnmarshalExtensible([]byte(`{"status":"ok","newField":{"status":1,"Status":2}}`), &dst); err != nil {
		t.Fatalf("external unknown-field compatibility lost: %v", err)
	}
	// Exact fields take precedence even if their spellings differ only in case.
	var distinct struct {
		Lower int `json:"x"`
		Upper int `json:"X"`
	}
	if err := Unmarshal([]byte(`{"x":1,"X":2}`), &distinct); err != nil || distinct.Lower != 1 || distinct.Upper != 2 {
		t.Fatalf("distinct exact schema fields: %+v: %v", distinct, err)
	}
}

// encoding/json truncates long input and zero-fills short input for Go
// fixed-size arrays, so [1,2,3] and [5] used to decode as [1 2] and [5 0].
func TestFixedArrayLengthMustMatch(t *testing.T) {
	type seeds struct {
		Seeds  [2]int64   `json:"seeds"`
		Nested [][2]int64 `json:"nested"`
		Ptr    *[1]string `json:"ptr"`
		Open   []int      `json:"open"`
	}
	for _, raw := range []string{
		`{"seeds":[1,2,3]}`,
		`{"seeds":[5]}`,
		`{"seeds":[]}`,
		`{"nested":[[1,2],[3]]}`,
		`{"ptr":["a","b"]}`,
	} {
		var s seeds
		if err := Unmarshal([]byte(raw), &s); err == nil || !strings.Contains(err.Error(), "want exactly") {
			t.Errorf("%s: array length mismatch accepted as %+v (err=%v)", raw, s, err)
		}
		if err := UnmarshalExtensible([]byte(raw), &s); err == nil {
			t.Errorf("%s: extensible decoding accepted an array length mismatch", raw)
		}
	}
	var s seeds
	if err := Unmarshal([]byte(`{"seeds":[5,6],"nested":[[1,2]],"ptr":["a"],"open":[1,2,3]}`), &s); err != nil {
		t.Fatalf("exact arrays refused: %v", err)
	}
	if s.Seeds != [2]int64{5, 6} || s.Nested[0] != [2]int64{1, 2} || s.Ptr[0] != "a" || len(s.Open) != 3 {
		t.Fatalf("decoded %+v", s)
	}
}
