package gen

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The E5 measurement witness: generated subjects, instrumented with an
// executed-statement counter and RUN. The arc-end review refuted the E4
// bound by measurement (seed 174813: 14,372,767 executed statements at
// an accepted config); the freeze and the string gates close the
// measured mechanisms, and this file keeps the measurement method in the
// suite so the claim can never again drift from what programs actually
// do. NOTE the honest scope: a finite sweep bounds these seeds, not all
// tapes — the universal bound is the subject of
// docs/2026-08-09_execution-bound-design-note.md.

// instrumentExec inserts `exeCtr++` before every statement of every
// block and a reporting defer at the top of fuzzSubject; exeCtr and the
// reporter live in an extra file so the subject stays import-free.
func instrumentExec(t *testing.T, src []byte) []byte {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "subject.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	skip := map[*ast.BlockStmt]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.SwitchStmt:
			skip[s.Body] = true // holds CaseClauses, not statements
		case *ast.TypeSwitchStmt:
			skip[s.Body] = true
		}
		return true
	})
	counter := func() ast.Stmt {
		return &ast.IncDecStmt{X: ast.NewIdent("exeCtr"), Tok: token.INC}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch b := n.(type) {
		case *ast.BlockStmt:
			if skip[b] {
				return true
			}
			var out []ast.Stmt
			for _, s := range b.List {
				out = append(out, counter(), s)
			}
			b.List = out
		case *ast.CaseClause:
			var out []ast.Stmt
			for _, s := range b.Body {
				out = append(out, counter(), s)
			}
			b.Body = out
		}
		return true
	})
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "fuzzSubject" {
			report := &ast.DeferStmt{Call: &ast.CallExpr{Fun: ast.NewIdent("reportExeCtr")}}
			fn.Body.List = append([]ast.Stmt{report}, fn.Body.List...)
		}
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// measureExec builds and runs one instrumented case, returning its
// executed-statement count. The counter reports on stderr via a defer,
// so a panicking subject still reports.
func measureExec(t *testing.T, c Case) int64 {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"subject.go": instrumentExec(t, c.Source),
		"driver.go":  c.Driver,
		"go.mod":     []byte("module grossmith-cases\n\ngo 1.26\n"),
		"count.go": []byte(`package main

import (
	"fmt"
	"os"
)

var exeCtr int64

func reportExeCtr() { fmt.Fprintf(os.Stderr, "EXECSTMTS %d\n", exeCtr) }
`),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", "case.bin", ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	run := exec.Command(filepath.Join(dir, "case.bin"))
	var stderr bytes.Buffer
	run.Stderr = &stderr
	run.Stdout = &bytes.Buffer{}
	_ = run.Run() // panic outcomes still report via the defer
	for _, line := range strings.Split(stderr.String(), "\n") {
		if rest, ok := strings.CutPrefix(line, "EXECSTMTS "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatalf("no counter line on stderr: %s", stderr.String())
	return 0
}

// TestExecutedStatementsMeasured replays the review's counterexample
// seed and neighbors under the statement counter. Seed 174813's
// regenerated program (the freeze changed its tape) must sit far below
// the 14,372,767 the review measured — the 4e6 ceiling is the bar used
// here because it is the figure the config claims. A small DefaultConfig
// sweep rides along; measured reality there is tiny (median 54, max 288
// over 60 seeds when this was written).
func TestExecutedStatementsMeasured(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	stress := func(seed int64) Config {
		cfg := DefaultConfig(seed)
		cfg.Stmts, cfg.Depth, cfg.LoopCap = 1, 2, 250
		return cfg
	}
	var worst int64
	measured := 0
	check := func(cfg Config, seed int64) {
		c, err := New(cfg).Generate()
		if err != nil {
			return
		}
		n := measureExec(t, c)
		measured++
		if n > worst {
			worst = n
		}
		if n > 4_000_000 {
			t.Fatalf("seed %d: %d executed statements, past the ceiling the config claims\n%s", seed, n, c.Source)
		}
	}
	for seed := int64(174808); seed <= 174818; seed++ {
		check(stress(seed), seed)
	}
	for seed := int64(1); seed <= 10; seed++ {
		check(DefaultConfig(seed), seed)
	}
	if measured == 0 {
		t.Fatal("nothing measured")
	}
	t.Logf("measured %d subjects, worst %d executed statements", measured, worst)
}

// TestChargedCoversMeasured is E6's parity witness — the strongest one:
// the budget's charges must DOMINATE what the program actually executes,
// for real programs, measured by instrumentation. An emitter that emits
// more executions than it charges shows up here as measured > charged.
// Configs include the ones the old worst-case formula refused.
func TestChargedCoversMeasured(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	configs := []struct {
		name  string
		cfg   func(int64) Config
		seeds []int64
	}{
		{"default", DefaultConfig, []int64{1, 2, 3, 4, 5, 6, 7, 8}},
		{"review counterexample", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 1, 2, 250
			return cfg
		}, []int64{174810, 174813, 174816}},
		{"deep", func(seed int64) Config {
			cfg := DefaultConfig(seed)
			cfg.Stmts, cfg.Depth, cfg.LoopCap = 64, 6, 4096
			return cfg
		}, []int64{1, 2, 3}},
	}
	var worstRatio float64
	for _, tc := range configs {
		for _, seed := range tc.seeds {
			g := New(tc.cfg(seed))
			c, err := g.Generate()
			if err != nil {
				t.Fatalf("%s seed %d: %v", tc.name, seed, err)
			}
			if g.budgetBreached {
				t.Fatalf("%s seed %d: budget accounting breached", tc.name, seed)
			}
			charged := int64(ExecBudget) - g.budgetLeft
			measured := measureExec(t, c)
			if measured > charged {
				t.Fatalf("%s seed %d: measured %d executed statements but only %d charged — an emitter under-charges\n%s",
					tc.name, seed, measured, charged, c.Source)
			}
			if measured > ExecBudget {
				t.Fatalf("%s seed %d: measured %d past the ceiling", tc.name, seed, measured)
			}
			if r := float64(measured) / float64(charged); r > worstRatio {
				worstRatio = r
			}
		}
	}
	t.Logf("measured/charged worst ratio %.3f (1.0 would mean an exactly-priced program)", worstRatio)
}
