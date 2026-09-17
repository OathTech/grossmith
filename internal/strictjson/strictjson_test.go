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
	for _, raw := range []string{`{}`, `{"x":1,"x":2}`, `{"a":[{"b":null}]}`, `[]]`, `"x"`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_ = Validate([]byte(raw)) // arbitrary input must never panic.
	})
}
