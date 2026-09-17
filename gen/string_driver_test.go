package gen

import (
	"strings"
	"testing"

	"grossmith/observe"
)

func TestDriverPreservesByteStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	subject := `package main
func fuzzSubject() (string, [3]string, map[string]string) {
    obsStr("point", "string", "\xc2")
    defer obsStr("defer", "string", "\xb5")
    return "\xc2", [3]string{"\xc2", "\xb5", "�"},
        map[string]string{"\xc2": "\xb5", "\xb5": "\xc2"}
}
`
	var g Generator
	doc := runCase(t, Case{Source: []byte(subject), Driver: []byte(g.driverSource(make([]binding, 3)))})
	if doc.Status != observe.StatusOK {
		t.Fatalf("status %s", doc.Status)
	}
	if got := doc.Values[0].StringData(); got != "\xc2" {
		t.Fatalf("scalar bytes changed: %x", got)
	}
	for i, want := range []string{"\xc2", "\xb5", "�"} {
		if got := doc.Values[1].Elems[i].StringData(); got != want {
			t.Fatalf("array[%d]: %x, want %x", i, got, want)
		}
	}
	entries := doc.Values[2].Entries
	if entries[0].Key.StringData() != "\xb5" || entries[0].Value.StringData() != "\xc2" ||
		entries[1].Key.StringData() != "\xc2" || entries[1].Value.StringData() != "\xb5" {
		t.Fatalf("map bytes or bytewise key ordering changed: %+v", entries)
	}
	for i, want := range []string{"\xc2", "\xb5"} {
		if got := doc.Events[i].Value.StringData(); got != want {
			t.Fatalf("event %d bytes changed: %x, want %x", i, got, want)
		}
	}
	mutated := strings.ReplaceAll(subject, "\\xc2", "\\xc3")
	other := runCase(t, Case{Source: []byte(mutated), Driver: []byte(g.driverSource(make([]binding, 3)))})
	if equal, err := observe.Equal(doc, other, observe.PanicExact); err != nil || equal {
		t.Fatalf("a byte mutation disappeared through the driver: equal=%v err=%v", equal, err)
	}
}
