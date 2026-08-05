package gen

import (
	"fmt"
	"strings"
)

// stmt emits one statement. depth is the remaining control-flow nesting
// allowance; inLoop gates break/continue (continue outside a loop is a
// compile error); last reports whether this is the final statement of its
// block.
//
// Terminal statements (break/continue) are legal anywhere in a block, but
// everything after one is dead. Per BRIEF ("Legality vs. weights") that is
// weighted content, not banned: heavily biased to last position, emitted
// mid-block as a deliberate minority and recorded as dead_code so
// measurements can stratify.
func (g *Generator) stmt(out *emitter, depth int) { g.stmtIn(out, depth, false, false) }

func (g *Generator) stmtIn(out *emitter, depth int, inLoop, last bool) {
	terminalWeight := 3
	if !last {
		terminalWeight = 1
	}
	terminal := func(kw string) func() {
		return func() {
			g.mark(kw, "control_flow")
			if !last {
				g.note(tagDeadCode)
			}
			out.line("%s", kw)
		}
	}
	g.c.choose("stmt", []arm{
		{name: "assign", weight: 4, ok: true, emit: func() { g.assign(out) }},
		{name: "compound", weight: 2, ok: true, emit: func() { g.compoundAssign(out) }},
		{name: "incdec", weight: 2, ok: true, emit: func() { g.incDec(out) }},
		{name: "if", weight: 3, ok: g.enabled("if") && depth > 0,
			emit: func() { g.ifStmt(out, depth, inLoop) }},
		{name: "for", weight: 3, ok: g.enabled("loops") && depth > 0,
			emit: func() { g.forStmt(out, depth) }},
		{name: "switch", weight: 2, ok: g.enabled("switch") && depth > 0,
			emit: func() { g.switchStmt(out, depth, inLoop) }},
		{name: "break", weight: terminalWeight, ok: g.enabled("break") && inLoop,
			emit: terminal("break")},
		{name: "continue", weight: terminalWeight, ok: g.enabled("continue") && inLoop,
			emit: terminal("continue")},
	}).emit()
}

// block emits a nested body. It never DECLARES a variable — a declaration
// inside a block would go out of scope before the observation could consume
// it, and Go rejects an unused local. Every declaration lives at function top
// level until the block-tree emitter rung (BRIEF growth ladder #5).
func (g *Generator) block(out *emitter, depth, count int, inLoop bool) {
	for i := 0; i < count; i++ {
		g.stmtIn(out, depth-1, inLoop, i == count-1)
	}
}

func (g *Generator) assign(out *emitter) {
	target := &g.vars[g.c.draw(len(g.vars))]
	g.mark("assignment")
	out.line("%s = %s", target.name, g.expr(target.typ, g.cfg.ExprFuel).text)
}

func (g *Generator) compoundAssign(out *emitter) {
	target := &g.vars[g.c.draw(len(g.vars))]
	if target.typ.Shape == ShapeBool {
		g.assign(out)
		return
	}
	if target.typ.Shape == ShapeString {
		if !g.enabled("concat") {
			g.assign(out)
			return
		}
		// String += takes LITERALS ONLY: `s += t` in a loop adds t's full
		// length every iteration while t may itself grow — the one
		// self-amplifying form the linear-growth rule must exclude.
		parts := make([]string, 1+g.c.draw(2))
		for i := range parts {
			parts[i] = g.literal(Str()).text
		}
		g.mark("assignment", "strings", "concat")
		out.line("%s += %s", target.name, strings.Join(parts, " + "))
		return
	}
	ops := []string{"+=", "-=", "*="}
	if g.enabled("bitwise") {
		ops = append(ops, "&=", "|=", "^=")
	}
	op := pick(g.c, ops)
	g.mark("assignment", "ints")
	switch op {
	case "+=", "-=", "*=":
		g.markWidthDep(target.typ)
	}
	// The right side is forced non-constant for the same reason binary
	// arithmetic operands are: the target must not let Go fold the whole
	// thing into an overflowing constant.
	out.line("%s %s %s", target.name, op, g.nonConstExpr(target.typ, g.cfg.ExprFuel-1).text)
}

func (g *Generator) incDec(out *emitter) {
	var numeric []int
	for i, v := range g.vars {
		if v.typ.Shape == ShapeInt {
			numeric = append(numeric, i)
		}
	}
	target := &g.vars[pick(g.c, numeric)]
	op := "++"
	if g.c.chance(2) {
		op = "--"
	}
	g.mark("assignment", "ints")
	g.markWidthDep(target.typ)
	out.line("%s%s", target.name, op)
}

func (g *Generator) ifStmt(out *emitter, depth int, inLoop bool) {
	g.mark("if", "control_flow")
	out.open("if %s {", g.boolExpr(g.cfg.ExprFuel).text)
	g.block(out, depth, 1+g.c.draw(2), inLoop)
	if g.c.chance(2) {
		out.dedent()
		out.line("} else {")
		out.indent++
		g.block(out, depth, 1+g.c.draw(2), inLoop)
	}
	out.close()
}

// forStmt emits a loop whose trip count is a LITERAL bounded by
// Config.LoopCap. Termination by construction: the bound is never an
// expression, the step is always +1, and the index is never assigned in the
// body, so no generated program can fail to halt.
func (g *Generator) forStmt(out *emitter, depth int) {
	index := fmt.Sprintf("i%d", g.loopSeq)
	g.loopSeq++
	trips := 1 + g.c.draw(g.cfg.LoopCap)
	g.mark("loops", "control_flow", "short_decl")
	out.open("for %s := 0; %s < %d; %s++ {", index, index, trips, index)
	// The index is folded into a variable FIRST, before the body can emit a
	// terminal statement — otherwise a trailing accumulation is dead on any
	// early-exit path and the loop can compute nothing at all (34% of the
	// prototype's loops did, before this ordering).
	g.consumeIndex(out, index)
	g.block(out, depth, 1+g.c.draw(2), true)
	out.close()
}

// consumeIndex folds the loop index into an accumulator. The conversion form
// needs the conversions construct; when that is disabled the target is the
// plain-int variable the declaration floor guarantees.
func (g *Generator) consumeIndex(out *emitter, index string) {
	if g.enabled("conversions") {
		t := pick(g.c, intTypes())
		if i, ok := g.pickVar(t); ok {
			target := &g.vars[i]
			target.reads++
			g.mark("assignment", "conversions")
			g.markWidthDep(target.typ)
			out.line("%s += %s(%s)", target.name, target.typ.GoName(), index)
			return
		}
	}
	if i, ok := g.pickVar(Int(0, false)); ok {
		target := &g.vars[i]
		target.reads++
		g.mark("assignment")
		g.markWidthDep(target.typ)
		out.line("%s += %s", target.name, index)
	}
}

// switchStmt emits a value switch. Case constants must be DISTINCT (Go
// rejects duplicates), so they are drawn without replacement by skipping.
//
// The tag is usually REDUCED (x % 3) so case labels are reachable; a wide,
// unreduced tag — labels mostly never hit — is legal Go and a deliberate
// weighted minority, recorded as unreachable_case (BRIEF: dead code is
// weighted content, not banned).
func (g *Generator) switchStmt(out *emitter, depth int, inLoop bool) {
	t := pick(g.c, intTypes())
	i, ok := g.pickVar(t)
	if !ok {
		g.assign(out)
		return
	}
	tag := &g.vars[i]
	tag.reads++
	g.mark("switch", "control_flow", "cases", "default")

	low, high := t.literalRange()
	tagExpr := tag.name
	reduce := g.enabled("modulo")
	if reduce {
		var reduced arm
		reduced = g.c.choose("switch-tag", []arm{
			{name: "reduced", weight: 4, ok: true},
			{name: "wide", weight: 1, ok: true},
		})
		reduce = reduced.name == "reduced"
	}
	if reduce {
		g.mark("modulo")
		if t.Bits == 0 && !t.Unsigned {
			tagExpr = fmt.Sprintf("%s %% 3", tag.name)
		} else {
			tagExpr = fmt.Sprintf("%s %% %s(3)", tag.name, t.GoName())
		}
		low, high = 0, 2
		if !t.Unsigned {
			low = -2 // Go's % keeps the dividend's sign.
		}
	} else {
		g.note(tagUnreachableCase)
	}
	out.open("switch %s {", tagExpr)
	seen := map[int]bool{}
	for i := 0; i < 1+g.c.draw(3); i++ {
		n := low + g.c.draw(high-low+1)
		if seen[n] {
			continue
		}
		seen[n] = true
		literal := fmt.Sprintf("%d", n)
		if !(t.Bits == 0 && !t.Unsigned) {
			literal = fmt.Sprintf("%s(%d)", t.GoName(), n)
		}
		out.dedent()
		out.line("case %s:", literal)
		out.indent++
		g.block(out, depth, 1, inLoop)
	}
	out.dedent()
	out.line("default:")
	out.indent++
	g.block(out, depth, 1, inLoop)
	out.dedent()
	out.line("}")
}
