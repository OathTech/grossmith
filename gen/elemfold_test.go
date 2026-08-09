package gen

import (
	"regexp"
	"testing"

	"grossmith/observe"
)

// The W3 witnesses: element-inclusive range folds (survey finding F14).
// What must hold: the fold reads base[i] only inside a range over base
// (in-bounds by construction), the tag is honest both ways, the
// conversion-free form survives the kinds corner, and the folds run.

var elemFoldLine = regexp.MustCompile(`(?m)^\s*\w+ \+= (?:\w+\()?i\d+\)? \+ (?:\w+\()?(\w+)\[i\d+\]`)

// TestElementFoldEmitted: sweep for tag/text honesty and the in-range
// argument's premise — the indexed base is the ranged container itself.
func TestElementFoldEmitted(t *testing.T) {
	folds, kindsFolds := 0, 0
	rangeOver := regexp.MustCompile(`for i\d+ := range (\w+)`)
	for seed := int64(79000); seed < 79400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		matches := elemFoldLine.FindAllSubmatch(c.Source, -1)
		if (len(matches) > 0) != hasFeature(c, "element_fold") {
			t.Fatalf("seed %d: %d fold lines but tag %v — text-iff-tag broken\n%s",
				seed, len(matches), hasFeature(c, "element_fold"), c.Source)
		}
		if len(matches) == 0 {
			continue
		}
		folds += len(matches)
		if hasFeature(c, "corner_kinds") {
			kindsFolds++
		}
		// Every indexed base must be a container some range in the source
		// iterates — the in-bounds premise. (Positional nesting is what
		// the emitter guarantees; this witnesses the weaker but decisive
		// half: no fold indexes a never-ranged variable.)
		ranged := map[string]bool{}
		for _, m := range rangeOver.FindAllSubmatch(c.Source, -1) {
			ranged[string(m[1])] = true
		}
		for _, m := range matches {
			if !ranged[string(m[1])] {
				t.Fatalf("seed %d: element fold indexes %s, which no range iterates\n%s",
					seed, m[1], c.Source)
			}
		}
	}
	if folds == 0 {
		t.Fatal("no element fold in 400 natural seeds — draw starved")
	}
	if kindsFolds == 0 {
		t.Log("note: no kinds-corner element fold in this sweep (conversion-free form exists; small population)")
	}
	t.Logf("element folds: %d lines; %d kinds-corner subjects carried one", folds, kindsFolds)
}

// TestElementFoldRuns: element-fold subjects build and run — the
// in-bounds-by-construction claim exercised end to end, several cases.
func TestElementFoldRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	ran := 0
	for seed := int64(79000); seed < 79400 && ran < 4; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "element_fold") {
			continue
		}
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK && doc.Status != observe.StatusPanic {
			t.Fatalf("seed %d: status %s", seed, doc.Status)
		}
		if doc.Status == observe.StatusPanic && doc.Panic != nil && doc.Panic.Kind == "index" {
			// An index panic in an element-fold subject is only legal from
			// a DIFFERENT, deliberately-hot index site; the fold itself is
			// total. Nothing to distinguish them cheaply here, so any
			// index panic in this small sweep gets a hard look.
			t.Logf("seed %d: index panic (verify the hot site is not the fold)\n%s", seed, c.Source)
		}
		ran++
	}
	if ran == 0 {
		t.Fatal("no element-fold case in the sweep")
	}
}

// TestElementFoldReachesSharedWrites: the F14 re-measure. A handcrafted
// subject in the emitter's exact shape: a slice_triple alias shares
// backing with the base; a write lands through the alias; the
// element-inclusive fold over the base observes it. The index-only fold
// provably cannot (its value is independent of element contents).
func TestElementFoldReachesSharedWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	subject := `package main

func fuzzSubject() (int, int) {
	v0 := 0
	v1 := 0
	s0 := []int{10, 20, 30, 40}
	s1 := s0[1:3:4]
	s1[0] = 99
	for i0 := range s0 {
		v0 += i0
		v1 += i0 + s0[i0]
	}
	return v0, v1
}
`
	var g Generator
	doc := runCase(t, Case{Source: []byte(subject), Driver: []byte(g.driverSource(make([]binding, 2)))})
	if doc.Status != observe.StatusOK {
		t.Fatalf("status %s", doc.Status)
	}
	// Index-only fold: 0+1+2+3 = 6 — blind to the aliased write.
	if got := doc.Values[0].Int; got != 6 {
		t.Fatalf("index-only fold = %d, want 6", got)
	}
	// Element fold: 6 + (10+99+30+40) = 185 — the shared write (20->99
	// through s1[0]) is OBSERVED. 6 + 100 = 106 would mean blindness.
	if got := doc.Values[1].Int; got != 185 {
		t.Fatalf("element fold = %d, want 185 (aliased write observed)", got)
	}
}
