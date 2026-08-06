package gen

// Construct gating is a plain map — the growth seam. A new language construct
// is a new tag in Optional() plus emission code gated on it.
//
// Two kinds of tags exist:
//
//   - CONSTRUCT tags gate generation. Core constructs are always available
//     (the generator cannot emit a program without them); optional constructs
//     are enabled per seed by the swarm mix. Emitting a disabled construct is
//     a generator bug and panics (mark).
//   - INFO tags record knowledge the generator has at emission — dead code,
//     panic risk, width dependence, liveness tiers. They are never gated;
//     they exist so measurements can stratify instead of banning legal
//     programs (note).
var coreConstructs = map[string]bool{
	"ints": true, "bools": true, "widths": true,
	"assignment": true, "literals": true, "short_decl": true,
	"functions": true, "return": true,
	"control_flow": true, "cases": true, "default": true,
}

// Optional lists the swarm-sampled constructs.
func Optional() []string {
	return []string{
		"if", "loops", "switch", "break", "continue",
		"division", "modulo", "bitwise", "shifts", "conversions",
		"comparisons", "equality", "min", "max",
		"strings", "concat", "len",
		"arrays", "index", "range",
		"structs", "field",
		"block_decl", "observe_point", "early_return",
		"slices", "append",
	}
}

// Info tags, for reference (not enforced as a closed set):
//
//	dead_code        — statements exist after a mid-block terminal
//	unreachable_case — a switch was emitted with a wide, unreduced tag
//	panic_risk       — a division/modulo site drew a variable divisor
//	width_dependent  — an emitted operation can differ between GOARCH widths
//	feeder_value     — a variable is read but not observed
//	dead_value       — a variable is discharged with _ = v, eliminable
const (
	tagBoundary        = "boundary"
	tagDeadCode        = "dead_code"
	tagUnreachableCase = "unreachable_case"
	tagPanicRisk       = "panic_risk"
	tagWidthDependent  = "width_dependent"
	tagFeederValue     = "feeder_value"
	tagDeadValue       = "dead_value"
)

// enabled reports whether every named construct may be emitted.
func (g *Generator) enabled(tags ...string) bool {
	for _, tag := range tags {
		if coreConstructs[tag] {
			continue
		}
		if g.constructs == nil {
			continue
		}
		if !g.constructs[tag] {
			return false
		}
	}
	return true
}

// mark records construct tags as emitted. Marking a disabled construct is a
// generator bug: the legality mask failed, so panic rather than emit a
// program the conformance run would have to catch.
func (g *Generator) mark(tags ...string) {
	for _, tag := range tags {
		if !g.enabled(tag) {
			panic("gen: emitted disabled construct " + tag)
		}
		g.used[tag]++
	}
}

// note records info tags — knowledge, never gated.
func (g *Generator) note(tags ...string) {
	for _, tag := range tags {
		g.used[tag]++
	}
}

// swarmMix draws a per-seed construct subset: population diversity comes from
// varying WHICH features a small program draws on. The draw goes through the
// chooser, so the mix is part of the tape and of reproducibility.
func swarmMix(c *chooser) map[string]bool {
	mix := make(map[string]bool, len(Optional()))
	for _, tag := range Optional() {
		mix[tag] = c.draw(2) == 0
	}
	return mix
}
