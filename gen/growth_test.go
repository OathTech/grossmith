package gen

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"testing"
)

// The E4 witnesses (audit P0/P1: cross-slice range/append amplification
// made the executed-statement bound unbounded — the measured worst case
// legal under the old rules was ~9e18 against a 4e6 budget). Growth is
// now masked while any slice range is open, and slice ranges are emitted
// only over slices with a small static length bound.

var tempSliceRe = regexp.MustCompile("^t[0-9]+$")

// sweep scales a generation sweep for the mode: full breadth in the
// default suite, the stated reduced breadth under -short. The race CI
// leg runs -short, and race instrumentation multiplies single-threaded
// generation cost roughly tenfold while adding no detection here — the
// Generator is single-use and unshared by design — so the sweeps'
// breadth belongs to the full leg (2026-08-11: the first race run over
// the E5/E6/containment sweeps blew the 10-minute package alarm at
// full breadth). Every witness still asserts a non-vacuous population
// in both modes.
func sweep(full, short int64) int64 {
	if testing.Short() {
		return short
	}
	return full
}

// TestNoGrowthUnderSliceRanges: structurally, no append call may appear
// inside the body of a range over a SLICE — to any target, not just the
// ranged one (the amplification fed through OTHER slices).
func TestNoGrowthUnderSliceRanges(t *testing.T) {
	ranges, checked := 0, 0
	for seed := int64(1); seed < sweep(1500, 150); seed++ {
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
		t.Fatal("no slice ranges in the sweep — the witness asserted nothing")
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

// TestNoAppendAfterRangeInLoopNest is the E5 freeze witness, the arc-end
// review's refutation made a permanent check. Generation emits source in
// one pass, but enclosing loops RE-EXECUTE it: an append emitted after a
// range, inside the same loop nest, grows what the range walks on the
// next iteration — past a gate that already passed against the
// pre-growth bound (the review's seed 174813: 39,750 trips against a
// 2,000-trip gate, 14,372,767 executed statements at an accepted
// config). The freeze (loopFrozenSlices) bans appends to a nest-ranged
// slice until the nest closes, so the shape must not occur at all.
//
// Incidence, re-measured for this witness (pre-freeze generator, the
// review's config): exactly ONE program in seeds 150,000-210,000 — seed
// 174813 itself, reproduced byte-for-byte. The converse order never
// occurs on a real slice, because an in-loop append charges its trip
// product to the bound and the range gate then refuses it; slice_triple's
// statement-local temps are the one exempt append target. The stress
// sweep below covers the seed range the counterexample lives in.
//
// Pure-AST check, no typecheck needed: only slices are legal append
// targets, so "ranged X that is also appended somewhere" identifies the
// slice ranges that matter.
func TestNoAppendAfterRangeInLoopNest(t *testing.T) {
	run := func(t *testing.T, cfg func(int64) Config, first, seeds int64) {
		nests := 0
		for seed := first; seed < first+seeds; seed++ {
			c, err := New(cfg(seed)).Generate()
			if err != nil {
				continue
			}
			_, file := parseCase(t, c.Source, seed)
			ast.Inspect(file, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch l := n.(type) {
				case *ast.ForStmt:
					body = l.Body
				case *ast.RangeStmt:
					body = l.Body
				default:
					return true
				}
				// Outermost loop of a nest: scan its whole subtree once,
				// then stop descending (inner loops are part of the nest).
				nests++
				rangedAt := map[string]token.Pos{}
				ast.Inspect(body, func(m ast.Node) bool {
					switch v := m.(type) {
					case *ast.RangeStmt:
						if id, ok := v.X.(*ast.Ident); ok {
							if _, seen := rangedAt[id.Name]; !seen {
								rangedAt[id.Name] = v.Pos()
							}
						}
					case *ast.CallExpr:
						id, ok := v.Fun.(*ast.Ident)
						if !ok || id.Name != "append" || len(v.Args) == 0 {
							return true
						}
						tgt, ok := v.Args[0].(*ast.Ident)
						if !ok || tempSliceRe.MatchString(tgt.Name) {
							return true // slice_triple's statement-local temp
						}
						if at, ranged := rangedAt[tgt.Name]; ranged && v.Pos() > at {
							t.Fatalf("seed %d: append(%s) after a range over it in the same loop nest — the E5 freeze leaked\n%s",
								seed, tgt.Name, c.Source)
						}
					}
					return true
				})
				return false
			})
		}
		if nests == 0 {
			t.Fatal("no loop nests generated — the witness asserted nothing")
		}
		t.Logf("checked %d loop nests", nests)
	}
	t.Run("default config", func(t *testing.T) {
		run(t, DefaultConfig, 1, sweep(1500, 200))
	})
	t.Run("review counterexample config", func(t *testing.T) {
		// Stmts=1, Depth=2, LoopCap=250, seeds around 174813. The freeze
		// legitimately changes that seed's tape, so the regenerated
		// program differs from the review's artifact; the CLASS is what
		// the sweep refuses. The short window narrows but stays centered
		// on the counterexample seed.
		start, n := int64(170000), int64(10000)
		if testing.Short() {
			start, n = 174300, 1000
		}
		run(t, func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 1, 2, 250
			return cfg
		}, start, n)
	})
}

// TestLoopNestFreezeMechanism checks the freeze arithmetic directly —
// the sweep above is an integration net, but the counterexample class is
// a ~1-in-60,000 event, so the mechanism gets its own deterministic
// check: a slice ranged inside an open loop nest leaves the appendable
// set, and returns to it only when the nest closes.
func TestLoopNestFreezeMechanism(t *testing.T) {
	g := &Generator{}
	g.vars = []binding{
		{name: "v0", typ: Slice(Int(0, false)), minLen: 2, maxLenBound: 2},
		{name: "v1", typ: Slice(Int(0, false)), minLen: 3, maxLenBound: 3},
	}
	if got := len(g.appendableSlices()); got != 2 {
		t.Fatalf("clean state: %d appendable, want 2", got)
	}
	// A range over v0 inside a loop nest freezes v0 — and only v0.
	g.loopDepth = 1
	g.loopFrozenSlices = append(g.loopFrozenSlices, 0)
	if got := g.appendableSlices(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("frozen state: appendable %v, want [1]", got)
	}
	// Closing an INNER loop does not release: the outer nest can still
	// re-execute the range.
	g.loopDepth = 1
	g.releaseFrozenSlices()
	if got := g.appendableSlices(); len(got) != 1 {
		t.Fatalf("inner close released the freeze: appendable %v", got)
	}
	// Closing the whole nest releases.
	g.loopDepth = 0
	g.releaseFrozenSlices()
	if got := len(g.appendableSlices()); got != 2 {
		t.Fatalf("nest close: %d appendable, want 2", got)
	}
}

// TestSwapMovesStringLengthBounds (E5 re-review, the blocking finding):
// the multi-assign swap bypassed both writeBound and setStrLen, so
// `s1, s2 = s2, s1` moved a loop-grown string into a binding still
// carrying its small pre-swap byte bound — and the top-level fold gate
// then admitted the grown string (measured: 52 trips past a 48-trip
// gate at an accepted config). The bound must move with the value,
// under the same context rules as the numeric bound. White-box, like
// the freeze mechanism test: the structural gate witness cannot see
// bounds, so the arithmetic gets its own deterministic check.
func TestSwapMovesStringLengthBounds(t *testing.T) {
	mk := func() (*Generator, *binding, *binding) {
		g := &Generator{}
		g.vars = []binding{
			{name: "s0", typ: Str(), maxLenBound: 4},
			{name: "s1", typ: Str(), maxLenBound: 500},
		}
		return g, &g.vars[0], &g.vars[1]
	}
	g, a, b := mk()
	g.swapStrLen(a, b)
	if a.maxLenBound != 500 || b.maxLenBound != 4 {
		t.Fatalf("top-level swap: bounds %d/%d, want exchanged 500/4", a.maxLenBound, b.maxLenBound)
	}
	g, a, b = mk()
	g.condDepth = 1
	g.swapStrLen(a, b)
	if a.maxLenBound != 500 || b.maxLenBound != 500 {
		t.Fatalf("conditional swap: bounds %d/%d, want joined 500/500", a.maxLenBound, b.maxLenBound)
	}
	g, a, b = mk()
	g.loopDepth = 1
	g.swapStrLen(a, b)
	if a.maxLenBound != 0 || b.maxLenBound != 0 {
		t.Fatalf("in-loop swap: bounds %d/%d, want unknown 0/0", a.maxLenBound, b.maxLenBound)
	}
}

// TestStringRangeOperandsGated is the E5 string-gate witness: string
// growth previously had no gate at all — 17 of 3,000 DefaultConfig
// seeds emitted a string range over a concat-grown string (the same
// growth-feeds-a-range family E4 closed for slices). Now a string range
// takes a VARIABLE operand only at the top level of the subject, where
// emission order is execution order and the byte-length bound is
// therefore trustworthy; inside loops and inside pure bodies (helpers,
// methods — parameter lengths are caller-decided) the operand is a
// LITERAL, whose trip count nothing can move.
func TestStringRangeOperandsGated(t *testing.T) {
	varFolds, litFolds := 0, 0
	for seed := int64(1); seed <= sweep(3000, 300); seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			continue
		}
		_, file := parseCase(t, c.Source, seed)
		appended := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" && len(call.Args) > 0 {
					if tgt, ok := call.Args[0].(*ast.Ident); ok {
						appended[tgt.Name] = true
					}
				}
			}
			return true
		})
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			pure := fn.Name.Name != "fuzzSubject"
			var walk func(n ast.Node, loops int)
			walk = func(n ast.Node, loops int) {
				ast.Inspect(n, func(m ast.Node) bool {
					switch v := m.(type) {
					case *ast.ForStmt:
						walk(v.Body, loops+1)
						return false
					case *ast.RangeStmt:
						// String folds are the two-value `for i, r :=`
						// form; slice/array/map ranges never bind a
						// second variable named r<n>.
						isStringFold := false
						if val, ok := v.Value.(*ast.Ident); ok && strings.HasPrefix(val.Name, "r") {
							isStringFold = true
						}
						if isStringFold {
							switch x := v.X.(type) {
							case *ast.BasicLit:
								litFolds++
							case *ast.Ident:
								varFolds++
								if loops > 0 || pure {
									t.Fatalf("seed %d: string range over variable %s inside a loop or pure body (%s) — the E5 string gate leaked\n%s",
										seed, x.Name, fn.Name.Name, c.Source)
								}
								if appended[x.Name] {
									t.Fatalf("seed %d: string range over appended name %s\n%s", seed, x.Name, c.Source)
								}
							default:
								t.Fatalf("seed %d: string range over %T\n%s", seed, v.X, c.Source)
							}
						}
						walk(v.Body, loops+1)
						return false
					}
					return true
				})
			}
			walk(fn.Body, 0)
		}
	}
	if varFolds == 0 || litFolds == 0 {
		t.Fatalf("string-fold population starved: %d variable folds, %d literal folds — the witness asserted nothing", varFolds, litFolds)
	}
	t.Logf("string folds: %d over top-level variables, %d over literals", varFolds, litFolds)
}
