package gen

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// Subject is the name of the generated function under test.
const Subject = "fuzzSubject"

// Config controls one generated program. Every bound exists to keep the case
// SMALL: divergence value comes from construct composition, not length.
type Config struct {
	Seed int64
	// Vars is how many extra variables to declare beyond the one-per-type
	// floor. The floor is load-bearing: every arithmetic expression needs a
	// non-constant operand available (see nonConstExpr).
	Vars int
	// Stmts is the number of top-level statements in the subject body.
	Stmts int
	// Depth bounds control-flow nesting.
	Depth int
	// ExprFuel bounds expression recursion.
	ExprFuel int
	// LoopCap bounds the trip count of EVERY generated loop; bounds are
	// literal constants, so no generated program can fail to halt.
	LoopCap int
	// Swarm draws a per-seed construct mix (population diversity). Ignored
	// when Constructs is set explicitly.
	Swarm bool
	// Constructs, when non-nil, is the exact optional-construct gate: a tag
	// absent or false is disabled. Nil with Swarm=false enables everything.
	Constructs map[string]bool
	// Corner selects a named-corner sub-config (BRIEF: edge cases are
	// hunted, not hoped for). "" draws one per seed when Swarm is on;
	// "none" disables corners; "boundary" biases literals, divisors, and
	// shift counts toward type boundaries.
	Corner string
}

// DefaultConfig is the MVP shape: small programs, shallow nesting, swarm on.
func DefaultConfig(seed int64) Config {
	return Config{Seed: seed, Vars: 4, Stmts: 8, Depth: 2, ExprFuel: 3, LoopCap: 6, Swarm: true}
}

// Validate rejects a Config the generator cannot honour — an error, never a
// panic (a bad config is a diagnosis, not a crash).
func (c Config) Validate() error {
	switch {
	case c.Vars < 0:
		return fmt.Errorf("config: Vars %d must be >= 0", c.Vars)
	case c.Stmts < 1:
		return fmt.Errorf("config: Stmts %d must be >= 1", c.Stmts)
	case c.Depth < 0:
		return fmt.Errorf("config: Depth %d must be >= 0", c.Depth)
	case c.ExprFuel < 1:
		return fmt.Errorf("config: ExprFuel %d must be >= 1", c.ExprFuel)
	case c.LoopCap < 1:
		return fmt.Errorf("config: LoopCap %d must be >= 1 — every loop draws a trip count in [1,LoopCap]", c.LoopCap)
	case c.Corner != "" && c.Corner != "none" && c.Corner != "boundary":
		return fmt.Errorf("config: unknown corner %q", c.Corner)
	case c.Vars > 128:
		return fmt.Errorf("config: Vars %d exceeds 128", c.Vars)
	case c.ExprFuel > 12:
		return fmt.Errorf("config: ExprFuel %d exceeds 12", c.ExprFuel)
	}
	// "Halts" must include "halts before the heat death": bound the
	// worst-case executed statement count, not just each knob (review
	// finding: LoopCap 1<<40 passed Validate and generated a years-long
	// loop). Worst case is ~Stmts * (2*LoopCap)^Depth.
	worst := float64(c.Stmts)
	for i := 0; i < c.Depth; i++ {
		worst *= 2 * float64(c.LoopCap)
	}
	if worst > 1e9 {
		return fmt.Errorf("config: worst-case executed statements ~%.0g exceeds 1e9 (Stmts=%d, LoopCap=%d, Depth=%d)",
			worst, c.Stmts, c.LoopCap, c.Depth)
	}
	// Unknown construct keys are misconfigurations, not empty gates: a typo
	// like "array" would silently degrade the population to core-only.
	if c.Constructs != nil {
		known := map[string]bool{}
		for _, tag := range Optional() {
			known[tag] = true
		}
		for tag := range c.Constructs {
			if !known[tag] && !coreConstructs[tag] {
				return fmt.Errorf("config: unknown construct %q", tag)
			}
		}
	}
	return nil
}

// Case is one generated program: self-contained, import-free, go-run-able.
type Case struct {
	Source []byte
	// Features are all tags recorded at emission — constructs used plus info
	// tags (knowledge-as-data). Sorted.
	Features []string
	// FeatureCounts is how many times each tag was recorded. Program-level
	// presence saturates for common tags (review finding: boundary at 98%
	// of programs while a 19% minority per draw); counts keep the tag
	// informative for stratification.
	FeatureCounts map[string]int
	// Tape is the recorded choice sequence: the program re-derives from it
	// byte-identically. The seam for shrinking and search.
	Tape []int
	// Stats is the per-site realized choice distribution.
	Stats map[string]*SiteStats
}

// binding is one declared variable. reads counts expression-position reads,
// which is what Go's unused-variable rule and the liveness tiers care about.
type binding struct {
	name     string
	typ      Type
	observed bool
	reads    int
	// minLen is a slice's guaranteed length lower bound: the initial
	// composite length. Appends only grow and whole-slice assignment is
	// never generated, so len >= minLen holds forever — which is what makes
	// constant indices < minLen safe by construction.
	minLen int
	// keys is a map variable's key alphabet: every key this map is ever
	// written, read, deleted, or observed with comes from here. Drawn
	// without replacement (duplicate constant keys in a literal are a
	// compile error), and it bounds the map's size by construction.
	keys []string
}

// Generator builds one program. Not safe for concurrent use; make one per case.
type Generator struct {
	c          *chooser
	cfg        Config
	constructs map[string]bool

	vars     []binding
	used     map[string]int
	loopSeq  int
	innerSeq int
	tmpSeq   int
	// structs are the per-seed named struct types, declared in the preamble.
	structs []Type
	// corner is the resolved named corner; boundaryBias is the weight of the
	// boundary arm at literal/divisor/shift-count sites (0 disables, 1 is
	// the everywhere-minority base, cornerBoundaryBias the hunted mix).
	corner       string
	boundaryBias int
	// riskSpent is the per-statement panic-risk budget (review finding: two
	// hot panic sites in one statement have SPEC-UNSPECIFIED panic identity
	// — a conformant clone may report either panic). Each statement context
	// resets it; the risky arms (variable divisor, raw index) consult and
	// spend it, so every panic-path program has a unique possible panic.
	// Multi-risk statements return later as a quotient-compared corner.
	riskSpent bool
	// done makes a Generator single-use: a second Generate would re-append
	// declarations and silently emit a corrupt program (review finding).
	done bool
}

// resetRisk opens a fresh statement context for the panic-risk budget.
func (g *Generator) resetRisk() { g.riskSpent = false }

// spendRisk claims the statement's single panic-risk slot.
func (g *Generator) spendRisk() bool {
	if g.riskSpent {
		return false
	}
	g.riskSpent = true
	return true
}

const cornerBoundaryBias = 5

// New builds a Generator. With cfg.Swarm and no explicit Constructs, the
// per-seed mix is drawn first — through the chooser, so it is on the tape.
func New(cfg Config) *Generator {
	g := &Generator{
		c:    newChooser(cfg.Seed),
		cfg:  cfg,
		used: map[string]int{},
	}
	switch {
	case cfg.Constructs != nil:
		g.constructs = cfg.Constructs
	case cfg.Swarm:
		g.constructs = swarmMix(g.c)
	}
	g.boundaryBias = 1
	switch cfg.Corner {
	case "boundary":
		g.corner = "boundary"
		g.boundaryBias = cornerBoundaryBias
	case "none":
		g.boundaryBias = 0
	case "":
		if cfg.Swarm {
			if g.c.choose("corner", []arm{
				{name: "plain", weight: 7, ok: true},
				{name: "boundary", weight: 1, ok: true},
			}).name == "boundary" {
				g.corner = "boundary"
				g.boundaryBias = cornerBoundaryBias
			}
		}
	}
	return g
}

// Generate produces one case. A Generator is single-use: a second call
// errors rather than silently emitting a corrupt program.
func (g *Generator) Generate() (Case, error) {
	if g.done {
		return Case{}, fmt.Errorf("gen: Generator is single-use — make one per case")
	}
	g.done = true
	if err := g.cfg.Validate(); err != nil {
		return Case{}, err
	}
	body := &emitter{indent: 1}

	if g.corner != "" {
		g.note("corner_" + g.corner)
	}
	g.declare(body)
	g.mark("functions", "short_decl", "literals", "return")

	for i := 0; i < g.cfg.Stmts; i++ {
		g.stmt(body, g.cfg.Depth)
	}
	observed := g.observe(body)
	resultTypes := make([]string, len(observed))
	for i, b := range observed {
		resultTypes[i] = b.typ.GoName()
	}

	var out strings.Builder
	out.WriteString("package main\n\n")
	for _, st := range g.structs {
		out.WriteString(st.decl())
	}
	fmt.Fprintf(&out, "func %s() (%s) {\n", Subject, strings.Join(resultTypes, ", "))
	out.WriteString(body.buf.String())
	out.WriteString("}\n\n")
	g.driver(&out, observed)

	source, err := format.Source([]byte(out.String()))
	if err != nil {
		return Case{}, fmt.Errorf("generated source does not parse (generator bug): %w\n%s", err, out.String())
	}

	features := make([]string, 0, len(g.used))
	counts := make(map[string]int, len(g.used))
	for tag, n := range g.used {
		features = append(features, tag)
		counts[tag] = n
	}
	sort.Strings(features)
	// Snapshots, not aliases: the returned Case must not share live chooser
	// state with the Generator (review finding: callers and generator could
	// clobber each other's Tape, and Stats mutated retroactively).
	tape := append([]int(nil), g.c.tape...)
	stats := make(map[string]*SiteStats, len(g.c.stats))
	for site, s := range g.c.stats {
		cp := &SiteStats{Valid: map[string]int{}, Chosen: map[string]int{}}
		for k, v := range s.Valid {
			cp.Valid[k] = v
		}
		for k, v := range s.Chosen {
			cp.Chosen[k] = v
		}
		stats[site] = cp
	}
	return Case{Source: source, Features: features, FeatureCounts: counts, Tape: tape, Stats: stats}, nil
}

// pickTarget draws a write target from candidates, biased 3:1 toward
// OBSERVED variables — writes landing on unobserved vars mostly echo the
// declaration literal back through the observation (review finding: 68% of
// observed output lines equalled the initializer).
func (g *Generator) pickTarget(candidates []int) int {
	weight := func(i int) int {
		if g.vars[i].observed {
			return 3
		}
		return 1
	}
	total := 0
	for _, i := range candidates {
		total += weight(i)
	}
	r := g.c.draw(total)
	for _, i := range candidates {
		if r < weight(i) {
			return i
		}
		r -= weight(i)
	}
	panic("gen: unreachable")
}

// typePool is the declarable type set: scalars always, string when enabled,
// plus two per-seed array types (element and length drawn through the tape).
func (g *Generator) typePool() []Type {
	pool := scalarTypes()
	if g.enabled("strings") {
		pool = append(pool, Str())
	}
	if g.enabled("arrays") {
		elems := pool
		for i := 0; i < 2; i++ {
			pool = append(pool, Array(pick(g.c, elems), 2+g.c.draw(3)))
		}
	}
	if g.enabled("slices") {
		elems := scalarTypes()
		if g.enabled("strings") {
			elems = append(elems, Str())
		}
		for i := 0; i < 2; i++ {
			pool = append(pool, Slice(pick(g.c, elems)))
		}
	}
	if g.enabled("maps") {
		kinds := []Type{Int(0, false), Int(8, false), Int(8, true)}
		elems := scalarTypes()
		if g.enabled("strings") {
			kinds = append(kinds, Str())
			elems = append(elems, Str())
		}
		pool = append(pool, Map(pick(g.c, kinds), pick(g.c, elems)))
	}
	if g.enabled("structs") {
		fieldPool := scalarTypes()
		if g.enabled("strings") {
			fieldPool = append(fieldPool, Str())
		}
		// Bound hoisted: a draw in a loop CONDITION re-draws every
		// iteration, decoupling tape length from decision count (review
		// finding — brittleness for the shrinking seam).
		structCount := 1 + g.c.draw(2)
		for s := 0; s < structCount; s++ {
			fields := make([]StructField, 2+g.c.draw(2))
			for f := range fields {
				fields[f] = StructField{Name: fmt.Sprintf("f%d", f), Typ: pick(g.c, fieldPool)}
			}
			st := Type{Shape: ShapeStruct, Name: fmt.Sprintf("S%d", s), Fields: fields}
			g.structs = append(g.structs, st)
			pool = append(pool, st)
		}
	}
	return pool
}

// declare emits the variable declarations: the one-per-type floor plus
// cfg.Vars extras, each with a drawn liveness tier. Initializers are plain
// literals so a declaration can never depend on generation order.
func (g *Generator) declare(out *emitter) {
	pool := g.typePool()
	for _, typ := range pool {
		g.declareOne(out, typ)
	}
	for i := 0; i < g.cfg.Vars; i++ {
		g.declareOne(out, pick(g.c, pool))
	}
	// The observed floor: a program observing nothing tests nothing. The
	// promotion is a deterministic fix-up drawn via the tape, not a retry.
	anyObserved := false
	for _, v := range g.vars {
		if v.observed {
			anyObserved = true
		}
	}
	if !anyObserved {
		g.vars[g.c.draw(len(g.vars))].observed = true
	}
}

func (g *Generator) declareOne(out *emitter, typ Type) {
	name := fmt.Sprintf("v%d", len(g.vars))
	observed := g.c.choose("liveness", []arm{
		{name: "observed", weight: 4, ok: true},
		{name: "unobserved", weight: 1, ok: true},
	}).name == "observed"
	b := binding{name: name, typ: typ, observed: observed}
	if typ.Shape == ShapeMap {
		// 4 keys drawn without replacement; the literal initializes 2 of
		// them, so hits AND misses are both reachable at every op site.
		b.keys = g.keyAlphabet(*typ.Key)
		entries := make([]string, 2)
		for i := range entries {
			entries[i] = b.keys[i] + ": " + g.literal(*typ.Elem).text
		}
		out.line("%s := %s{%s}", name, typ.GoName(), strings.Join(entries, ", "))
		g.vars = append(g.vars, b)
		g.mark(typ.Tags()...)
		return
	}
	if typ.Shape == ShapeSlice {
		// Slices are born from a composite literal: never nil, length known.
		b.minLen = 2 + g.c.draw(3)
		elems := make([]string, b.minLen)
		for i := range elems {
			elems[i] = g.literal(*typ.Elem).text
		}
		out.line("%s := %s{%s}", name, typ.GoName(), strings.Join(elems, ", "))
	} else {
		out.line("%s := %s", name, g.literal(typ).text)
	}
	g.vars = append(g.vars, b)
	g.mark(typ.Tags()...)
}

// observe closes the subject body: returns the observed tuple and discharges
// every unobserved, never-read variable with `_ = v` (legal deadness — the
// eliminable case, recorded as dead_value). An unobserved variable that WAS
// read is a feeder: visible through what it feeds.
func (g *Generator) observe(out *emitter) []binding {
	var observed []binding
	var names []string
	for i := range g.vars {
		v := &g.vars[i]
		switch {
		case v.observed:
			observed = append(observed, *v)
			names = append(names, v.name)
		case v.reads == 0:
			out.line("_ = %s", v.name)
			g.note(tagDeadValue)
		default:
			g.note(tagFeederValue)
		}
	}
	g.mark("return")
	out.line("return %s", strings.Join(names, ", "))
	return observed
}

// driver emits func main: call the subject, print every observed value on its
// own line, and turn any panic into an ordinary observation ("panic: <msg>")
// with exit status 0 — panic paths are comparable outcomes, not failures.
// println needs no imports and prints ints, uints, and bools deterministically.
func (g *Generator) driver(out *strings.Builder, observed []binding) {
	rs := make([]string, len(observed))
	for i := range observed {
		rs[i] = fmt.Sprintf("r%d", i)
	}
	out.WriteString("func main() {\n")
	out.WriteString("\tdefer func() {\n")
	out.WriteString("\t\tif r := recover(); r != nil {\n")
	out.WriteString("\t\t\tif err, ok := r.(error); ok {\n")
	out.WriteString("\t\t\t\tprintln(\"panic:\", err.Error())\n")
	out.WriteString("\t\t\t} else {\n")
	out.WriteString("\t\t\t\tprintln(\"panic\")\n")
	out.WriteString("\t\t\t}\n")
	out.WriteString("\t\t}\n")
	out.WriteString("\t}()\n")
	fmt.Fprintf(out, "\t%s := %s()\n", strings.Join(rs, ", "), Subject)
	for i, r := range rs {
		// println takes scalars only; an observed array is printed
		// element-wise, so the whole value stays injectively visible.
		switch observed[i].typ.Shape {
		case ShapeArray:
			for j := 0; j < observed[i].typ.Len; j++ {
				fmt.Fprintf(out, "\tprintln(%s[%d])\n", r, j)
			}
			continue
		case ShapeStruct:
			for _, f := range observed[i].typ.Fields {
				fmt.Fprintf(out, "\tprintln(%s.%s)\n", r, f.Name)
			}
			continue
		case ShapeMap:
			// len plus the value at every alphabet key (missing keys print
			// their zero value) — deterministic and injective over the only
			// keys the program can ever have touched. Never a map range.
			fmt.Fprintf(out, "\tprintln(len(%s))\n", r)
			for _, k := range observed[i].keys {
				fmt.Fprintf(out, "\tprintln(%s[%s])\n", r, k)
			}
			continue
		case ShapeSlice:
			// Dynamic length: print it, then every element in order — the
			// whole value stays injectively visible. cap is NEVER observed
			// (unspecified after append).
			fmt.Fprintf(out, "\tprintln(len(%s))\n", r)
			fmt.Fprintf(out, "\tfor _, e := range %s {\n\t\tprintln(e)\n\t}\n", r)
			continue
		}
		fmt.Fprintf(out, "\tprintln(%s)\n", r)
	}
	out.WriteString("}\n")
}

// pickVar returns the index of a variable of exactly type t. The one-per-type
// floor makes this total for every pool type.
func (g *Generator) pickVar(t Type) (int, bool) {
	var found []int
	for i, v := range g.vars {
		if v.typ.Equal(t) {
			found = append(found, i)
		}
	}
	if len(found) == 0 {
		return 0, false
	}
	return pick(g.c, found), true
}

// arrayVars returns the indices of array-typed variables, optionally
// restricted to a given element type. Pure scan: no draws.
func (g *Generator) arrayVars(elem *Type) []int { return g.varsOfShape(ShapeArray, elem) }

// sliceVars is arrayVars for slices.
func (g *Generator) sliceVars(elem *Type) []int { return g.varsOfShape(ShapeSlice, elem) }

// mapVars returns map-typed variable indices, optionally by element type.
func (g *Generator) mapVars(elem *Type) []int { return g.varsOfShape(ShapeMap, elem) }

// keyAlphabet draws 4 distinct key literals for one map variable — distinct
// by remove-and-redraw over a candidate slice, never a rejection loop.
func (g *Generator) keyAlphabet(key Type) []string {
	var candidates []string
	if key.Shape == ShapeString {
		candidates = append([]string(nil), stringWords...)
		for i, w := range candidates {
			candidates[i] = fmt.Sprintf("%q", w)
		}
	} else {
		for n := 0; n < 8; n++ {
			candidates = append(candidates, g.intLiteral(key, n).text)
		}
	}
	keys := make([]string, 4)
	for i := range keys {
		j := g.c.draw(len(candidates))
		keys[i] = candidates[j]
		candidates = append(candidates[:j], candidates[j+1:]...)
	}
	return keys
}

func (g *Generator) varsOfShape(shape Shape, elem *Type) []int {
	var found []int
	for i, v := range g.vars {
		if v.typ.Shape != shape {
			continue
		}
		if elem != nil && !v.typ.Elem.Equal(*elem) {
			continue
		}
		found = append(found, i)
	}
	return found
}

// indexableVars are array and slice variables together (for element writes
// and range loops), honoring each family's construct gate.
func (g *Generator) indexableVars() []int {
	var found []int
	if g.enabled("arrays") {
		found = append(found, g.arrayVars(nil)...)
	}
	if g.enabled("slices") {
		found = append(found, g.sliceVars(nil)...)
	}
	return found
}

// indexBound is the guaranteed length lower bound for one indexable binding:
// the type's Len for arrays, minLen for slices.
func (b binding) indexBound() int {
	if b.typ.Shape == ShapeSlice {
		return b.minLen
	}
	return b.typ.Len
}

func (g *Generator) hasArrayOfElem(t Type) bool { return len(g.arrayVars(&t)) > 0 }

func (g *Generator) pickArrayOfElem(t Type) int { return pick(g.c, g.arrayVars(&t)) }

// fieldSource is one reachable struct field of a wanted type.
type fieldSource struct {
	varIdx int
	field  string
}

// fieldSources scans struct variables for fields of exactly type t (nil t:
// any field). Pure scan: no draws.
func (g *Generator) fieldSources(t *Type) []fieldSource {
	var found []fieldSource
	for i, v := range g.vars {
		if v.typ.Shape != ShapeStruct {
			continue
		}
		for _, f := range v.typ.Fields {
			if t == nil || f.Typ.Equal(*t) {
				found = append(found, fieldSource{varIdx: i, field: f.Name})
			}
		}
	}
	return found
}

// pickOuter picks a variable of type t from everything EXCEPT the most
// recently pushed binding — the projection target for an inner declaration
// must be an enclosing variable, never the inner one itself.
func (g *Generator) pickOuter(t Type) int {
	var found []int
	for i, v := range g.vars[:len(g.vars)-1] {
		if v.typ.Equal(t) {
			found = append(found, i)
		}
	}
	return pick(g.c, found)
}

// observedNames lists the observed tuple in declaration order — the same
// order observe() returns, so every return site is signature-identical.
func (g *Generator) observedNames() []string {
	var names []string
	for _, v := range g.vars {
		if v.observed {
			names = append(names, v.name)
		}
	}
	return names
}

// structVars returns the indices of struct-typed variables.
func (g *Generator) structVars() []int {
	var found []int
	for i, v := range g.vars {
		if v.typ.Shape == ShapeStruct {
			found = append(found, i)
		}
	}
	return found
}

// ---- emitter ---------------------------------------------------------

type emitter struct {
	buf    strings.Builder
	indent int
}

func (e *emitter) line(format_ string, args ...any) {
	e.buf.WriteString(strings.Repeat("\t", e.indent))
	fmt.Fprintf(&e.buf, format_, args...)
	e.buf.WriteString("\n")
}

func (e *emitter) open(format_ string, args ...any) {
	e.line(format_, args...)
	e.indent++
}

func (e *emitter) dedent() { e.indent-- }

func (e *emitter) close() {
	e.indent--
	e.line("}")
}
