package gen

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func generate(t *testing.T, seed int64) Case {
	t.Helper()
	c, err := New(DefaultConfig(seed)).Generate()
	if err != nil {
		t.Fatalf("seed %d: %v", seed, err)
	}
	return c
}

func parseCase(t *testing.T, src []byte, seed int64) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "case.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("seed %d: parse: %v", seed, err)
	}
	return fset, file
}

// TestGeneratedProgramsTypecheck witnesses the compile bar (charter 5) and,
// through Go's unused-variable rule, the liveness discharge: cases are
// import-free, so go/types checking them in-process is the whole compile
// front end minus code generation.
func TestGeneratedProgramsTypecheck(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		fset, file := parseCase(t, c.Source, seed)
		conf := types.Config{}
		if _, err := conf.Check("main", fset, []*ast.File{file}, nil); err != nil {
			t.Fatalf("seed %d does not typecheck: %v\n%s", seed, err, c.Source)
		}
	}
}

// TestGenerationIsReproducible: a case is identified by (generator version,
// seed) — same seed, same bytes, same tape. The swarm mix is drawn through
// the tape, so it is covered too.
func TestGenerationIsReproducible(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		a, b := generate(t, seed), generate(t, seed)
		if !bytes.Equal(a.Source, b.Source) {
			t.Fatalf("seed %d is not reproducible", seed)
		}
		if len(a.Tape) != len(b.Tape) {
			t.Fatalf("seed %d: tapes differ in length", seed)
		}
		for i := range a.Tape {
			if a.Tape[i] != b.Tape[i] {
				t.Fatalf("seed %d: tapes diverge at draw %d", seed, i)
			}
		}
	}
}

// TestLoopsAreBoundedByConstruction witnesses the termination invariant: the
// bound is a literal, the step is always ++, and the index is never assigned
// in the body — checked structurally, not by waiting for a counterexample.
func TestLoopsAreBoundedByConstruction(t *testing.T) {
	loops := 0
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		ast.Inspect(file, func(n ast.Node) bool {
			loop, ok := n.(*ast.ForStmt)
			if !ok {
				return true
			}
			loops++
			cond, ok := loop.Cond.(*ast.BinaryExpr)
			if !ok {
				t.Fatalf("seed %d: loop condition is not a comparison: %T", seed, loop.Cond)
			}
			if _, ok := cond.Y.(*ast.BasicLit); !ok {
				t.Fatalf("seed %d: loop bound is not a literal: %T — termination is no longer structural", seed, cond.Y)
			}
			post, ok := loop.Post.(*ast.IncDecStmt)
			if !ok || post.Tok != token.INC {
				t.Fatalf("seed %d: loop step is not ++", seed)
			}
			index := cond.X.(*ast.Ident).Name
			ast.Inspect(loop.Body, func(inner ast.Node) bool {
				switch stmt := inner.(type) {
				case *ast.AssignStmt:
					for _, lhs := range stmt.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && id.Name == index {
							t.Fatalf("seed %d: loop index %s is assigned in the body", seed, index)
						}
					}
				case *ast.IncDecStmt:
					if id, ok := stmt.X.(*ast.Ident); ok && id.Name == index {
						t.Fatalf("seed %d: loop index %s is inc/decremented in the body", seed, index)
					}
				}
				return true
			})
			return true
		})
	}
	if loops == 0 {
		t.Fatal("no loops generated: the test asserted nothing")
	}
	t.Logf("checked %d loops", loops)
}

// TestObservedFloor: every program returns at least one value — a program
// observing nothing tests nothing.
func TestObservedFloor(t *testing.T) {
	for seed := int64(1); seed <= 100; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != Subject {
				continue
			}
			if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
				t.Fatalf("seed %d: subject observes nothing", seed)
			}
		}
	}
}

// TestDeadCodeIsTagged witnesses tag honesty for the weighted-not-banned
// taxonomy: a terminal statement anywhere but last makes the rest of its
// block dead — legal, generated as a minority, and it MUST carry dead_code.
func TestDeadCodeIsTagged(t *testing.T) {
	tagged, present := 0, 0
	for seed := int64(1); seed <= 400; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		deadCode := false
		check := func(list []ast.Stmt) {
			for i, stmt := range list {
				if _, ok := stmt.(*ast.BranchStmt); ok && i != len(list)-1 {
					deadCode = true
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BlockStmt:
				check(node.List)
			case *ast.CaseClause:
				check(node.Body)
			}
			return true
		})
		hasTag := false
		for _, f := range c.Features {
			if f == tagDeadCode {
				hasTag = true
			}
		}
		if deadCode {
			present++
			if !hasTag {
				t.Fatalf("seed %d has a mid-block terminal but no %s tag\n%s", seed, tagDeadCode, c.Source)
			}
			tagged++
		}
	}
	if present == 0 {
		t.Fatal("no mid-block terminals in 400 seeds — the weighted minority is not being generated")
	}
	t.Logf("%d/400 programs carry dead code, all tagged", present)
}

// TestLivenessTiersExist: the population must contain observed, feeder, and
// dead variables — a tier that never occurs is a constant, not a knob.
func TestLivenessTiersExist(t *testing.T) {
	feeder, dead := 0, 0
	for seed := int64(1); seed <= 200; seed++ {
		c := generate(t, seed)
		for _, f := range c.Features {
			switch f {
			case tagFeederValue:
				feeder++
			case tagDeadValue:
				dead++
			}
		}
	}
	if feeder == 0 || dead == 0 {
		t.Fatalf("liveness tiers missing from population: feeder=%d dead=%d over 200 seeds", feeder, dead)
	}
	t.Logf("feeder in %d/200, dead in %d/200 programs", feeder, dead)
}

// TestPanicRiskIsTagged: variable divisors are the deliberate panic-path
// minority; the tag must exist in the population (knowledge-as-data).
func TestPanicRiskIsTagged(t *testing.T) {
	risky := 0
	for seed := int64(1); seed <= 300; seed++ {
		for _, f := range generate(t, seed).Features {
			if f == tagPanicRisk {
				risky++
			}
		}
	}
	if risky == 0 {
		t.Fatal("no panic-risk sites in 300 seeds — the panic-path population is missing")
	}
	t.Logf("panic risk in %d/300 programs", risky)
}

// TestNonConstExprIsNeverConstant witnesses the constant-overflow invariant:
// every overflow-capable operator needs a non-constant operand, or Go
// rejects the program at compile time.
func TestNonConstExprIsNeverConstant(t *testing.T) {
	for seed := int64(1); seed <= 60; seed++ {
		g := New(DefaultConfig(seed))
		body := &emitter{indent: 1}
		g.declare(body)
		for _, typ := range scalarTypes() {
			for fuel := 0; fuel <= 3; fuel++ {
				if v := g.nonConstExpr(typ, fuel); v.constant {
					t.Fatalf("seed %d: nonConstExpr(%s, fuel=%d) returned a CONSTANT %q",
						seed, typ.GoName(), fuel, v.text)
				}
			}
		}
	}
}

// TestConstructGatingRespected: with every optional construct disabled the
// program is straight-line scalar assignments — and still valid.
func TestConstructGatingRespected(t *testing.T) {
	off := map[string]bool{}
	for seed := int64(1); seed <= 40; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Constructs = off
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		// Scan only the subject: the driver legitimately uses `if` to report
		// recovered panics.
		src := string(c.Source)
		if i := strings.Index(src, "func main"); i >= 0 {
			src = src[:i]
		}
		for _, kw := range []string{"for ", "if ", "switch ", "break", "continue", " / ", " % ", "<<", ">>", "min(", "max("} {
			if strings.Contains(src, kw) {
				t.Fatalf("seed %d: %q emitted with all optional constructs disabled\n%s", seed, kw, src)
			}
		}
		fset, file := parseCase(t, c.Source, seed)
		if _, err := (&types.Config{}).Check("main", fset, []*ast.File{file}, nil); err != nil {
			t.Fatalf("seed %d does not typecheck with constructs off: %v\n%s", seed, err, c.Source)
		}
	}
}

// TestInvalidConfigIsRejectedNotPanicked: a bad config is a diagnosis.
func TestInvalidConfigIsRejectedNotPanicked(t *testing.T) {
	bad := []Config{
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 0},
		{Seed: 1, Vars: -1, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 0, Depth: 2, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: -1, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 0, LoopCap: 6},
	}
	for i, cfg := range bad {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("config %d PANICKED: %v", i, r)
				}
			}()
			if _, err := New(cfg).Generate(); err == nil {
				t.Errorf("config %d accepted: %+v", i, cfg)
			}
		}()
	}
	if err := DefaultConfig(1).Validate(); err != nil {
		t.Fatalf("the default config fails its own validation: %v", err)
	}
}
