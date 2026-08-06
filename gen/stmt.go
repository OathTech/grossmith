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
		{name: "elem-assign", weight: 2, ok: g.enabled("arrays", "index") && len(g.arrayVars(nil)) > 0,
			emit: func() { g.elemAssign(out) }},
		{name: "field-assign", weight: 2, ok: g.enabled("structs", "field") && len(g.fieldSources(nil)) > 0,
			emit: func() { g.fieldAssign(out) }},
		{name: "if", weight: 3, ok: g.enabled("if") && depth > 0,
			emit: func() { g.ifStmt(out, depth, inLoop) }},
		{name: "for", weight: 3, ok: g.enabled("loops") && depth > 0,
			emit: func() { g.forStmt(out, depth) }},
		{name: "range", weight: 2, ok: g.enabled("arrays", "range") && depth > 0 && len(g.arrayVars(nil)) > 0,
			emit: func() { g.rangeStmt(out, depth) }},
		{name: "switch", weight: 2, ok: g.enabled("switch") && depth > 0,
			emit: func() { g.switchStmt(out, depth, inLoop) }},
		{name: "break", weight: terminalWeight, ok: g.enabled("break") && inLoop,
			emit: terminal("break")},
		{name: "continue", weight: terminalWeight, ok: g.enabled("continue") && inLoop,
			emit: terminal("continue")},
		{name: "observe", weight: 2, ok: g.enabled("observe_point"),
			emit: func() { g.observePoint(out) }},
	}).emit()
}

// observePoint prints one scalar/string variable mid-execution — an
// interleaved observation. It pins intermediate state (localizing WHERE two
// implementations diverge), gives unobserved variables observational reach,
// and its output survives a later panic: the review found panic masking was
// total because observation happened only at exit.
func (g *Generator) observePoint(out *emitter) {
	var cands []int
	for i, v := range g.vars {
		switch v.typ.Shape {
		case ShapeInt, ShapeBool, ShapeString:
			cands = append(cands, i)
		}
	}
	v := &g.vars[pick(g.c, cands)]
	v.reads++
	g.mark("observe_point")
	out.line("println(%s)", v.name)
}

// block emits a nested body, optionally opening a BLOCK-SCOPED declaration.
// The scope problem that once forbade inner declarations — out of scope
// before the observation, unused rejection — is solved by the PROJECTION
// RULE: an inner declaration is folded into an enclosing variable before its
// block exits, so its value reaches the observation and its read discharges
// the unused rule. Projection appends at block exit, which is why no
// block-tree emitter is needed for real scopes.
//
// When a projection is pending, no statement in the block counts as "last":
// a terminal drawn anywhere in it is the ordinary tagged dead-code minority
// (the projection behind it goes dead, recorded as dead_code).
func (g *Generator) block(out *emitter, depth, count int, inLoop bool) {
	inner := g.maybeDeclareInner(out)
	for i := 0; i < count; i++ {
		g.stmtIn(out, depth-1, inLoop, i == count-1 && inner == nil)
	}
	if inner != nil {
		g.projectInner(out, *inner)
		g.vars = g.vars[:len(g.vars)-1]
	}
}

// maybeDeclareInner opens a block-scoped variable: scalar or string, drawn
// initializer, fresh name. Bool inner declarations need the equality
// construct — the projection `o = (o != w)` is an equality operator.
func (g *Generator) maybeDeclareInner(out *emitter) *binding {
	if !g.enabled("block_decl") || !g.c.chance(3) {
		return nil
	}
	g.resetRisk()
	pool := intTypes()
	if g.enabled("equality") {
		pool = append(pool, Bool())
	}
	if g.enabled("strings") {
		pool = append(pool, Str())
	}
	t := pick(g.c, pool)
	name := fmt.Sprintf("w%d", g.innerSeq)
	g.innerSeq++
	g.mark(append([]string{"block_decl", "short_decl"}, t.Tags()...)...)
	out.line("%s := %s", name, g.expr(t, g.cfg.ExprFuel).text)
	g.vars = append(g.vars, binding{name: name, typ: t})
	w := g.vars[len(g.vars)-1]
	return &w
}

// projectInner folds the inner variable into an enclosing one before scope
// exit — the observation path for block-scoped state.
func (g *Generator) projectInner(out *emitter, w binding) {
	switch w.typ.Shape {
	case ShapeBool:
		o := &g.vars[g.pickOuter(Bool())]
		o.reads++
		g.mark("assignment", "equality")
		out.line("%s = (%s != %s)", o.name, o.name, w.name)
	case ShapeString:
		o := &g.vars[g.pickOuter(Str())]
		g.mark("assignment", "strings")
		if g.enabled("concat") {
			g.mark("concat")
			out.line("%s = (%s + %s)", o.name, w.name, g.literal(Str()).text)
			return
		}
		out.line("%s = %s", o.name, w.name)
	default:
		if g.enabled("conversions") {
			t := pick(g.c, intTypes())
			o := &g.vars[g.pickOuter(t)]
			o.reads++
			g.mark("assignment", "conversions")
			g.markWidthDep(o.typ)
			out.line("%s += %s(%s)", o.name, o.typ.GoName(), w.name)
			return
		}
		o := &g.vars[g.pickOuter(w.typ)]
		o.reads++
		g.mark("assignment")
		g.markWidthDep(o.typ)
		out.line("%s += %s", o.name, w.name)
	}
}

func (g *Generator) assign(out *emitter) {
	g.resetRisk()
	all := make([]int, len(g.vars))
	for i := range all {
		all[i] = i
	}
	target := &g.vars[g.pickTarget(all)]
	g.mark("assignment")
	rhs := g.expr(target.typ, g.cfg.ExprFuel)
	// `v = v` is an identity no-op that arises whenever a type has one
	// variable (review finding: ~19% of plain assigns). Re-initializing
	// with a literal at least writes a value. The discarded bare-variable
	// RHS was counted as a read — un-count it, or the phantom read would
	// suppress the `_ = v` discharge and break the compile bar.
	if rhs.text == target.name {
		target.reads--
		rhs = g.literal(target.typ)
	}
	out.line("%s = %s", target.name, rhs.text)
}

func (g *Generator) compoundAssign(out *emitter) {
	g.resetRisk()
	all := make([]int, len(g.vars))
	for i := range all {
		all[i] = i
	}
	target := &g.vars[g.pickTarget(all)]
	if target.typ.Shape == ShapeBool {
		g.assign(out)
		return
	}
	if target.typ.Shape == ShapeArray || target.typ.Shape == ShapeStruct {
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
	rhs := g.nonConstExpr(target.typ, g.cfg.ExprFuel-1)
	// `v ^= v` and friends zero or fix the target — identity no-ops. A
	// single literal RHS is safe here: the TARGET is non-constant, so the
	// statement cannot constant-fold. Un-count the discarded read.
	if rhs.text == target.name {
		target.reads--
		rhs = g.literal(target.typ)
	}
	out.line("%s %s %s", target.name, op, rhs.text)
}

func (g *Generator) incDec(out *emitter) {
	var numeric []int
	for i, v := range g.vars {
		if v.typ.Shape == ShapeInt {
			numeric = append(numeric, i)
		}
	}
	target := &g.vars[g.pickTarget(numeric)]
	op := "++"
	if g.c.chance(2) {
		op = "--"
	}
	g.mark("assignment", "ints")
	g.markWidthDep(target.typ)
	out.line("%s%s", target.name, op)
}

// elemAssign writes one array element under the drawn index policy. The
// write alone does not discharge Go's unused-variable rule, so observe()'s
// reads==0 fallback still covers a write-only array.
func (g *Generator) elemAssign(out *emitter) {
	g.resetRisk()
	i := pick(g.c, g.arrayVars(nil))
	arr := &g.vars[i]
	g.mark("arrays", "index", "assignment")
	out.line("%s[%s] = %s", arr.name, g.indexExpr(arr.typ), g.expr(*arr.typ.Elem, g.cfg.ExprFuel).text)
}

// fieldAssign writes one struct field. Like element writes, a field write
// alone does not discharge the unused-variable rule; observe() covers it.
func (g *Generator) fieldAssign(out *emitter) {
	g.resetRisk()
	src := pick(g.c, g.fieldSources(nil))
	sv := &g.vars[src.varIdx]
	var fieldType Type
	for _, f := range sv.typ.Fields {
		if f.Name == src.field {
			fieldType = f.Typ
		}
	}
	g.mark("structs", "field", "assignment")
	out.line("%s.%s = %s", sv.name, src.field, g.expr(fieldType, g.cfg.ExprFuel).text)
}

// rangeStmt loops over a fixed-length array: termination comes free with the
// data (the Xsmith loop-over-container observation), no literal bound
// needed. The index is folded into an accumulator first, like forStmt.
func (g *Generator) rangeStmt(out *emitter, depth int) {
	i := pick(g.c, g.arrayVars(nil))
	arr := &g.vars[i]
	arr.reads++
	index := fmt.Sprintf("i%d", g.loopSeq)
	g.loopSeq++
	g.mark("arrays", "range", "control_flow", "short_decl")
	out.open("for %s := range %s {", index, arr.name)
	g.consumeIndex(out, index)
	g.block(out, depth, 1+g.c.draw(2), true)
	out.close()
}

func (g *Generator) ifStmt(out *emitter, depth int, inLoop bool) {
	g.resetRisk()
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
	// Reduction keeps case labels reachable. Modulo is preferred; a bitmask
	// serves when the swarm mix lacks modulo — previously any modulo-less
	// mix forced EVERY switch wide, making unreachable_case a swarm
	// side-effect instead of the drawn 4:1 minority (review finding).
	reduceVia := ""
	switch {
	case g.enabled("modulo"):
		reduceVia = "%"
	case g.enabled("bitwise"):
		reduceVia = "&"
	}
	reduce := reduceVia != ""
	if reduce {
		reduce = g.c.choose("switch-tag", []arm{
			{name: "reduced", weight: 4, ok: true},
			{name: "wide", weight: 1, ok: true},
		}).name == "reduced"
	}
	switch {
	case reduce && reduceVia == "%":
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
	case reduce:
		// x & 3 is in [0,3] for every integer type (two's complement).
		g.mark("bitwise")
		if t.Bits == 0 && !t.Unsigned {
			tagExpr = fmt.Sprintf("%s & 3", tag.name)
		} else {
			tagExpr = fmt.Sprintf("%s & %s(3)", tag.name, t.GoName())
		}
		low, high = 0, 3
	default:
		g.note(tagUnreachableCase)
	}
	out.open("switch %s {", tagExpr)
	seen := map[int]bool{}
	caseCount := 1 + g.c.draw(3) // hoisted: never draw in a loop condition
	for i := 0; i < caseCount; i++ {
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
