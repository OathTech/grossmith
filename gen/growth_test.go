package gen

import (
	"go/ast"
	"regexp"
	"go/types"
	"testing"
)

// The E4 witnesses (audit P0/P1: cross-slice range/append amplification
// made the executed-statement bound unbounded — the measured worst case
// legal under the old rules was ~9e18 against a 4e6 budget). Growth is
// now masked while any slice range is open, and slice ranges are emitted
// only over slices with a small static length bound.

var tempSliceRe = regexp.MustCompile("^t[0-9]+$")

// TestNoGrowthUnderSliceRanges: structurally, no append call may appear
// inside the body of a range over a SLICE — to any target, not just the
// ranged one (the amplification fed through OTHER slices).
func TestNoGrowthUnderSliceRanges(t *testing.T) {
	ranges, checked := 0, 0
	for seed := int64(1); seed < 1500; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		checked++
		_, file, info := typecheckCase(t, c, seed, nil)
		ast.Inspect(file, func(n ast.Node) bool {
			r, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			tv, ok := info.Types[r.X]
			if !ok {
				return true
			}
			if _, isSlice := tv.Type.Underlying().(*types.Slice); !isSlice {
				return true
			}
			ranges++
			ast.Inspect(r.Body, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || id.Name != "append" {
					return true
				}
				// The one exemption: slice_triple's controlled append to
				// its STATEMENT-LOCAL temp (t<n> := s[a:b:c]). No binding
				// grows, the temp dies with the statement, and nothing
				// feeds back into any range's trip count. Every other
				// append target here is a leak.
				tgt, ok := call.Args[0].(*ast.Ident)
				if !ok || !tempSliceRe.MatchString(tgt.Name) {
					t.Fatalf("seed %d: append(%v) inside a slice-range body — the E4 growth mask leaked\n%s", seed, call.Args[0], c.Source)
				}
				return true
			})
			return true
		})
	}
	if ranges == 0 {
		t.Fatal("no slice ranges in 1500 seeds — the witness asserted nothing")
	}
	t.Logf("checked %d slice ranges over %d programs, none grow anything", ranges, checked)
}

// TestValidateSourceSizeBound: the execution formula is not the only
// cost — Stmts bounds source size too (audit: Stmts=4e6 at Depth=0
// passed the old check while being gigabytes of source).
func TestValidateSourceSizeBound(t *testing.T) {
	cfg := DefaultConfig(1)
	cfg.Stmts = 4097
	cfg.Depth = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Stmts=4097 accepted")
	}
}
