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
	}
	return nil
}

// Case is one generated program: self-contained, import-free, go-run-able.
type Case struct {
	Source []byte
	// Features are all tags recorded at emission — constructs used plus info
	// tags (knowledge-as-data). Sorted.
	Features []string
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
}

// Generator builds one program. Not safe for concurrent use; make one per case.
type Generator struct {
	c          *chooser
	cfg        Config
	constructs map[string]bool

	vars    []binding
	used    map[string]bool
	loopSeq int
}

// New builds a Generator. With cfg.Swarm and no explicit Constructs, the
// per-seed mix is drawn first — through the chooser, so it is on the tape.
func New(cfg Config) *Generator {
	g := &Generator{
		c:    newChooser(cfg.Seed),
		cfg:  cfg,
		used: map[string]bool{},
	}
	switch {
	case cfg.Constructs != nil:
		g.constructs = cfg.Constructs
	case cfg.Swarm:
		g.constructs = swarmMix(g.c)
	}
	return g
}

// Generate produces one case.
func (g *Generator) Generate() (Case, error) {
	if err := g.cfg.Validate(); err != nil {
		return Case{}, err
	}
	body := &emitter{indent: 1}

	g.declare(body)
	g.mark("functions", "short_decl", "literals", "return")

	for i := 0; i < g.cfg.Stmts; i++ {
		g.stmt(body, g.cfg.Depth)
	}
	resultTypes, resultNames := g.observe(body)

	var out strings.Builder
	out.WriteString("package main\n\n")
	fmt.Fprintf(&out, "func %s() (%s) {\n", Subject, strings.Join(resultTypes, ", "))
	out.WriteString(body.buf.String())
	out.WriteString("}\n\n")
	g.driver(&out, resultNames)

	source, err := format.Source([]byte(out.String()))
	if err != nil {
		return Case{}, fmt.Errorf("generated source does not parse (generator bug): %w\n%s", err, out.String())
	}

	features := make([]string, 0, len(g.used))
	for tag := range g.used {
		features = append(features, tag)
	}
	sort.Strings(features)
	return Case{Source: source, Features: features, Tape: g.c.tape, Stats: g.c.stats}, nil
}

// typePool is the declarable type set: scalars always, string when enabled.
func (g *Generator) typePool() []Type {
	pool := scalarTypes()
	if g.enabled("strings") {
		pool = append(pool, Str())
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
	out.line("%s := %s", name, g.literal(typ).text)
	g.vars = append(g.vars, binding{name: name, typ: typ, observed: observed})
	g.mark(typ.Tags()...)
}

// observe closes the subject body: returns the observed tuple and discharges
// every unobserved, never-read variable with `_ = v` (legal deadness — the
// eliminable case, recorded as dead_value). An unobserved variable that WAS
// read is a feeder: visible through what it feeds.
func (g *Generator) observe(out *emitter) (types, names []string) {
	for i := range g.vars {
		v := &g.vars[i]
		switch {
		case v.observed:
			types = append(types, v.typ.GoName())
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
	return types, names
}

// driver emits func main: call the subject, print every observed value on its
// own line, and turn any panic into an ordinary observation ("panic: <msg>")
// with exit status 0 — panic paths are comparable outcomes, not failures.
// println needs no imports and prints ints, uints, and bools deterministically.
func (g *Generator) driver(out *strings.Builder, resultNames []string) {
	rs := make([]string, len(resultNames))
	for i := range resultNames {
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
	for _, r := range rs {
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
