package gen

import "testing"

// The E6 white-box witnesses: the budget's arithmetic and accounting
// invariants. The measured half — instrumented execution counts covered
// by the charges — lives in execmeasure_test.go (TestChargedCoversMeasured).

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
			cfg.Depth, cfg.LoopCap = 3, 1 << 40
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

// TestForwardPairCostPinned: the tuple-forward arm's gate runs BEFORE
// the pair exists, so its worst-cost constant must dominate every real
// pair cost (slots are 2-3 by construction; cost = len(slots)+6).
func TestForwardPairCostPinned(t *testing.T) {
	if worst := int64(3) + 6; worst > fwdPairWorstCost {
		t.Fatalf("fwdPairWorstCost %d below the 3-slot pair cost %d", fwdPairWorstCost, worst)
	}
	// And against real pairs from a sweep that forces the arm.
	for seed := int64(1); seed <= 300; seed++ {
		cfg := DefaultConfig(seed)
		g := New(cfg)
		if _, err := g.Generate(); err != nil {
			t.Fatal(err)
		}
		for _, p := range g.fwdPairs {
			if p.cost > fwdPairWorstCost {
				t.Fatalf("seed %d: pair %s/%s cost %d exceeds the gate's %d", seed, p.srcName, p.sinkName, p.cost, fwdPairWorstCost)
			}
		}
	}
}
