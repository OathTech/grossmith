// Package observe is the versioned observation protocol: what a runtime
// reports about one case execution, as typed structured data.
//
// It descends from the grossmith-proto observe package (same authors,
// Apache-2.0), which existed because the previous channel compared stdout
// STRINGS: `int8(5)` and `int32(5)` print identically, so width defects were
// unobservable, and Println is not injective over strings. This version
// additionally replaces the `println` channel the audit rejected (C2:
// implementation-specific, not demandable from a conforming clone) and
// carries slices, maps, interfaces-with-payload (C4), ordered mid-execution
// events, and a structured panic identity with a closed kind taxonomy (C3).
package observe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Schema is the wire tag carried by every document. A reader that does not
// recognise it must fail rather than guess.
const Schema = "grossmith-observation-v2"

// Status is how a run terminated.
type Status string

const (
	// StatusOK: the subject returned; events and values are present.
	StatusOK Status = "ok"
	// StatusPanic: the subject panicked (unrecovered). Events observed
	// before the panic are present; values are absent; Panic identifies it.
	StatusPanic Status = "panic"
	// StatusError: no usable observation (compile failure, timeout, ...).
	StatusError Status = "error"
)

// PanicKind is the closed panic taxonomy. Each adapter's driver owns the
// mapping from its implementation's panic representation into this set; the
// raw message rides along untouched.
type PanicKind string

const (
	PanicDivide         PanicKind = "divide"
	PanicIndexRange     PanicKind = "index-out-of-range"
	PanicSliceBounds    PanicKind = "slice-bounds"
	PanicIfaceConv      PanicKind = "interface-conversion"
	PanicNilDeref       PanicKind = "nil-dereference"
	PanicOther          PanicKind = "other"
)

// KindFromMessage maps a Go runtime panic message to its kind. This is the
// REFERENCE implementation's mapping (gc message prose); other adapters map
// from their own representations.
func KindFromMessage(msg string) PanicKind {
	switch {
	case contains(msg, "integer divide by zero"):
		return PanicDivide
	case contains(msg, "index out of range"):
		return PanicIndexRange
	case contains(msg, "slice bounds out of range"):
		return PanicSliceBounds
	case contains(msg, "interface conversion"):
		return PanicIfaceConv
	case contains(msg, "nil pointer dereference"), contains(msg, "invalid memory address"):
		return PanicNilDeref
	}
	return PanicOther
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

// ErrorKind classifies WHY there is no observation — typed, never parsed
// back out of a message.
type ErrorKind string

const (
	ErrCompile       ErrorKind = "compile"
	ErrTimeout       ErrorKind = "timeout"
	ErrRun           ErrorKind = "run"
	ErrNoObservation ErrorKind = "no-observation"
	ErrAdapter       ErrorKind = "adapter"
)

// Value is one observed value. GoType is the SPELLING of the static Go type
// (`int8`, `uint32`, `T0`), which is what makes width and signedness
// comparable: two values of different GoType are never equal even when
// numeric content matches. Fields are kind-discriminated; absent fields
// marshal as their zero values inside the kinds that use them (the canonical
// form is the deterministic field order of this struct).
type Value struct {
	Kind   string  `json:"kind"`
	GoType string  `json:"goType"`
	Bool   bool    `json:"bool,omitempty"`
	Int    int64   `json:"int,omitempty"`
	Uint   uint64  `json:"uint,omitempty"`
	Str    string  `json:"str,omitempty"`
	Len    int     `json:"len,omitempty"`
	Elems  []Value `json:"elems,omitempty"`
	Fields []Field `json:"fields,omitempty"`
	// Entries carries map content with KEYS SORTED by the driver — the
	// deterministic full-map observation (stronger than alphabet probes).
	Entries []Entry `json:"entries,omitempty"`
	// DynType and Payload carry an interface's dynamic type name and boxed
	// value (the C4 fix: type identity AND payload).
	DynType string `json:"dynType,omitempty"`
	Payload *Value `json:"payload,omitempty"`
}

// Field is one member of an observed struct.
type Field struct {
	Name  string `json:"name"`
	Value Value  `json:"value"`
}

// Entry is one observed map entry.
type Entry struct {
	Key   Value `json:"key"`
	Value Value `json:"value"`
}

// Event is one ordered mid-execution observation.
type Event struct {
	// At is the event source: "point" (observation point), "defer"
	// (deferred exit observation), "recovered" (a guarded statement caught
	// a panic and execution continued).
	At string `json:"at"`
	// Value is present for point/defer events.
	Value *Value `json:"value,omitempty"`
	// Panic is present for recovered events.
	Panic *PanicInfo `json:"panic,omitempty"`
}

// PanicInfo is a structured panic identity.
type PanicInfo struct {
	Kind    PanicKind `json:"kind"`
	Message string    `json:"message"`
}

// ErrorInfo is a structured non-observation.
type ErrorInfo struct {
	Kind   ErrorKind `json:"kind"`
	Detail string    `json:"detail"`
}

// Document is one complete observation.
type Document struct {
	Schema string     `json:"schema"`
	Status Status     `json:"status"`
	Events []Event    `json:"events,omitempty"`
	Values []Value    `json:"values,omitempty"`
	Panic  *PanicInfo `json:"panic,omitempty"`
	Error  *ErrorInfo `json:"error,omitempty"`
}

// OK builds a successful document.
func OK(events []Event, values []Value) Document {
	return Document{Schema: Schema, Status: StatusOK, Events: events, Values: values}
}

// Panicked builds an unrecovered-panic document.
func Panicked(events []Event, kind PanicKind, message string) Document {
	return Document{Schema: Schema, Status: StatusPanic, Events: events,
		Panic: &PanicInfo{Kind: kind, Message: message}}
}

// Errored builds a non-observation.
func Errored(kind ErrorKind, detail string) Document {
	return Document{Schema: Schema, Status: StatusError,
		Error: &ErrorInfo{Kind: kind, Detail: detail}}
}

// Failed reports whether this is a non-observation.
func (d Document) Failed() bool { return d.Status == StatusError }

// Canonical is the deterministic wire form: encoding/json over fixed struct
// field order, no indentation. Byte equality of Canonical() is the base
// conformance equivalence.
func (d Document) Canonical() ([]byte, error) {
	return json.Marshal(d)
}

// Parse reads a document, failing closed: unknown schema, malformed JSON,
// unknown status, or unknown kinds are errors, never partial reads.
func Parse(data []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var d Document
	if err := dec.Decode(&d); err != nil {
		return Document{}, fmt.Errorf("observe: %w", err)
	}
	if dec.More() {
		// One document per parse: trailing bytes mean the producer is not
		// speaking the protocol (two concatenated documents, stray output).
		return Document{}, fmt.Errorf("observe: trailing data after document")
	}
	if d.Schema != Schema {
		return Document{}, fmt.Errorf("observe: unknown schema %q (want %q)", d.Schema, Schema)
	}
	switch d.Status {
	case StatusOK, StatusPanic, StatusError:
	default:
		return Document{}, fmt.Errorf("observe: unknown status %q", d.Status)
	}
	var checkValue func(v Value) error
	checkValue = func(v Value) error {
		switch v.Kind {
		case "bool", "int", "uint", "string", "array", "struct", "slice", "map", "interface":
		default:
			return fmt.Errorf("observe: unknown value kind %q", v.Kind)
		}
		for _, e := range v.Elems {
			if err := checkValue(e); err != nil {
				return err
			}
		}
		for _, f := range v.Fields {
			if err := checkValue(f.Value); err != nil {
				return err
			}
		}
		for _, e := range v.Entries {
			if err := checkValue(e.Key); err != nil {
				return err
			}
			if err := checkValue(e.Value); err != nil {
				return err
			}
		}
		if v.Payload != nil {
			return checkValue(*v.Payload)
		}
		return nil
	}
	for _, v := range d.Values {
		if err := checkValue(v); err != nil {
			return Document{}, err
		}
	}
	// Every closed vocabulary is closed at parse time, not just the three
	// the driver happens to exercise (audit finding: a clone misspelling a
	// panic kind parsed cleanly, and under PanicKindOnly the kind IS the
	// verdict).
	checkPanic := func(p *PanicInfo) error {
		if p == nil {
			return nil
		}
		switch p.Kind {
		case PanicDivide, PanicIndexRange, PanicSliceBounds, PanicIfaceConv, PanicNilDeref, PanicOther:
			return nil
		}
		return fmt.Errorf("observe: unknown panic kind %q", p.Kind)
	}
	if err := checkPanic(d.Panic); err != nil {
		return Document{}, err
	}
	if d.Error != nil {
		switch d.Error.Kind {
		case ErrCompile, ErrTimeout, ErrRun, ErrNoObservation, ErrAdapter:
		default:
			return Document{}, fmt.Errorf("observe: unknown error kind %q", d.Error.Kind)
		}
	}
	for _, e := range d.Events {
		switch e.At {
		case "point", "defer", "recovered":
		default:
			return Document{}, fmt.Errorf("observe: unknown event position %q", e.At)
		}
		if e.Value != nil {
			if err := checkValue(*e.Value); err != nil {
				return Document{}, err
			}
		}
		if err := checkPanic(e.Panic); err != nil {
			return Document{}, err
		}
	}
	return d, nil
}

// PanicPolicy selects the panic-identity equivalence (audit C3).
type PanicPolicy string

const (
	// PanicExact: kind AND message byte-equal.
	PanicExact PanicPolicy = "exact"
	// PanicKindOnly: kind equal; the message is informative only (gc's
	// panic prose is implementation detail a conforming clone need not
	// reproduce).
	PanicKindOnly PanicPolicy = "kind"
)

// Equal compares two documents under the policy: byte equality of the
// canonical form, with panic messages normalized away first under
// PanicKindOnly (in the top-level panic and in recovered events).
func Equal(a, b Document, policy PanicPolicy) (bool, error) {
	if policy == PanicKindOnly {
		a = normalizePanicMessages(a)
		b = normalizePanicMessages(b)
	}
	ab, err := a.Canonical()
	if err != nil {
		return false, err
	}
	bb, err := b.Canonical()
	if err != nil {
		return false, err
	}
	return bytes.Equal(ab, bb), nil
}

func normalizePanicMessages(d Document) Document {
	if d.Panic != nil {
		p := *d.Panic
		p.Message = ""
		d.Panic = &p
	}
	events := make([]Event, len(d.Events))
	copy(events, d.Events)
	for i, e := range events {
		if e.Panic != nil {
			p := *e.Panic
			p.Message = ""
			events[i].Panic = &p
		}
	}
	d.Events = events
	return d
}
