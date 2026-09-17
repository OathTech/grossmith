package gen

import (
	"fmt"
	"strings"
	"testing"

	"grossmith/observe"
)

func TestDriverForSourceSignature(t *testing.T) {
	for _, source := range []string{
		`package other; func fuzzSubject() int { return 1 }`,
		`package main; func different() int { return 1 }`,
		`package main; type S int; func (S) fuzzSubject() int { return 1 }`,
		`package main; func fuzzSubject(x int) int { return x }`,
		`package main; func fuzzSubject[T any]() int { return 1 }`,
		`package main; func fuzzSubject() {}`,
		`package main; func fuzzSubject() int`,
		`package main; func fuzzSubject() int { return 1 }; func main() {}`,
		`package main; func fuzzSubject() int { return 1 }; func fuzzSubject() int { return 2 }`,
		`not Go`,
	} {
		if _, err := DriverForSource([]byte(source)); err == nil {
			t.Errorf("accepted unsuitable entry point: %s", source)
		}
	}
	for _, sig := range []string{"(int, int, bool)", "(a, b int, c bool)"} {
		source := "package main; func fuzzSubject() " + sig + " { return 1, 2, true }"
		driver, err := DriverForSource([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(driver), "r0, r1, r2 := fuzzSubject()") {
			t.Fatalf("wrong result arity for %s", sig)
		}
	}
}

func TestDriverPreservesPanicMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	for _, recovered := range []bool{false, true} {
		for _, message := range []string{"", "\xc2", "\xb5"} {
			body := fmt.Sprintf("panic(%q)", message)
			if recovered {
				body = `defer func() { if r := recover(); r != nil { obsRecovered(r.(string)) } }(); ` + body
			}
			source := []byte("package main; func fuzzSubject() (n int) { " + body + " }")
			driver, err := DriverForSource(source)
			if err != nil {
				t.Fatal(err)
			}
			doc := runCase(t, Case{Source: source, Driver: driver})
			p := doc.Panic
			if recovered {
				if doc.Status != observe.StatusOK || len(doc.Events) != 1 || doc.Events[0].At != "recovered" {
					t.Fatalf("missing recovered event: %+v", doc)
				}
				p = doc.Events[0].Panic
			} else if doc.Status != observe.StatusPanic {
				t.Fatalf("explicit panic became %s", doc.Status)
			}
			if p == nil || p.MessageData() != message {
				t.Fatalf("panic message %x changed: %+v", message, p)
			}
		}
	}
}

func TestDriverForEditedSource(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	// Named results, a defined type and an interface retain their static
	// types even though the driver knows only the number of results.
	source := []byte(`package main
type T int8
func fuzzSubject() (a, b T, c any) { return 1, 2, T(3) }
`)
	driver, err := DriverForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	doc := runCase(t, Case{Source: source, Driver: driver})
	if doc.Status != observe.StatusOK || len(doc.Values) != 3 {
		t.Fatalf("unexpected observation: %+v", doc)
	}
	if doc.Values[0].GoType != "T" || doc.Values[1].Int != 2 ||
		doc.Values[2].Kind != "interface" || doc.Values[2].DynType != "T" ||
		doc.Values[2].Payload.Int != 3 {
		t.Fatalf("source result types or values lost: %+v", doc.Values)
	}
}
