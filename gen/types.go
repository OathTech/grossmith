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

import (
	"fmt"
	"strings"
)

// Shape is the top-level classification of a generated type.
type Shape uint8

const (
	// ShapeInt is any of Go's fixed integer kinds, signed or unsigned.
	ShapeInt Shape = iota
	// ShapeBool is bool.
	ShapeBool
	// ShapeString is string.
	ShapeString
	// ShapeArray is a fixed-length array of a scalar or string element.
	ShapeArray
	// ShapeStruct is a NAMED struct type with scalar/string fields, declared
	// in the program preamble. Identity is the name, per Go's named-type
	// rules.
	ShapeStruct
	// ShapeMap is a map from a small scalar/string key type to a
	// scalar/string element. Everything about our maps is deterministic
	// EXCEPT iteration order — so range over a map is never generated (the
	// commutative-fold quotient is a future option, recorded in BRIEF).
	// Maps are never nil (born from literals), so writes cannot panic;
	// reads of missing keys yield zero values, deterministically.
	ShapeMap
	// ShapeSlice is a slice of a scalar or string element. Slices carry two
	// spec-nondeterminism traps handled by construction: cap after append is
	// unspecified (cap is NEVER observed) and whether append reallocates is
	// unspecified, so an aliased-then-appended slice has unspecified write
	// visibility (whole-slice assignment is NEVER generated — every slice
	// owns its backing).
	ShapeSlice
)

// StructField is one field of a generated struct type.
type StructField struct {
	Name string
	Typ  Type
}

// Type is one generated Go type. A single concrete struct rather than an
// interface per kind: the type space is small and closed, and a closed enum
// makes exhaustive switches checkable.
type Type struct {
	Shape Shape
	// Bits is 0 for platform-width `int`/`uint`, else 8, 16, 32, or 64.
	Bits int
	// Unsigned distinguishes uintN from intN.
	Unsigned bool
	// Elem and Len describe ShapeArray. Len is small and fixed: termination
	// of range loops and the size of element-wise observation both ride on
	// it being a literal known at generation.
	Elem *Type
	Len  int
	// Name and Fields describe ShapeStruct.
	Name   string
	Fields []StructField
	// Key describes ShapeMap (Elem is shared with arrays/slices).
	Key *Type
	// Named, when set on an integer shape, makes this a DEFINED type
	// (`type T0 int16`): same operators as the underlying kind, but a
	// DISTINCT identity — no assignability to or from the underlying type
	// without conversion. Exactly the identity rules clones get wrong.
	Named string
}

// Bool is the bool type.
func Bool() Type { return Type{Shape: ShapeBool} }

// Str is the string type.
func Str() Type { return Type{Shape: ShapeString} }

// Array builds a fixed-length array type over elem.
func Array(elem Type, n int) Type {
	e := elem
	return Type{Shape: ShapeArray, Elem: &e, Len: n}
}

// Slice builds a slice type over elem.
func Slice(elem Type) Type {
	e := elem
	return Type{Shape: ShapeSlice, Elem: &e}
}

// Map builds a map type from key to elem.
func Map(key, elem Type) Type {
	k, e := key, elem
	return Type{Shape: ShapeMap, Key: &k, Elem: &e}
}

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
	if t.Named != "" {
		return t.Named
	}
	if t.Shape == ShapeBool {
		return "bool"
	}
	if t.Shape == ShapeString {
		return "string"
	}
	if t.Shape == ShapeArray {
		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.GoName())
	}
	if t.Shape == ShapeStruct {
		return t.Name
	}
	if t.Shape == ShapeSlice {
		return "[]" + t.Elem.GoName()
	}
	if t.Shape == ShapeMap {
		return fmt.Sprintf("map[%s]%s", t.Key.GoName(), t.Elem.GoName())
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
	if t.Shape == ShapeArray {
		return append([]string{"arrays"}, t.Elem.Tags()...)
	}
	if t.Shape == ShapeStruct {
		tags := []string{"structs"}
		for _, f := range t.Fields {
			tags = append(tags, f.Typ.Tags()...)
		}
		return tags
	}
	if t.Shape == ShapeSlice {
		return append([]string{"slices"}, t.Elem.Tags()...)
	}
	if t.Shape == ShapeMap {
		return append(append([]string{"maps"}, t.Key.Tags()...), t.Elem.Tags()...)
	}
	tags := []string{"ints"}
	if t.Named != "" {
		tags = append(tags, "defined_types")
	}
	if t.Bits != 0 || t.Unsigned {
		// A sized or unsigned kind is what `widths` records.
		tags = append(tags, "widths")
	}
	return tags
}

// underlyingName is the spelling of a defined type's underlying kind — used
// by the driver to observe named values through an explicit conversion.
func (t Type) underlyingName() string {
	u := t
	u.Named = ""
	return u.GoName()
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

// Equal reports type identity — STRUCTURAL, never pointer identity (the
// gosmith lesson: pointer-identity types foreclose assignability).
func (t Type) Equal(other Type) bool {
	if t.Shape != other.Shape || t.Bits != other.Bits || t.Unsigned != other.Unsigned {
		return false
	}
	if t.Shape == ShapeArray {
		return t.Len == other.Len && t.Elem.Equal(*other.Elem)
	}
	if t.Shape == ShapeSlice {
		return t.Elem.Equal(*other.Elem)
	}
	if t.Shape == ShapeMap {
		return t.Key.Equal(*other.Key) && t.Elem.Equal(*other.Elem)
	}
	if t.Shape == ShapeStruct {
		// Named types: the name IS the identity (fields cannot differ under
		// one name — decl() is the single source).
		return t.Name == other.Name
	}
	// Defined types are distinct from their underlying kind: T0 != int16.
	return t.Named == other.Named
}

// decl is the preamble type declaration for a named struct type.
func (t Type) decl() string {
	var b strings.Builder
	fmt.Fprintf(&b, "type %s struct {\n", t.Name)
	for _, f := range t.Fields {
		fmt.Fprintf(&b, "\t%s %s\n", f.Name, f.Typ.GoName())
	}
	b.WriteString("}\n\n")
	return b.String()
}
