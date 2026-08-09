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
// search — and the sink is called ONLY through the forwarding call
// (witnessed: TestTupleForwardEmitted fails on any other tg call shape).
// That single-call-site guarantee is load-bearing twice:
//
//   - type assertions on any-slots are guaranteed to succeed (the generator
//     knows the dynamic type it forwards), so the sink stays panic-free —
//     the same argument as implementer-restricted interface assertions;
//   - constant indices into a variadic tail are always in range (len(xs) is
//     exactly the forwarded tail's component count).
//
// Both halves stay pure helpers: params in, results out, no globals, no
// output, panic-free. The variadic PARAMETER form exists here in the minimal
// shape tuple-forwarding needs (typed `...T` and `...any` sinks, folded by
// constant index); general variadic calls (spread, ordinary variadic
// arguments) remain deferred on the ledger (sx g24, c55).
//
// The form matrix (destinations × parameter types) is drawn per call site:
//
//	dest:  fixed (a0, a1, ...) | variadic (xs ...E) | mixed (a0, xs ...E)
//	ptype: concrete (exact component types) | any (every slot boxed) |
//	       mixed (both kinds; masked for pure-variadic — one element type)
//
// which covers all six of the review's boundary-matrix rows, controls
// included.

// fwdSlot is one forwarded tuple component and how the sink receives it.
type fwdSlot struct {
	comp  Type // the concrete component type the source returns
	boxed bool // the sink declares this slot (or the variadic element) as any
}

// fwdPair is one generated source/sink helper pair plus the form it covers.
type fwdPair struct {
	srcName  string
	sinkName string
	params   []Type // source helper parameters (p0 int guaranteed first)
	dest     string // fixed | variadic | mixed
	ptype    string // concrete | any | mixed
	src      string // both function declarations
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
	i, _ := g.pickVar(Int(0, false))
	target := &g.vars[i]
	target.reads++
	g.mark("helpers", "tuple_forward", "assignment", "ints")
	g.markWidthDep(target.typ)
	out.line("%s += %s(%s(%s))", target.name, pair.sinkName, pair.srcName,
		g.argList(pair.params, g.cfg.ExprFuel-1))
}

// forwardPair returns the pair for a form, building it on first use.
func (g *Generator) forwardPair(dest, ptype string) fwdPair {
	key := dest + "/" + ptype
	if i, ok := g.fwdByForm[key]; ok {
		return g.fwdPairs[i]
	}
	slots := g.drawForwardSlots(dest, ptype)
	srcName := fmt.Sprintf("tf%d", g.fwdSeq)
	sinkName := fmt.Sprintf("tg%d", g.fwdSeq)
	g.fwdSeq++
	comps := make([]Type, len(slots))
	for i, s := range slots {
		comps[i] = s.comp
	}
	params, srcText := g.buildForwardSource(srcName, comps)
	pair := fwdPair{srcName: srcName, sinkName: sinkName, params: params,
		dest: dest, ptype: ptype,
		src: srcText + g.buildForwardSink(sinkName, slots, dest)}
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
	return slots
}

// buildForwardSource is generateHelper with PRESCRIBED results: the source
// half of a forward pair returns exactly the component tuple its sink's
// parameters were built to receive. Same purity machinery: params only,
// pureMode masks every hot arm, p0 int is the non-constant base.
func (g *Generator) buildForwardSource(name string, comps []Type) ([]Type, string) {
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
	for j := 1 + g.c.draw(2); j > 0; j-- {
		g.stmtIn(body, 1, false, false)
	}
	rs := make([]string, len(comps))
	for j, rt := range comps {
		rs[j] = g.expr(rt, g.cfg.ExprFuel).text
	}
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
	}
	return params, fmt.Sprintf("func %s(%s) (%s) {\n%s}\n\n",
		name, strings.Join(ps, ", "), strings.Join(rt, ", "), body.buf.String())
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
			// its machine a raw value (BUG-049).
			access = fmt.Sprintf("%s.(%s)", access, s.comp.GoName())
			g.mark("interfaces", "assertion")
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
