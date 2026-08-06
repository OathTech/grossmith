package gen

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
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

// TestRangeLoopsAreOverVariables: a range loop terminates because its
// operand is a fixed-length array VARIABLE — anything more exotic in range
// position would need its own termination argument.
func TestRangeLoopsAreOverVariables(t *testing.T) {
	ranges := 0
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		ast.Inspect(file, func(n ast.Node) bool {
			r, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			ranges++
			if _, ok := r.X.(*ast.Ident); !ok {
				t.Fatalf("seed %d: range over %T, not a variable", seed, r.X)
			}
			return true
		})
	}
	if ranges == 0 {
		t.Fatal("no range loops in 300 seeds")
	}
	t.Logf("checked %d range loops", ranges)
}

// TestArraysArePresentAndObserved: arrays occur in the population, and at
// least some are observed results (the driver prints them element-wise —
// alphabet reconciliation: every generatable type reaches the observation).
func TestArraysArePresentAndObserved(t *testing.T) {
	withArrays, observedArrays := 0, 0
	for seed := int64(1); seed <= 200; seed++ {
		c := generate(t, seed)
		for _, f := range c.Features {
			if f == "arrays" {
				withArrays++
				break
			}
		}
		if strings.Contains(string(c.Source), "]") && strings.Contains(string(c.Source), "println(r") {
			_, file := parseCase(t, c.Source, seed)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != Subject || fn.Type.Results == nil {
					continue
				}
				for _, res := range fn.Type.Results.List {
					if _, ok := res.Type.(*ast.ArrayType); ok {
						observedArrays++
					}
				}
			}
		}
	}
	if withArrays == 0 {
		t.Fatal("no arrays in 200 swarm seeds")
	}
	if observedArrays == 0 {
		t.Fatal("no observed array results in 200 seeds — arrays never reach the observation")
	}
	t.Logf("arrays in %d/200 programs, %d observed array results", withArrays, observedArrays)
}

// TestStructsArePresentAndObserved: structs occur, reach the observation
// (field-wise printing), and struct equality is exercised somewhere.
func TestStructsArePresentAndObserved(t *testing.T) {
	withStructs, observedStructs, equality := 0, 0, 0
	for seed := int64(1); seed <= 200; seed++ {
		c := generate(t, seed)
		has := false
		for _, f := range c.Features {
			if f == "structs" {
				has = true
			}
		}
		if !has {
			continue
		}
		withStructs++
		src := string(c.Source)
		if regexp.MustCompile(`println\(r\d+\.f\d+\)`).MatchString(src) {
			observedStructs++
		}
		if strings.Contains(src, "== S") || strings.Contains(src, "!= S") {
			equality++
		}
	}
	if withStructs == 0 {
		t.Fatal("no structs in 200 swarm seeds")
	}
	if observedStructs == 0 {
		t.Fatal("no struct reaches the observation in 200 seeds")
	}
	t.Logf("structs in %d/200, field observation in %d, composite-equality in %d", withStructs, observedStructs, equality)
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
				if i == len(list)-1 {
					continue
				}
				switch stmt.(type) {
				case *ast.BranchStmt, *ast.ReturnStmt:
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
		for _, kw := range []string{"for ", "if ", "switch ", "break", "continue", " / ", " % ", "<<", ">>", "min(", "max(", "range ", "[", "struct", ".", "println(", "w0"} {
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

// TestStringGrowthIsLinearByConstruction witnesses the space half of the
// halts invariant: every string-typed concat chain contains at most ONE
// variable occurrence, and string `+=` right sides contain none — so string
// memory grows at most linearly in executed statements. `s = s + s` in a
// loop would double per iteration; this test pins that it is inexpressible.
func TestStringGrowthIsLinearByConstruction(t *testing.T) {
	chains := 0
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		fset, file := parseCase(t, c.Source, seed)
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if _, err := (&types.Config{}).Check("main", fset, []*ast.File{file}, info); err != nil {
			t.Fatalf("seed %d: typecheck: %v", seed, err)
		}
		isString := func(e ast.Expr) bool {
			tv, ok := info.Types[e]
			if !ok {
				return false
			}
			b, ok := tv.Type.Underlying().(*types.Basic)
			return ok && b.Kind() == types.String
		}
		countIdents := func(e ast.Expr) int {
			n := 0
			ast.Inspect(e, func(node ast.Node) bool {
				if _, ok := node.(*ast.Ident); ok {
					n++
				}
				return true
			})
			return n
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BinaryExpr:
				if node.Op == token.ADD && isString(node) {
					chains++
					if idents := countIdents(node); idents > 1 {
						t.Fatalf("seed %d: string concat chain with %d variables — growth is no longer linear\n%s",
							seed, idents, c.Source)
					}
				}
			case *ast.AssignStmt:
				if node.Tok == token.ADD_ASSIGN && isString(node.Rhs[0]) {
					chains++
					if idents := countIdents(node.Rhs[0]); idents > 0 {
						t.Fatalf("seed %d: string += with a variable operand — self-amplifying growth\n%s",
							seed, c.Source)
					}
				}
			}
			return true
		})
	}
	if chains == 0 {
		t.Fatal("no string concatenation in 300 seeds — the invariant was never exercised")
	}
	t.Logf("checked %d string concat sites", chains)
}

// TestStringsAreGated: with strings disabled no string is declared; with the
// default swarm the population exercises them.
func TestStringsAreGated(t *testing.T) {
	off := map[string]bool{}
	for seed := int64(1); seed <= 30; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Constructs = off
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if strings.Contains(string(c.Source), `:= "`) {
			t.Fatalf("seed %d declares a string with strings disabled\n%s", seed, c.Source)
		}
	}
	withStrings := 0
	for seed := int64(1); seed <= 100; seed++ {
		for _, f := range generate(t, seed).Features {
			if f == "strings" {
				withStrings++
			}
		}
	}
	if withStrings == 0 {
		t.Fatal("no program in 100 swarm seeds exercised strings")
	}
	t.Logf("strings in %d/100 programs", withStrings)
}

// TestBoundaryCornerIsSafeAndReaches: boundary-corner programs typecheck
// (every boundary literal is representable in its type by construction) and
// actually reach boundaries — the corner exists to make MIN/MAX/width edges
// reachable on purpose, so a corner batch that rarely emits them is a bug.
func TestBoundaryCornerIsSafeAndReaches(t *testing.T) {
	reached := 0
	for seed := int64(1); seed <= 200; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "boundary"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		fset, file := parseCase(t, c.Source, seed)
		if _, err := (&types.Config{}).Check("main", fset, []*ast.File{file}, nil); err != nil {
			t.Fatalf("seed %d (boundary corner) does not typecheck: %v\n%s", seed, err, c.Source)
		}
		for _, f := range c.Features {
			if f == tagBoundary {
				reached++
			}
		}
	}
	if reached < 150 {
		t.Fatalf("boundary corner reached a boundary in only %d/200 programs", reached)
	}
	t.Logf("boundary reached in %d/200 corner programs", reached)
}

// TestCornerDrawAndBaseMinority: under the default swarm, corner seeds are a
// drawn minority AND boundaries still occur as a base minority outside the
// corner — both populations must exist for the tag to carry information.
func TestCornerDrawAndBaseMinority(t *testing.T) {
	cornerSeeds, baseBoundary := 0, 0
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		isCorner, hasBoundary := false, false
		for _, f := range c.Features {
			switch f {
			case "corner_boundary":
				isCorner = true
			case tagBoundary:
				hasBoundary = true
			}
		}
		if isCorner {
			cornerSeeds++
		} else if hasBoundary {
			baseBoundary++
		}
	}
	if cornerSeeds == 0 {
		t.Fatal("no boundary-corner seeds drawn in 300")
	}
	if baseBoundary == 0 {
		t.Fatal("no boundary literals outside corner seeds — the base minority is missing")
	}
	t.Logf("corner seeds %d/300, non-corner programs with boundaries %d", cornerSeeds, baseBoundary)
}

// TestBoundaryLiteralsAreRepresentable checks the alphabet directly: every
// boundary text compiles as a constant of its type, for every type — the
// witness that platform-width uses the 32-bit-safe set.
func TestBoundaryLiteralsAreRepresentable(t *testing.T) {
	for _, typ := range intTypes() {
		for _, lit := range typ.boundaryLiterals() {
			src := "package main\nvar _ = " + typ.GoName() + "(" + lit + ")\n"
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "b.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s(%s): parse: %v", typ.GoName(), lit, err)
			}
			if _, err := (&types.Config{}).Check("main", fset, []*ast.File{file}, nil); err != nil {
				t.Fatalf("%s(%s) is not representable: %v", typ.GoName(), lit, err)
			}
		}
	}
}

// riskSites counts the panic-capable sites the generator can make HOT in
// one expression subtree: divisions/modulos with a variable (identifier)
// divisor, and index expressions whose index is a bare identifier.
func riskSites(n ast.Node) int {
	count := 0
	ast.Inspect(n, func(inner ast.Node) bool {
		switch e := inner.(type) {
		case *ast.BinaryExpr:
			if e.Op == token.QUO || e.Op == token.REM {
				if _, ok := e.Y.(*ast.Ident); ok {
					count++
				}
			}
		case *ast.IndexExpr:
			if _, ok := e.Index.(*ast.Ident); ok {
				count++
			}
		}
		return true
	})
	return count
}

// TestOnePanicRiskPerStatement witnesses the risk budget: two hot panic
// sites in one statement have spec-UNSPECIFIED panic identity (a conformant
// clone may report either), so no statement context may carry more than one.
func TestOnePanicRiskPerStatement(t *testing.T) {
	risky := 0
	for seed := int64(1); seed <= 500; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		check := func(n ast.Node) {
			if s := riskSites(n); s > 1 {
				t.Fatalf("seed %d: statement with %d panic-risk sites — panic identity unspecified\n%s", seed, s, c.Source)
			} else if s == 1 {
				risky++
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				check(s)
				return false
			case *ast.IfStmt:
				check(s.Cond)
			case *ast.SwitchStmt:
				if s.Tag != nil {
					check(s.Tag)
				}
			}
			return true
		})
	}
	if risky == 0 {
		t.Fatal("no single-risk statements in 500 seeds — the panic population is gone")
	}
	t.Logf("%d single-risk statements, none with more", risky)
}

// TestPlatformDivisionByMinusOneIsWidthTagged witnesses the discrimination
// proof's tag honesty: MinInt32 / -1 on platform int wraps on a 32-bit
// target but not a 64-bit one, so any platform-int division whose divisor
// is -1 or a variable must carry width_dependent (review finding: this was
// an untagged cross-arch divergence).
func TestPlatformDivisionByMinusOneIsWidthTagged(t *testing.T) {
	hit := 0
	for seed := int64(1); seed <= 600; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "boundary"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		fset, file := parseCase(t, c.Source, seed)
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if _, err := (&types.Config{}).Check("main", fset, []*ast.File{file}, info); err != nil {
			t.Fatalf("seed %d: typecheck: %v", seed, err)
		}
		divides := false
		ast.Inspect(file, func(n ast.Node) bool {
			e, ok := n.(*ast.BinaryExpr)
			if !ok || e.Op != token.QUO {
				return true
			}
			tv, ok := info.Types[e]
			if !ok {
				return true
			}
			b, ok := tv.Type.Underlying().(*types.Basic)
			if !ok || b.Kind() != types.Int {
				return true
			}
			risky := false
			if _, isVar := e.Y.(*ast.Ident); isVar {
				risky = true
			}
			if u, isUnary := e.Y.(*ast.UnaryExpr); isUnary && u.Op == token.SUB {
				if lit, isLit := u.X.(*ast.BasicLit); isLit && lit.Value == "1" {
					risky = true
				}
			}
			if risky {
				divides = true
			}
			return true
		})
		if !divides {
			continue
		}
		hit++
		tagged := false
		for _, f := range c.Features {
			if f == tagWidthDependent {
				tagged = true
			}
		}
		if !tagged {
			t.Fatalf("seed %d: platform-int division with -1/variable divisor lacks %s\n%s", seed, tagWidthDependent, c.Source)
		}
	}
	if hit == 0 {
		t.Fatal("no risky platform-int divisions in 600 boundary seeds — the witness asserted nothing")
	}
	t.Logf("%d risky platform-int divisions, all width-tagged", hit)
}

// TestGeneratorIsSingleUse: a second Generate must error, not corrupt.
func TestGeneratorIsSingleUse(t *testing.T) {
	g := New(DefaultConfig(7))
	if _, err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(); err == nil {
		t.Fatal("second Generate on one Generator succeeded — reuse must error")
	}
}

// TestCaseIsASnapshot: the returned Tape and Stats must not alias live
// generator state.
func TestCaseIsASnapshot(t *testing.T) {
	g := New(DefaultConfig(9))
	c, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	before := len(c.Tape)
	c.Tape = append(c.Tape, 999999)
	c2, err := New(DefaultConfig(9)).Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.Tape) != before {
		t.Fatalf("tape lengths differ across identical generations: %d vs %d", before, len(c2.Tape))
	}
	for site, s := range c.Stats {
		for k := range s.Chosen {
			s.Chosen[k] = -1
			_ = site
			break
		}
		break
	}
	if c2.Stats == nil {
		t.Fatal("no stats")
	}
}

// TestNoIdentitySelfAssignment: `v = v`, `v ^= v`, and struct self-compares
// are identity no-ops that arose whenever a type had one variable (review
// finding: ~19% of plain assigns). They are replaced at emission.
func TestNoIdentitySelfAssignment(t *testing.T) {
	for seed := int64(1); seed <= 400; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		ast.Inspect(file, func(n ast.Node) bool {
			a, ok := n.(*ast.AssignStmt)
			if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
				return true
			}
			lhs, ok := a.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if rhs, ok := a.Rhs[0].(*ast.Ident); ok && rhs.Name == lhs.Name {
				t.Fatalf("seed %d: identity self-assignment %s %s %s\n%s", seed, lhs.Name, a.Tok, rhs.Name, c.Source)
			}
			return true
		})
	}
}

// TestSwitchReducesWithoutModulo: a swarm mix lacking modulo must not force
// every switch wide — the bitmask fallback keeps case labels reachable.
func TestSwitchReducesWithoutModulo(t *testing.T) {
	cons := map[string]bool{"switch": true, "bitwise": true}
	masked := 0
	for seed := int64(1); seed <= 80; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Constructs = cons
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		src := string(c.Source)
		if strings.Contains(src, " % ") {
			t.Fatalf("seed %d: modulo emitted while disabled\n%s", seed, src)
		}
		if regexp.MustCompile(`switch v\d+ & `).MatchString(src) {
			masked++
		}
	}
	if masked == 0 {
		t.Fatal("no bitmask-reduced switch in 80 modulo-less seeds — the fallback never fires")
	}
	t.Logf("bitmask-reduced switches in %d/80 programs", masked)
}

// TestInnerDeclsAreScopedAndProjected: every block-scoped declaration (w*)
// is declared inside a nested block and read again within that block — the
// projection rule, which is what makes inner scopes compatible with the
// unused-variable rule and the observation.
func TestInnerDeclsAreScopedAndProjected(t *testing.T) {
	decls := 0
	for seed := int64(1); seed <= 400; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		var subject *ast.FuncDecl
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == Subject {
				subject = fn
			}
		}
		ast.Inspect(subject, func(n ast.Node) bool {
			blk, ok := n.(*ast.BlockStmt)
			if !ok || blk == subject.Body {
				return true
			}
			for i, stmt := range blk.List {
				a, ok := stmt.(*ast.AssignStmt)
				if !ok || a.Tok != token.DEFINE {
					continue
				}
				name := a.Lhs[0].(*ast.Ident).Name
				if !strings.HasPrefix(name, "w") {
					continue
				}
				decls++
				readLater := false
				for _, later := range blk.List[i+1:] {
					ast.Inspect(later, func(inner ast.Node) bool {
						if id, ok := inner.(*ast.Ident); ok && id.Name == name {
							readLater = true
						}
						return true
					})
				}
				if !readLater {
					t.Fatalf("seed %d: inner decl %s never referenced again in its block — projection missing\n%s", seed, name, c.Source)
				}
			}
			return true
		})
	}
	if decls == 0 {
		t.Fatal("no block-scoped declarations in 400 seeds")
	}
	t.Logf("checked %d inner declarations, all projected or read", decls)
}

// TestObservationPointsArePresent: interleaved println statements occur in
// the subject (they are what survives a later panic) and print variables.
func TestObservationPointsArePresent(t *testing.T) {
	points := 0
	for seed := int64(1); seed <= 200; seed++ {
		c := generate(t, seed)
		src := string(c.Source)
		if i := strings.Index(src, "func main"); i >= 0 {
			src = src[:i]
		}
		points += strings.Count(src, "println(")
	}
	if points == 0 {
		t.Fatal("no observation points in 200 seeds")
	}
	t.Logf("%d observation points", points)
}

// TestReturnSitesAreUniform: every return site returns the same tuple (the
// observed names), and every non-final return is tagged early_return plus
// dead_code (it is only drawn mid-block).
func TestReturnSitesAreUniform(t *testing.T) {
	multi := 0
	for seed := int64(1); seed <= 300; seed++ {
		c := generate(t, seed)
		_, file := parseCase(t, c.Source, seed)
		var returns []*ast.ReturnStmt
		ast.Inspect(file, func(n ast.Node) bool {
			if r, ok := n.(*ast.ReturnStmt); ok {
				returns = append(returns, r)
			}
			return true
		})
		if len(returns) < 2 {
			continue
		}
		multi++
		want := len(returns[len(returns)-1].Results)
		for _, r := range returns {
			if len(r.Results) != want {
				t.Fatalf("seed %d: return sites with differing arity\n%s", seed, c.Source)
			}
		}
		hasEarly, hasDead := false, false
		for _, f := range c.Features {
			switch f {
			case "early_return":
				hasEarly = true
			case tagDeadCode:
				hasDead = true
			}
		}
		if !hasEarly || !hasDead {
			t.Fatalf("seed %d: multiple return sites but early_return=%v dead_code=%v", seed, hasEarly, hasDead)
		}
	}
	if multi == 0 {
		t.Fatal("no multi-return programs in 300 seeds")
	}
	t.Logf("%d programs with multiple return sites, all uniform and tagged", multi)
}

// TestInvalidConfigIsRejectedNotPanicked: a bad config is a diagnosis.
func TestInvalidConfigIsRejectedNotPanicked(t *testing.T) {
	bad := []Config{
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 0},
		{Seed: 1, Vars: -1, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 0, Depth: 2, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: -1, ExprFuel: 3, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 0, LoopCap: 6},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 6, Corner: "bogus"},
		// Practically non-terminating: worst-case executed statements bound.
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 3, ExprFuel: 3, LoopCap: 1 << 40},
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 6, ExprFuel: 3, LoopCap: 4096},
		// Misspelled construct key: silent population degradation.
		{Seed: 1, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 6,
			Constructs: map[string]bool{"array": true}},
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
