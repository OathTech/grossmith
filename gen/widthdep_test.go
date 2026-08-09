package gen

import (
	"testing"

	"grossmith/observe"
)

// The W4 witnesses: width_dependent precision via static magnitude
// bounds. The tag's soundness contract: a program WITHOUT the tag must
// never compute a value that could differ between GOARCH widths. The
// empirical half: on this (64-bit) host, an untagged program must never
// OBSERVE a plain int/uint whose magnitude reaches the 32-bit window —
// if one did, the bound tracking missed a write site and the cross-arch
// CI proof is at risk (the charter's revert condition). The observed
// values are not all values, so this is a strong screen, not a proof;
// the CI discrimination job remains the oracle.

func TestWidthDepUntaggedObservesInWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	// Wide sweep, real sample (arc-end review finding 5: 25 programs in
	// one narrow window missed all three under-tag mechanisms; the same
	// screen over 700 seeds caught four genuine breaches).
	untagged, checked := 0, 0
	for seed := int64(1); seed < 2000 && untagged < 120; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if hasFeature(c, "width_dependent") {
			continue
		}
		untagged++
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK {
			continue
		}
		for i, v := range doc.Values {
			switch v.Kind {
			case "int":
				// Strictly below MinInt32: MinInt32 itself is 32-bit
				// representable (finding 5's off-by-one).
				if v.GoType == "int" && (v.Int >= 1<<31 || v.Int < -(1<<31)) {
					t.Fatalf("seed %d: untagged program observed int %d at slot %d — width soundness broken\n%s",
						seed, v.Int, i, c.Source)
				}
			case "uint":
				if v.GoType == "uint" && v.Uint >= 1<<32 {
					t.Fatalf("seed %d: untagged program observed uint %d at slot %d — width soundness broken\n%s",
						seed, v.Uint, i, c.Source)
				}
			}
			checked++
		}
	}
	if untagged == 0 {
		t.Fatal("no untagged program in the sweep — W4's precision regressed to saturation")
	}
	t.Logf("screened %d untagged programs (%d observed values), all in-window", untagged, checked)
}

// TestWidthDepSaturation is the regression guard on the measured W4
// numbers: program-level saturation stays meaningfully below the old
// ~98%, and the untagged (off-tag) population — what the cross-arch CI
// job's discrimination check runs against — stays a real minority, not
// a rounding error. Measured 87.5% (n=2000) after the arc-end review's
// soundness hardening (unsigned underflow, conditional-write joins,
// loop-staleness widening); the pre-hardening decomposition (n=1000,
// 82.8%) attributed 287 tagged programs to window-fold constructs and
// 537 to boundary/unknown-value arithmetic, and the hardening moved a
// further ~5% from unsound-untagged to tagged. The charter's <50%
// aspiration is unreachable WITHOUT under-tagging because boundary
// literals (a deliberate, near-universal corpus feature) genuinely
// diverge when they meet plain-width arithmetic. Recorded in the
// ledger; the sound floor wins.
func TestWidthDepSaturation(t *testing.T) {
	tagged, total := 0, 0
	for seed := int64(53000); seed < 53400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		total++
		if hasFeature(c, "width_dependent") {
			tagged++
		}
	}
	rate := float64(tagged) / float64(total)
	if rate > 0.94 {
		t.Fatalf("width_dependent saturation %.0f%% — W4's precision regressed toward the old 98%%", 100*rate)
	}
	if rate < 0.40 {
		t.Fatalf("width_dependent saturation %.0f%% — suspiciously low; check for under-tagging before celebrating", 100*rate)
	}
	t.Logf("saturation %d/%d = %.0f%% (off-tag population %.0f%%)", tagged, total, 100*rate, 100*(1-rate))
}
