// Package gen generates small, valid, outcome-deterministic Go programs.
//
// Salvaged from grossmith-proto (deps/grossmith-proto, frozen 2026-08-05,
// ours, Apache-2.0) per its LESSONS.md salvage map, rebuilt to BRIEF.md:
// construct gating as a plain map, all randomness through a tape-recording
// choice primitive, liveness tiers, dead code as weighted content.
//
// Three properties hold by construction, never by filtering (BRIEF
// "Legality vs. weights"):
//
//   - COMPILES: the trap catalogue (LESSONS.md §3) — constant-overflow
//     tracking, no unused anything, distinct case labels.
//   - HALTS: literal loop bounds, +1 steps, index never assigned in the body.
//   - OUTCOME-DETERMINISTIC: no unordered iteration, side effects in
//     statement position only, panic sites decided on purpose.
package gen

import "fmt"

// Shape is the top-level classification of a generated type.
type Shape uint8

const (
	// ShapeInt is any of Go's fixed integer kinds, signed or unsigned.
	ShapeInt Shape = iota
	// ShapeBool is bool.
	ShapeBool
	// ShapeString is string.
	ShapeString
)

// Type is one generated Go type. A single concrete struct rather than an
// interface per kind: the type space is small and closed, and a closed enum
// makes exhaustive switches checkable.
type Type struct {
	Shape Shape
	// Bits is 0 for platform-width `int`/`uint`, else 8, 16, 32, or 64.
	Bits int
	// Unsigned distinguishes uintN from intN.
	Unsigned bool
}

// Bool is the bool type.
func Bool() Type { return Type{Shape: ShapeBool} }

// Str is the string type.
func Str() Type { return Type{Shape: ShapeString} }

// Int builds an integer type. bits 0 means platform width.
func Int(bits int, unsigned bool) Type {
	return Type{Shape: ShapeInt, Bits: bits, Unsigned: unsigned}
}

// intTypes is every fixed integer kind.
func intTypes() []Type {
	return []Type{
		Int(0, false), Int(8, false), Int(16, false), Int(32, false), Int(64, false),
		Int(0, true), Int(8, true), Int(16, true), Int(32, true), Int(64, true),
	}
}

// scalarTypes is the MVP type pool: every fixed integer kind plus bool.
func scalarTypes() []Type {
	return append(intTypes(), Bool())
}

// GoName is the type's Go spelling.
func (t Type) GoName() string {
	if t.Shape == ShapeBool {
		return "bool"
	}
	if t.Shape == ShapeString {
		return "string"
	}
	stem := "int"
	if t.Unsigned {
		stem = "uint"
	}
	if t.Bits == 0 {
		return stem
	}
	return fmt.Sprintf("%s%d", stem, t.Bits)
}

// Tags are the construct tags a value of this type implies.
func (t Type) Tags() []string {
	if t.Shape == ShapeBool {
		return []string{"bools"}
	}
	if t.Shape == ShapeString {
		return []string{"strings"}
	}
	if t.Bits == 0 && !t.Unsigned {
		return []string{"ints"}
	}
	// A sized or unsigned kind is what `widths` records.
	return []string{"ints", "widths"}
}

// literalRange is the closed interval integer literals of this type are drawn
// from. Every bound is representable in the NARROWEST kind (int8/uint8), so a
// literal can never overflow its type — an unrepresentable constant is a
// compile error, and the charter wants the non-compile rate near zero.
//
// This is also a REACHABILITY limit: confining literals to [-60,60] makes
// type boundaries (MinInt8, MaxUint64) unreachable, and width defects live
// exactly there. Boundary pursuit is the first named corner on the growth
// ladder (BRIEF.md).
func (t Type) literalRange() (low, high int) {
	if t.Unsigned {
		return 0, 60
	}
	return -60, 60
}

// boundaryLiterals is the type's boundary-value alphabet as decimal literal
// texts: MIN/MAX, their ±1 neighbours, the sign-bit value, and an alternating
// bit pattern — where width defects live. Every value is representable in
// the type (an unrepresentable constant is a compile error).
//
// Platform-width int/uint uses the 32-BIT set: the same literal must compile
// at every GOARCH the conformance run targets (int(1<<63-1) is a compile
// error on 386), and 32-bit boundaries remain genuine boundaries on the
// narrower target while being ordinary large values on the wider one — the
// cross-width divergence surface itself.
func (t Type) boundaryLiterals() []string {
	if t.Shape != ShapeInt {
		return nil
	}
	bits := t.Bits
	if bits == 0 {
		bits = 32
	}
	if t.Unsigned {
		max := ^uint64(0) >> (64 - bits)
		return []string{
			fmt.Sprintf("%d", max),
			fmt.Sprintf("%d", max-1),
			fmt.Sprintf("%d", uint64(1)<<(bits-1)),           // sign bit alone
			fmt.Sprintf("%d", (alternating(bits)<<1)&max),    // 0xAA…
		}
	}
	max := int64(^uint64(0) >> (65 - bits))
	min := -max - 1
	return []string{
		fmt.Sprintf("%d", min),
		fmt.Sprintf("%d", min+1),
		"-1",
		fmt.Sprintf("%d", max-1),
		fmt.Sprintf("%d", max),
		fmt.Sprintf("%d", int64(alternating(bits))), // 0x55…: sign bit clear
	}
}

// alternating is the 0101… pattern filling the low bits (0x55, 0x5555, …).
func alternating(bits int) uint64 {
	return 0x5555555555555555 >> (64 - bits)
}

// boundaryDivisors are boundary values legal as divisors (nonzero). The
// signed set includes -1: Go DEFINES MinInt / -1 as MinInt (two's-complement
// wrap, no panic) where C has UB — exactly the corner a clone gets wrong.
func (t Type) boundaryDivisors() []string {
	if t.Shape != ShapeInt {
		return nil
	}
	b := t.boundaryLiterals()
	if t.Unsigned {
		return b[:2] // max, max-1
	}
	return []string{"-1", b[0], b[4]} // -1, min, max
}

// Equal reports type identity.
func (t Type) Equal(other Type) bool {
	return t.Shape == other.Shape && t.Bits == other.Bits && t.Unsigned == other.Unsigned
}
