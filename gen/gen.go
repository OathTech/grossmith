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
	// Exclude force-disables optional construct tags on top of whatever
	// Swarm or Constructs decided — the construct half of an adapter
	// capability profile (NoObserve is the shape half). Exclusion consumes
	// no draws, so the swarm-mix and corner draws for a seed are
	// unperturbed — but downstream statement draws naturally diverge once
	// arms are masked, so the full draw trace is NOT profile-invariant
	// (audit M6 corrected the earlier overclaim).
	Exclude []string
	// Include force-ENABLES optional construct tags on top of whatever
	// Swarm or Constructs decided, applied after Exclude (rung 5, GoLean
	// R4: the pairwise coverage objective forces a tag PAIR into every
	// mix while swarm keeps the rest diverse). Enabling a tag arms its
	// emission sites; realized co-emission still depends on draws and
	// legality and is measured by the composition histogram, never
	// assumed.
	Include []string
	// NoObserve lists shapes masked OUT of the observed liveness tier — an
	// adapter capability profile (e.g. GoLean's harness fails closed on
	// slices and maps). Generation is unrestricted; only observation is.
	NoObserve []Shape
	// Corner selects a named-corner sub-config (BRIEF: edge cases are
	// hunted, not hoped for). "" draws one per seed when Swarm is on;
	// "none" disables corners; "boundary" biases literals, divisors, and
	// shift counts toward type boundaries.
	Corner string
}

// DefaultConfig: small programs, shallow nesting, swarm on. Stmts tracks
// the grown pool floor (~20 declarations): at Stmts 8 the second review
// measured a 76% initializer-echo rate in observed output — eight writes
// cannot touch sixteen observed variables.
func DefaultConfig(seed int64) Config {
	return Config{Seed: seed, Vars: 4, Stmts: 14, Depth: 2, ExprFuel: 3, LoopCap: 6, Swarm: true}
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
	case c.Corner != "" && c.Corner != "none" && c.Corner != "boundary" && c.Corner != "kinds" && c.Corner != "order":
		return fmt.Errorf("config: unknown corner %q (use none, boundary, kinds, or order)", c.Corner)
	// A forced order corner with its instrument disabled would be a
	// corner in name only: corner_order noted, masks and weight biases
	// applied, zero witness text (mid-arc review findings 3 and 5). Both
	// disablement mechanisms are contracts and both are rejected loudly
	// at config time rather than silently overridden or silently voided.
	case c.Corner == "order" && containsTag(c.Exclude, "order_witness"):
		return fmt.Errorf("config: Corner \"order\" with order_witness excluded — the corner IS the witness; drop the corner or the exclusion")
	case c.Corner == "order" && c.Constructs != nil && !c.Constructs["order_witness"]:
		return fmt.Errorf("config: Corner \"order\" with order_witness disabled by Constructs — the corner IS the witness; enable the tag or drop the corner")
	case c.Vars > 128:
		return fmt.Errorf("config: Vars %d exceeds 128", c.Vars)
	case c.ExprFuel > 12:
		return fmt.Errorf("config: ExprFuel %d exceeds 12", c.ExprFuel)
	}
	// "Halts" must include "halts before the heat death" AND "halts before
	// the OOM killer": bound worst-case executed statements. The cap is
	// memory-aware (second review): a defer record or slice append can
	// retain ~56B per executed statement, so 4e6 statements bounds retained
	// memory to ~224MB. Time was capped at 1e9 before; memory is the
	// binding constraint.
	worst := float64(c.Stmts)
	for i := 0; i < c.Depth; i++ {
		worst *= 2 * float64(c.LoopCap)
	}
	if worst > 4e6 {
		return fmt.Errorf("config: worst-case executed statements ~%.0g exceeds 4e6 (Stmts=%d, LoopCap=%d, Depth=%d)",
			worst, c.Stmts, c.LoopCap, c.Depth)
	}
	// Unknown construct keys are misconfigurations, not empty gates: a typo
	// like "array" would silently degrade the population to core-only. Core
	// keys are rejected too — enabled() short-circuits them, so a caller
	// writing {"assignment": false} would be silently ignored otherwise.
	// Keys are sorted so the diagnosis is deterministic (map order reached
	// an artifact — review finding).
	if c.Constructs != nil || len(c.Exclude) > 0 || len(c.Include) > 0 {
		known := map[string]bool{}
		for _, tag := range Optional() {
			known[tag] = true
		}
		var bad []string
		check := func(tag string) {
			if coreConstructs[tag] {
				bad = append(bad, tag+" (core, not configurable)")
			} else if !known[tag] {
				bad = append(bad, tag)
			}
		}
		for tag := range c.Constructs {
			check(tag)
		}
		for _, tag := range c.Exclude {
			check(tag)
		}
		for _, tag := range c.Include {
			check(tag)
		}
		if len(bad) > 0 {
			sort.Strings(bad)
			return fmt.Errorf("config: unknown constructs: %s", strings.Join(bad, ", "))
		}
	}
	return nil
}

// Case is one generated program: self-contained, import-free, go-run-able.
type Case struct {
	// Source is the SUBJECT file: import-free, no main, observation via the
	// obs* protocol API the driver provides.
	Source []byte
	// Driver is the generated gc reference driver: implements obs*, calls
	// the subject, emits a grossmith-observation-v2 document on stdout.
	Driver []byte
	// Features are all tags recorded at emission — constructs used plus info
	// tags (knowledge-as-data). Sorted.
	Features []string
	// FeatureCounts is how many times each tag was recorded. Program-level
	// presence saturates for common tags (review finding: boundary at 98%
	// of programs while a 19% minority per draw); counts keep the tag
	// informative for stratification.
	FeatureCounts map[string]int
	// Tape is the recorded DRAW TRACE — a log of every draw, in order,
	// and a DECODER input: NewReplay(config, tape) regenerates the case
	// byte-for-byte, with trace/generator disagreements surfacing as
	// typed *ReplayError (audit Phase 3; H1 resolved).
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
	// aggObserved (rung 4, GoLean R5): the liveness draw chose observed
	// but the shape is profile-masked (NoObserve) — observed THROUGH an
	// int aggregate at function end instead of dropped to the feeder tier.
	aggObserved bool
	reads       int
	// bound is the binding's static magnitude bound (W4): 0 = unknown,
	// b > 0 = |value| <= b over every execution. For containers and
	// structs it covers every element/field ever written. Updated at
	// every write site; a missed write site would UNDER-tag, which the
	// runtime soundness witness and the cross-arch CI proof police.
	bound int64
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
	// helpers are the generated top-level functions. PURE BY CONSTRUCTION:
	// params only (no globals, no pointers, no closures), no output, and
	// panic-free (pureMode masks every hot arm) — so a helper call has NO
	// effect the subject can observe, expression evaluation order cannot
	// matter, and the effect discipline is dissolved rather than built. Its
	// revisit trigger: pointer parameters, closures over subject state, or
	// package-level variables.
	helpers []helper
	// defined are the per-seed defined types and their methods.
	defined   []definedType
	methodSeq int
	// ifaces are the per-seed interface types.
	ifaces []ifaceType
	// pureMode is set while generating a helper or method body; pureBase
	// names the environment's guaranteed non-constant int-ish variable (a
	// helper's p0, a method's receiver r) for the nonConstExpr fallback.
	pureMode bool
	pureBase string
	// corner is the resolved named corner; boundaryBias is the weight of the
	// boundary arm at literal/divisor/shift-count sites (0 disables, 1 is
	// the everywhere-minority base, cornerBoundaryBias the hunted mix).
	corner       string
	boundaryBias int
	// rangedSlices are variable indices of slices an ENCLOSING range is
	// currently iterating. Appending to one is banned by mask: an inner
	// range over the same slice re-evaluates len, so range+append composes
	// into len*2^len executed statements — the second review's charter
	// violation, found live in the corpus (case_00934: len 3->6->12->24).
	rangedSlices []int
	// wrapped: this subject carries the recover-observation wrapper
	// (GoLean R1): named results, a deferred recover encoding the panic
	// kind into a dedicated int result and snapshotting the observed
	// locals — panic identity AND store-before-panic partial state become
	// ordinary observed values, with no obs* events (profile-safe).
	wrapped bool
	// guardBias raises the hot-arm odds inside a guarded IIFE, so the
	// recover path is actually exercised.
	guardBias bool
	// witSeq counts emitted order-witness wraps (R2b, witness arc W2): each
	// wit(x, tag) call at an evaluation-order-rich site gets the next tag,
	// starting at 1 (tag 0 would make "no calls ran" and "first call had
	// tag 0" both fold to a zero accumulator). The `var wOrd` declaration,
	// the wit helper, and the trailing observed slot are emitted iff
	// witSeq > 0 at assembly — text exists iff a call does, the same
	// honesty rule as helper/pair text. The names wit/wOrd cannot collide
	// with generated identifiers: every generated name is a short prefix
	// plus a decimal suffix (v0, q1, h0, agg0, p2, S0, T0, m0, w0, ...),
	// and neither wit nor wOrd matches that shape.
	witSeq int
	// maxExec is the config's worst-case executed-statement count (the
	// Validate formula) — the multiplier a loop-carried write's
	// contribution gets in bound arithmetic (W4). loopDepth counts
	// enclosing loop bodies so write sites know to apply it.
	maxExec   int64
	loopDepth int
	// condDepth counts enclosing conditionally-executed bodies (if arms,
	// switch cases, guarded statements): a replacing write there must
	// JOIN the old bound, not discard it (arc-end review finding 3 — a
	// branch assignment replaced a window-sized bound with 15).
	condDepth int
	// shortCircuitDepth counts enclosing && / || operand contexts, so
	// witness() can tag wraps that land in conditionally-executed
	// position (witness_shortcircuit — the quarantined population).
	shortCircuitDepth int
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

// helper is one generated top-level function.
type helper struct {
	name    string
	params  []Type
	results []Type
	// resultBounds are the return expressions' static magnitude bounds
	// (W4): computable at generation because bodies are pure — a return
	// that reads a parameter is unknown (0), a literal-rooted one is not.
	resultBounds []int64
	src          string
}

// definedType is one `type T0 <int-kind>` declaration with its methods.
type definedType struct {
	typ     Type
	methods []method
}

// method is one pure value-receiver method on a defined type.
type method struct {
	name   string
	params []Type
	result Type
	src    string
}

// methodRef locates one method: defined-type index, method index.
type methodRef struct{ di, mi int }

// ifaceType is one generated interface: a subset of one defined type's
// method set, so satisfaction holds by construction. source is that defined
// type's index; with globally-unique method names it is the ONLY satisfier.
type ifaceType struct {
	typ     Type
	source  int
	methods []int // indices into g.defined[source].methods
}

// riskOK reports whether the current statement may still draw a hot panic
// site: never inside a helper (helpers are panic-free so their calls compose
// into expressions without multi-trap identity issues).
func (g *Generator) riskOK() bool { return !g.riskSpent && !g.pureMode }

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

// snapshotConfig deep-copies the caller-owned reference fields so a
// mutation between New and Generate cannot change program bytes for the
// same seed (review finding; the Phase 3 audit found the window REOPENED
// when the setup draws moved into Generate, because the copy moved with
// them — it now happens at construction, where the promise is made).
func snapshotConfig(cfg Config) Config {
	if cfg.Constructs != nil {
		m := make(map[string]bool, len(cfg.Constructs))
		for k, v := range cfg.Constructs {
			m[k] = v
		}
		cfg.Constructs = m
	}
	cfg.Exclude = append([]string(nil), cfg.Exclude...)
	cfg.Include = append([]string(nil), cfg.Include...)
	cfg.NoObserve = append([]Shape(nil), cfg.NoObserve...)
	return cfg
}

// New builds a seeded Generator. The per-seed setup draws (swarm mix,
// corner) happen at the START of Generate — through the chooser, so they
// are on the tape and inside Generate's single recovery point.
func New(cfg Config) *Generator {
	return &Generator{
		c:    newChooser(cfg.Seed),
		cfg:  snapshotConfig(cfg),
		used: map[string]int{},
	}
}

// NewReplay builds a Generator that DECODES a recorded draw trace instead
// of drawing from a seed (audit Phase 3): the same config plus the same
// trace reproduces the same case byte-for-byte, and any disagreement
// between trace and generator — exhaustion, out-of-range, surplus — is a
// typed *ReplayError from Generate, never a silent fallback to
// randomness. cfg must be the RESOLVED config the trace was recorded
// under (case.json carries it); cfg.Seed is retained for record equality
// but no PRNG is consulted.
func NewReplay(cfg Config, trace []int) *Generator {
	return &Generator{
		c:    newReplayChooser(trace),
		cfg:  snapshotConfig(cfg),
		used: map[string]int{},
	}
}

// containsTag reports whether a construct-tag slice names tag.
func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// drawSetup resolves the construct mix and corner — the first draws on
// the tape.
func (g *Generator) drawSetup() {
	cfg := g.cfg
	g.maxExec = int64(cfg.Stmts)
	for i := 0; i < cfg.Depth; i++ {
		g.maxExec *= 2 * int64(cfg.LoopCap)
	}
	switch {
	case cfg.Constructs != nil:
		// g.cfg's map is already OUR copy (snapshotConfig at construction
		// — that is where the no-mutation-window promise is kept); this
		// second copy just keeps g.constructs free to absorb Exclude below
		// without touching the recorded config.
		g.constructs = make(map[string]bool, len(cfg.Constructs))
		for k, v := range cfg.Constructs {
			g.constructs[k] = v
		}
	case cfg.Swarm:
		g.constructs = swarmMix(g.c)
	}
	if len(cfg.Exclude) > 0 {
		if g.constructs == nil {
			// Everything-enabled becomes an explicit map so the exclusions
			// have something to subtract from.
			g.constructs = make(map[string]bool, len(Optional()))
			for _, tag := range Optional() {
				g.constructs[tag] = true
			}
		}
		for _, tag := range cfg.Exclude {
			g.constructs[tag] = false
		}
	}
	if len(cfg.Include) > 0 {
		if g.constructs == nil {
			// Everything already enabled — Include is a no-op, but the
			// map keeps the semantics uniform.
			g.constructs = make(map[string]bool, len(Optional()))
			for _, tag := range Optional() {
				g.constructs[tag] = true
			}
		}
		for _, tag := range cfg.Include {
			g.constructs[tag] = true
		}
	}
	g.boundaryBias = 1
	switch cfg.Corner {
	case "boundary":
		g.corner = "boundary"
		g.boundaryBias = cornerBoundaryBias
	case "kinds":
		g.corner = "kinds"
	case "order":
		g.corner = "order"
	case "none":
		g.boundaryBias = 0
	case "":
		if cfg.Swarm {
			// Each named corner keeps the 1-in-8 class weight (the R2b
			// design's containment number): plain dropped 6->5 when the
			// order corner joined, so the corners' rates held rather than
			// diluting to 1-in-9. The order arm is masked only by an
			// explicit profile Exclude, not by the swarm mix — corners
			// override the mix in their own domain (kinds masks
			// conversions, boundary reshapes literal draws), and a corner
			// whose instrument the mix disabled would be a corner in name
			// only.
			switch g.c.choose("corner", []arm{
				{name: "plain", weight: 5, ok: true},
				{name: "boundary", weight: 1, ok: true},
				{name: "kinds", weight: 1, ok: true},
				// Masked by BOTH explicit disablement contracts (review
				// findings 3/5): a profile Exclude and an explicit
				// Constructs map each veto the arm; only the swarm mix's
				// own draw is overridden below.
				{name: "order", weight: 1, ok: !containsTag(cfg.Exclude, "order_witness") &&
					(cfg.Constructs == nil || cfg.Constructs["order_witness"])},
			}).name {
			case "boundary":
				g.corner = "boundary"
				g.boundaryBias = cornerBoundaryBias
			case "kinds":
				g.corner = "kinds"
			case "order":
				g.corner = "order"
			}
		}
	}
	if g.corner == "order" && g.constructs != nil {
		// The order corner force-enables its instrument tag OVER THE SWARM
		// MIX ONLY: the corner is the witness opt-in, and a corner whose
		// own mix draw disabled the tag would be a corner in name only.
		// Both explicit contracts are already honoured before this line —
		// Exclude and explicit-Constructs disablement mask the drawn arm
		// above and reject the forced corner in Validate (review findings
		// 3/5) — so the only opinion overridden here is the swarm's.
		g.constructs["order_witness"] = true
	}
}

// Generate produces one case. A Generator is single-use: a second call
// errors rather than silently emitting a corrupt program. In replay mode
// a trace/generator disagreement surfaces as a *ReplayError.
func (g *Generator) Generate() (c Case, err error) {
	if g.done {
		return Case{}, fmt.Errorf("gen: Generator is single-use — make one per case")
	}
	if err := g.cfg.Validate(); err != nil {
		// Not marked done: a config error should repeat, not turn into a
		// misleading single-use diagnosis (review finding).
		return Case{}, err
	}
	g.done = true
	// The one recovery point for replay violations: the replay source
	// panics with *ReplayError at the failing draw (any deeper plumbing
	// would thread errors through every emit arm); everything else
	// propagates — a non-replay panic is a generator bug.
	defer func() {
		if r := recover(); r != nil {
			re, ok := r.(*ReplayError)
			if !ok {
				panic(r)
			}
			c, err = Case{}, re
		}
	}()
	g.drawSetup()
	body := &emitter{indent: 1}

	if g.corner != "" {
		g.note("corner_" + g.corner)
	}
	// The recover-observation wrapper (GoLean R1) is a per-seed draw: a
	// minority of subjects get named results plus a deferred recover, so
	// panic identity and partial state are observed with zero events.
	if g.enabled("recover_wrapper") && g.c.chance(3) {
		g.wrapped = true
		g.mark("recover_wrapper")
	}
	g.generateHelpers()
	g.createDefinedTypes()
	g.generateMethods()
	g.createInterfaces()
	g.declare(body)
	g.mark("functions", "short_decl", "literals", "return")

	if g.wrapped {
		// The wrapper exists to CATCH panics: bias the hot arms up for the
		// whole body (same lever as the guarded statement), or most
		// wrapped subjects would return on the boring path.
		g.guardBias = true
	}
	// Statements build in their own emitter so the wrapper prologue can be
	// emitted AFTER the body is generated: the defer's slot arithmetic must
	// know whether the order-witness slot exists (witSeq > 0, decided by the
	// statement draws). emitWrapperDefer makes no draws, so the tape is
	// unchanged — only text assembly is reordered.
	stmts := &emitter{indent: 1}
	for i := 0; i < g.cfg.Stmts; i++ {
		if g.wrapped {
			stmts.line("psite = %d", i+1)
		}
		g.stmt(stmts, g.cfg.Depth)
	}
	g.guardBias = false
	if g.wrapped {
		// Sentinel for the final observation region (tight audit F4): the
		// folds below are panic-free by construction TODAY, but a future
		// hot fold would otherwise report the LAST statement's site with
		// no tell. Stmts+1 is out of the witness's accepted range, so the
		// day a fold can panic, the witness fails loudly instead of the
		// site lying quietly.
		stmts.line("psite = %d", g.cfg.Stmts+1)
	}
	observed := g.observe(stmts)
	if g.wrapped {
		// SITE encoding (2026-08-08 review, G1): the old message-prefix
		// table required p.(error) + Error(), which hits GoLean's open
		// $runtime.Error method-set gap — every caught panic became
		// clone-infra and R1's surface was generated-but-never-judged.
		// psite tracks the top-level statement being executed; on a catch
		// the trailing slot reports WHICH statement panicked — sharper
		// ordering discrimination than kind (R1's stated purpose), each
		// site's kind is statically known to the generator, and the
		// kind/message cross-check stays covered by the unwrapped panic
		// paths through the clone harness's expected_reason comparison.
		// No dispatch, no assertions: portable to any clone.
		body.line("psite := 0")
		g.emitWrapperDefer(body)
	}
	resultTypes := make([]string, len(observed))
	for i, b := range observed {
		resultTypes[i] = b.typ.GoName()
	}

	var out strings.Builder
	out.WriteString("package main\n\n")
	for _, st := range g.structs {
		out.WriteString(st.decl())
	}
	for _, h := range g.helpers {
		out.WriteString(h.src)
	}
	for _, dt := range g.defined {
		fmt.Fprintf(&out, "type %s %s\n\n", dt.typ.Named, dt.typ.underlyingName())
		for _, m := range dt.methods {
			out.WriteString(m.src)
		}
	}
	for _, it := range g.ifaces {
		fmt.Fprintf(&out, "type %s interface {\n", it.typ.Name)
		for _, mi := range it.methods {
			m := g.defined[it.source].methods[mi]
			sig := make([]string, len(m.params))
			for pi, pt := range m.params {
				sig[pi] = pt.GoName()
			}
			fmt.Fprintf(&out, "\t%s(%s) %s\n", m.name, strings.Join(sig, ", "), m.result.GoName())
		}
		out.WriteString("}\n\n")
	}
	if g.witSeq > 0 {
		// The order-witness accumulator and its designated impure helper
		// (R2b mechanism 1; E4's amendment covers exactly this pair): the
		// only package-level state and the only impure function the
		// generator emits. Effects are confined to wOrd; wit calls sit in
		// call-argument/operand position, which the spec orders
		// left-to-right, so the accumulator value is a deterministic
		// fingerprint of evaluation order. Emitted iff a wit call was.
		out.WriteString("var wOrd int\n\n")
		out.WriteString("func wit(x int, tag int) int {\n\twOrd = wOrd*31 + tag\n\treturn x\n}\n\n")
	}
	if g.wrapped {
		// Named results: the wrapper's deferred recover writes them
		// directly, so a caught panic returns partial state + the code.
		named := make([]string, len(observed))
		for i, b := range observed {
			named[i] = fmt.Sprintf("q%d %s", i, b.typ.GoName())
		}
		fmt.Fprintf(&out, "func %s() (%s) {\n", Subject, strings.Join(named, ", "))
	} else {
		fmt.Fprintf(&out, "func %s() (%s) {\n", Subject, strings.Join(resultTypes, ", "))
	}
	out.WriteString(body.buf.String())
	out.WriteString(stmts.buf.String())
	out.WriteString("}\n")

	source, err := format.Source([]byte(out.String()))
	if err != nil {
		return Case{}, fmt.Errorf("generated subject does not parse (generator bug): %w\n%s", err, out.String())
	}
	driver, err := format.Source([]byte(g.driverSource(observed)))
	if err != nil {
		return Case{}, fmt.Errorf("generated driver does not parse (generator bug): %w", err)
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
	// Surplus is a replay violation too: leftover trace means the decode
	// path diverged from the recording (shorter program), even though a
	// source was produced — fail closed rather than hand back a case the
	// trace does not describe.
	if rs := g.c.replay; rs != nil && rs.pos != len(rs.trace) {
		return Case{}, &ReplayError{Pos: rs.pos, Bound: len(rs.trace) - rs.pos,
			Value: rs.trace[rs.pos], Reason: "surplus"}
	}
	return Case{Source: source, Driver: driver, Features: features, FeatureCounts: counts, Tape: tape, Stats: stats}, nil
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

// generateHelpers builds 1-2 top-level functions before the subject. The
// call graph is acyclic for free: a helper is registered only after its body
// is generated, so bodies can call only EARLIER helpers; the subject calls
// any. Every helper's first parameter is plain int — the guaranteed
// non-constant source that keeps constant-overflow safety without the
// subject's one-per-type pool floor.
func (g *Generator) generateHelpers() {
	if !g.enabled("helpers") {
		return
	}
	count := 1 + g.c.draw(2)
	for i := 0; i < count; i++ {
		g.helpers = append(g.helpers, g.generateHelper(i))
	}
}

func (g *Generator) generateHelper(idx int) helper {
	pool := scalarTypes()
	if g.enabled("strings") {
		pool = append(pool, Str())
	}
	params := []Type{Int(0, false)}
	for j := g.c.draw(3); j > 0; j-- {
		params = append(params, pick(g.c, pool))
	}
	var results []Type
	for j := 1 + g.c.draw(2); j > 0; j-- {
		results = append(results, pick(g.c, pool))
	}

	savedVars, savedRisk := g.vars, g.riskSpent
	g.vars = nil
	g.pureMode = true
	g.pureBase = "p0"
	for j, pt := range params {
		g.vars = append(g.vars, binding{name: fmt.Sprintf("p%d", j), typ: pt})
	}
	body := &emitter{indent: 1}
	for j := 1 + g.c.draw(3); j > 0; j-- {
		g.stmtIn(body, 1, false, false)
	}
	rs := make([]string, len(results))
	rbs := make([]int64, len(results))
	for j, rt := range results {
		rv := g.expr(rt, g.cfg.ExprFuel)
		rs[j] = rv.text
		rbs[j] = rv.bound
	}
	body.line("return %s", strings.Join(rs, ", "))
	g.vars, g.riskSpent, g.pureMode, g.pureBase = savedVars, savedRisk, false, ""
	g.mark("helpers")

	ps := make([]string, len(params))
	for j, pt := range params {
		ps[j] = fmt.Sprintf("p%d %s", j, pt.GoName())
	}
	rt := make([]string, len(results))
	for j, r := range results {
		rt[j] = r.GoName()
	}
	name := fmt.Sprintf("h%d", idx)
	src := fmt.Sprintf("func %s(%s) (%s) {\n%s}\n\n",
		name, strings.Join(ps, ", "), strings.Join(rt, ", "), body.buf.String())
	return helper{name: name, params: params, results: results, resultBounds: rbs, src: src}
}

// createDefinedTypes draws the per-seed defined types (before methods, which
// need them, and before declare, whose pool includes them).
func (g *Generator) createDefinedTypes() {
	if !g.enabled("defined_types") {
		return
	}
	definedCount := 1 + g.c.draw(2) // hoisted: never draw in a loop condition
	for i := 0; i < definedCount; i++ {
		t := pick(g.c, intTypes())
		t.Named = fmt.Sprintf("T%d", i)
		g.defined = append(g.defined, definedType{typ: t})
	}
}

// createInterfaces builds 1-2 interfaces. Two forms: DERIVED (a 1-2 method
// subset of one source type's set — with globally-unique method names, the
// source is provably the only implementer, so assertions to any other type
// are COMPILE errors and only source-type assertions are emitted) and EMPTY
// (interface{} — every type implements it, which is the one compile-legal
// route to failing assertions and mixed dynamic types).
func (g *Generator) createInterfaces() {
	if !g.enabled("interfaces", "defined_types") || len(g.defined) == 0 {
		return
	}
	var sources []int
	for di, dt := range g.defined {
		if len(dt.methods) > 0 {
			sources = append(sources, di)
		}
	}
	ifaceCount := 1 + g.c.draw(2) // hoisted: never draw in a loop condition
	for i := 0; i < ifaceCount; i++ {
		it := ifaceType{typ: Type{Shape: ShapeInterface, Name: fmt.Sprintf("I%d", i)}}
		derived := g.c.choose("iface-kind", []arm{
			{name: "derived", weight: 5, ok: len(sources) > 0},
			{name: "empty", weight: 2, ok: true},
		}).name == "derived"
		if derived {
			src := pick(g.c, sources)
			count := 1 + g.c.draw(2)
			if count > len(g.defined[src].methods) {
				count = len(g.defined[src].methods)
			}
			start := g.c.draw(len(g.defined[src].methods) - count + 1)
			it.source = src
			for m := start; m < start+count; m++ {
				it.methods = append(it.methods, m)
			}
		} else {
			// Empty: initializer source drawn from all defined types.
			it.source = g.c.draw(len(g.defined))
		}
		g.ifaces = append(g.ifaces, it)
	}
}

// implementers lists the defined types an assertion against this interface
// may legally name: everything for an empty interface, only the source for a
// derived one (anything else is an "impossible type assertion" COMPILE
// error — the trap this arc paid for).
func (g *Generator) implementers(it *ifaceType) []int {
	if len(it.methods) == 0 {
		all := make([]int, len(g.defined))
		for i := range all {
			all[i] = i
		}
		return all
	}
	return []int{it.source}
}

// ifaceByName resolves an interface Type back to its record.
func (g *Generator) ifaceByName(name string) *ifaceType {
	for i := range g.ifaces {
		if g.ifaces[i].typ.Name == name {
			return &g.ifaces[i]
		}
	}
	return nil
}

// ifaceVars returns interface-typed variable indices.
func (g *Generator) ifaceVars() []int { return g.varsOfShape(ShapeInterface, nil) }

// dispatchSite is one (interface variable, method) pair.
type dispatchSite struct {
	varIdx int
	di, mi int
}

// dispatchSites lists interface-variable method calls whose result type is t.
func (g *Generator) dispatchSites(t Type) []dispatchSite {
	var found []dispatchSite
	for _, vi := range g.ifaceVars() {
		info := g.ifaceByName(g.vars[vi].typ.Name)
		if info == nil {
			continue
		}
		for _, mi := range info.methods {
			if g.defined[info.source].methods[mi].result.Equal(t) {
				found = append(found, dispatchSite{varIdx: vi, di: info.source, mi: mi})
			}
		}
	}
	return found
}

// assertSources lists interface variables that type t may legally be
// asserted from.
func (g *Generator) assertSources(t Type) []int {
	var found []int
	for _, vi := range g.ifaceVars() {
		info := g.ifaceByName(g.vars[vi].typ.Name)
		if info == nil {
			continue
		}
		for _, di := range g.implementers(info) {
			if g.defined[di].typ.Equal(t) {
				found = append(found, vi)
				break
			}
		}
	}
	return found
}

// generateMethods builds 0-2 pure value-receiver methods per defined type.
// Same purity story as helpers (no globals, output, or hot panic sites) with
// one difference: the guaranteed non-constant source is the RECEIVER, so no
// forced int parameter is needed. Methods register after generation, so
// bodies can call only earlier methods and helpers — acyclic for free.
func (g *Generator) generateMethods() {
	if !g.enabled("methods") {
		return
	}
	for di := range g.defined {
		// 1-2 per type (was 0-2): 22% of methodful mixes drew zero methods,
		// starving derived interfaces (second review).
		for j := 1 + g.c.draw(2); j > 0; j-- {
			m := g.generateMethod(di)
			g.defined[di].methods = append(g.defined[di].methods, m)
		}
	}
}

func (g *Generator) generateMethod(di int) method {
	dt := g.defined[di].typ
	var params []Type
	for j := g.c.draw(3); j > 0; j-- {
		params = append(params, pick(g.c, intTypes()))
	}
	result := pick(g.c, append(intTypes(), dt))

	savedVars, savedRisk := g.vars, g.riskSpent
	g.vars = []binding{{name: "r", typ: dt}}
	g.pureMode = true
	g.pureBase = "r"
	for j, pt := range params {
		g.vars = append(g.vars, binding{name: fmt.Sprintf("p%d", j), typ: pt})
	}
	body := &emitter{indent: 1}
	for j := 1 + g.c.draw(2); j > 0; j-- {
		g.stmtIn(body, 1, false, false)
	}
	body.line("return %s", g.expr(result, g.cfg.ExprFuel).text)
	g.vars, g.riskSpent, g.pureMode, g.pureBase = savedVars, savedRisk, false, ""
	g.mark("methods")

	ps := make([]string, len(params))
	for j, pt := range params {
		ps[j] = fmt.Sprintf("p%d %s", j, pt.GoName())
	}
	name := fmt.Sprintf("m%d", g.methodSeq)
	g.methodSeq++
	src := fmt.Sprintf("func (r %s) %s(%s) %s {\n%s}\n\n",
		dt.GoName(), name, strings.Join(ps, ", "), result.GoName(), body.buf.String())
	return method{name: name, params: params, result: result, src: src}
}

// methodsWithResult returns the methods whose single result has type t.
func (g *Generator) methodsWithResult(t Type) []methodRef {
	var found []methodRef
	for di, dt := range g.defined {
		for mi, m := range dt.methods {
			if m.result.Equal(t) {
				found = append(found, methodRef{di: di, mi: mi})
			}
		}
	}
	return found
}

// singleResultHelpers returns helpers with exactly one result of type t.
func (g *Generator) singleResultHelpers(t Type) []int {
	var found []int
	for i, h := range g.helpers {
		if len(h.results) == 1 && h.results[0].Equal(t) {
			found = append(found, i)
		}
	}
	return found
}

// callArgs renders drawn argument expressions for a helper's parameters.
func (g *Generator) callArgs(h helper, fuel int) string {
	return g.argList(h.params, fuel)
}

func (g *Generator) argList(params []Type, fuel int) string {
	args := make([]string, len(params))
	for i, pt := range params {
		// Call arguments are one of R2b's evaluation-order-rich sites: the
		// spec orders them left-to-right, so a witness here fingerprints
		// exactly what a clone's argument-evaluation order can get wrong.
		args[i] = g.witness(g.expr(pt, fuel), pt).text
	}
	return strings.Join(args, ", ")
}

// witness wraps an int-typed drawn expression in a wit(x, tag) call — R2b
// mechanism 1 (witness arc W2; the effect-discipline design's first
// instrument). Wrapping happens ONLY inside the order corner (instruments
// are minorities; the corner IS the minority mechanism), never in pure
// helper/method bodies (E4: helpers stay pure — wit itself is the sole
// designated impure helper), and only for plain int in v1 (other types
// need per-type helpers; extension noted in the ledger). The density draw
// keeps wrapped and unwrapped operands mixed within a site. Wrapping
// erases constness (a call is never a Go constant), which is exactly why
// witness sites are the caller's choice: every named site tolerates a
// non-constant operand.
func (g *Generator) witness(v value, t Type) value {
	if g.corner != "order" || g.pureMode || !g.enabled("order_witness") {
		return v
	}
	if t.Shape != ShapeInt || t.Bits != 0 || t.Unsigned || t.Named != "" {
		return v
	}
	// Density draw: wrap 2-in-3. Wrapped and unwrapped operands stay
	// mixed across seeds, so the corner's population varies which
	// operands carry witnesses rather than saturating every site the
	// same way in every subject.
	if g.c.chance(3) {
		return v
	}
	g.witSeq++
	g.mark("order_witness")
	if g.shortCircuitDepth > 0 {
		// The wrap sits under && / || — conditionally executed, the
		// sharpest R2b shape and the one GoLean's frontend quarantines.
		// Tagged so composition/compositionJudged can show its judged
		// coverage separately (review finding 8).
		g.note("witness_shortcircuit")
	}
	// The *31+tag accumulator wraps platform-width int — the same
	// width_dependent convention as the aggregate folds.
	g.markWidthDep(Int(0, false))
	return value{text: fmt.Sprintf("wit(%s, %d)", v.text, g.witSeq), bound: v.bound}
}

// writeBound records a write's effect on the target binding's bound (W4;
// hardened per the arc-end review findings 3/4). op semantics:
//   - "=" REPLACES the bound — but under a conditional body it JOINS
//     (either branch may have run), and inside a loop it goes UNKNOWN
//     (the RHS bound was computed from pre-iteration state; `v = v + x`
//     iterated is growth the static bound cannot follow);
//   - "+" accumulates outside loops; INSIDE a loop it goes unknown for
//     the same staleness reason — a loop-body RHS may read state the
//     loop itself grows (the reviewer's w0 := v0*(v0+v0) case);
//   - "+fixed" is the exempt accumulation for contributions that are
//     INDEPENDENT of loop-mutated state (a loop index bounded by
//     construction, the ++/-- constant, a whole-fold contribution
//     already multiplied for its own iterations): the contribution is
//     multiplied by the worst-case execution count and added — sound
//     under any nesting because it never reads a stale bound;
//   - "*" multiplies; iterated multiplication is exponential — unknown
//     inside loops;
//   - "max" raises the bound to cover a new element/field value; inside
//     a loop the written value may itself be loop-grown, so it goes
//     unknown there too.
func (g *Generator) writeBound(target *binding, rhs int64, op string) {
	if g.loopDepth > 0 && op != "+fixed" {
		target.bound = 0
		return
	}
	switch op {
	case "=":
		if g.condDepth > 0 {
			target.bound = boundMax(target.bound, rhs)
			return
		}
		target.bound = rhs
	case "+":
		target.bound = boundAdd(target.bound, rhs)
	case "+fixed":
		target.bound = boundAdd(target.bound, boundMul(rhs, g.maxExec))
	case "*":
		target.bound = boundMul(target.bound, rhs)
	case "max":
		target.bound = boundMax(target.bound, rhs)
	}
}

// witnessOperandType biases comparison/equality operand types toward plain
// int inside the order corner — the corner's density lever, the same class
// of move as boundaryBias reshaping literal draws. v1's witness wraps only
// plain int; without the bias most operands draw unwrappable sized kinds
// and the corner is a corner in name only. Outside the corner (or wherever
// wrapping cannot fire) the draw is untouched — and no tape is consumed,
// so non-corner seeds decode identically.
func (g *Generator) witnessOperandType(t Type) Type {
	if g.corner != "order" || g.pureMode || !g.enabled("order_witness") {
		return t
	}
	if g.c.chance(2) {
		return Int(0, false)
	}
	return t
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
	for _, dt := range g.defined {
		pool = append(pool, dt.typ)
	}
	for _, it := range g.ifaces {
		pool = append(pool, it.typ)
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
		var candidates []int
		for i, v := range g.vars {
			allowed := true
			for _, shape := range g.cfg.NoObserve {
				if v.typ.Shape == shape {
					allowed = false
				}
			}
			if allowed {
				candidates = append(candidates, i)
			}
		}
		// Scalars are never in a NoObserve profile, so candidates is
		// non-empty for every profile we define.
		g.vars[pick(g.c, candidates)].observed = true
	}
}

func (g *Generator) declareOne(out *emitter, typ Type) {
	name := fmt.Sprintf("v%d", len(g.vars))
	observed := g.c.choose("liveness", []arm{
		{name: "observed", weight: 4, ok: true},
		{name: "unobserved", weight: 1, ok: true},
	}).name == "observed"
	agg := false
	for _, shape := range g.cfg.NoObserve {
		if typ.Shape == shape {
			// R5 (rung 4): a masked shape the draw wanted observed is
			// observed through an aggregate instead of silently demoted —
			// for the range-able shapes, and ONLY when the constructs the
			// fold emits are enabled (audit F1: the fold bypassed the
			// capability-profile contract, handing range/len/conversions
			// to mixes that declared them off).
			if observed && (typ.Shape == ShapeSlice || typ.Shape == ShapeMap) &&
				g.enabled("range", "len") {
				// conversions is required only when the fold actually
				// EMITS int() — integer elements, or integer map keys
				// (hunt F13: demanding it unconditionally suppressed
				// observation of bool/string containers for a construct
				// their folds never use, leaving 92% of masked container
				// state unobserved).
				needsConv := typ.Elem.Shape == ShapeInt ||
					(typ.Shape == ShapeMap && typ.Key.Shape == ShapeInt)
				if !needsConv || g.enabled("conversions") {
					agg = true
				}
			}
			observed = false
		}
	}
	b := binding{name: name, typ: typ, observed: observed, aggObserved: agg, bound: 1}
	if typ.Shape == ShapeInterface {
		// Never nil: initialized by implicit conversion from a satisfying
		// concrete value — THE satisfaction corner — so dispatch cannot
		// panic. The source type's floor variable always precedes interface
		// declarations in pool order.
		info := g.ifaceByName(typ.Name)
		ci, _ := g.pickVar(g.defined[info.source].typ)
		cv := &g.vars[ci]
		cv.reads++
		b.bound = cv.bound
		out.line("%s := %s(%s)", name, typ.Name, cv.name)
		g.vars = append(g.vars, b)
		g.mark(typ.Tags()...)
		return
	}
	if typ.Shape == ShapeMap {
		// 4 keys drawn without replacement; the literal initializes 2 of
		// them, so hits AND misses are both reachable at every op site.
		b.keys = g.keyAlphabet(*typ.Key)
		entries := make([]string, 2)
		for i := range entries {
			ev := g.literal(*typ.Elem)
			entries[i] = b.keys[i] + ": " + ev.text
			b.bound = boundMax(b.bound, ev.bound)
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
			ev := g.literal(*typ.Elem)
			elems[i] = ev.text
			b.bound = boundMax(b.bound, ev.bound)
		}
		out.line("%s := %s{%s}", name, typ.GoName(), strings.Join(elems, ", "))
	} else {
		lit := g.literal(typ)
		b.bound = lit.bound
		out.line("%s := %s", name, lit.text)
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
		case v.aggObserved:
			// Folded below — a real use (audit F4: the discharge loop was
			// emitting `_ = v` and tagging dead_value for containers the
			// aggregate pass observes two lines later).
		case v.reads == 0:
			out.line("_ = %s", v.name)
			g.note(tagDeadValue)
		default:
			g.note(tagFeederValue)
		}
	}
	// Aggregate observation (rung 4, GoLean R5): profile-masked
	// containers the liveness draw wanted observed are folded into plain
	// ints at function end — key-weighted commutative sums for maps (an
	// order-safe map observation), position-weighted chains for slices
	// (order is specified, so the stronger encoding is free). Plain-int
	// accumulators wrap platform-width: width_dependent, tagged so.
	aggIdx := 0
	for i := range g.vars {
		v := &g.vars[i]
		if !v.aggObserved {
			continue
		}
		v.reads++
		name := fmt.Sprintf("agg%d", aggIdx)
		aggIdx++
		g.note(tagAggObserved)
		g.markWidthDep(Int(0, false))
		elem := *v.typ.Elem
		out.line("%s := len(%s)", name, v.name)
		if v.typ.Shape == ShapeMap {
			// Key-WEIGHTED commutative fold (audit F5: a value-only sum
			// made delete(k1) and delete(k2) indistinguishable when the
			// values collided): Σ over entries of kTerm*31 + eTerm stays
			// order-safe and is key-sensitive.
			g.mark("maps", "range", "len")
			kTerm := "int(k)"
			if v.typ.Key.Shape == ShapeString {
				kTerm = "len(k)"
			} else {
				g.mark("conversions")
			}
			out.open("for k, e := range %s {", v.name)
			switch elem.Shape {
			case ShapeString:
				out.line("%s += %s*31 + len(e)", name, kTerm)
			case ShapeBool:
				out.line("%s += %s * 31", name, kTerm)
				out.line("if e {")
				out.indent++
				out.line("%s++", name)
				out.close()
			default:
				g.mark("conversions")
				out.line("%s += %s*31 + int(e)", name, kTerm)
			}
			out.dedent()
			out.line("}")
		} else {
			// Slices: order is specified, so the stronger position-
			// weighted chain is free.
			g.mark("slices", "range", "len")
			out.open("for _, e := range %s {", v.name)
			switch elem.Shape {
			case ShapeString:
				out.line("%s = %s*31 + len(e)", name, name)
			case ShapeBool:
				out.line("%s *= 31", name)
				out.line("if e {")
				out.indent++
				out.line("%s++", name)
				out.close()
			default:
				g.mark("conversions")
				out.line("%s = %s*31 + int(e)", name, name)
			}
			out.dedent()
			out.line("}")
		}
		observed = append(observed, binding{name: name, typ: Int(0, false)})
		names = append(names, name)
	}
	g.mark("return")
	if g.witSeq > 0 {
		// The order-witness slot (R2b): the accumulator's final value as a
		// trailing observed int, after the aggregate slots and before the
		// panic-code slot. Unlike the aggregate folds it is LIVE during the
		// body, so the wrapper's defer snapshots it on the panic path — a
		// mid-expression panic truncates the accumulator, and site + partial
		// state + order-before-panic compose (E3).
		observed = append(observed, binding{name: "wOrd", typ: Int(0, false)})
		names = append(names, "wOrd")
	}
	if g.wrapped {
		// The panic-code slot: a synthetic trailing int result, zero on
		// the normal path, written by the wrapper's recover on the panic
		// path. The binding gives the driver its arity and type.
		observed = append(observed, binding{name: "qP", typ: Int(0, false)})
		names = append(names, "0")
	}
	out.line("return %s", strings.Join(names, ", "))
	return observed
}

// emitWrapperDefer emits the R1 recover-observation prologue: on panic,
// encode the panic into the trailing result and snapshot every observed
// local — store-before-panic partial state as ordinary observed values.
// Capture semantics are the point: the locals are read at RECOVER time.
func (g *Generator) emitWrapperDefer(out *emitter) {
	names := g.observedNames()
	// Aggregate slots (rung 4) sit between the observed locals and the
	// panic code in the result tuple; on the panic path they stay zero
	// (the folds only run at normal exit), so only the slot INDEX moves.
	// The order-witness slot (W2) sits after them and, unlike them, is
	// LIVE during the body — the defer snapshots it, so a caught panic
	// reports the order-before-panic prefix (E3 composition). Callers run
	// this AFTER body generation, so witSeq is settled.
	aggs := 0
	for _, v := range g.vars {
		if v.aggObserved {
			aggs++
		}
	}
	wits := 0
	if g.witSeq > 0 {
		wits = 1
	}
	out.open("defer func() {")
	out.open("if recover() != nil {")
	out.line("q%d = psite", len(names)+aggs+wits)
	if g.witSeq > 0 {
		out.line("q%d = wOrd", len(names)+aggs)
	}
	for i, n := range names {
		out.line("q%d = %s", i, n)
	}
	out.close()
	out.dedent()
	out.line("}()")
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

// appendableSlices are slice variables no enclosing range is iterating.
func (g *Generator) appendableSlices() []int {
	var found []int
	for _, i := range g.sliceVars(nil) {
		ranged := false
		for _, ri := range g.rangedSlices {
			if ri == i {
				ranged = true
			}
		}
		if !ranged {
			found = append(found, i)
		}
	}
	return found
}

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
