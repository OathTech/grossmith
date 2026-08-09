package gen

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"
	"testing"

	"grossmith/observe"
)

// fwdForms are the legal cells of the W0 form matrix (destinations ×
// parameter types; pure-variadic×mixed is unrepresentable — one element
// type).
var fwdForms = []string{
	"fixed/concrete", "fixed/any", "fixed/mixed",
	"variadic/concrete", "variadic/any",
	"mixed/concrete", "mixed/any", "mixed/mixed",
}

// classifySink reads a tg* declaration's FORM back from its signature —
// independent of anything the emitter recorded (the F1-vacuity lesson:
// never assert only on tags the emitter itself sets).
func classifySink(fn *ast.FuncDecl) (dest, ptype string) {
	variadic, head, anySlots, concSlots := false, 0, 0, 0
	classify := func(e ast.Expr, n int) {
		if id, ok := e.(*ast.Ident); ok && id.Name == "any" {
			anySlots += n
		} else {
			concSlots += n
		}
	}
	for _, p := range fn.Type.Params.List {
		n := len(p.Names)
		if n == 0 {
			n = 1
		}
		if ell, ok := p.Type.(*ast.Ellipsis); ok {
			variadic = true
			classify(ell.Elt, 1)
			continue
		}
		head += n
		classify(p.Type, n)
	}
	dest = "fixed"
	if variadic {
		dest = "mixed"
		if head == 0 {
			dest = "variadic"
		}
	}
	switch {
	case anySlots > 0 && concSlots > 0:
		ptype = "mixed"
	case anySlots > 0:
		ptype = "any"
	default:
		ptype = "concrete"
	}
	return dest, ptype
}

// TestTupleForwardEmitted (witness arc W0; 2026-08-08 review §1): the
// grammar emits sink(src()) tuple-forwarded calls, every cell of the form
// matrix appears across the sweep (typed counting: the forwarded argument
// must BE a >=2-value tuple per go/types), every tg call in the population
// is the forwarding shape (the single-call-site guarantee that makes sink
// assertions and constant tail indices safe by construction), and the pair
// halves honor helper purity (no hot panic sites, no defer, no output).
func TestTupleForwardEmitted(t *testing.T) {
	cells := map[string]int{}
	tgCall := regexp.MustCompile(`\btg\d+\(`)
	tagged, srcBoxed := 0, 0
	for seed := int64(84000); seed < 84400; seed++ {
		// Force-include the rung's gate tags (the TestSliceTripleEmitted
		// precedent): density robust to draw-stream shifts; realized forms
		// still depend on draws and are what this witness counts.
		cfg := DefaultConfig(seed)
		cfg.Include = []string{"tuple_forward", "helpers", "interfaces", "assertion",
			"slices", "index", "conversions", "strings", "len"}
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "tuple_forward") {
			// Text-without-tag is the other honesty direction.
			if tgCall.Match(c.Source) {
				t.Fatalf("seed %d: tg call without tuple_forward tag\n%s", seed, c.Source)
			}
			continue
		}
		tagged++
		_, file, info := typecheckCase(t, c, seed, nil)
		sinks := map[string][2]string{}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			isPair := strings.HasPrefix(fn.Name.Name, "tf") || strings.HasPrefix(fn.Name.Name, "tg")
			if !isPair {
				continue
			}
			// Purity: same bar as helpers (pairs are helpers).
			if n := riskSites(fn.Body); n != 0 {
				t.Fatalf("seed %d: forward pair %s has %d hot panic sites\n%s", seed, fn.Name.Name, n, c.Source)
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.DeferStmt:
					t.Fatalf("seed %d: defer inside %s\n%s", seed, fn.Name.Name, c.Source)
				case *ast.CallExpr:
					if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "println" {
						t.Fatalf("seed %d: output inside %s\n%s", seed, fn.Name.Name, c.Source)
					}
				}
				return true
			})
			if strings.HasPrefix(fn.Name.Name, "tg") {
				d, p := classifySink(fn)
				sinks[fn.Name.Name] = [2]string{d, p}
			}
			if strings.HasPrefix(fn.Name.Name, "tf") && fn.Type.Results != nil {
				// The srcBoxed axis (review finding 2): sources whose
				// result types include any move the boxing out of the
				// forwarding call — the negative-control population.
				for _, r := range fn.Type.Results.List {
					if id, ok := r.Type.(*ast.Ident); ok && id.Name == "any" {
						srcBoxed++
						break
					}
				}
			}
		}
		forwards := 0
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || !strings.HasPrefix(fn.Name, "tg") {
				return true
			}
			// EVERY tg call must be the forwarding shape — the safety
			// argument rests on the sink having no other call sites.
			if len(call.Args) != 1 {
				t.Fatalf("seed %d: sink %s called with %d args — not a tuple forward\n%s",
					seed, fn.Name, len(call.Args), c.Source)
			}
			inner, ok := call.Args[0].(*ast.CallExpr)
			if !ok {
				t.Fatalf("seed %d: sink %s argument is %T, not a call\n%s", seed, fn.Name, call.Args[0], c.Source)
			}
			// The PAIRED-index half of the call-shape guarantee (review
			// finding 4): tgN's argument must be a call to tfN — the same
			// index — or a tuple-shaped helper could reach a sink whose
			// assertions its results do not satisfy.
			innerFn, ok := inner.Fun.(*ast.Ident)
			if !ok || innerFn.Name != "tf"+strings.TrimPrefix(fn.Name, "tg") {
				t.Fatalf("seed %d: sink %s forwarded from %v, want its paired tf\n%s",
					seed, fn.Name, inner.Fun, c.Source)
			}
			tv, ok := info.Types[ast.Expr(inner)]
			if !ok {
				t.Fatalf("seed %d: no type info for forwarded call", seed)
			}
			tup, ok := tv.Type.(*types.Tuple)
			if !ok || tup.Len() < 2 {
				t.Fatalf("seed %d: forwarded argument is not a multi-value tuple (%v)\n%s", seed, tv.Type, c.Source)
			}
			form, known := sinks[fn.Name]
			if !known {
				t.Fatalf("seed %d: call to undeclared sink %s", seed, fn.Name)
			}
			cells[form[0]+"/"+form[1]]++
			forwards++
			return true
		})
		if forwards == 0 {
			t.Fatalf("seed %d: tuple_forward tagged but no forwarding call\n%s", seed, c.Source)
		}
	}
	if tagged == 0 {
		t.Fatal("no seed in 84000..84400 drew tuple_forward — arm starved")
	}
	for _, form := range fwdForms {
		if cells[form] == 0 {
			t.Fatalf("form matrix cell %s never emitted over 400 seeds: %v", form, cells)
		}
	}
	if srcBoxed == 0 {
		t.Fatal("no source-boxed forward over 400 seeds — the negative-control axis starved")
	}
	t.Logf("tuple_forward in %d cases; matrix cells: %v; source-boxed sources: %d", tagged, cells, srcBoxed)
}

// TestTupleForwardMatrixObserved is the RUNTIME witness, one handcrafted
// case per matrix cell in the emitter's exact shape (the site-witness
// precedent): the forwarded components — interface-boxed ones included —
// must reach the observation with the fold value real Go computes. The
// boxed cells are exactly where GoLean's BUG-049 handed its machine a raw
// value in an any slot.
func TestTupleForwardMatrixObserved(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	driver := []byte((&Generator{}).driverSource([]binding{{name: "q0", typ: Int(0, false)}}))
	run := func(name, pair string, want int64) {
		subject := "package main\n\n" + pair + `
func fuzzSubject() int {
	v0 := 0
	v0 += tg0(tf0(7))
	return v0
}
`
		doc := runCase(t, Case{Source: []byte(subject), Driver: driver})
		if doc.Status != observe.StatusOK {
			t.Fatalf("%s: status %s", name, doc.Status)
		}
		if got := doc.Values[0].Int; got != want {
			t.Fatalf("%s: observed %d, want %d", name, got, want)
		}
	}
	// (7, "x") through concrete slots: (0*31+7)*31+len("x") = 218.
	run("fixed/concrete", `func tf0(p0 int) (int, string) {
	return p0, "x"
}

func tg0(a0 int, a1 string) int {
	acc := 0
	acc = acc*31 + a0
	acc = acc*31 + len(a1)
	return acc
}
`, 218)
	// Same tuple, both slots boxed — the review's minimal fixed witness.
	run("fixed/any", `func tf0(p0 int) (int, string) {
	return p0, "x"
}

func tg0(a0 any, a1 any) int {
	acc := 0
	acc = acc*31 + a0.(int)
	acc = acc*31 + len(a1.(string))
	return acc
}
`, 218)
	// The NEGATIVE CONTROL — (any, any) -> (any, any): both sides
	// interface-typed, so the forwarding call performs no implicit
	// conversion at all (the boxing happened at tf0's return). A failure
	// here indicts generic tuple forwarding — splat arity, temp ordering —
	// not interface boxing; it is what separates a BUG-049 recurrence from
	// a broader regression (review finding 2).
	run("fixed/any-src", `func tf0(p0 int) (any, any) {
	return p0, "x"
}

func tg0(a0 any, a1 any) int {
	acc := 0
	acc = acc*31 + a0.(int)
	acc = acc*31 + len(a1.(string))
	return acc
}
`, 218)
	// Mixed slots: concrete int, boxed string — the (int, any) row.
	run("fixed/mixed", `func tf0(p0 int) (int, string) {
	return p0, "x"
}

func tg0(a0 int, a1 any) int {
	acc := 0
	acc = acc*31 + a0
	acc = acc*31 + len(a1.(string))
	return acc
}
`, 218)
	// Typed variadic: (9, 4) as ...int16 — (0*31+9)*31+4 = 283.
	run("variadic/concrete", `func tf0(p0 int) (int16, int16) {
	return int16(p0 + 2), int16(4)
}

func tg0(xs ...int16) int {
	acc := 0
	acc = acc*31 + int(xs[0])
	acc = acc*31 + int(xs[1])
	return acc
}
`, 283)
	// (7, "x") into ...any — the review's minimal variadic witness.
	run("variadic/any", `func tf0(p0 int) (int, string) {
	return p0, "x"
}

func tg0(xs ...any) int {
	acc := 0
	acc = acc*31 + xs[0].(int)
	acc = acc*31 + len(xs[1].(string))
	return acc
}
`, 218)
	// (7, 3, 5) into (int, ...int8): ((0*31+7)*31+3)*31+5 = 6825.
	run("mixed/concrete", `func tf0(p0 int) (int, int8, int8) {
	return p0, int8(3), int8(5)
}

func tg0(a0 int, xs ...int8) int {
	acc := 0
	acc = acc*31 + a0
	acc = acc*31 + int(xs[0])
	acc = acc*31 + int(xs[1])
	return acc
}
`, 6825)
	// (7, "x") into (any, ...any).
	run("mixed/any", `func tf0(p0 int) (int, string) {
	return p0, "x"
}

func tg0(a0 any, xs ...any) int {
	acc := 0
	acc = acc*31 + a0.(int)
	acc = acc*31 + len(xs[0].(string))
	return acc
}
`, 218)
	// (7, "x", 9) into (int, ...any) — the (int, ...any) row:
	// ((0*31+7)*31+1)*31+9 = 6767.
	run("mixed/mixed", `func tf0(p0 int) (int, string, int) {
	return p0, "x", p0 + 2
}

func tg0(a0 int, xs ...any) int {
	acc := 0
	acc = acc*31 + a0
	acc = acc*31 + len(xs[0].(string))
	acc = acc*31 + xs[1].(int)
	return acc
}
`, 6767)
}

// TestTupleForwardGeneratedRuns: GENERATED tuple-forward cases build and
// run end to end (the handcrafted matrix pins shapes; this pins that the
// emitter's real composition — pair, call site, observation — does). Runs
// several, not one (review finding 9: an n=1 runtime witness for
// by-construction claims), capped to keep the gate fast.
func TestTupleForwardGeneratedRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	ran := 0
	for seed := int64(84000); seed < 84100 && ran < 5; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Include = []string{"tuple_forward", "helpers", "interfaces", "assertion"}
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "tuple_forward") {
			continue
		}
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK && doc.Status != observe.StatusPanic {
			t.Fatalf("seed %d: status %s", seed, doc.Status)
		}
		ran++
	}
	if ran == 0 {
		t.Fatal("no tuple_forward case in 84000..84100")
	}
	t.Logf("ran %d generated tuple-forward cases", ran)
}

// TestTupleForwardExcludedLeaksNothing (capability-profile honesty): with
// the tag excluded, ZERO pair text reaches a subject — no tf/tg
// identifiers, no variadic ellipsis, no `any`.
func TestTupleForwardExcludedLeaksNothing(t *testing.T) {
	leak := regexp.MustCompile(`\bt[fg]\d+\b|\.\.\.|\bany\b`)
	for seed := int64(84000); seed < 84150; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Exclude = []string{"tuple_forward"}
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if m := leak.Find(c.Source); m != nil {
			t.Fatalf("seed %d: %q leaked with tuple_forward excluded\n%s", seed, m, c.Source)
		}
	}
}
