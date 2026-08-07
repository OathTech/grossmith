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
		{name: "elem-assign", weight: 3, ok: g.enabled("index") && len(g.indexableVars()) > 0,
			emit: func() { g.elemAssign(out) }},
		{name: "append", weight: 3, ok: g.enabled("slices", "append") && len(g.appendableSlices()) > 0,
			emit: func() { g.appendStmt(out) }},
		{name: "map-write", weight: 3, ok: g.enabled("maps") && len(g.mapVars(nil)) > 0,
			emit: func() { g.mapWrite(out) }},
		{name: "map-delete", weight: 2, ok: g.enabled("maps", "delete") && len(g.mapVars(nil)) > 0,
			emit: func() { g.mapDelete(out) }},
		{name: "comma-ok", weight: 1, ok: g.enabled("maps", "comma_ok") && len(g.mapVars(nil)) > 0,
			emit: func() { g.commaOk(out) }},
		{name: "assert-ok", weight: 1, ok: g.enabled("interfaces", "assertion") && len(g.ifaceVars()) > 0 && len(g.defined) > 0,
			emit: func() { g.assertOk(out) }},
		{name: "iface-assign", weight: 2, ok: g.enabled("interfaces") && len(g.ifaceVars()) > 0,
			emit: func() { g.ifaceAssign(out) }},
		{name: "defer", weight: 2, ok: g.enabled("defer") && !g.pureMode,
			emit: func() { g.deferPrint(out) }},
		{name: "guarded", weight: 1, ok: g.enabled("recover") && depth > 0 && !g.pureMode,
			emit: func() { g.guardedStmt(out) }},
		{name: "linearize", weight: 1, ok: g.enabled("linearize", "division", "modulo") && !g.pureMode,
			emit: func() { g.linearizedRisk(out) }},
		{name: "map-fold", weight: 2, ok: g.enabled("maps", "range") && len(g.intElemMapVars()) > 0,
			emit: func() { g.mapRangeFold(out) }},

		{name: "field-assign", weight: 2, ok: g.enabled("structs", "field") && len(g.fieldSources(nil)) > 0,
			emit: func() { g.fieldAssign(out) }},
		{name: "if", weight: 3, ok: g.enabled("if") && depth > 0,
			emit: func() { g.ifStmt(out, depth, inLoop) }},
		{name: "for", weight: 3, ok: g.enabled("loops") && depth > 0,
			emit: func() { g.forStmt(out, depth) }},
		{name: "range", weight: 2, ok: g.enabled("range") && depth > 0 && len(g.indexableVars()) > 0,
			emit: func() { g.rangeStmt(out, depth) }},
		{name: "switch", weight: 2, ok: g.enabled("switch") && depth > 0,
			emit: func() { g.switchStmt(out, depth, inLoop) }},
		{name: "break", weight: terminalWeight, ok: g.enabled("break") && inLoop,
			emit: terminal("break")},
		{name: "continue", weight: terminalWeight, ok: g.enabled("continue") && inLoop,
			emit: terminal("continue")},
		{name: "call", weight: 2, ok: g.enabled("helpers") && len(g.helpers) > 0,
			emit: func() { g.callStmt(out) }},
		{name: "observe", weight: 2, ok: g.enabled("observe_point") && !g.pureMode,
			emit: func() { g.observePoint(out) }},
		{name: "early-return", weight: 1, ok: g.enabled("early_return") && !last && !g.pureMode,
			emit: func() { g.earlyReturn(out) }},
	}).emit()
}

// earlyReturn is a second return site: the observed tuple at THIS point,
// path-dependent liveness for everything after. Only drawn mid-block, so
// the code behind it is the ordinary tagged dead_code minority.
func (g *Generator) earlyReturn(out *emitter) {
	g.mark("early_return", "return")
	g.note(tagDeadCode)
	out.line("return %s", strings.Join(g.observedNames(), ", "))
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
	out.line("%s", g.obsCall("point", v))
}

// obsCall renders one obs* protocol call for a scalar/string variable: the
// goType spelling plus a widening conversion, so the event carries width
// and named-type identity (protocol §3).
func (g *Generator) obsCall(at string, v *binding) string {
	t := v.typ
	switch {
	case t.Shape == ShapeBool:
		return fmt.Sprintf("obsBool(%q, %q, %s)", at, "bool", v.name)
	case t.Shape == ShapeString:
		return fmt.Sprintf("obsStr(%q, %q, %s)", at, "string", v.name)
	case t.Unsigned:
		return fmt.Sprintf("obsUint(%q, %q, uint64(%s))", at, t.GoName(), v.name)
	default:
		return fmt.Sprintf("obsInt(%q, %q, int64(%s))", at, t.GoName(), v.name)
	}
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
	// Helper bodies have no type-pool floor, which the projection targets
	// assume — inner declarations stay a subject-side construct.
	if g.pureMode || !g.enabled("block_decl") || !g.c.chance(3) {
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
	// Slices and maps are excluded: whole-slice assignment aliases backing
	// arrays (alias + append has spec-UNSPECIFIED write visibility), and
	// container mutation happens via dedicated arms. Every slice owns its
	// backing; maps mutate through writes and deletes only.
	var all []int
	for i, v := range g.vars {
		if v.typ.Shape != ShapeSlice && v.typ.Shape != ShapeMap {
			all = append(all, i)
		}
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
		if target.typ.Shape == ShapeInterface {
			// No interface literal exists; a fresh RHS is a concrete
			// implementer instead (implicit conversion — still content).
			info := g.ifaceByName(target.typ.Name)
			rhs = g.variable(g.defined[pick(g.c, g.implementers(info))].typ)
		} else {
			rhs = g.literal(target.typ)
		}
	}
	out.line("%s = %s", target.name, rhs.text)
}

func (g *Generator) compoundAssign(out *emitter) {
	g.resetRisk()
	var all []int
	for i, v := range g.vars {
		if v.typ.Shape != ShapeSlice && v.typ.Shape != ShapeMap {
			all = append(all, i)
		}
	}
	target := &g.vars[g.pickTarget(all)]
	if target.typ.Shape == ShapeBool {
		g.assign(out)
		return
	}
	if target.typ.Shape == ShapeArray || target.typ.Shape == ShapeStruct || target.typ.Shape == ShapeInterface {
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

// elemAssign writes one array or slice element under the drawn index
// policy. The write alone does not discharge Go's unused-variable rule, so
// observe()'s reads==0 fallback still covers a write-only container.
func (g *Generator) elemAssign(out *emitter) {
	g.resetRisk()
	i := pick(g.c, g.indexableVars())
	arr := &g.vars[i]
	if arr.typ.Shape == ShapeSlice {
		g.mark("slices", "index", "assignment")
	} else {
		g.mark("arrays", "index", "assignment")
	}
	out.line("%s[%s] = %s", arr.name, g.indexExpr(arr.indexBound()), g.expr(*arr.typ.Elem, g.cfg.ExprFuel).text)
}

// appendStmt grows a slice: s = append(s, elem). Growth per execution is one
// element, so total slice memory stays linear in executed statements; len
// only ever grows, preserving the minLen index bound.
func (g *Generator) appendStmt(out *emitter) {
	g.resetRisk()
	i := pick(g.c, g.appendableSlices())
	s := &g.vars[i]
	s.reads++ // append reads its first operand
	g.mark("slices", "append", "assignment")
	var elem value
	if s.typ.Elem.Shape == ShapeString {
		// Literal elements only, decided BEFORE any draw (a discarded
		// expression would leave phantom reads): appending a
		// variable-derived string retains a full copy per executed
		// statement — quadratic retention the linear-growth rule cannot
		// see (second review).
		elem = g.literal(Str())
	} else {
		elem = g.expr(*s.typ.Elem, g.cfg.ExprFuel)
	}
	out.line("%s = append(%s, %s)", s.name, s.name, elem.text)
}

// mapWrite sets one alphabet key. Maps are never nil, so writes cannot
// panic; the alphabet bounds the map's size by construction.
func (g *Generator) mapWrite(out *emitter) {
	g.resetRisk()
	m := &g.vars[pick(g.c, g.mapVars(nil))]
	g.mark("maps", "assignment")
	out.line("%s[%s] = %s", m.name, pick(g.c, m.keys), g.expr(*m.typ.Elem, g.cfg.ExprFuel).text)
}

// mapDelete removes one alphabet key — present or not, deterministically.
func (g *Generator) mapDelete(out *emitter) {
	g.resetRisk()
	m := &g.vars[pick(g.c, g.mapVars(nil))]
	m.reads++
	g.mark("maps", "delete")
	out.line("delete(%s, %s)", m.name, pick(g.c, m.keys))
}

// commaOk is the two-value map read: x, ok = m[k]. Both targets are
// existing variables (the elem-type floor and the bool floor guarantee
// them); presence and value are both observable state.
func (g *Generator) commaOk(out *emitter) {
	g.resetRisk()
	m := &g.vars[pick(g.c, g.mapVars(nil))]
	m.reads++
	xi, _ := g.pickVar(*m.typ.Elem)
	oki, _ := g.pickVar(Bool())
	g.mark("maps", "comma_ok", "assignment")
	out.line("%s, %s = %s[%s]", g.vars[xi].name, g.vars[oki].name, m.name, pick(g.c, m.keys))
}

// callStmt invokes a helper as a statement, assigning every result: targets
// are drawn per result type, with the blank identifier on a miss (helper
// env) or a target collision (`v1, v1 = h()` reads poorly even if legal).
func (g *Generator) callStmt(out *emitter) {
	g.resetRisk()
	h := g.helpers[g.c.draw(len(g.helpers))]
	targets := make([]string, len(h.results))
	seen := map[int]bool{}
	for i, rt := range h.results {
		targets[i] = "_"
		if idx, ok := g.pickVar(rt); ok && !seen[idx] {
			seen[idx] = true
			targets[i] = g.vars[idx].name
		}
	}
	g.mark("helpers", "assignment")
	out.line("%s = %s(%s)", strings.Join(targets, ", "), h.name, g.callArgs(h, g.cfg.ExprFuel-1))
}

// ifaceAssign reassigns an interface variable — dynamic-type churn, which
// is what makes assertion outcomes and probes vary (second review: 98.6% of
// interface probes matched the declaration because reassignment was rare).
func (g *Generator) ifaceAssign(out *emitter) {
	g.resetRisk()
	iv := &g.vars[pick(g.c, g.ifaceVars())]
	rhs := g.ifaceExpr(iv.typ)
	if rhs.text == iv.name {
		iv.reads--
		info := g.ifaceByName(iv.typ.Name)
		rhs = g.variable(g.defined[pick(g.c, g.implementers(info))].typ)
	}
	g.mark("interfaces", "assignment")
	out.line("%s = %s", iv.name, rhs.text)
}

// assertOk is the two-result type assertion: x, ok = iv.(T) — never panics,
// and with one satisfier per interface both results are static knowledge.
func (g *Generator) assertOk(out *emitter) {
	g.resetRisk()
	iv := &g.vars[pick(g.c, g.ifaceVars())]
	iv.reads++
	info := g.ifaceByName(iv.typ.Name)
	dt := g.defined[pick(g.c, g.implementers(info))].typ
	target := "_"
	if ti, ok := g.pickVar(dt); ok {
		target = g.vars[ti].name
	}
	okTarget := "_"
	if oi, ok := g.pickVar(Bool()); ok {
		okTarget = g.vars[oi].name
	}
	g.mark("interfaces", "assertion", "assignment")
	out.line("%s, %s = %s.(%s)", target, okTarget, iv.name, dt.Named)
}

// deferPrint schedules an exit observation: the argument is evaluated AT
// DEFER TIME (a corner clones get wrong — evaluating at call time), the
// prints run LIFO at function exit (ordering is observable), and they run
// during panic unwinding too — guaranteed exit observations even on panic
// paths.
func (g *Generator) deferPrint(out *emitter) {
	var cands []int
	for i, v := range g.vars {
		switch v.typ.Shape {
		case ShapeInt, ShapeBool, ShapeString:
			cands = append(cands, i)
		}
	}
	v := &g.vars[pick(g.c, cands)]
	v.reads++
	g.mark("defer")
	// The obs* argument is evaluated AT DEFER TIME — the semantics this
	// construct exists to exercise — because defer evaluates call arguments
	// immediately.
	out.line("defer %s", g.obsCall("defer", v))
}

// guardedStmt wraps ONE statement in an immediately-invoked function literal
// with defer/recover — the statement-level catch (BRIEF panic-identity
// disposition): a panic becomes an inline "recovered: <msg>" observation and
// EXECUTION CONTINUES, so the rest of the program still observes. The inner
// statement is drawn at depth 0 with inLoop=false and last=true, which masks
// everything illegal inside a closure (break/continue targeting an outer
// loop, the early return whose tuple the closure cannot return) — the
// typecheck witness would catch any leak.
func (g *Generator) guardedStmt(out *emitter) {
	g.mark("recover")
	// The guard exists to CATCH panics, but at ordinary risk minorities the
	// inner statement panicked in 0.7% of guards (second review) — the
	// statement-level-catch semantics were effectively untested. Bias the
	// hot arms up inside the guard.
	g.guardBias = true
	defer func() { g.guardBias = false }()
	out.open("func() {")
	out.open("defer func() {")
	out.open("if r := recover(); r != nil {")
	out.open("if err, ok := r.(error); ok {")
	out.line("obsRecovered(err.Error())")
	out.dedent()
	out.line("} else {")
	out.indent++
	out.line("obsRecovered(%q)", "")
	out.close()
	out.close()
	out.dedent()
	out.line("}()")
	g.stmtIn(out, 0, false, true)
	out.dedent()
	out.line("}()")
}

// linearizedRisk is the deterministic form of a multi-trap computation:
// operands are hoisted into temporaries, one per statement, so the panic
// order is pinned by STATEMENT SEQUENCING — two hot sites, specified order
// (the multi-site-in-one-expression form has spec-unspecified panic
// identity and stays excluded by the risk budget).
func (g *Generator) linearizedRisk(out *emitter) {
	t := pick(g.c, intTypes())
	t0 := fmt.Sprintf("t%d", g.tmpSeq)
	t1 := fmt.Sprintf("t%d", g.tmpSeq+1)
	g.tmpSeq += 2
	// Spend each statement's risk slot FIRST: the hot divisor below is the
	// statement's one risk site, and claiming the budget up front masks any
	// nested risky arm inside the left operand.
	g.resetRisk()
	g.spendRisk()
	g.mark("division", "short_decl")
	g.note(tagPanicRisk)
	left0 := g.nonConstExpr(t, 1).text
	out.line("%s := (%s / %s)", t0, left0, g.variable(t).text)
	g.resetRisk()
	g.spendRisk()
	g.mark("modulo", "short_decl")
	g.note(tagPanicRisk)
	left1 := g.nonConstExpr(t, 1).text
	out.line("%s := (%s %% %s)", t1, left1, g.variable(t).text)
	g.resetRisk()
	i, _ := g.pickVar(t)
	target := &g.vars[i]
	g.mark("assignment", "ints")
	g.markWidthDep(t)
	g.note("linearized")
	out.line("%s = (%s + %s)", target.name, t0, t1)
}

// intElemMapVars are map variables with integer elements — the ones whose
// values fold order-invariantly.
func (g *Generator) intElemMapVars() []int {
	var found []int
	for _, i := range g.mapVars(nil) {
		if g.vars[i].typ.Elem.Shape == ShapeInt {
			found = append(found, i)
		}
	}
	return found
}

// mapRangeFold is the quotiented return of map iteration: range over a map
// with a body that is EXACTLY one commutative fold (acc += value), so the
// observed outcome is invariant under any iteration order — the clone must
// still iterate correctly, but the observation quotients away the one thing
// the spec leaves open. Nothing else may enter the body: any other side
// effect would execute in map order.
func (g *Generator) mapRangeFold(out *emitter) {
	m := &g.vars[pick(g.c, g.intElemMapVars())]
	m.reads++
	i, _ := g.pickVar(*m.typ.Elem)
	acc := &g.vars[i]
	acc.reads++
	e := fmt.Sprintf("e%d", g.tmpSeq)
	g.tmpSeq++
	g.mark("maps", "range", "assignment", "control_flow", "short_decl")
	g.note("map_range_fold")
	g.markWidthDep(acc.typ)
	out.open("for _, %s := range %s {", e, m.name)
	out.line("%s += %s", acc.name, e)
	out.close()
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
	i := pick(g.c, g.indexableVars())
	arr := &g.vars[i]
	if arr.typ.Shape == ShapeSlice {
		// One range's own trip count is fixed (the range expression is
		// evaluated once) — but an INNER range over the same slice
		// re-evaluates len, so append-under-enclosing-range composes into
		// len*2^len executed statements (second review, live in corpus).
		// The mask, not a weight: appends to this slice are banned for the
		// duration of the body.
		g.rangedSlices = append(g.rangedSlices, i)
		defer func() { g.rangedSlices = g.rangedSlices[:len(g.rangedSlices)-1] }()
		g.mark("slices", "range")
	} else {
		g.mark("arrays", "range")
	}
	arr.reads++
	index := fmt.Sprintf("i%d", g.loopSeq)
	g.loopSeq++
	g.mark("control_flow", "short_decl")
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
