package gen

import (
	"fmt"
	"strings"
	"testing"

	"grossmith/observe"
)

// Exercise the real return/fold/recovery emitters with controlled mutations.
// Random seed sweeps alone cannot tell whether an aggregate slot contains
// state from this exit or a zero substituted for the whole container.
func aggregateExitCase(exit string, mutate bool) Case {
	g := New(DefaultConfig(1))
	g.vars = []binding{
		{name: "v0", typ: Int(0, false), observed: true},
		{name: "v1", typ: Slice(Int(0, false)), aggObserved: true},
		{name: "v2", typ: Map(Int(0, false), Bool()), aggObserved: true},
	}
	g.wrapped = exit == "wrapper"
	body := &emitter{indent: 1}
	body.line("v0 := 7")
	body.line("v1 := []int{10, 20}")
	body.line("v2 := map[int]bool{1: false, 2: false}")
	if g.wrapped {
		body.line("psite := 1")
		g.emitWrapperDefer(body)
	}
	if mutate {
		body.line("v1[0] = 99")
		body.line("v2[1] = true")
	}
	switch exit {
	case "early":
		// Two return sites at the same lexical depth also exercise the
		// scoping of the synthetic aggregate locals.
		g.earlyReturn(body)
		g.earlyReturn(body)
	case "loop-early":
		body.open("for i := 0; i < 4; i++ {")
		body.open("if i == 3 {")
		g.earlyReturn(body)
		body.close()
		body.close()
	case "wrapper", "panic":
		body.line("v0 = 0")
		body.line("v0 = 1 / v0")
	case "guarded":
		body.open("func() {")
		body.line("defer func() { _ = recover() }()")
		body.line("v0 = 0")
		body.line("v0 = 1 / v0")
		body.dedent()
		body.line("}()")
	}
	observed := g.observe(body)
	results := make([]string, len(observed))
	for i, b := range observed {
		results[i] = b.typ.GoName()
		if g.wrapped {
			results[i] = fmt.Sprintf("q%d %s", i, results[i])
		}
	}
	source := fmt.Sprintf("package main\nfunc fuzzSubject() (%s) {\n%s}\n", strings.Join(results, ", "), body.buf.String())
	return Case{Source: []byte(source), Driver: []byte(g.driverSource(observed))}
}

func TestAggregateObservationAtEveryExit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	for _, exit := range []string{"normal", "early", "loop-early", "guarded", "wrapper", "panic"} {
		t.Run(exit, func(t *testing.T) {
			before := runCase(t, aggregateExitCase(exit, false))
			after := runCase(t, aggregateExitCase(exit, true))
			if exit == "panic" {
				// Without a recovery wrapper the function does not return
				// values. This path deliberately has only panic evidence.
				if before.Status != observe.StatusPanic || after.Status != observe.StatusPanic {
					t.Fatal("unrecovered panic did not reach the driver")
				}
				return
			}
			if before.Status != observe.StatusOK || after.Status != observe.StatusOK {
				t.Fatalf("status before=%s after=%s", before.Status, after.Status)
			}
			// Slice: len*31^2 + first*31 + second. Map: len +
			// sum(key*31 + bool). Both mutations must be visible.
			for i, pair := range [][2]int64{{2252, 5011}, {95, 96}} {
				if got := before.Values[i+1].Int; got != pair[0] {
					t.Errorf("aggregate %d before mutation = %d, want %d", i, got, pair[0])
				}
				if got := after.Values[i+1].Int; got != pair[1] {
					t.Errorf("aggregate %d after mutation = %d, want %d", i, got, pair[1])
				}
			}
			if exit == "wrapper" && (before.Values[3].Int != 1 || after.Values[3].Int != 1) {
				t.Fatal("aggregate snapshot displaced the panic-site slot")
			}
		})
	}
}

func TestAggregateExitGeneratedPrograms(t *testing.T) {
	seenEarly, seenWrapper := false, false
	measured := 0
	for seed := int64(1); seed <= 80; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Swarm, cfg.Corner = false, "none"
		cfg.NoObserve = []Shape{ShapeSlice, ShapeMap}
		g := New(cfg)
		c, err := g.Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		typecheckCase(t, c, seed, nil)
		seenEarly = seenEarly || hasFeature(c, "early_return")
		seenWrapper = seenWrapper || hasFeature(c, "recover_wrapper")
		// Full all-construct subjects include aggregate exits composed
		// with appends, nested control flow and deferred observations.
		if !testing.Short() && measured < 6 && hasFeature(c, "aggregate_observed") &&
			(hasFeature(c, "early_return") || hasFeature(c, "recover_wrapper")) {
			charged := int64(ExecBudget) - g.budgetLeft
			if got := measureExec(t, c); got > charged {
				t.Fatalf("seed %d: %d executed statements exceed charge %d", seed, got, charged)
			}
			measured++
		}
	}
	if !seenEarly || !seenWrapper {
		t.Fatalf("missing aggregate exit composition: early=%v wrapper=%v", seenEarly, seenWrapper)
	}
}
