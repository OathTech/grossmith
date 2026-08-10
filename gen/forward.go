package gen

import (
	"fmt"
	"strings"
)

// The tuple-forwarding rung (witness arc W0; 2026-08-08 semantic-divergence
// review §1 — GoLean's BUG-049 habitat): sink(src(args)) calls where a
// multi-result helper call is forwarded as the COMPLETE argument list of a
// sink helper whose parameters are GENERATED to match the result tuple. Go's
// special case makes the call legal exactly when the results are individually
// assignable to the parameters — and assignability into an `any` slot is an
// IMPLICIT INTERFACE CONVERSION, the composition their fix pair
// (2e05313 -> 264520e) repaired: their splat path handed the machine raw
// values in interface-typed slots.
//
// The pair is built deliberately — the generator controls both sides, so
// callee signatures match forwardable tuples by construction rather than by
// search — and every sink call has the forwarding SHAPE tgN(tfN(...)) with
// MATCHING index (witnessed: TestTupleForwardEmitted asserts the paired
// index on every tg call). A cached sink may have several call sites
// (pairs are reused per form — mid-arc review finding 4 corrected the
// earlier "single call site" overstatement), but all of them forward the
// same paired source, and that single-call-SHAPE guarantee is what is
// load-bearing, twice:
//
//   - type assertions on any-slots are guaranteed to succeed (the generator
//     knows the dynamic type its paired source forwards), so the sink stays
//     panic-free — the same argument as implementer-restricted interface
//     assertions;
//   - constant indices into a variadic tail are always in range (len(xs) is
//     exactly the paired tail's component count).
//
// Both halves stay pure helpers: params in, results out, no globals, no
// output, panic-free. The variadic PARAMETER form exists here in the minimal
// shape tuple-forwarding needs (typed `...T` and `...any` sinks, folded by
// constant index); general variadic calls (spread, ordinary variadic
// arguments) remain deferred on the ledger (sx g24, c55).
//
// The form matrix (destinations × parameter types) is drawn per form:
//
//	dest:  fixed (a0, a1, ...) | variadic (xs ...E) | mixed (a0, xs ...E)
//	ptype: concrete (exact component types) | any (every slot boxed) |
//	       mixed (both kinds; masked for pure-variadic — one element type)
//	src:   each boxed slot may additionally draw srcBoxed (source result
//	       typed any), moving the boxing into the source — the
//	       conversion-free negative-control rows, (any, ...) -> (any, ...)
//
// which covers all six of the review's boundary-matrix rows, controls
// included (the (any, any) -> (any, any) control entered with the
// srcBoxed axis — mid-arc review finding 2; before it, every generated
// forward performed its boxing at the call and the generic-forwarding
// control was unrepresentable).

// fwdSlot is one forwarded tuple component and how each side types it.
type fwdSlot struct {
	comp  Type // the concrete component type underlying the slot
	boxed bool // the sink declares this slot (or the variadic element) as any
	// srcBoxed: the SOURCE's result type for this slot is any too — the
	// boxing happens at the source's return, inside the source, so the
	// forwarding call itself moves an interface value with NO implicit
	// conversion. This is the review matrix's negative-control row
	// ((any, any) -> (any, any)): a divergence here indicts generic
	// tuple forwarding (splat arity, temp ordering), not interface
	// boxing — the discriminator BUG-049's shape needs (mid-arc review
	// finding 2). Only legal under a boxed sink slot (any is not
	// assignable to a concrete parameter), enforced by construction in
	// drawForwardSlots.
	srcBoxed bool
}

// fwdPair is one generated source/sink helper pair plus the form it covers.
type fwdPair struct {
	srcName  string
	sinkName string
	params   []Type // source helper parameters (p0 int guaranteed first)
	dest     string // fixed | variadic | mixed
	ptype    string // concrete | any | mixed
	src      string // both function declarations
	// cost is the pair's worst-case executed statements per forwarding
	// call (E6): the source body PRICED through the emitters (it emits
	// real statements at depth 1, loops included — the re-review's
	// blocking finding was a hand count of 8 here against a measured
	// ~10k-statement source), plus the sink's fixed draw-free fold,
	// which stays counted.
	cost int64
}

// tupleForwardStmt emits `v += sink(src(args))` — the tuple-forwarded call
// statement. Pairs are created lazily at first use of a form and reused, so
// pair text exists in a program iff a forwarding call does (tag honesty both
// ways) and repeated draws stay inside the size budget.
func (g *Generator) tupleForwardStmt(out *emitter) {
	g.resetRisk()
	boxOK := g.enabled("interfaces", "assertion")
	varOK := g.enabled("slices", "index")
	dest := g.c.choose("fwd-dest", []arm{
		{name: "fixed", weight: 3, ok: true},
		// The variadic sink folds by indexing its tail slice — gated and
		// marked as the constructs that emits (audit F1's rule).
		{name: "variadic", weight: 2, ok: varOK},
		{name: "mixed", weight: 2, ok: varOK},
	}).name
	ptype := g.c.choose("fwd-ptype", []arm{
		{name: "concrete", weight: 1, ok: true},
		// Any-typed slots are the rung's point (BUG-049's shape): weighted
		// up so interface-typed destinations receiving concrete forwarded
		// components stay dense in the population. The assertion the fold
		// performs is an interface assertion — gated and marked as such.
		{name: "any", weight: 3, ok: boxOK},
		// A pure-variadic sink has ONE element type, so mixed slot kinds
		// are unrepresentable there.
		{name: "mixed", weight: 3, ok: boxOK && dest != "variadic"},
	}).name
	pair := g.forwardPair(dest, ptype)
	// The REAL per-call cost is knowable only once the pair exists
	// (a fresh source body is generated above), so affordability is
	// decided here, with the plain-assign fallback other arms use when
	// their premise fails — and charged BEFORE the argument expressions
	// can spend the pool under it (E6 re-review R2).
	if !g.afford(satMul(pair.cost, g.execMul)) {
		g.assign(out)
		return
	}
	g.charge(satMul(pair.cost, g.execMul))
	i, _ := g.pickVar(Int(0, false))
	target := &g.vars[i]
	target.reads++
	g.mark("helpers", "tuple_forward", "assignment", "ints")
	// The sink's *31 fold is window-capable at any input (the W4
	// keep-set), and its result carries no tracked bound — the target
	// accumulates unknown (fold reconciliation: W0 predates W4).
	g.markWidthDep(target.typ)
	g.writeBound(target, 0, "+")
	out.line("%s += %s(%s(%s))", target.name, pair.sinkName, pair.srcName,
		g.argList(pair.params, g.cfg.ExprFuel-1))
}

// forwardPair returns the pair for a form, building it on first use.
//
// TAPE-SHAPE NOTE (mid-arc review finding 11, accepted brittleness): a
// cache hit consumes zero draws while a miss consumes the slot draws plus
// a whole source-body generation, so the tape length is coupled to
// form-visit ORDER. Replay is exact regardless; the cost lands on the
// future shrinker (the same class as helper-count coupling, gen.go's
// generateHelpers note), recorded rather than redesigned away.
func (g *Generator) forwardPair(dest, ptype string) fwdPair {
	key := dest + "/" + ptype
	if i, ok := g.fwdByForm[key]; ok {
		return g.fwdPairs[i]
	}
	slots := g.drawForwardSlots(dest, ptype)
	srcName := fmt.Sprintf("tf%d", g.fwdSeq)
	sinkName := fmt.Sprintf("tg%d", g.fwdSeq)
	g.fwdSeq++
	params, srcText, srcCost := g.buildForwardSource(srcName, slots)
	pair := fwdPair{srcName: srcName, sinkName: sinkName, params: params,
		dest: dest, ptype: ptype,
		src: srcText + g.buildForwardSink(sinkName, slots, dest),
		// The source's PRICED per-call cost (its body carries real
		// statements, loops included) plus the sink's fixed, draw-free
		// fold — one line per slot with header and return slack.
		cost: satAdd(srcCost, int64(len(slots))+4)}
	if g.fwdByForm == nil {
		g.fwdByForm = map[string]int{}
	}
	g.fwdByForm[key] = len(g.fwdPairs)
	g.fwdPairs = append(g.fwdPairs, pair)
	return pair
}

// fwdComponentPool is the component-type pool, honestly gated by the fold
// each type needs: sized and unsigned kinds fold through int(x) — a
// conversion, masked with the kinds corner like every laundering site —
// and strings fold through len. Plain int folds bare and is always legal.
func (g *Generator) fwdComponentPool() []Type {
	pool := []Type{Int(0, false)}
	if g.enabled("conversions") && g.corner != "kinds" {
		for _, t := range intTypes() {
			if t.Bits != 0 || t.Unsigned {
				pool = append(pool, t)
			}
		}
	}
	if g.enabled("strings", "len") {
		pool = append(pool, Str())
	}
	return pool
}

// drawForwardSlots draws 2-3 components and their boxing under the form's
// constraints. A variadic tail has ONE element type, so the concrete
// variadic forms share a drawn type across the tail; `...any` tails admit a
// free mix. Boxing fix-ups are by construction, never a redraw.
func (g *Generator) drawForwardSlots(dest, ptype string) []fwdSlot {
	n := 2 + g.c.draw(2) // forwarding needs a real tuple
	slots := make([]fwdSlot, n)
	pool := g.fwdComponentPool()
	tailFrom := len(slots) // first slot the variadic tail receives
	switch dest {
	case "variadic":
		tailFrom = 0
	case "mixed":
		tailFrom = 1
	}
	for i := range slots {
		if ptype == "concrete" && i > tailFrom {
			slots[i].comp = slots[tailFrom].comp // shared tail element type
			continue
		}
		slots[i].comp = pick(g.c, pool)
	}
	switch ptype {
	case "any":
		for i := range slots {
			slots[i].boxed = true
		}
	case "mixed":
		if dest == "mixed" {
			// The canonical (T, ...any) shape — the review's
			// fixed-plus-variadic failure row.
			for i := 1; i < n; i++ {
				slots[i].boxed = true
			}
			break
		}
		for i := range slots {
			slots[i].boxed = g.c.chance(2)
		}
		// The form needs at least one boxed and one concrete slot.
		allBoxed, noneBoxed := true, true
		for _, s := range slots {
			if s.boxed {
				noneBoxed = false
			} else {
				allBoxed = false
			}
		}
		if allBoxed {
			slots[g.c.draw(n)].boxed = false
		} else if noneBoxed {
			slots[g.c.draw(n)].boxed = true
		}
	}
	// Source boxing (the negative-control axis): a boxed sink slot may
	// draw an any-typed SOURCE result too, moving the implicit conversion
	// from the forwarding call into the source's own return. Boxed-only
	// by construction — any is not assignable to a concrete parameter.
	for i := range slots {
		if slots[i].boxed && g.c.chance(2) {
			slots[i].srcBoxed = true
		}
	}
	return slots
}

// buildForwardSource is generateHelper with PRESCRIBED results: the source
// half of a forward pair returns exactly the component tuple its sink's
// parameters were built to receive. Same purity machinery: params only,
// pureMode masks every hot arm, p0 int is the non-constant base. A
// srcBoxed slot declares its RESULT type any — the return statement's
// concrete expression boxes at the source's return, so the forwarding
// call moves a ready-made interface value (the negative-control row).
func (g *Generator) buildForwardSource(name string, slots []fwdSlot) ([]Type, string, int64) {
	comps := make([]Type, len(slots))
	for i, s := range slots {
		comps[i] = s.comp
	}
	pool := scalarTypes()
	if g.enabled("strings") {
		pool = append(pool, Str())
	}
	params := []Type{Int(0, false)}
	for j := g.c.draw(2); j > 0; j-- {
		params = append(params, pick(g.c, pool))
	}
	savedVars, savedRisk := g.vars, g.riskSpent
	g.vars = nil
	g.pureMode = true
	g.pureBase = "p0"
	for j, pt := range params {
		g.vars = append(g.vars, binding{name: fmt.Sprintf("p%d", j), typ: pt})
	}
	body := &emitter{indent: 1}
	rs := make([]string, len(comps))
	// Priced like every other pure body (E6 re-review, the blocking
	// finding: this builder emits full statements at depth 1 — loops
	// included — and its cost was a hand count of 8, so a reused pair
	// carrying a big loop under-charged every cache-hit site; measured
	// 9,354,662 executed statements against the 4e6 ceiling at an
	// accepted config, seed 80).
	cost := g.priceBody(func() {
		stmts := 1 + g.c.draw(2)
		release := g.commitFloor(int64(stmts))
		for j := 0; j < stmts; j++ {
			g.stmtIn(body, 1, false, false)
		}
		release()
		for j, rt := range comps {
			rs[j] = g.expr(rt, g.cfg.ExprFuel).text
		}
	})
	body.line("return %s", strings.Join(rs, ", "))
	g.vars, g.riskSpent, g.pureMode, g.pureBase = savedVars, savedRisk, false, ""
	g.mark("helpers")

	ps := make([]string, len(params))
	for j, pt := range params {
		ps[j] = fmt.Sprintf("p%d %s", j, pt.GoName())
	}
	rt := make([]string, len(comps))
	for j, r := range comps {
		rt[j] = r.GoName()
		if slots[j].srcBoxed {
			// The result type is any; the returned concrete expression
			// boxes HERE, not at the forwarding call. Gated by the same
			// interfaces+assertion gate as the sink's boxed slots (the
			// slot cannot be srcBoxed without being boxed).
			rt[j] = "any"
		}
	}
	return params, fmt.Sprintf("func %s(%s) (%s) {\n%s}\n\n",
		name, strings.Join(ps, ", "), strings.Join(rt, ", "), body.buf.String()), cost
}

// buildForwardSink emits the sink half: parameters shaped by the form, and a
// body that folds every component into one int deterministically — the
// *31 chain, so position and value both discriminate. NO DRAWS: the sink is
// fully determined by its slots, which is what makes its assertions and
// constant tail indices safe by construction.
func (g *Generator) buildForwardSink(name string, slots []fwdSlot, dest string) string {
	headCount := len(slots)
	switch dest {
	case "variadic":
		headCount = 0
	case "mixed":
		headCount = 1
	}
	var ps []string
	for i := 0; i < headCount; i++ {
		tn := slots[i].comp.GoName()
		if slots[i].boxed {
			tn = "any"
		}
		ps = append(ps, fmt.Sprintf("a%d %s", i, tn))
	}
	if headCount < len(slots) {
		et := slots[headCount].comp.GoName()
		if slots[headCount].boxed {
			et = "any"
		}
		ps = append(ps, "xs ..."+et)
		// The tail is a slice the fold indexes — the arm gate checked both.
		g.mark("slices", "index")
	}
	body := &emitter{indent: 1}
	body.line("acc := 0")
	for i, s := range slots {
		access := fmt.Sprintf("a%d", i)
		if i >= headCount {
			// Constant index, in range by construction: the sink's ONLY
			// call site is the forwarding call, so len(xs) is exactly the
			// tail's component count.
			access = fmt.Sprintf("xs[%d]", i-headCount)
		}
		if s.boxed {
			// Guaranteed-success assertion (the generator controls the
			// dynamic type it forwards) — panic-free. This is the exact
			// site where a clone that skipped the implicit boxing hands
			// its machine a raw value (BUG-049). Noted any_slot (review
			// finding 7): a can't-fail assertion inflates the assertion/
			// interfaces judged-coverage numbers unless stratifiable —
			// the sharp empty-interface assertion is a different
			// population and the tag keeps them separable.
			access = fmt.Sprintf("%s.(%s)", access, s.comp.GoName())
			g.mark("interfaces", "assertion")
			g.note("any_slot")
		}
		term := access
		switch {
		case s.comp.Shape == ShapeString:
			g.mark("len", "strings")
			term = fmt.Sprintf("len(%s)", access)
		case s.comp.Bits != 0 || s.comp.Unsigned:
			g.mark("conversions")
			term = fmt.Sprintf("int(%s)", access)
		}
		g.mark("ints", "assignment")
		body.line("acc = acc*31 + %s", term)
	}
	// The *31 chain on platform int is wrap-capable — width-dependent, the
	// same convention as the aggregate folds.
	g.markWidthDep(Int(0, false))
	body.line("return acc")
	g.mark("short_decl", "return", "functions")
	return fmt.Sprintf("func %s(%s) int {\n%s}\n\n", name, strings.Join(ps, ", "), body.buf.String())
}
