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
		{name: "compound", weight: 2 + 3*boolToInt(g.corner == "kinds"), ok: true,
			emit: func() { g.compoundAssign(out) }},
		{name: "incdec", weight: 2 + 3*boolToInt(g.corner == "kinds"), ok: true,
			emit: func() { g.incDec(out) }},
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
		{name: "string-fold", weight: 2,
			// int(r) is a conversion, so the fold is conversion-gated and
			// masked under the kinds corner like every laundering site.
			ok: g.enabled("strings", "string_range", "range", "conversions") && g.corner != "kinds" &&
				len(g.varsOfShape(ShapeString, nil)) > 0,
			emit: func() { g.stringRangeFold(out) }},

		{name: "slice-triple", weight: 2,
			// The observation fold converts (int(e)) — conversion-gated and
			// masked under the kinds corner like every laundering site.
			ok: g.enabled("slices", "slice_triple", "append", "range", "conversions") &&
				g.corner != "kinds" && len(g.intElemSliceVars()) > 0,
			emit: func() { g.sliceTripleStmt(out) }},
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
		{name: "bare-call", weight: 1, ok: g.enabled("helpers", "bare_call") && len(g.helpers) > 0,
			emit: func() { g.bareCallStmt(out) }},
		{name: "multi-assign", weight: 3, ok: g.enabled("multi_assign") && len(g.vars) >= 2,
			emit: func() { g.multiAssign(out) }},
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
	names := g.observedNames()
	// Aggregate slots (rung 4) are computed only at the final return;
	// an early exit reports them as zero — path-dependent liveness, the
	// same honesty as any early return, and deterministic.
	for _, v := range g.vars {
		if v.aggObserved {
			names = append(names, "0")
		}
	}
	if g.wrapped {
		names = append(names, "0")
	}
	out.line("return %s", strings.Join(names, ", "))
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
		if g.enabled("conversions") && g.corner != "kinds" {
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
			// Kinds corner: defined-type compound targets weighted up
			// (see incDec).
			if g.corner == "kinds" && v.typ.Shape == ShapeInt && v.typ.Named != "" {
				all = append(all, i, i)
			}
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
			// The kinds corner (rung 3, GoLean R3): weight DEFINED-type
			// targets up — the synthetic-literal kind-defaulting family
			// (their BUG-042/043, started by our seed 559) lives exactly
			// where a defaulted literal meets a defined kind.
			if g.corner == "kinds" && v.typ.Named != "" {
				numeric = append(numeric, i, i)
			}
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
	target := g.vars[xi].name
	// Element-target minority (rung 2, R2a's comma-ok forms):
	// a[i], ok = m[k] — the two-phase semantics apply to the index too.
	if g.enabled("index", "multi_assign") && g.c.chance(3) {
		if cands := g.indexableVarsOfElem(*m.typ.Elem); len(cands) > 0 {
			arr := &g.vars[pick(g.c, cands)]
			target = fmt.Sprintf("%s[%s]", arr.name, g.indexExpr(arr.indexBound()))
			g.mark("index", "multi_assign")
		}
	}
	oki, _ := g.pickVar(Bool())
	g.mark("maps", "comma_ok", "assignment")
	out.line("%s, %s = %s[%s]", target, g.vars[oki].name, m.name, pick(g.c, m.keys))
}

// indexableVarsOfElem: indexable vars whose element type is exactly t.
func (g *Generator) indexableVarsOfElem(t Type) []int {
	var out []int
	for _, i := range g.indexableVars() {
		if g.vars[i].typ.Elem.Equal(t) {
			out = append(out, i)
		}
	}
	return out
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
		// Element-target minority (rung 2, R2a's multi-result forms):
		// v, a[i] = h(...) — mixed targets from one call's results.
		if g.enabled("index", "multi_assign") && g.c.chance(4) {
			if cands := g.indexableVarsOfElem(rt); len(cands) > 0 {
				arr := &g.vars[pick(g.c, cands)]
				targets[i] = fmt.Sprintf("%s[%s]", arr.name, g.indexExpr(arr.indexBound()))
				g.mark("index", "multi_assign")
				continue
			}
		}
		if idx, ok := g.pickVar(rt); ok && !seen[idx] {
			seen[idx] = true
			targets[i] = g.vars[idx].name
		}
	}
	g.mark("helpers", "assignment")
	out.line("%s = %s(%s)", strings.Join(targets, ", "), h.name, g.callArgs(h, g.cfg.ExprFuel-1))
}

// bareCallStmt invokes a helper as a statement with NO targets — a legal,
// ubiquitous Go shape (discarding results needs no `_ =`) the grammar
// previously never emitted. Phase 2's first measured detection gap: GoLean's
// BUG-012 (bare value-returning calls went stuck) was their own audit's most
// frequent novel-program failure, and grossmith could not have found it
// because callStmt always writes targets. The RESULTS are dead (helpers are
// pure) but the statement is not eliminable — the argument list can carry
// the statement's hot panic site — so it does NOT join the dead_value tier
// (audit: that tag means `_ = v`, a different phenomenon); bare_call itself
// is the stratification tag. Its value is the call lowering it forces a
// clone to perform.
func (g *Generator) bareCallStmt(out *emitter) {
	g.resetRisk()
	h := g.helpers[g.c.draw(len(g.helpers))]
	g.mark("helpers", "bare_call")
	out.line("%s(%s)", h.name, g.callArgs(h, g.cfg.ExprFuel-1))
}

// sameTypePair finds two distinct variables of the same type, preferring
// scalar/string kinds (swap targets).
func (g *Generator) sameTypePairs() [][2]int {
	var pairs [][2]int
	for i := range g.vars {
		switch g.vars[i].typ.Shape {
		case ShapeInt, ShapeBool, ShapeString:
		default:
			continue
		}
		for j := i + 1; j < len(g.vars); j++ {
			if g.vars[i].typ.Equal(g.vars[j].typ) {
				pairs = append(pairs, [2]int{i, j})
			}
		}
	}
	return pairs
}

// unsignedAliasCandidates: unsigned int vars usable as a self-aliasing
// index (`u, a[u%N] = ...` — u%N is in range because u is unsigned).
func (g *Generator) unsignedAliasCandidates() []int {
	var out []int
	for i, v := range g.vars {
		if v.typ.Shape == ShapeInt && v.typ.Unsigned && v.typ.Named == "" {
			out = append(out, i)
		}
	}
	return out
}

// multiAssign emits a multi-target assignment (Phase 4 rung 2, GoLean
// R2a — their most bug-dense corner, 8+ ledger entries): Go's two-phase
// semantics (index operands and RHS all evaluated first, stores applied
// left-to-right) are fully specified, so aliased targets are
// DETERMINISTIC and the final state witnesses the order.
func (g *Generator) multiAssign(out *emitter) {
	g.resetRisk()
	pairs := g.sameTypePairs()
	aliases := g.unsignedAliasCandidates()
	g.c.choose("multi-assign", []arm{
		// a, b = b, a — the canonical phase-order shape: both RHS reads
		// see OLD values.
		{name: "swap", weight: 3, ok: len(pairs) > 0, emit: func() {
			p := pick(g.c, pairs)
			a, b := &g.vars[p[0]], &g.vars[p[1]]
			a.reads++
			b.reads++
			g.mark("assignment", "multi_assign")
			out.line("%s, %s = %s, %s", a.name, b.name, b.name, a.name)
		}},
		// u, a[u%N] = e1, e2 — the aliased-target shape: the index reads
		// u in phase ONE, the store to u lands first, and the element
		// store still uses the OLD u. Unsigned modulo keeps it on the
		// ok-path; a raw signed index is the hot minority.
		{name: "alias-index", weight: 3,
			// The safe index is a conversion of a modulo — gated AND
			// marked as both, like indexExpr's mod arm (audit F1: this
			// emitter bypassed the capability-profile contract), and
			// corner-gated so the kinds corner stays conversion-free.
			ok: g.enabled("index", "conversions", "modulo") && g.corner != "kinds" &&
				len(aliases) > 0 && len(g.indexableVars()) > 0, emit: func() {
				u := &g.vars[pick(g.c, aliases)]
				arr := &g.vars[pick(g.c, g.indexableVars())]
				u.reads++ // the index read
				g.mark("conversions", "modulo")
				idx := fmt.Sprintf("int(%s %% %s(%d))", u.name, u.typ.GoName(), arr.indexBound())
				if g.riskOK() && g.c.chance(4) && g.spendRisk() {
					// Hot: index by a raw signed int var instead.
					if iv, ok := g.pickVar(Int(0, false)); ok {
						u.reads-- // the safe form's read is not taken
						g.vars[iv].reads++
						idx = g.vars[iv].name
						g.note(tagPanicRisk)
					}
				}
				rhs1 := g.expr(u.typ, g.cfg.ExprFuel-1)
				rhs2 := g.expr(*arr.typ.Elem, g.cfg.ExprFuel-1)
				if arr.typ.Shape == ShapeSlice {
					g.mark("slices", "index", "assignment", "multi_assign")
				} else {
					g.mark("arrays", "index", "assignment", "multi_assign")
				}
				out.line("%s, %s[%s] = %s, %s", u.name, arr.name, idx, rhs1.text, rhs2.text)
			}},
		// Mixed plain/element/blank targets with independent right sides —
		// total (>=2 vars guaranteed by the arm gate).
		{name: "mixed", weight: 2, ok: true, emit: func() {
			n := 2 + g.c.draw(2)
			targets := make([]string, n)
			rhs := make([]string, n)
			seen := map[int]bool{}
			for k := 0; k < n; k++ {
				g.c.choose("multi-assign-target", []arm{
					{name: "var", weight: 3, ok: true, emit: func() {
						vi := g.c.draw(len(g.vars))
						t := g.vars[vi].typ
						if seen[vi] || t.Shape == ShapeMap || t.Shape == ShapeSlice {
							// Duplicate or unassignable-wholesale target:
							// discard through the blank identifier.
							targets[k] = "_"
							rhs[k] = g.expr(Int(0, false), g.cfg.ExprFuel-1).text
							return
						}
						seen[vi] = true
						targets[k] = g.vars[vi].name
						rhs[k] = g.expr(t, g.cfg.ExprFuel-1).text
					}},
					{name: "elem", weight: 2, ok: g.enabled("index") && len(g.indexableVars()) > 0, emit: func() {
						arr := &g.vars[pick(g.c, g.indexableVars())]
						targets[k] = fmt.Sprintf("%s[%s]", arr.name, g.indexExpr(arr.indexBound()))
						rhs[k] = g.expr(*arr.typ.Elem, g.cfg.ExprFuel-1).text
						g.mark("index")
					}},
					{name: "blank", weight: 1, ok: true, emit: func() {
						targets[k] = "_"
						rhs[k] = g.expr(Int(0, false), g.cfg.ExprFuel-1).text
					}},
				}).emit()
			}
			g.mark("assignment", "multi_assign")
			out.line("%s = %s", strings.Join(targets, ", "), strings.Join(rhs, ", "))
		}},
	}).emit()
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
	// Save/restore, not set/clear: under the recover wrapper the bias is
	// already on for the whole body and must survive a nested guard.
	prev := g.guardBias
	g.guardBias = true
	defer func() { g.guardBias = prev }()
	out.open("func() {")
	out.open("defer func() {")
	out.open("if r := recover(); r != nil {")
	out.open("if err, ok := r.(error); ok {")
	out.line("obsRecovered(err.Error())")
	out.dedent()
	out.line("} else {")
	out.indent++
	// The non-error arm discards the panic value (the import-free subject
	// cannot Sprint it), unlike the driver's main recover which preserves
	// it. Unreachable today — every generated panic is a runtime.Error —
	// and the arms must be reconciled before explicit panic(v) enters the
	// grammar (audit L2).
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

// intElemSliceVars are slice variables with integer elements — the ones the
// three-index rung's derived-slice fold can observe through int(e).
func (g *Generator) intElemSliceVars() []int {
	var found []int
	for _, i := range g.sliceVars(nil) {
		if g.vars[i].typ.Elem.Shape == ShapeInt {
			found = append(found, i)
		}
	}
	return found
}

// sliceTripleStmt is the three-index-slicing carve-out from the slice
// aliasing quotient (sx g32/g03/c18/c21). The quotient exists because
// whether a PLAIN append reallocates is unspecified (the new capacity after
// any reallocation is only "sufficiently large"), so alias + append has
// unspecified write visibility. A full slice expression dissolves that for
// exactly one step: t := s[a:b:c] pins cap(t) = c-a by spec, so the spec's
// append rule becomes deterministic BOTH ways —
//
//   - b < c: len(t) < cap(t), append MUST re-use the backing array
//     ("Otherwise, append re-uses the underlying array") — the write lands
//     in s's backing at s[b], VISIBLE through the base (b < c <= minLen, so
//     s[b] is within s's observed length);
//   - b == c: len(t) == cap(t), append MUST allocate anew ("If the capacity
//     of s is not large enough ... append allocates") — t and s are provably
//     unshared after, and the base is provably UNCHANGED.
//
// The emission is ATOMIC — derive, one controlled append, fold — and the
// derived slice is a statement-local temporary, so no later statement can
// extend the aliasing chain past the spec-pinned first append (a SECOND
// append after a reallocation would depend on the reallocation's
// unspecified new capacity; that family stays quotiented, recorded on the
// ledger). Constant a <= b <= c <= minLen(s) keeps the slice expression
// legal on every run. Termination: appending to t never changes len(s), so
// enclosing ranges over the base stay bounded; the fold ranges the
// temporary exactly once. Both slices are observed: the derived through the
// position-weighted int fold (rung 4's slice encoding), the base through
// its ordinary observation paths — where the aliasing effect, not cap,
// shows. cap itself remains UNOBSERVED (that quotient stands).
func (g *Generator) sliceTripleStmt(out *emitter) {
	g.resetRisk()
	s := &g.vars[pick(g.c, g.intElemSliceVars())]
	s.reads++
	// 0 <= a <= b <= c <= minLen, with cap = c-a >= 1 so the shared case is
	// reachable; minLen >= 2 for every slice declaration.
	a := g.c.draw(s.minLen - 1)          // [0, minLen-2]
	cc := a + 1 + g.c.draw(s.minLen-a)   // [a+1, minLen]
	b := a + g.c.draw(cc-a+1)            // [a, cc]
	tn := fmt.Sprintf("t%d", g.tmpSeq)
	en := fmt.Sprintf("e%d", g.tmpSeq+1)
	g.tmpSeq += 2
	g.mark("slices", "slice_triple", "short_decl")
	out.line("%s := %s[%d:%d:%d]", tn, s.name, a, b, cc)
	// The controlled append: within cap iff b < cc (shared write to s[b]),
	// at cap iff b == cc (reallocation, base untouched). The element draw
	// may spend this statement's own risk slot.
	g.resetRisk()
	elem := g.expr(*s.typ.Elem, g.cfg.ExprFuel-1)
	g.mark("slices", "append", "assignment")
	out.line("%s = append(%s, %s)", tn, tn, elem.text)
	g.resetRisk()
	ai, _ := g.pickVar(Int(0, false))
	acc := &g.vars[ai]
	acc.reads++
	g.mark("slices", "range", "conversions", "assignment", "control_flow", "short_decl")
	// The *31 chain on platform int is wrap-capable: width-dependent, the
	// same convention as the rung-4 aggregate folds.
	g.markWidthDep(acc.typ)
	out.open("for _, %s := range %s {", en, tn)
	out.line("%s = %s*31 + int(%s)", acc.name, acc.name, en)
	out.close()
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

// stringRangeFold ranges over a string variable: the index is the BYTE
// OFFSET of each rune and the value is the rune (int32) — the byte-offset
// semantics GoLean's g09 pins ("µx" yields offsets 0 and 2, not 0 and 1).
// String range order is spec-DEFINED (increasing byte offset), so unlike the
// map fold this one may be position-sensitive: acc += i*31 + int(r) keeps
// both offset and rune value discriminating. Termination is free — the range
// operand is evaluated once and strings only grow linearly — and the body is
// exactly the fold, so nothing executes in any order the spec leaves open.
func (g *Generator) stringRangeFold(out *emitter) {
	g.resetRisk()
	s := &g.vars[pick(g.c, g.varsOfShape(ShapeString, nil))]
	s.reads++
	// The accumulator is the guaranteed plain-int variable: the subject's
	// pool floor, or a helper's p0 (methods have no string vars, so the arm
	// never fires there).
	ai, _ := g.pickVar(Int(0, false))
	acc := &g.vars[ai]
	acc.reads++
	idx := fmt.Sprintf("i%d", g.loopSeq)
	r := fmt.Sprintf("r%d", g.loopSeq)
	g.loopSeq++
	g.mark("strings", "string_range", "range", "conversions", "assignment", "control_flow", "short_decl")
	// += on platform int is wrap-capable arithmetic: width-dependent by the
	// house convention (conservative over values, exact over operations).
	g.markWidthDep(acc.typ)
	out.open("for %s, %s := range %s {", idx, r, s.name)
	out.line("%s += %s*31 + int(%s)", acc.name, idx, r)
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
	if g.enabled("conversions") && g.corner != "kinds" {
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
