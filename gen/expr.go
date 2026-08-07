package gen

import (
	"fmt"
	"strings"
)

// value is a generated expression together with the one fact the generator
// must track about it: whether Go will treat it as a CONSTANT.
//
// Go evaluates typed constant expressions at compile time and rejects any
// that overflow their type, so `int8(50) * int8(50)` is a compile ERROR, not
// a wrapping multiply. Every overflow-capable operator therefore requires at
// least one non-constant operand, which turns the same expression into a
// runtime wraparound — defined behaviour in Go, and deterministic.
type value struct {
	text     string
	constant bool
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stringWords is the string-literal alphabet. Deliberately includes the empty
// string and a multi-byte rune: len counts BYTES (len("µ") == 2), a classic
// clone divergence, and concatenation with "" is an identity edge.
var stringWords = []string{``, `go`, `fuzz`, `gros`, `mith`, `µ`, `ab`, `x`}

// literal builds a typed constant of type t, drawn from a range representable
// in every integer kind.
func (g *Generator) literal(t Type) value {
	switch t.Shape {
	case ShapeBool:
		if g.c.chance(2) {
			return value{text: "true", constant: true}
		}
		return value{text: "false", constant: true}
	case ShapeString:
		return value{text: fmt.Sprintf("%q", pick(g.c, stringWords)), constant: true}
	case ShapeArray:
		// A composite literal is never a Go constant, so boundary elements
		// are safe: no compile-time folding can reject them.
		elems := make([]string, t.Len)
		for i := range elems {
			elems[i] = g.literal(*t.Elem).text
		}
		return value{text: fmt.Sprintf("%s{%s}", t.GoName(), strings.Join(elems, ", "))}
	case ShapeStruct:
		// Field-keyed composite; never a constant.
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			parts[i] = f.Name + ": " + g.literal(f.Typ).text
		}
		return value{text: fmt.Sprintf("%s{%s}", t.Name, strings.Join(parts, ", "))}
	case ShapeSlice:
		// Totality (second review: literal() fell through to the int path
		// for these shapes — a latent panic one refactor away). A fresh
		// two-element composite owns its backing.
		return value{text: fmt.Sprintf("%s{%s, %s}", t.GoName(),
			g.literal(*t.Elem).text, g.literal(*t.Elem).text)}
	case ShapeMap:
		return value{text: t.GoName() + "{}"}
	case ShapeInterface:
		info := g.ifaceByName(t.Name)
		return value{text: fmt.Sprintf("%s(%s)", t.Name, g.literal(g.defined[info.source].typ).text)}
	}
	if g.boundaryLiteralDrawn() {
		return g.typedText(t, pick(g.c, t.boundaryLiterals()))
	}
	low, high := t.literalRange()
	return g.intLiteral(t, low+g.c.draw(high-low+1))
}

// boundaryLiteralDrawn is the boundary-vs-normal draw shared by the three
// boundary sites (literals, divisors, shift counts). The weight is the
// corner mechanism: 1 everywhere (a measured minority), cornerBoundaryBias
// on boundary-corner seeds, 0 under Corner "none".
func (g *Generator) boundaryLiteralDrawn() bool {
	return g.c.choose("literal", []arm{
		{name: "normal", weight: 6, ok: true},
		{name: "boundary", weight: g.boundaryBias, ok: g.boundaryBias > 0},
	}).name == "boundary"
}

// typedText wraps a pre-formatted literal text in the type's conversion and
// notes the boundary tag (knowledge-as-data).
func (g *Generator) typedText(t Type, text string) value {
	g.note(tagBoundary)
	if t.Bits == 0 && !t.Unsigned && t.Named == "" {
		return value{text: text, constant: true}
	}
	return value{text: fmt.Sprintf("%s(%s)", t.GoName(), text), constant: true}
}

// nonZeroLiteral is a divisor literal that cannot trap: the zero is excluded
// from the draw itself, not rejected after (no rejection loops). The
// boundary divisor set includes -1 for signed types: Go DEFINES
// MinInt / -1 = MinInt (wrap, no panic) where C has UB — a corner clones
// get wrong.
func (g *Generator) nonZeroLiteral(t Type) value {
	if g.boundaryLiteralDrawn() {
		return g.typedText(t, pick(g.c, t.boundaryDivisors()))
	}
	low, high := t.literalRange()
	n := low + g.c.draw(high-low)
	if n >= 0 {
		n++ // skip zero; [low,-1] stays, [0,high-1] shifts to [1,high]
	}
	return g.intLiteral(t, n)
}

func (g *Generator) intLiteral(t Type, n int) value {
	if t.Bits == 0 && !t.Unsigned && t.Named == "" {
		// Plain `int`: an untyped decimal already has the right default type.
		return value{text: fmt.Sprintf("%d", n), constant: true}
	}
	return value{text: fmt.Sprintf("%s(%d)", t.GoName(), n), constant: true}
}

// variable returns a variable of type t as a value. The one-per-type
// declaration floor makes this total; reads are counted for liveness tiers.
func (g *Generator) variable(t Type) value {
	if i, ok := g.pickVar(t); ok {
		g.vars[i].reads++
		return value{text: g.vars[i].name, constant: false}
	}
	// Unreachable while declare() keeps the one-per-type floor; the literal
	// keeps the leaf total anyway (every type carries a cheap total literal).
	return g.literal(t)
}

// expr builds an expression of exactly type t. Exactness matters: Go has no
// implicit numeric conversion, so a mismatched operand is a compile error.
func (g *Generator) expr(t Type, fuel int) value {
	if fuel <= 0 {
		if g.c.chance(3) {
			return g.literal(t)
		}
		return g.variable(t)
	}
	switch t.Shape {
	case ShapeBool:
		return g.boolExpr(fuel)
	case ShapeString:
		return g.stringExpr(fuel)
	case ShapeArray:
		return g.arrayExpr(t)
	case ShapeStruct:
		return g.structExpr(t)
	case ShapeInterface:
		return g.ifaceExpr(t)
	case ShapeSlice, ShapeMap:
		// No expression arms by design (owned backing / dedicated mutation
		// arms); the leaf is total.
		return g.variable(t)
	}
	return g.intExpr(t, fuel)
}

// ifaceExpr is an interface value: another variable of the same interface
// type, or an implicit conversion from the satisfying concrete type — never
// nil, so any dispatch on the result is panic-free.
func (g *Generator) ifaceExpr(t Type) value {
	var out value
	g.c.choose("iface-expr", []arm{
		{name: "var", weight: 2, ok: true, emit: func() {
			out = g.variable(t)
		}},
		{name: "concrete", weight: 3, ok: true, emit: func() {
			// Any implementer: for an empty interface that means any defined
			// type — mixed dynamic types across program paths, which is what
			// makes assertions and equality discriminating.
			info := g.ifaceByName(t.Name)
			di := pick(g.c, g.implementers(info))
			out = g.variable(g.defined[di].typ)
			g.mark("interfaces")
		}},
	}).emit()
	return out
}

// structExpr is a whole-struct value: variable (copy semantics) or composite.
func (g *Generator) structExpr(t Type) value {
	var out value
	g.c.choose("struct-expr", []arm{
		{name: "var", weight: 3, ok: true, emit: func() {
			out = g.variable(t)
		}},
		{name: "composite", weight: 1, ok: true, emit: func() {
			g.mark("structs")
			out = g.literal(t)
		}},
	}).emit()
	return out
}

// fieldRead emits sv.fN for a struct variable carrying a field of type t.
func (g *Generator) fieldRead(t Type) value {
	src := pick(g.c, g.fieldSources(&t))
	sv := &g.vars[src.varIdx]
	sv.reads++
	g.mark("structs", "field")
	return value{text: sv.name + "." + src.field}
}

// arrayExpr is a whole-array value: a variable (assignment then COPIES — Go
// arrays are values, a semantics clones get wrong) or a composite literal.
func (g *Generator) arrayExpr(t Type) value {
	var out value
	g.c.choose("array-expr", []arm{
		{name: "var", weight: 3, ok: true, emit: func() {
			out = g.variable(t)
		}},
		{name: "composite", weight: 1, ok: true, emit: func() {
			g.mark("arrays")
			out = g.literal(t)
		}},
	}).emit()
	return out
}

// indexExpr draws the index policy for one access into an array or slice
// whose length is guaranteed >= bound — the panic decision made ON PURPOSE
// (trap catalogue: a constant index out of range of a fixed length is a
// COMPILE error for arrays and a certain runtime panic for slices, so the
// constant arm stays under the bound; a variable index panics at runtime,
// drawn as a recorded minority).
func (g *Generator) indexExpr(bound int) string {
	var out string
	g.c.choose("index", []arm{
		{name: "const", weight: 4, ok: true, emit: func() {
			out = fmt.Sprintf("%d", g.c.draw(bound))
		}},
		{name: "mod", weight: 3, ok: g.enabled("conversions", "modulo"), emit: func() {
			// Unsigned % bound is always in range — a safe NON-CONSTANT
			// index (signed % can be negative and would panic).
			u := g.variable(Int(8, true))
			g.mark("conversions", "modulo")
			out = fmt.Sprintf("int(%s%%%d)", u.text, bound)
		}},
		{name: "panicky", weight: 1 + 3*boolToInt(g.guardBias), ok: g.riskOK(), emit: func() {
			// A raw int variable: usually out of range, so usually a
			// deterministic index panic. Deliberate, tagged content —
			// and it spends the statement's one panic-risk slot.
			g.spendRisk()
			v := g.variable(Int(0, false))
			g.note(tagPanicRisk)
			out = v.text
		}},
	}).emit()
	return out
}

// nonConstExpr builds an expression of type t that is guaranteed
// non-constant, which is what makes an overflow-capable operator safe to
// emit. In the subject the variable leaf is total (one-per-type floor); in a
// helper body there is no floor, so the fallback converts the guaranteed
// plain-int first parameter — a conversion of a variable is non-constant for
// every integer target.
func (g *Generator) nonConstExpr(t Type, fuel int) value {
	if e := g.expr(t, fuel); !e.constant {
		return e
	}
	if g.pureMode {
		// The environment's guaranteed int-ish variable: a helper's p0 or a
		// method's receiver. A conversion of a variable is non-constant.
		return value{text: fmt.Sprintf("%s(%s)", t.GoName(), g.pureBase)}
	}
	return g.variable(t)
}

// markWidthDep records that an operation of type t can yield different values
// on 32- and 64-bit targets. Conservative over VALUES but exact over
// operations: wrap-capable arithmetic, unsigned complement, and conversions
// TO a platform-width type qualify; division, right shift, comparisons, and
// min/max are value-preserving or magnitude-bounded and do not.
func (g *Generator) markWidthDep(t Type) {
	if t.Shape == ShapeInt && t.Bits == 0 {
		g.note(tagWidthDependent)
	}
}

func (g *Generator) intExpr(t Type, fuel int) value {
	var out value
	g.c.choose("int-expr", []arm{
		{name: "leaf", weight: 2, ok: true, emit: func() {
			out = g.expr(t, 0)
		}},
		{name: "arith", weight: 3, ok: true, emit: func() {
			// Wrapping arithmetic. One non-constant operand keeps it out of
			// compile-time constant evaluation.
			op := pick(g.c, []string{"+", "-", "*"})
			left := g.nonConstExpr(t, fuel-1)
			right := g.expr(t, fuel-1)
			g.mark("ints")
			g.markWidthDep(t)
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "division", weight: 2, ok: g.enabled("division"), emit: func() {
			out = g.divModExpr(t, fuel, "/", "division")
		}},
		{name: "modulo", weight: 2, ok: g.enabled("modulo"), emit: func() {
			out = g.divModExpr(t, fuel, "%%", "modulo")
		}},
		{name: "bitwise", weight: 2, ok: g.enabled("bitwise"), emit: func() {
			op := pick(g.c, []string{"&", "|", "^", "&^"})
			left := g.nonConstExpr(t, fuel-1)
			right := g.expr(t, fuel-1)
			g.mark("bitwise")
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "shift", weight: 2, ok: g.enabled("shifts"), emit: func() {
			// The shift count is an untyped constant in [0,7]: a negative
			// count is a compile error and a huge one merely zeroes, so this
			// keeps shifts total without special cases.
			left := g.nonConstExpr(t, fuel-1)
			g.mark("shifts")
			op := pick(g.c, []string{"<<", ">>"})
			if op == "<<" {
				// Left shift wraps at the width; right shift only shrinks.
				g.markWidthDep(t)
			}
			count := g.c.draw(8)
			if g.boundaryLiteralDrawn() {
				// Shift AT and PAST the width: legal for a non-constant left
				// operand (the result is 0 / sign-fill), and a classic clone
				// divergence. Platform width uses 32: legal-and-edge on both
				// conformance targets.
				w := t.Bits
				if w == 0 {
					w = 32
				}
				count = w - 1 + g.c.draw(3)
				g.note(tagBoundary)
			}
			out = value{text: fmt.Sprintf("(%s %s %d)", left.text, op, count)}
		}},
		{name: "conversion", weight: 2, ok: g.enabled("conversions"), emit: func() {
			// The operand must be non-constant, and more sharply than for
			// arithmetic: converting a CONSTANT the target cannot represent
			// is a compile error, so a signed literal reaching `uint16(-10)`
			// would fail to build.
			fromPool := intTypes()
			for _, dt := range g.defined {
				// Named-to-unnamed and cross-named conversions: the
				// defined-type identity surface.
				fromPool = append(fromPool, dt.typ)
			}
			from := pick(g.c, fromPool)
			g.mark("conversions")
			// Converting INTO a platform-width type cuts at that width:
			// uint(-1) is 2^32-1 or 2^64-1 depending on the target.
			g.markWidthDep(t)
			inner := g.nonConstExpr(from, fuel-1)
			out = value{text: fmt.Sprintf("%s(%s)", t.GoName(), inner.text)}
		}},
		{name: "index", weight: 2, ok: g.enabled("arrays", "index") && g.hasArrayOfElem(t), emit: func() {
			i := g.pickArrayOfElem(t)
			arr := &g.vars[i]
			arr.reads++
			g.mark("arrays", "index")
			out = value{text: fmt.Sprintf("%s[%s]", arr.name, g.indexExpr(arr.typ.Len))}
		}},
		{name: "slice-index", weight: 2, ok: g.enabled("slices", "index") && len(g.sliceVars(&t)) > 0, emit: func() {
			s := &g.vars[pick(g.c, g.sliceVars(&t))]
			s.reads++
			g.mark("slices", "index")
			out = value{text: fmt.Sprintf("%s[%s]", s.name, g.indexExpr(s.minLen))}
		}},
		{name: "slice-len", weight: 1,
			ok: t.Equal(Int(0, false)) && g.enabled("len", "slices") && len(g.sliceVars(nil)) > 0, emit: func() {
				s := &g.vars[pick(g.c, g.sliceVars(nil))]
				s.reads++
				g.mark("len", "slices")
				out = value{text: fmt.Sprintf("len(%s)", s.name)}
			}},
		{name: "map-read", weight: 2, ok: g.enabled("maps") && len(g.mapVars(&t)) > 0, emit: func() {
			m := &g.vars[pick(g.c, g.mapVars(&t))]
			m.reads++
			g.mark("maps")
			// A missing key yields the zero value — deterministic, no panic.
			out = value{text: fmt.Sprintf("%s[%s]", m.name, pick(g.c, m.keys))}
		}},
		{name: "map-len", weight: 1,
			ok: t.Equal(Int(0, false)) && g.enabled("len", "maps") && len(g.mapVars(nil)) > 0, emit: func() {
				m := &g.vars[pick(g.c, g.mapVars(nil))]
				m.reads++
				g.mark("len", "maps")
				out = value{text: fmt.Sprintf("len(%s)", m.name)}
			}},
		{name: "field", weight: 2, ok: g.enabled("structs", "field") && len(g.fieldSources(&t)) > 0, emit: func() {
			out = g.fieldRead(t)
		}},
		{name: "call", weight: 2, ok: g.enabled("helpers") && len(g.singleResultHelpers(t)) > 0, emit: func() {
			// Helper calls are pure and panic-free, so a call composes into
			// any expression without effect or panic-identity hazards.
			h := g.helpers[pick(g.c, g.singleResultHelpers(t))]
			g.mark("helpers")
			out = value{text: fmt.Sprintf("%s(%s)", h.name, g.callArgs(h, fuel-1))}
		}},
		{name: "dispatch", weight: 2, ok: g.enabled("interfaces", "methods") && len(g.dispatchSites(t)) > 0, emit: func() {
			// Dynamic dispatch through a never-nil interface over a pure
			// method: effect-free, panic-free, deterministic.
			site := pick(g.c, g.dispatchSites(t))
			iv := &g.vars[site.varIdx]
			iv.reads++
			m := g.defined[site.di].methods[site.mi]
			g.mark("interfaces", "methods")
			out = value{text: fmt.Sprintf("%s.%s(%s)", iv.name, m.name, g.argList(m.params, fuel-1))}
		}},
		{name: "assert", weight: 1,
			// One-result assertion to type t, from an interface var that t
			// legally implements (a derived interface admits only its
			// source; an empty one admits every defined type). Derived-
			// source assertions always succeed; empty-interface assertions
			// may panic ("interface conversion") depending on the dynamic
			// type, so they spend the risk budget.
			ok: t.Named != "" && g.enabled("interfaces", "assertion") && len(g.assertSources(t)) > 0, emit: func() {
				cands := g.assertSources(t)
				iv := &g.vars[pick(g.c, cands)]
				info := g.ifaceByName(iv.typ.Name)
				if len(info.methods) == 0 {
					if !g.riskOK() {
						out = g.expr(t, 0)
						return
					}
					g.spendRisk()
					g.note(tagPanicRisk)
				}
				iv.reads++
				g.mark("interfaces", "assertion")
				out = value{text: fmt.Sprintf("%s.(%s)", iv.name, t.Named)}
			}},
		{name: "method", weight: 2, ok: g.enabled("methods") && len(g.methodsWithResult(t)) > 0, emit: func() {
			// Same purity story as helpers; the receiver is a variable of
			// the defined type, or its literal when the environment lacks
			// one (T0(5).m0(...) is legal Go).
			mr := pick(g.c, g.methodsWithResult(t))
			dt := g.defined[mr.di]
			m := dt.methods[mr.mi]
			recv := g.variable(dt.typ)
			g.mark("methods")
			out = value{text: fmt.Sprintf("%s.%s(%s)", recv.text, m.name, g.argList(m.params, fuel-1))}
		}},
		{name: "len", weight: 1,
			// len returns exactly `int`. len of a string VARIABLE is
			// non-constant, but variable() can fall back to a string
			// LITERAL (pure helper bodies have no string in scope), and
			// len("µ") is a typed CONSTANT — the constness must follow the
			// operand or a downstream shift chain overflows at compile
			// time on 32-bit targets (latent until the bare-call arm
			// shifted draw paths; seed 36 in the 386 witness).
			ok: t.Equal(Int(0, false)) && g.enabled("len", "strings"), emit: func() {
				g.mark("len", "strings")
				s := g.variable(Str())
				out = value{text: fmt.Sprintf("len(%s)", s.text), constant: s.constant}
			}},
		{name: "minmax", weight: 1, ok: g.enabled("min") || g.enabled("max"), emit: func() {
			name := "min"
			if !g.enabled("min") || (g.enabled("max") && g.c.chance(2)) {
				name = "max"
			}
			left := g.nonConstExpr(t, fuel-1)
			right := g.expr(t, fuel-1)
			g.mark(name)
			out = value{text: fmt.Sprintf("%s(%s, %s)", name, left.text, right.text)}
		}},
		{name: "unary", weight: 1, ok: true, emit: func() {
			// Complement and negation both require a non-constant operand:
			// `-uint8(60)` is a constant overflow, hence an error.
			operand := g.nonConstExpr(t, fuel-1)
			if g.enabled("bitwise") && (t.Unsigned || g.c.chance(2)) {
				g.mark("bitwise")
				if t.Unsigned {
					// Unsigned complement is 2^W-1-x: width-dependent for any
					// operand. Signed complement is -x-1: width-independent.
					g.markWidthDep(t)
				}
				out = value{text: fmt.Sprintf("(^%s)", operand.text)}
				return
			}
			g.mark("ints")
			g.markWidthDep(t)
			out = value{text: fmt.Sprintf("(-%s)", operand.text)}
		}},
	}).emit()
	return out
}

// divModExpr emits division or modulo. A literal divisor keeps it on the
// ok-path; a variable divisor is a deliberate, deterministic panic-risk path
// (the program panics on every run or on none), drawn as a minority and
// recorded as knowledge.
func (g *Generator) divModExpr(t Type, fuel int, op, tag string) value {
	left := g.nonConstExpr(t, fuel-1)
	divisor := g.nonZeroLiteral(t)
	variableDivisor := false
	hotChance := 8
	if g.guardBias {
		hotChance = 2
	}
	if g.riskOK() && g.c.chance(hotChance) && g.spendRisk() {
		divisor = g.variable(t)
		variableDivisor = true
		g.note(tagPanicRisk)
	}
	g.mark(tag)
	// Division of platform-width signed int is width-dependent when the
	// divisor can be -1: MinInt32 / -1 wraps to MinInt32 on a 32-bit target
	// but is +2^31 on a 64-bit one (review finding: this was the untagged
	// divergence that would have broken the discrimination proof). Modulo
	// stays value-preserving (MinInt % -1 is 0 at every width).
	if op == "/" && (variableDivisor || divisor.text == "-1") {
		g.markWidthDep(t)
	}
	return value{text: fmt.Sprintf("(%s "+op+" %s)", left.text, divisor.text)}
}

// stringExpr builds a string expression under the LINEAR GROWTH rule: a
// concat chain contains AT MOST ONE variable occurrence; every other operand
// is a literal. Any assignment then grows the longest string by at most a
// constant, so total string memory is linear in executed statements — the
// halts-by-construction property extended to space. (`s = s + s` in a loop
// doubles per iteration; the one-variable rule makes that inexpressible.)
func (g *Generator) stringExpr(fuel int) value {
	if fuel <= 0 || !g.enabled("concat") {
		if g.c.chance(3) {
			return g.literal(Str())
		}
		return g.variable(Str())
	}
	var out value
	g.c.choose("string-expr", []arm{
		{name: "leaf", weight: 2, ok: true, emit: func() {
			out = g.expr(Str(), 0)
		}},
		{name: "concat", weight: 3, ok: g.enabled("strings", "concat"), emit: func() {
			operands := 2 + g.c.draw(2)
			// varPos == operands means an all-literal (constant) chain —
			// constant string concatenation cannot overflow, so it is safe.
			varPos := g.c.draw(operands + 1)
			parts := make([]string, operands)
			// The variable slot can itself fall back to a literal (pure
			// helper bodies have no string in scope), making the whole
			// chain constant — constness must follow the actual operand,
			// the same class as the len fix (audit finding 6, latent).
			chainConst := true
			for i := range parts {
				if i == varPos {
					v := g.variable(Str())
					parts[i] = v.text
					chainConst = chainConst && v.constant
				} else {
					parts[i] = g.literal(Str()).text
				}
			}
			g.mark("strings", "concat")
			out = value{
				text:     "(" + strings.Join(parts, " + ") + ")",
				constant: varPos == operands || chainConst,
			}
		}},
	}).emit()
	return out
}

// unSelf replaces an identical-to-left right operand with a literal — a
// self-compare is constant-in-effect (second review: 56 in 1300 programs).
// A discarded bare-variable read is un-counted; identical COMPLEX operands
// keep their reads, which remain real via the left copy.
func (g *Generator) unSelf(left, right value, t Type) value {
	if left.text != right.text {
		return right
	}
	for i := range g.vars {
		if g.vars[i].name == right.text {
			g.vars[i].reads--
			break
		}
	}
	return g.literal(t)
}

func (g *Generator) boolExpr(fuel int) value {
	if fuel <= 0 {
		return g.variable(Bool())
	}
	var out value
	g.c.choose("bool-expr", []arm{
		{name: "leaf", weight: 1, ok: true, emit: func() {
			out = g.variable(Bool())
		}},
		{name: "compare", weight: 3, ok: g.enabled("comparisons"), emit: func() {
			// Comparison operands must share a type, so one is drawn and reused.
			t := pick(g.c, intTypes())
			op := pick(g.c, []string{"<", "<=", ">", ">="})
			g.mark("comparisons")
			left := g.expr(t, fuel-1)
			right := g.unSelf(left, g.expr(t, fuel-1), t)
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "equality", weight: 2, ok: g.enabled("equality"), emit: func() {
			t := pick(g.c, intTypes())
			op := "=="
			if g.c.chance(2) {
				op = "!="
			}
			g.mark("equality")
			left := g.expr(t, fuel-1)
			right := g.unSelf(left, g.expr(t, fuel-1), t)
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "field", weight: 1, ok: g.enabled("structs", "field") && len(g.fieldSources(&Type{Shape: ShapeBool})) > 0, emit: func() {
			out = g.fieldRead(Bool())
		}},
		{name: "map-read", weight: 1, ok: g.enabled("maps") && len(g.mapVars(&Type{Shape: ShapeBool})) > 0, emit: func() {
			m := &g.vars[pick(g.c, g.mapVars(&Type{Shape: ShapeBool}))]
			m.reads++
			g.mark("maps")
			out = value{text: fmt.Sprintf("%s[%s]", m.name, pick(g.c, m.keys))}
		}},
		{name: "call", weight: 1, ok: g.enabled("helpers") && len(g.singleResultHelpers(Bool())) > 0, emit: func() {
			h := g.helpers[pick(g.c, g.singleResultHelpers(Bool()))]
			g.mark("helpers")
			out = value{text: fmt.Sprintf("%s(%s)", h.name, g.callArgs(h, fuel-1))}
		}},
		{name: "iface-equal", weight: 1, ok: g.enabled("interfaces", "equality") && len(g.ifaceVars()) > 0, emit: func() {
			// Interface equality: dynamic types are comparable ints, so no
			// comparison panic; a nil comparand is legal and static.
			iv := &g.vars[pick(g.c, g.ifaceVars())]
			iv.reads++
			op := "=="
			if g.c.chance(2) {
				op = "!="
			}
			rhs := "nil"
			var others []int
			for _, vi := range g.varsOfShape(ShapeInterface, nil) {
				if g.vars[vi].name != iv.name && g.vars[vi].typ.Equal(iv.typ) {
					others = append(others, vi)
				}
			}
			if len(others) > 0 && g.c.chance(2) {
				ov := &g.vars[pick(g.c, others)]
				ov.reads++
				rhs = ov.name
			}
			g.mark("interfaces", "equality")
			out = value{text: fmt.Sprintf("(%s %s %s)", iv.name, op, rhs)}
		}},
		{name: "struct-equal", weight: 1, ok: g.enabled("structs", "equality") && len(g.structVars()) > 0, emit: func() {
			// Whole-struct comparison: our structs' fields are all
			// comparable types, so == is legal — Go's comparability rules
			// are themselves clone-divergence territory.
			i := pick(g.c, g.structVars())
			sv := &g.vars[i]
			sv.reads++
			op := "=="
			if g.c.chance(2) {
				op = "!="
			}
			g.mark("structs", "equality")
			rhs := g.structExpr(sv.typ)
			// s == s is constant-in-effect; compare against a composite
			// instead (review finding: identity self-compares). Un-count
			// the discarded bare-variable read.
			if rhs.text == sv.name {
				sv.reads--
				rhs = g.literal(sv.typ)
			}
			out = value{text: fmt.Sprintf("(%s %s %s)", sv.name, op, rhs.text)}
		}},
		{name: "str-compare", weight: 1, ok: g.enabled("strings", "comparisons"), emit: func() {
			// Strings order lexically by bytes — a comparison edge clones
			// get wrong around multi-byte runes.
			op := pick(g.c, []string{"<", "<=", ">", ">="})
			g.mark("strings", "comparisons")
			left := g.stringExpr(fuel - 1)
			right := g.unSelf(left, g.stringExpr(fuel-1), Str())
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "str-equal", weight: 1, ok: g.enabled("strings", "equality"), emit: func() {
			op := "=="
			if g.c.chance(2) {
				op = "!="
			}
			g.mark("strings", "equality")
			left := g.stringExpr(fuel - 1)
			right := g.unSelf(left, g.stringExpr(fuel-1), Str())
			out = value{text: fmt.Sprintf("(%s %s %s)", left.text, op, right.text)}
		}},
		{name: "logic", weight: 2, ok: true, emit: func() {
			op := "&&"
			if g.c.chance(2) {
				op = "||"
			}
			g.mark("bools")
			out = value{text: fmt.Sprintf("(%s %s %s)",
				g.boolExpr(fuel-1).text, op, g.boolExpr(fuel-1).text)}
		}},
		{name: "not", weight: 1, ok: true, emit: func() {
			g.mark("bools")
			out = value{text: fmt.Sprintf("(!%s)", g.boolExpr(fuel-1).text)}
		}},
	}).emit()
	return out
}
