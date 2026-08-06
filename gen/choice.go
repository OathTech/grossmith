package gen

import "math/rand"

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

// chooser is the single choice primitive: weights × legality mask →
// renormalize → one draw. Never a rejection loop, never a silent fallback.
// Every random draw in the generator flows through here and is appended to
// the draw trace. The INTENDED end state is a decoder from choice sequence
// to program (shrinking-by-regeneration, coverage-guided search); until a
// replay source with exhaustion/out-of-range semantics exists, the trace is
// a log, not an input.
type chooser struct {
	rng   *rand.Rand
	tape  []int
	stats map[string]*SiteStats
}

func newChooser(seed int64) *chooser {
	return &chooser{rng: rand.New(rand.NewSource(seed)), stats: map[string]*SiteStats{}}
}

// draw returns a uniform int in [0,n), recorded on the tape.
func (c *chooser) draw(n int) int {
	v := c.rng.Intn(n)
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
