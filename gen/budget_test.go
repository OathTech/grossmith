package gen

import (
	"strings"
	"testing"
)

// The E6 white-box witnesses: the budget's arithmetic and accounting
// invariants. The measured half — instrumented execution counts covered
// by the charges — lives in execmeasure_test.go (TestChargedCoversMeasured).

// TestConfigBoundsKeepArithmeticExact (2026-08-10 audit, P1/P2: Depth
// and LoopCap had lower bounds only, so tripCap = 8*LoopCap could
// overflow and extreme Depth moved generator recursion cost outside the
// budget — "extreme configs degrade to cheap arms" was not true for
// every accepted integer). The caps are checked, and everything they
// admit keeps derived quantities positive and representable.
func TestConfigBoundsKeepArithmeticExact(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"LoopCap past the cap", Config{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: MaxLoopCap + 1}},
		{"LoopCap at MaxInt64", Config{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 1<<63 - 1}},
		{"Depth past the cap", Config{Seed: 1, Vars: 4, Stmts: 8, Depth: MaxDepth + 1, ExprFuel: 3, LoopCap: 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("accepted")
			}
			// And through the public entry point, as a diagnosis not a panic.
			if _, err := New(tc.cfg).Generate(); err == nil {
				t.Fatal("Generate accepted")
			}
		})
	}
	// At the boundaries the config ADMITS, every derived quantity stays
	// positive: tripCap does not overflow, and the per-site multiplier
	// saturates rather than wrapping.
	for _, cfg := range []Config{
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: MaxLoopCap, Swarm: true},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: MaxDepth, ExprFuel: 3, LoopCap: 6, Swarm: true},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: MaxDepth, ExprFuel: 3, LoopCap: MaxLoopCap, Swarm: true},
	} {
		g := New(cfg)
		if _, err := g.Generate(); err != nil {
			t.Fatalf("boundary config refused: %v", err)
		}
		if g.tripCap <= 0 || g.execMul <= 0 || g.budgetLeft < 0 {
			t.Fatalf("derived arithmetic went non-positive: tripCap=%d execMul=%d budgetLeft=%d",
				g.tripCap, g.execMul, g.budgetLeft)
		}
		if g.budgetBreached {
			t.Fatal("boundary config breached the budget accounting")
		}
	}
}

func TestBudgetArithmeticSaturates(t *testing.T) {
	if got := satAdd(budgetCap-1, 5); got != budgetCap {
		t.Fatalf("satAdd near cap: %d", got)
	}
	if got := satMul(budgetCap/2, 3); got != budgetCap {
		t.Fatalf("satMul near cap: %d", got)
	}
	if got := satAdd(0, 5); got != 5 {
		t.Fatalf("satAdd treats 0 as unknown: %d — budget zero is a real zero, unlike boundAdd", got)
	}
	if got := satMul(0, 5); got != 0 {
		t.Fatalf("satMul(0,5) = %d, want 0", got)
	}
}

// TestBudgetAccountingInvariants: across DefaultConfig and the configs
// the old worst-case formula refused, generation completes with the
// accounting intact — no breach (a mandatory line always had its floor),
// liability balanced back to zero (every commitFloor released), and the
// pool never oversubscribed.
func TestBudgetAccountingInvariants(t *testing.T) {
	configs := []struct {
		name string
		cfg  func(int64) Config
	}{
		{"default", DefaultConfig},
		{"review counterexample", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 1, 2, 250
			return cfg
		}},
		{"huge loopcap", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Depth, cfg.LoopCap = 3, MaxLoopCap
			return cfg
		}},
		{"deep", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 64, 6, 4096
			return cfg
		}},
		{"wide and deep", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 4096, 6, 4096
			return cfg
		}},
		// The E6 re-review's refutation family: forward-pair sources
		// carrying big loops, reused from inside loops (seed 80 measured
		// 9.35M executed statements against a 56k charge before the
		// source builder was priced).
		{"pair reuse", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 24, 3, 4096
			return cfg
		}},
	}
	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			seeds := int64(400)
			if tc.name == "wide and deep" {
				seeds = 40 // 4096 statements per program; keep the sweep sane
			}
			for seed := int64(1); seed <= seeds; seed++ {
				g := New(tc.cfg(seed))
				if _, err := g.Generate(); err != nil {
					t.Fatalf("seed %d refused under the budget: %v", seed, err)
				}
				if g.budgetBreached {
					t.Fatalf("seed %d: a mandatory line overdrew the pool — a floor is under-sized", seed)
				}
				if g.floorLiability != 0 {
					t.Fatalf("seed %d: floor liability %d after generation — an unreleased commitFloor", seed, g.floorLiability)
				}
				if g.budgetLeft < 0 || g.budgetLeft > ExecBudget {
					t.Fatalf("seed %d: budgetLeft %d outside [0, %d]", seed, g.budgetLeft, ExecBudget)
				}
			}
		})
	}
}

// TestForwardPairCostCoversBody (rewritten after the E6 re-review found
// the original vacuous — it compared the cost formula to a constant
// derived from the same formula, so the one hand-counted body escaped
// pricing unnoticed). The check is now against the BUILT TEXT: a pair's
// per-call cost must be at least the number of body lines in its two
// function declarations, since every emitted line executes at least
// once per call (loops make the priced cost strictly larger).
func TestForwardPairCostCoversBody(t *testing.T) {
	pairs := 0
	for seed := int64(1); seed <= 300; seed++ {
		g := New(DefaultConfig(seed))
		if _, err := g.Generate(); err != nil {
			t.Fatal(err)
		}
		for _, p := range g.fwdPairs {
			pairs++
			bodyLines := int64(strings.Count(p.src, "\n\t"))
			if p.cost < bodyLines {
				t.Fatalf("seed %d: pair %s/%s cost %d below its own %d body lines\n%s",
					seed, p.srcName, p.sinkName, p.cost, bodyLines, p.src)
			}
		}
	}
	if pairs == 0 {
		t.Fatal("no pairs generated — the witness asserted nothing")
	}
	t.Logf("checked %d pairs", pairs)
}
