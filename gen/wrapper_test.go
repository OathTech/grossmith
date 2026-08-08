package gen

import (
	"bytes"
	"go/ast"
	"go/types"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"grossmith/observe"
)

func hasFeature(c Case, tag string) bool {
	for _, f := range c.Features {
		if f == tag {
			return true
		}
	}
	return false
}

// runCase builds and runs one generated case, returning the parsed
// observation document (driver_test pattern; go.mod per dir per the
// module-mode fix).
func runCase(t *testing.T, c Case) observe.Document {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"subject.go": c.Source,
		"driver.go":  c.Driver,
		"go.mod":     []byte("module grossmith-cases\n\ngo 1.26\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-o", "case.bin", ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	run := exec.Command(filepath.Join(dir, "case.bin"))
	run.Env = []string{}
	var stdout bytes.Buffer
	run.Stdout = &stdout
	if err := run.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	doc, err := observe.Parse(bytes.TrimSpace(stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestMultiAssignEmitted (Phase 4 rung 2, GoLean R2a): the grammar emits
// multi-target assignment in its three shapes — swap, aliased index, and
// mixed targets — across a seed sweep.
func TestMultiAssignEmitted(t *testing.T) {
	swapLine := regexp.MustCompile(`(?m)^\t+(v\d+), (v\d+) = (v\d+), (v\d+)$`)
	isSwap := func(src []byte) bool {
		for _, m := range swapLine.FindAllSubmatch(src, -1) {
			if string(m[1]) == string(m[4]) && string(m[2]) == string(m[3]) {
				return true
			}
		}
		return false
	}
	alias := regexp.MustCompile(`(?m)^\t+v\d+, v\d+\[int\(v\d+%`)
	// A multi-TARGET line: two-plus targets before one `=`. The RHS may be
	// a single multi-result call (`a[i], v = h(x)` — callStmt's
	// element-target minority marks multi_assign with no RHS comma), so
	// the witness pins the target side only (latent until the strings/
	// slices/type-switch rungs shifted draw streams and surfaced it).
	multi := regexp.MustCompile(`(?m)^\t+[^=\n]+, [^=\n]+ = `)
	swaps, aliases, tagged := 0, 0, 0
	for seed := int64(500); seed < 900; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "multi_assign") {
			continue
		}
		tagged++
		if !multi.Match(c.Source) {
			t.Fatalf("seed %d: multi_assign tagged but no multi-target line\n%s", seed, c.Source)
		}
		if isSwap(c.Source) {
			swaps++
		}
		if alias.Match(c.Source) {
			aliases++
		}
	}
	if tagged == 0 || swaps == 0 || aliases == 0 {
		t.Fatalf("multi-assign shapes starved: %d tagged, %d swaps, %d aliased", tagged, swaps, aliases)
	}
	t.Logf("multi_assign: %d tagged, %d with swaps, %d with aliased indexes", tagged, swaps, aliases)
}

// TestIncludeForcesTags (Phase 4 rung 5, GoLean R4): Include force-
// enables tags in every mix, after Exclude, without disturbing draws.
func TestIncludeForcesTags(t *testing.T) {
	for seed := int64(7000); seed < 7040; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Include = []string{"maps", "recover_wrapper"}
		cfg.Exclude = []string{"maps"} // Include wins, applied after
		g := New(cfg)
		g.drawSetup()
		if !g.constructs["maps"] || !g.constructs["recover_wrapper"] {
			t.Fatalf("seed %d: Include not applied: maps=%v wrapper=%v",
				seed, g.constructs["maps"], g.constructs["recover_wrapper"])
		}
	}
	bad := DefaultConfig(1)
	bad.Include = []string{"quaternions"}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown Include tag validated")
	}
}

// TestAggregateObservation (Phase 4 rung 4, GoLean R5): under a
// capability profile that masks slices/maps, containers the liveness
// draw wanted observed are folded into trailing int results — sums for
// maps (commutative, the only order-safe map observation),
// position-weighted chains for slices — and the built case runs with
// those aggregates observed.
func TestAggregateObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	fold := regexp.MustCompile(`(?m)^\tagg0 := len\(v\d+\)`)
	seen := false
	for seed := int64(97000); seed < 97200 && !seen; seed++ {
		cfg := DefaultConfig(seed)
		cfg.NoObserve = []Shape{ShapeSlice, ShapeMap}
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "aggregate_observed") {
			continue
		}
		seen = true
		if !fold.Match(c.Source) {
			t.Fatalf("seed %d: aggregate tag without a fold\n%s", seed, c.Source)
		}
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK && doc.Status != observe.StatusPanic {
			t.Fatalf("seed %d: status %s", seed, doc.Status)
		}
		if doc.Status == observe.StatusOK {
			last := doc.Values[len(doc.Values)-1]
			if last.Kind != "int" {
				t.Fatalf("seed %d: trailing aggregate is %s, want int", seed, last.Kind)
			}
		}
	}
	if !seen {
		t.Fatal("no seed in 97000..97200 drew an aggregate-observed container")
	}
}

// TestKindsCorner (Phase 4 rung 3, GoLean R3): the kinds corner is
// conversion-FREE (their constraint: int(x) laundering masks the
// kind-defaulting bug class), densifies inc/dec and compound sites, and
// reaches defined-type targets — the BUG-042/043 family's habitat.
func TestKindsCorner(t *testing.T) {
	incdec := regexp.MustCompile(`(?m)^\t+v\d+(\+\+|--)$`)
	varConv := regexp.MustCompile(`\b(u?int(8|16|32|64)?|T\d+)\(\(?v\d+`)
	obsLines := regexp.MustCompile(`(?m)^.*\bobs(Bool|Int|Uint|Str|Recovered)\(.*$`)
	sites, definedTyped := 0, 0
	for seed := int64(4000); seed < 4060; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "kinds"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "corner_kinds") {
			t.Fatalf("seed %d: kinds corner not noted", seed)
		}
		if hasFeature(c, "conversions") {
			t.Fatalf("seed %d: conversions tagged inside the kinds corner\n%s", seed, c.Source)
		}
		// TEXT-level check too (Phase 4 audit F1: the tag check alone was
		// vacuous when an emitter forgot to mark): a conversion applied to
		// a VARIABLE is the laundering shape; typed literals T(5) are not.
		// Observation-channel conversions are exempt BY NAME (the obs* API
		// widens its argument; the ledger states the exemption) — never a
		// blanket exemption.
		src := obsLines.ReplaceAll(c.Source, nil)
		if m := varConv.Find(src); m != nil {
			t.Fatalf("seed %d: conversion text %q inside the kinds corner\n%s", seed, m, c.Source)
		}
		sites += len(incdec.FindAll(c.Source, -1))
		if hasFeature(c, "defined_types") && incdec.Match(c.Source) {
			definedTyped++
		}
	}
	if sites < 60 {
		t.Fatalf("kinds corner emitted only %d inc/dec sites over 60 seeds — density lever broken", sites)
	}
	if definedTyped == 0 {
		t.Fatal("no kinds-corner case combines defined types with inc/dec sites")
	}
	t.Logf("kinds corner: %d inc/dec sites over 60 seeds, %d cases with defined types present", sites, definedTyped)
}

// TestStringFamilyEmitted (strings rung; sx c12/c13/g09): the grammar
// emits string indexing (byte-typed; known-length literal safe majority,
// raw-variable hot minority), string slicing (constant rune-boundary
// bounds on literals safe, ASCII-literal variable low bound hot), and the
// string range fold (byte offsets + rune values), each under its own tag,
// across a seed sweep — and every tag is backed by its shape.
func TestStringFamilyEmitted(t *testing.T) {
	litIndex := regexp.MustCompile(`"[^"]*"\[\d+\]`)
	varIndex := regexp.MustCompile(`\bv\d+\[v\d+\]`)
	litSlice := regexp.MustCompile(`"[^"]*"\[\d+:\d+\]`)
	hotSlice := regexp.MustCompile(`"[^"]*"\[v\d+:\]`)
	fold := regexp.MustCompile(`(?m)^\t+for i\d+, r\d+ := range \w+ \{\n\t+\w+ \+= i\d+\*31 \+ int\(r\d+\)`)
	indexed, sliced, hotSliced, folds, hotIndexed := 0, 0, 0, 0, 0
	for seed := int64(20000); seed < 20400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		// Typed walk (audit finding 3: the regex \bv\d+\[v\d+\] also
		// matches array/slice indexing, and the pass gate never required
		// the runtime-meaningful variable forms at all): count an
		// occurrence only when the indexed operand IS a string.
		if hasFeature(c, "string_index") || hasFeature(c, "string_slice") {
			fset, file, info := typecheckCase(t, c, seed, nil)
			_ = fset
			ast.Inspect(file, func(n ast.Node) bool {
				ie, ok := n.(*ast.IndexExpr)
				if !ok {
					return true
				}
				tv, ok := info.Types[ie.X]
				if !ok {
					return true
				}
				if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Kind() == types.String {
					if _, isVar := ie.Index.(*ast.Ident); isVar {
						hotIndexed++
					}
				}
				return true
			})
		}
		if hasFeature(c, "string_index") {
			if !litIndex.Match(c.Source) && !varIndex.Match(c.Source) {
				t.Fatalf("seed %d: string_index tagged but no index shape\n%s", seed, c.Source)
			}
			if litIndex.Match(c.Source) {
				indexed++
			}
		}
		if hasFeature(c, "string_slice") {
			if !litSlice.Match(c.Source) && !hotSlice.Match(c.Source) {
				t.Fatalf("seed %d: string_slice tagged but no slice shape\n%s", seed, c.Source)
			}
			if litSlice.Match(c.Source) {
				sliced++
			}
			if hotSlice.Match(c.Source) {
				hotSliced++
			}
		}
		if hasFeature(c, "string_range") {
			if !fold.Match(c.Source) {
				t.Fatalf("seed %d: string_range tagged but no range fold\n%s", seed, c.Source)
			}
			folds++
		}
	}
	if indexed == 0 || sliced == 0 || hotSliced == 0 || folds == 0 || hotIndexed == 0 {
		t.Fatalf("string family starved over 400 seeds: %d literal-indexed, %d string-var hot-indexed, %d const-sliced, %d hot-sliced, %d folds",
			indexed, hotIndexed, sliced, hotSliced, folds)
	}
	t.Logf("string family: %d literal-indexed, %d string-var hot-indexed (typed count), %d const-sliced, %d hot-sliced, %d range folds",
		indexed, hotIndexed, sliced, hotSliced, folds)
}

// TestSliceTripleEmitted (three-index rung; sx g32/g03/c18/c21): the
// grammar emits the atomic derive/append/fold shape, every triple's
// constant bounds satisfy 0 <= a <= b <= c <= the base's initial
// composite length (always-legal by construction), and both aliasing
// regimes (b < c shared, b == c reallocating) occur in the population.
func TestSliceTripleEmitted(t *testing.T) {
	triple := regexp.MustCompile(`(?m)^\t+(t\d+) := (v\d+)\[(\d+):(\d+):(\d+)\]\n\t+t\d+ = append\(t\d+, `)
	declLen := regexp.MustCompile(`(?m)^\t+(v\d+) := \[\]\w+\{([^}]*)\}`)
	tagged, shared, realloc := 0, 0, 0
	for seed := int64(30000); seed < 30400; seed++ {
		// Force-include the rung's gate tags (audit finding 6: the
		// swarm-drawn window left a 3-instance margin on the reallocating
		// regime — dense forcing makes both regimes' counts robust to
		// draw-stream shifts).
		cfg := DefaultConfig(seed)
		cfg.Include = []string{"slices", "slice_triple", "append", "range", "conversions"}
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "slice_triple") {
			continue
		}
		tagged++
		ms := triple.FindAllSubmatch(c.Source, -1)
		if len(ms) == 0 {
			t.Fatalf("seed %d: slice_triple tagged but no derive+append shape\n%s", seed, c.Source)
		}
		initLen := map[string]int{}
		for _, d := range declLen.FindAllSubmatch(c.Source, -1) {
			initLen[string(d[1])] = len(strings.Split(string(d[2]), ","))
		}
		for _, m := range ms {
			var a, b, cc int
			fmt.Sscanf(string(m[3]), "%d", &a)
			fmt.Sscanf(string(m[4]), "%d", &b)
			fmt.Sscanf(string(m[5]), "%d", &cc)
			bound, ok := initLen[string(m[2])]
			if !ok {
				t.Fatalf("seed %d: triple over %s with no composite declaration\n%s", seed, m[2], c.Source)
			}
			if !(0 <= a && a <= b && b <= cc && cc <= bound) {
				t.Fatalf("seed %d: triple bounds [%d:%d:%d] not within initial length %d\n%s",
					seed, a, b, cc, bound, c.Source)
			}
			if b < cc {
				shared++
			} else {
				realloc++
			}
		}
	}
	if tagged == 0 || shared == 0 || realloc == 0 {
		t.Fatalf("slice_triple starved: %d tagged, %d shared-regime, %d realloc-regime", tagged, shared, realloc)
	}
	t.Logf("slice_triple: %d tagged, %d shared (b<c), %d reallocating (b==c)", tagged, shared, realloc)
}

// TestSliceTripleAliasingObserved is the RUNTIME witness for the aliasing
// carve-out, in the emitter's exact three-line shape: with b < c the
// controlled append MUST write the base's backing (s[b] changes — the
// shared-backing write visible through the base), and with b == c it MUST
// reallocate (base untouched). Both are spec-mandated ("If the capacity of
// s is not large enough ... append allocates ... Otherwise, append re-uses
// the underlying array"), which is what makes the family generable at all.
func TestSliceTripleAliasingObserved(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	obs := []binding{
		{name: "q0", typ: Int(0, false)},
		{name: "q1", typ: Int(0, false)},
		{name: "q2", typ: Int(0, false)},
	}
	driver := []byte((&Generator{}).driverSource(obs))
	run := func(name, subject string, wantFold, wantElem, wantLast int64) {
		doc := runCase(t, Case{Source: []byte(subject), Driver: driver})
		if doc.Status != observe.StatusOK {
			t.Fatalf("%s: status %s", name, doc.Status)
		}
		got := []int64{doc.Values[0].Int, doc.Values[1].Int, doc.Values[2].Int}
		want := []int64{wantFold, wantElem, wantLast}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: observed %v, want %v", name, got, want)
			}
		}
	}
	// b(2) < c(4): cap(t0)=3, len(t0)=1 — the append re-uses v1's backing
	// and the write lands at v1[2]; the fold sees [20, 99].
	run("shared", `package main

func fuzzSubject() (int, int, int) {
	v0 := 0
	v1 := []int{10, 20, 30, 40}
	t0 := v1[1:2:4]
	t0 = append(t0, 99)
	for _, e0 := range t0 {
		v0 = v0*31 + int(e0)
	}
	return v0, v1[2], v1[3]
}
`, 20*31+99, 99, 40)
	// b(2) == c(2): cap(t0)=1 == len(t0) — the append MUST reallocate;
	// the fold still sees [20, 99] but v1 is provably untouched.
	run("realloc", `package main

func fuzzSubject() (int, int, int) {
	v0 := 0
	v1 := []int{10, 20, 30, 40}
	t0 := v1[1:2:2]
	t0 = append(t0, 99)
	for _, e0 := range t0 {
		v0 = v0*31 + int(e0)
	}
	return v0, v1[2], v1[3]
}
`, 20*31+99, 30, 40)
}

// TestTypeSwitchEmitted (type-switch rung; sx c03): the grammar emits
// `switch w := v.(type)` over interface vars, every switch carries a
// default arm and reads the binding in every clause, and the population
// contains both the derived form (single satisfier — default provably
// dead, noted unreachable_case) and multi-case empty-interface forms.
// Case-type legality is witnessed by the typecheck sweeps: an impossible
// case type would fail to compile.
func TestTypeSwitchEmitted(t *testing.T) {
	head := regexp.MustCompile(`(?m)^\t+switch (w\d+) := v\d+\.\(type\) \{`)
	caseLine := regexp.MustCompile(`(?m)^\t+case T\d+:`)
	tagged, multiCase := 0, 0
	for seed := int64(40000); seed < 40400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "type_switch") {
			continue
		}
		tagged++
		m := head.FindSubmatch(c.Source)
		if m == nil {
			t.Fatalf("seed %d: type_switch tagged but no type-switch head\n%s", seed, c.Source)
		}
		if !caseLine.Match(c.Source) {
			t.Fatalf("seed %d: type switch without a concrete case\n%s", seed, c.Source)
		}
		// The binding is read in the default arm (the discharge) — the
		// unused-binding rule holds under every clause interpretation.
		if !bytes.Contains(c.Source, append([]byte("_ = "), m[1]...)) {
			t.Fatalf("seed %d: type-switch binding %s not discharged in default\n%s", seed, m[1], c.Source)
		}
		// Per-SWITCH structure via the AST (audit: the old global regex
		// count was ~80% vacuous — programs routinely carry several
		// single-arm switches, and a value switch's default satisfied
		// the default check).
		_, file := parseCase(t, c.Source, seed)
		sawMulti := false
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSwitchStmt)
			if !ok {
				return true
			}
			concrete, hasDefault := 0, false
			for _, cl := range ts.Body.List {
				cc := cl.(*ast.CaseClause)
				if cc.List == nil {
					hasDefault = true
				} else {
					concrete += len(cc.List)
				}
			}
			if concrete == 0 {
				t.Fatalf("seed %d: a type switch has no concrete arm", seed)
			}
			if !hasDefault {
				t.Fatalf("seed %d: a type switch has no default arm", seed)
			}
			if concrete > 1 {
				sawMulti = true
			}
			return true
		})
		if sawMulti {
			multiCase++
		}
	}
	if tagged == 0 {
		t.Fatal("no seed in 40000..40400 drew a type switch — arm starved")
	}
	if multiCase == 0 {
		t.Fatal("no multi-case (empty-interface) type switch in the sweep")
	}
	t.Logf("type switches in %d cases, %d with a genuinely multi-arm switch", tagged, multiCase)
}

// TestRecoverWrapperObservesPanics (Phase 4 rung 1, GoLean R1): wrapped
// subjects return status OK with the panic ENCODED in the trailing int
// result and partial state in the others — panic identity and
// store-before-panic state as ordinary observed values, no obs* events.
// Sweeps seeds until both outcomes are seen: a wrapped subject that
// caught a panic (code != 0) and one that exited normally (code == 0).
func TestRecoverWrapperObservesPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	shape := regexp.MustCompile(`(?s)defer func\(\) \{\n\s*if p := recover\(\); p != nil \{`)
	caught, clean, wrapped := 0, 0, 0
	for seed := int64(95000); seed < 95400 && (caught == 0 || clean == 0); seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "recover_wrapper") {
			continue
		}
		wrapped++
		if !shape.Match(c.Source) {
			t.Fatalf("seed %d: wrapper tag without wrapper shape\n%s", seed, c.Source)
		}
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK {
			// A wrapped subject can still panic-terminate only via a
			// DRIVER-visible escape, which the wrapper should prevent.
			t.Fatalf("seed %d: wrapped subject status %s", seed, doc.Status)
		}
		if len(doc.Values) == 0 {
			t.Fatalf("seed %d: no observed values", seed)
		}
		code := doc.Values[len(doc.Values)-1]
		if code.Kind != "int" || code.GoType != "int" {
			t.Fatalf("seed %d: trailing result is %s/%s, want the int panic code", seed, code.Kind, code.GoType)
		}
		if code.Int != 0 {
			caught++
			if code.Int < 1 || code.Int > 5 {
				t.Fatalf("seed %d: panic code %d outside the table", seed, code.Int)
			}
		} else {
			clean++
		}
	}
	if caught == 0 || clean == 0 {
		t.Fatalf("wrapper sweep did not see both outcomes: %d caught, %d clean over %d wrapped subjects",
			caught, clean, wrapped)
	}
	t.Logf("wrapped subjects: %d caught / %d clean (of %d run before both seen)", caught, clean, caught+clean)
}
