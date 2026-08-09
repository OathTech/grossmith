package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grossmith/gen"
	"grossmith/observe"
)

// control is one row of the Phase 2 positive-control matrix: a driver
// perturbation targeting exactly one observation shape, and the
// requirement a pool case must satisfy for the perturbation to be
// detectable at all.
type control struct {
	name string
	// old→new applied to the doctored clone's driver.go (every
	// occurrence; the witness fails loudly if none applied — that means
	// the driver drifted and the matrix is stale).
	old, new string
	// requires returns true if this reference document exercises the
	// shape this control perturbs.
	requires func(observe.Document) bool
	// wantInfra: this control corrupts the document's CANONICAL FORM
	// rather than an observed value, so the fail-closed parse (E1,
	// Document.Validate) must classify it as clone infrastructure — a
	// producer speaking the protocol wrong is not a semantic divergence.
	// The matrix witnesses BOTH channels: value corruption must
	// mismatch, form corruption must fail closed.
	wantInfra bool
}

// hasKind checks the RETURN tuple only: the scalar perturbations target
// _gReflect's serializer arms, which events never pass through (the obs*
// payload sites spell their fields differently) — an event-only
// occurrence cannot exercise them (audit finding 3: the string
// requirement was being satisfied by an event-only case and the control
// passed on an accidental carrier).
func hasKind(d observe.Document, kind string) bool {
	found := false
	walkReturnValues(d, func(v observe.Value) {
		if v.Kind == kind {
			found = true
		}
	})
	return found
}

func walkReturnValues(d observe.Document, f func(observe.Value)) {
	var walk func(v observe.Value)
	walk = func(v observe.Value) {
		f(v)
		for _, e := range v.Elems {
			walk(e)
		}
		for _, fl := range v.Fields {
			walk(fl.Value)
		}
		for _, e := range v.Entries {
			walk(e.Key)
			walk(e.Value)
		}
		if v.Payload != nil {
			walk(*v.Payload)
		}
	}
	for _, v := range d.Values {
		walk(v)
	}
}

func sensitivityControls() []control {
	return []control{
		// Value shifts are ORDER-PRESERVING (+1, not negation/bit-flip):
		// the mutation must corrupt values while leaving map keys sorted,
		// or the fail-closed parse rejects the document before the value
		// divergence can be judged (E1 made canonical form enforceable).
		{"int", `"int": v.Int()`, `"int": v.Int() + 1`,
			func(d observe.Document) bool { return hasKind(d, "int") }, false},
		{"uint", `"uint": v.Uint()`, `"uint": v.Uint() + 1`,
			func(d observe.Document) bool { return hasKind(d, "uint") }, false},
		{"bool", `"bool": v.Bool()`, `"bool": !v.Bool()`,
			func(d observe.Document) bool { return hasKind(d, "bool") }, false},
		{"string", `"str": v.String()`, `"str": v.String() + "x"`,
			func(d observe.Document) bool { return hasKind(d, "string") }, false},
		// len and map-order corrupt the CANONICAL FORM (len no longer
		// matches the element count; keys no longer sorted): since E1
		// these are documents the protocol cannot produce, and the
		// expected outcome is the fail-closed infra classification.
		{"len", `"len": v.Len()`, `"len": v.Len() + 1`,
			func(d observe.Document) bool {
				return hasKind(d, "slice") || hasKind(d, "array") || hasKind(d, "map")
			}, true},
		{"field-name", `v.Type().Field(i).Name`, `v.Type().Field(i).Name + "X"`,
			func(d observe.Document) bool { return hasKind(d, "struct") }, false},
		// Flipping every comparator reverses map entry order; canonical
		// form makes order part of equality, so any observed map with two
		// or more entries must diverge.
		{"map-order", ` < b.`, ` > b.`,
			func(d observe.Document) bool {
				ok := false
				walkReturnValues(d, func(v observe.Value) {
					if v.Kind == "map" && len(v.Entries) >= 2 {
						ok = true
					}
				})
				return ok
			}, true},
		{"dynType", `_gGoType(inner.Type())`, `_gGoType(inner.Type()) + "X"`,
			func(d observe.Document) bool {
				ok := false
				walkReturnValues(d, func(v observe.Value) {
					if v.Kind == "interface" && v.Payload != nil {
						ok = true
					}
				})
				return ok
			}, false},
		{"event-order",
			`_gEvents = append(_gEvents, map[string]any{"at": at, "value": v})`,
			`_gEvents = append([]map[string]any{{"at": at, "value": v}}, _gEvents...)`,
			func(d observe.Document) bool {
				// Prepending reverses the value-event sequence, visible
				// only if it is not a palindrome (audit finding 4: two
				// identical events reversed are indistinguishable).
				keys := []string{}
				for _, e := range d.Events {
					if e.Value != nil {
						keys = append(keys, fmt.Sprintf("%s|%s|%v|%v|%v|%v",
							e.At, e.Value.GoType, e.Value.Bool, e.Value.Int, e.Value.Uint, e.Value.Str))
					}
				}
				for i := range keys {
					if keys[i] != keys[len(keys)-1-i] {
						return true
					}
				}
				return false
			}, false},
		// Event POSITION identity (audit gap: Event.At was a compared
		// field with no unequal-state witness): collapse every value
		// event's at to "point"; a doc with a defer event diverges.
		{"event-at",
			`_gEvents = append(_gEvents, map[string]any{"at": at, "value": v})`,
			`_gEvents = append(_gEvents, map[string]any{"at": "point", "value": v})`,
			func(d observe.Document) bool {
				for _, e := range d.Events {
					if e.Value != nil && e.At == "defer" {
						return true
					}
				}
				return false
			}, false},
		{"panic-kind", `return "divide"`, `return "other"`,
			func(d observe.Document) bool {
				return d.Panic != nil && d.Panic.Kind == observe.PanicDivide
			}, false},
		// Width erasure: int8 reported as int16. (ReplaceAll also turns
		// uint8 into uint16 — a broader goType perturbation, same shape.)
		{"width",
			`return _gstrings.ReplaceAll(t.String(), "main.", "")`,
			`return _gstrings.ReplaceAll(_gstrings.ReplaceAll(t.String(), "main.", ""), "int8", "int16")`,
			func(d observe.Document) bool {
				ok := false
				walkReturnValues(d, func(v observe.Value) {
					if v.GoType == "int8" {
						ok = true
					}
				})
				return ok
			}, false},
	}
}

// TestSensitivityMatrix is Phase 2's per-shape positive control (audit:
// "every supported observation shape has a targeted unequal-state
// witness"), extended by E1 with a second channel: a clone doctored in
// a VALUE shape must produce observation-mismatch, while a clone
// doctored in the document's CANONICAL FORM (unsorted keys, wrong len)
// must be stopped by the fail-closed parse as clone infrastructure —
// each control declares which channel it witnesses, and neither may
// leak into the other.
//
// The pool is requirement-driven: seeds are consumed in order and a
// case is kept iff its REFERENCE document satisfies a still-unmet
// control requirement. If the generator stops producing a shape, this
// fails at pool-build naming the shape — never by silently thinning
// the matrix.
func TestSensitivityMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	controls := sensitivityControls()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := &GcAdapter{AdapterName: "ref", Timeout: 20 * time.Second}

	// Build the pool.
	unmet := map[string]bool{}
	for _, c := range controls {
		unmet[c.name] = true
	}
	type poolCase struct {
		dir string
		ref Outcome
	}
	var pool []poolCase
	const seedBase, seedCap = 90000, 90300
	for seed := int64(seedBase); seed < seedCap && len(unmet) > 0; seed++ {
		c, err := gen.New(gen.DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, fmt.Sprintf("case_%05d", seed-seedBase))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), c.Source, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), c.Driver, 0o644); err != nil {
			t.Fatal(err)
		}
		out := ref.Run(ctx, dir)
		if out.Status != StatusRan {
			t.Fatalf("seed %d reference %s: %s", seed, out.Status, out.Detail)
		}
		keep := false
		for _, ctl := range controls {
			if unmet[ctl.name] && ctl.requires(out.Document) {
				delete(unmet, ctl.name)
				keep = true
			}
		}
		if keep {
			pool = append(pool, poolCase{dir: dir, ref: out})
		} else if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if len(unmet) > 0 {
		missing := make([]string, 0, len(unmet))
		for name := range unmet {
			missing = append(missing, name)
		}
		t.Fatalf("pool requirements unmet after %d seeds: %v — the generator no longer produces these shapes",
			seedCap-seedBase, missing)
	}
	t.Logf("pool: %d cases", len(pool))

	// Specificity: the undoctored clone matches everywhere.
	clone := &GcAdapter{AdapterName: "clone", Timeout: 20 * time.Second}
	for _, pc := range pool {
		v, d := Judge(pc.ref, clone.Run(ctx, pc.dir), observe.PanicExact)
		if v != VerdictMatch {
			t.Fatalf("undoctored clone on %s: %s (%s)", pc.dir, v, d)
		}
	}

	// Sensitivity: each doctoring yields >=1 mismatch, and ONLY
	// match/mismatch — a perturbed observation must never be classified
	// as infrastructure.
	for _, ctl := range controls {
		altRoot := t.TempDir()
		if err := copyTree(root, altRoot); err != nil {
			t.Fatal(err)
		}
		drivers, err := filepath.Glob(filepath.Join(altRoot, "*", "driver.go"))
		if err != nil {
			t.Fatal(err)
		}
		applied := 0
		for _, path := range drivers {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			nb := strings.ReplaceAll(string(b), ctl.old, ctl.new)
			if nb != string(b) {
				applied++
			}
			if err := os.WriteFile(path, []byte(nb), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if applied == 0 {
			t.Fatalf("control %s: replacement %q matched no driver — matrix is stale", ctl.name, ctl.old)
		}
		doctored := &treeAdapter{
			inner:   &GcAdapter{AdapterName: "doctored-" + ctl.name, Timeout: 20 * time.Second},
			root:    root,
			altRoot: altRoot,
		}
		hits := 0
		want, wantName := VerdictMismatch, "mismatch"
		if ctl.wantInfra {
			// Canonical-form corruption: the fail-closed parse must stop
			// it BEFORE judging (E1) — infra, never a semantic verdict.
			want, wantName = VerdictCloneInfra, "clone-infra (fail-closed)"
		}
		for _, pc := range pool {
			v, d := Judge(pc.ref, doctored.Run(ctx, pc.dir), observe.PanicExact)
			switch v {
			case VerdictMatch:
			case want:
				hits++
			default:
				t.Fatalf("control %s on %s: verdict %s (want match or %s): %s",
					ctl.name, pc.dir, v, wantName, d)
			}
		}
		if hits == 0 {
			t.Errorf("control %s: no case reached %s — shape not proven end-to-end", ctl.name, wantName)
		} else {
			t.Logf("control %-12s %d/%d cases -> %s", ctl.name, hits, len(pool), wantName)
		}
	}
}
