package gen

import (
	"fmt"
	"math/rand"
)

// arm is one weighted candidate at a choice site. ok is the legality mask;
// weight is the bias. An arm that is illegal or zero-weighted takes no
// probability mass — mass is renormalized over what remains, never
// redistributed silently.
type arm struct {
	name   string
	weight int
	ok     bool
	emit   func()
}

// SiteStats records, for one named choice site, how often each arm was legal
// and how often it was chosen. This is the realized distribution as an
// inspectable artifact (BRIEF: "the weights mean what they say").
type SiteStats struct {
	Valid  map[string]int
	Chosen map[string]int
}

// drawSource produces the raw draws the chooser records: seeded (a PRNG)
// or replay (a recorded trace, consumed with fail-closed semantics).
type drawSource interface {
	draw(n int) int
}

type seededSource struct{ rng *rand.Rand }

func (s *seededSource) draw(n int) int { return s.rng.Intn(n) }

// ReplayError is a replay-mode violation: the trace and the generator
// disagreed. Every field is diagnosis, not prose — a mismatched trace
// means the config or generator revision differs from the one that
// recorded it (or a shrinker mutated the trace into an invalid decode,
// which is an ordinary rejected candidate, not a bug).
type ReplayError struct {
	// Pos is the number of draws consumed when replay failed — the index
	// of the failing draw for exhausted/out-of-range, and the index of
	// the FIRST UNCONSUMED trace value for surplus (audit F4: the doc
	// previously disagreed with the constructors; the shrinker-facing
	// contract is this one).
	Pos int
	// Bound is the bound the generator requested for exhausted and
	// out-of-range; for surplus it is the COUNT of unconsumed draws.
	Bound int
	// Value is the offending trace value (-1 for exhaustion; the first
	// unconsumed value for surplus).
	Value  int
	Reason string // "exhausted" | "out-of-range" | "surplus"
}

func (e *ReplayError) Error() string {
	switch e.Reason {
	case "exhausted":
		return fmt.Sprintf("gen: replay trace exhausted at draw %d (generator requested a draw in [0,%d))", e.Pos, e.Bound)
	case "out-of-range":
		return fmt.Sprintf("gen: replay value %d at draw %d is outside the requested bound [0,%d)", e.Value, e.Pos, e.Bound)
	case "surplus":
		return fmt.Sprintf("gen: replay trace has %d unconsumed draws after generation completed (next: %d)", e.Bound, e.Value)
	}
	return "gen: replay violation (" + e.Reason + ")"
}

// replaySource consumes a recorded trace. Violations PANIC with a
// *ReplayError — Generate recovers them into an ordinary error (the same
// discipline as the empty-choice-space assertion); there is never a
// silent fallback to randomness.
type replaySource struct {
	trace []int
	pos   int
}

func (s *replaySource) draw(n int) int {
	if s.pos >= len(s.trace) {
		panic(&ReplayError{Pos: s.pos, Bound: n, Value: -1, Reason: "exhausted"})
	}
	v := s.trace[s.pos]
	if v < 0 || v >= n {
		panic(&ReplayError{Pos: s.pos, Bound: n, Value: v, Reason: "out-of-range"})
	}
	s.pos++
	return v
}

// chooser is the single choice primitive: weights × legality mask →
// renormalize → one draw. Never a rejection loop, never a silent fallback.
// Every random draw in the generator flows through here and is appended to
// the draw trace; with a replay source the chooser is a DECODER from choice
// sequence to program (audit Phase 3) — the seam shrinking-by-regeneration
// builds on.
type chooser struct {
	src    drawSource
	replay *replaySource // nil when seeded
	tape   []int
	stats  map[string]*SiteStats
}

func newChooser(seed int64) *chooser {
	return &chooser{src: &seededSource{rng: rand.New(rand.NewSource(seed))}, stats: map[string]*SiteStats{}}
}

func newReplayChooser(trace []int) *chooser {
	// Copied, not aliased: the caller's slice must not change decode
	// behavior mid-generation (the same rule as Config maps).
	rs := &replaySource{trace: append([]int(nil), trace...)}
	return &chooser{src: rs, replay: rs, stats: map[string]*SiteStats{}}
}

// draw returns an int in [0,n) from the source, recorded on the tape.
func (c *chooser) draw(n int) int {
	// A non-positive bound is a GENERATOR bug (empty pick, missing total
	// leaf), and must stay one in replay mode too — without this guard the
	// replay source reported it as a trace violation, inverting blame onto
	// the trace and letting a shrinker silently walk past a real defect
	// (audit F3: the disguised third outcome).
	if n <= 0 {
		panic(fmt.Sprintf("gen: draw with non-positive bound %d — generator bug, not a trace violation", n))
	}
	v := c.src.draw(n)
	c.tape = append(c.tape, v)
	return v
}

// chance reports a 1-in-n draw.
func (c *chooser) chance(n int) bool { return c.draw(n) == 0 }

// choose picks one arm by masked weighted draw. site names the hole for the
// frequency report. An empty masked space is a generator bug — every site
// must include a total leaf arm — so it panics rather than falling back.
func (c *chooser) choose(site string, arms []arm) arm {
	st := c.stats[site]
	if st == nil {
		st = &SiteStats{Valid: map[string]int{}, Chosen: map[string]int{}}
		c.stats[site] = st
	}
	total := 0
	for _, a := range arms {
		if a.ok && a.weight > 0 {
			total += a.weight
			st.Valid[a.name]++
		}
	}
	if total == 0 {
		panic("gen: empty choice space at site " + site + " — missing total leaf arm (generator bug)")
	}
	r := c.draw(total)
	for _, a := range arms {
		if !a.ok || a.weight <= 0 {
			continue
		}
		if r < a.weight {
			st.Chosen[a.name]++
			return a
		}
		r -= a.weight
	}
	panic("gen: unreachable")
}

// pick returns a uniform element of items, via the tape.
func pick[T any](c *chooser, items []T) T {
	return items[c.draw(len(items))]
}
