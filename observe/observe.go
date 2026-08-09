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
// unknown status, unknown kinds, or a STRUCTURALLY impossible document
// (Validate) are errors, never partial reads.
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
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}

// Validate checks the document as a TAGGED UNION, not just a vocabulary
// (evidence arc E1; audit P0: `status:"panic"` with no panic payload
// parsed and judged as a semantic match). Every closed enum is closed
// here, and every status/payload and kind/field combination the protocol
// cannot produce is rejected. Called from Parse AND from Equal — adapters
// return exported Documents directly, so parse-time checking alone leaves
// the judge trusting unvalidated structs.
func (d Document) Validate() error {
	if d.Schema != Schema {
		return fmt.Errorf("observe: unknown schema %q (want %q)", d.Schema, Schema)
	}
	// Status/payload cardinality: exactly the payloads of the status, no
	// others. StatusOK requires at least one value — every generated
	// subject observes at least one result (the observed floor), so a
	// value-free ok document is a producer defect, not an observation.
	switch d.Status {
	case StatusOK:
		if d.Panic != nil || d.Error != nil {
			return fmt.Errorf("observe: status ok with panic/error payload")
		}
		if len(d.Values) == 0 {
			return fmt.Errorf("observe: status ok with no values")
		}
	case StatusPanic:
		if d.Panic == nil {
			return fmt.Errorf("observe: status panic without panic payload")
		}
		if d.Error != nil {
			return fmt.Errorf("observe: status panic with error payload")
		}
		if len(d.Values) != 0 {
			return fmt.Errorf("observe: status panic with values (values are absent on the panic path)")
		}
	case StatusError:
		if d.Error == nil {
			return fmt.Errorf("observe: status error without error payload")
		}
		if d.Panic != nil || len(d.Values) != 0 || len(d.Events) != 0 {
			return fmt.Errorf("observe: status error with observation payloads")
		}
	default:
		return fmt.Errorf("observe: unknown status %q", d.Status)
	}
	for _, v := range d.Values {
		if err := checkValue(v); err != nil {
			return err
		}
	}
	if err := checkPanicInfo(d.Panic); err != nil {
		return err
	}
	if d.Error != nil {
		switch d.Error.Kind {
		case ErrCompile, ErrTimeout, ErrRun, ErrNoObservation, ErrAdapter:
		default:
			return fmt.Errorf("observe: unknown error kind %q", d.Error.Kind)
		}
	}
	for _, e := range d.Events {
		// Event shape: point/defer carry a value and no panic; recovered
		// carries a panic and no value.
		switch e.At {
		case "point", "defer":
			if e.Value == nil {
				return fmt.Errorf("observe: %s event without a value", e.At)
			}
			if e.Panic != nil {
				return fmt.Errorf("observe: %s event with a panic payload", e.At)
			}
			if err := checkValue(*e.Value); err != nil {
				return err
			}
		case "recovered":
			if e.Panic == nil {
				return fmt.Errorf("observe: recovered event without panic payload")
			}
			if e.Value != nil {
				return fmt.Errorf("observe: recovered event with a value payload")
			}
			if err := checkPanicInfo(e.Panic); err != nil {
				return err
			}
		default:
			return fmt.Errorf("observe: unknown event position %q", e.At)
		}
	}
	return nil
}

// checkPanicInfo: the closed panic taxonomy plus a non-empty message —
// every producer maps a concrete runtime panic, which always has prose.
func checkPanicInfo(p *PanicInfo) error {
	if p == nil {
		return nil
	}
	switch p.Kind {
	case PanicDivide, PanicIndexRange, PanicSliceBounds, PanicIfaceConv, PanicNilDeref, PanicOther:
	default:
		return fmt.Errorf("observe: unknown panic kind %q", p.Kind)
	}
	if p.Message == "" {
		return fmt.Errorf("observe: panic %q without a message", p.Kind)
	}
	return nil
}

// checkValue enforces kind-discriminated structure: the fields OF the
// kind, none of the others, a non-empty static type spelling, container
// length consistency, and map-key uniqueness (keys arrive driver-sorted;
// scalar keys are additionally checked strictly increasing, which
// subsumes uniqueness for them).
func checkValue(v Value) error {
	if v.GoType == "" {
		return fmt.Errorf("observe: value kind %q with empty goType", v.Kind)
	}
	scalarOnly := func(kind string) error {
		if len(v.Elems) != 0 || len(v.Fields) != 0 || len(v.Entries) != 0 || v.Payload != nil || v.DynType != "" || v.Len != 0 {
			return fmt.Errorf("observe: scalar kind %q carries container/interface fields", v.Kind)
		}
		// A scalar kind carries ITS payload field and no other (mid-arc
		// review finding 1: cross-contaminated scalars passed the gate
		// and judged as semantic mismatches — the exact fail-open class
		// E1 exists to close, reproduced inside the fix).
		if kind != "bool" && v.Bool {
			return fmt.Errorf("observe: kind %q carries a bool payload", kind)
		}
		if kind != "int" && v.Int != 0 {
			return fmt.Errorf("observe: kind %q carries an int payload", kind)
		}
		if kind != "uint" && v.Uint != 0 {
			return fmt.Errorf("observe: kind %q carries a uint payload", kind)
		}
		if kind != "string" && v.Str != "" {
			return fmt.Errorf("observe: kind %q carries a string payload", kind)
		}
		return nil
	}
	switch v.Kind {
	case "bool", "int", "uint", "string":
		if err := scalarOnly(v.Kind); err != nil {
			return err
		}
	case "array", "slice":
		if err := noScalarPayload(v); err != nil {
			return err
		}
		if v.Len != len(v.Elems) {
			return fmt.Errorf("observe: %s len %d != %d elems", v.Kind, v.Len, len(v.Elems))
		}
		if len(v.Fields) != 0 || len(v.Entries) != 0 || v.Payload != nil || v.DynType != "" {
			return fmt.Errorf("observe: %s kind carries non-element fields", v.Kind)
		}
		for _, e := range v.Elems {
			if err := checkValue(e); err != nil {
				return err
			}
		}
	case "struct":
		if err := noScalarPayload(v); err != nil {
			return err
		}
		if len(v.Elems) != 0 || len(v.Entries) != 0 || v.Payload != nil || v.DynType != "" || v.Len != 0 {
			return fmt.Errorf("observe: struct kind carries non-field payloads")
		}
		for _, f := range v.Fields {
			if f.Name == "" {
				return fmt.Errorf("observe: struct field without a name")
			}
			if err := checkValue(f.Value); err != nil {
				return err
			}
		}
	case "map":
		if err := noScalarPayload(v); err != nil {
			return err
		}
		if v.Len != len(v.Entries) {
			return fmt.Errorf("observe: map len %d != %d entries", v.Len, len(v.Entries))
		}
		if len(v.Elems) != 0 || len(v.Fields) != 0 || v.Payload != nil || v.DynType != "" {
			return fmt.Errorf("observe: map kind carries non-entry fields")
		}
		for i, e := range v.Entries {
			if err := checkValue(e.Key); err != nil {
				return err
			}
			if err := checkValue(e.Value); err != nil {
				return err
			}
			if i > 0 {
				if err := keyOrdered(v.Entries[i-1].Key, e.Key); err != nil {
					return err
				}
			}
		}
	case "interface":
		if err := noScalarPayload(v); err != nil {
			return err
		}
		if v.DynType == "" || v.Payload == nil {
			return fmt.Errorf("observe: interface kind without dynType/payload")
		}
		if len(v.Elems) != 0 || len(v.Fields) != 0 || len(v.Entries) != 0 || v.Len != 0 {
			return fmt.Errorf("observe: interface kind carries container fields")
		}
		if err := checkValue(*v.Payload); err != nil {
			return err
		}
	default:
		return fmt.Errorf("observe: unknown value kind %q", v.Kind)
	}
	return nil
}

// noScalarPayload: container/interface kinds carry no scalar payload
// field at all (finding 1's other half).
func noScalarPayload(v Value) error {
	if v.Bool || v.Int != 0 || v.Uint != 0 || v.Str != "" {
		return fmt.Errorf("observe: kind %q carries scalar payload fields", v.Kind)
	}
	return nil
}

// keyOrdered checks the driver's sorted-keys canonical form for scalar
// keys (strictly increasing by value — which also proves uniqueness).
// Non-scalar keys fall back to a canonical-bytes inequality check.
func keyOrdered(prev, cur Value) error {
	switch {
	case prev.Kind == "int" && cur.Kind == "int":
		if prev.Int >= cur.Int {
			return fmt.Errorf("observe: map keys not strictly increasing (%d, %d)", prev.Int, cur.Int)
		}
	case prev.Kind == "uint" && cur.Kind == "uint":
		if prev.Uint >= cur.Uint {
			return fmt.Errorf("observe: map keys not strictly increasing (%d, %d)", prev.Uint, cur.Uint)
		}
	case prev.Kind == "string" && cur.Kind == "string":
		if prev.Str >= cur.Str {
			return fmt.Errorf("observe: map keys not strictly increasing (%q, %q)", prev.Str, cur.Str)
		}
	default:
		pb, err := json.Marshal(prev)
		if err != nil {
			return err
		}
		cb, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		if bytes.Equal(pb, cb) {
			return fmt.Errorf("observe: duplicate map key")
		}
	}
	return nil
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
//
// Both documents are VALIDATED first (E1): adapters hand back exported
// Documents without a Parse round-trip, so this is the judge's own
// fail-closed gate — an invalid document is an error the caller must
// classify as infrastructure, never a match or mismatch. The policy
// switch is exhaustive: an unknown policy is an error, not silent exact
// comparison.
func Equal(a, b Document, policy PanicPolicy) (bool, error) {
	if err := a.Validate(); err != nil {
		return false, fmt.Errorf("left document: %w", err)
	}
	if err := b.Validate(); err != nil {
		return false, fmt.Errorf("right document: %w", err)
	}
	switch policy {
	case PanicExact:
	case PanicKindOnly:
		a = normalizePanicMessages(a)
		b = normalizePanicMessages(b)
	default:
		return false, fmt.Errorf("observe: unknown panic policy %q", policy)
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
