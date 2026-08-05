package gen

import "fmt"

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

// literal builds a typed constant of type t, drawn from a range representable
// in every integer kind.
func (g *Generator) literal(t Type) value {
	if t.Shape == ShapeBool {
		if g.c.chance(2) {
			return value{text: "true", constant: true}
		}
		return value{text: "false", constant: true}
	}
	low, high := t.literalRange()
	return g.intLiteral(t, low+g.c.draw(high-low+1))
}

// nonZeroLiteral is a divisor literal that cannot trap: the zero is excluded
// from the draw itself, not rejected after (no rejection loops).
func (g *Generator) nonZeroLiteral(t Type) value {
	low, high := t.literalRange()
	n := low + g.c.draw(high-low)
	if n >= 0 {
		n++ // skip zero; [low,-1] stays, [0,high-1] shifts to [1,high]
	}
	return g.intLiteral(t, n)
}

func (g *Generator) intLiteral(t Type, n int) value {
	if t.Bits == 0 && !t.Unsigned {
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
	if t.Shape == ShapeBool {
		return g.boolExpr(fuel)
	}
	return g.intExpr(t, fuel)
}

// nonConstExpr builds an expression of type t that is guaranteed
// non-constant, which is what makes an overflow-capable operator safe to
// emit. The variable leaf is total (one-per-type floor), so no retry.
func (g *Generator) nonConstExpr(t Type, fuel int) value {
	if e := g.expr(t, fuel); !e.constant {
		return e
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
			out = value{text: fmt.Sprintf("(%s %s %d)", left.text, op, g.c.draw(8))}
		}},
		{name: "conversion", weight: 2, ok: g.enabled("conversions"), emit: func() {
			// The operand must be non-constant, and more sharply than for
			// arithmetic: converting a CONSTANT the target cannot represent
			// is a compile error, so a signed literal reaching `uint16(-10)`
			// would fail to build.
			from := pick(g.c, intTypes())
			g.mark("conversions")
			// Converting INTO a platform-width type cuts at that width:
			// uint(-1) is 2^32-1 or 2^64-1 depending on the target.
			g.markWidthDep(t)
			inner := g.nonConstExpr(from, fuel-1)
			out = value{text: fmt.Sprintf("%s(%s)", t.GoName(), inner.text)}
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
	if g.c.chance(8) {
		divisor = g.variable(t)
		g.note(tagPanicRisk)
	}
	g.mark(tag)
	return value{text: fmt.Sprintf("(%s "+op+" %s)", left.text, divisor.text)}
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
			out = value{text: fmt.Sprintf("(%s %s %s)",
				g.expr(t, fuel-1).text, op, g.expr(t, fuel-1).text)}
		}},
		{name: "equality", weight: 2, ok: g.enabled("equality"), emit: func() {
			t := pick(g.c, intTypes())
			op := "=="
			if g.c.chance(2) {
				op = "!="
			}
			g.mark("equality")
			out = value{text: fmt.Sprintf("(%s %s %s)",
				g.expr(t, fuel-1).text, op, g.expr(t, fuel-1).text)}
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
