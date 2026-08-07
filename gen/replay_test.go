package gen

import (
	"bytes"
	"errors"
	"testing"
)

func replayConfigs(seed int64) []Config {
	swarmOff := DefaultConfig(seed)
	swarmOff.Swarm = false
	corner := DefaultConfig(seed)
	corner.Corner = "boundary"
	profile := DefaultConfig(seed)
	profile.NoObserve = []Shape{ShapeSlice, ShapeMap}
	profile.Exclude = []string{"observe_point", "defer", "recover"}
	explicit := DefaultConfig(seed)
	explicit.Swarm = false
	explicit.Constructs = map[string]bool{"loops": true, "if": true, "division": true}
	return []Config{DefaultConfig(seed), swarmOff, corner, profile, explicit}
}

// TestReplayRoundTrip is Phase 3's core witness: decoding a recorded
// trace under the recorded config reproduces the case BYTE-IDENTICALLY —
// source, driver, features, and the re-recorded trace itself — across
// swarm, corners, capability profiles, and explicit construct sets.
func TestReplayRoundTrip(t *testing.T) {
	for seed := int64(60000); seed < 60040; seed++ {
		for ci, cfg := range replayConfigs(seed) {
			orig, err := New(cfg).Generate()
			if err != nil {
				t.Fatalf("seed %d cfg %d: %v", seed, ci, err)
			}
			rep, err := NewReplay(cfg, orig.Tape).Generate()
			if err != nil {
				t.Fatalf("seed %d cfg %d replay: %v", seed, ci, err)
			}
			if !bytes.Equal(orig.Source, rep.Source) {
				t.Fatalf("seed %d cfg %d: replayed source differs", seed, ci)
			}
			if !bytes.Equal(orig.Driver, rep.Driver) {
				t.Fatalf("seed %d cfg %d: replayed driver differs", seed, ci)
			}
			if len(orig.Tape) != len(rep.Tape) {
				t.Fatalf("seed %d cfg %d: re-recorded trace length %d != %d", seed, ci, len(rep.Tape), len(orig.Tape))
			}
			for i := range orig.Tape {
				if orig.Tape[i] != rep.Tape[i] {
					t.Fatalf("seed %d cfg %d: trace diverges at %d", seed, ci, i)
				}
			}
		}
	}
}

// TestReplayFailsClosed: exhaustion, out-of-range, and surplus are typed
// errors carrying the failing position — never a silent fallback and
// never a partial case.
func TestReplayFailsClosed(t *testing.T) {
	cfg := DefaultConfig(424242)
	orig, err := New(cfg).Generate()
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, trace []int, reason string) {
		t.Helper()
		c, err := NewReplay(cfg, trace).Generate()
		var re *ReplayError
		if !errors.As(err, &re) {
			t.Fatalf("%s: got %v, want *ReplayError", name, err)
		}
		if re.Reason != reason {
			t.Fatalf("%s: reason %q, want %q (%v)", name, re.Reason, reason, re)
		}
		if c.Source != nil {
			t.Fatalf("%s: violation returned a partial case", name)
		}
	}
	check("truncated", orig.Tape[:len(orig.Tape)/2], "exhausted")

	oor := append([]int(nil), orig.Tape...)
	oor[0] = 1 << 30
	check("out-of-range", oor, "out-of-range")

	check("surplus", append(append([]int(nil), orig.Tape...), 0, 0, 0), "surplus")

	check("empty", nil, "exhausted")
}

// TestReplayMutationIsValidByConstruction: the shrinking precondition
// (salvage notes: "shrink the draw trace and regenerate — every candidate
// valid by construction"). A mutated trace either fails closed with a
// *ReplayError (a rejected candidate) or decodes to a program that
// typechecks — never an invalid program, never a non-replay panic.
func TestReplayMutationIsValidByConstruction(t *testing.T) {
	mutated, rejected := 0, 0
	for seed := int64(61000); seed < 61030; seed++ {
		cfg := DefaultConfig(seed)
		orig, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		// Mutate a spread of positions; values drawn small (most bounds
		// are small, so small values are the interesting decodes).
		for _, frac := range []int{7, 3, 2} {
			pos := len(orig.Tape) / frac
			for delta := 0; delta < 3; delta++ {
				tr := append([]int(nil), orig.Tape...)
				tr[pos] = delta
				c, err := NewReplay(cfg, tr).Generate()
				if err != nil {
					var re *ReplayError
					if !errors.As(err, &re) {
						t.Fatalf("seed %d pos %d: non-replay error: %v", seed, pos, err)
					}
					rejected++
					continue
				}
				mutated++
				typecheckCase(t, c, seed, nil)
			}
		}
	}
	if mutated == 0 {
		t.Fatal("every mutation was rejected — the decoder admits no variation, which starves shrinking")
	}
	t.Logf("mutations: %d decoded valid, %d rejected fail-closed", mutated, rejected)
}
