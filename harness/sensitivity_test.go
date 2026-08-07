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
}

func hasKind(d observe.Document, kind string) bool {
	found := false
	walkValues(d, func(v observe.Value) {
		if v.Kind == kind {
			found = true
		}
	})
	return found
}

func walkValues(d observe.Document, f func(observe.Value)) {
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
	for _, e := range d.Events {
		if e.Value != nil {
			walk(*e.Value)
		}
	}
}

func sensitivityControls() []control {
	return []control{
		{"int", `"int": v.Int()`, `"int": -(v.Int() + 1)`,
			func(d observe.Document) bool { return hasKind(d, "int") }},
		{"uint", `"uint": v.Uint()`, `"uint": v.Uint() ^ 1`,
			func(d observe.Document) bool { return hasKind(d, "uint") }},
		{"bool", `"bool": v.Bool()`, `"bool": !v.Bool()`,
			func(d observe.Document) bool { return hasKind(d, "bool") }},
		{"string", `"str": v.String()`, `"str": v.String() + "x"`,
			func(d observe.Document) bool { return hasKind(d, "string") }},
		{"len", `"len": v.Len()`, `"len": v.Len() + 1`,
			func(d observe.Document) bool {
				return hasKind(d, "slice") || hasKind(d, "array") || hasKind(d, "map")
			}},
		{"field-name", `v.Type().Field(i).Name`, `v.Type().Field(i).Name + "X"`,
			func(d observe.Document) bool { return hasKind(d, "struct") }},
		// Flipping every comparator reverses map entry order; canonical
		// form makes order part of equality, so any observed map with two
		// or more entries must diverge.
		{"map-order", ` < b.`, ` > b.`,
			func(d observe.Document) bool {
				ok := false
				walkValues(d, func(v observe.Value) {
					if v.Kind == "map" && len(v.Entries) >= 2 {
						ok = true
					}
				})
				return ok
			}},
		{"dynType", `_gGoType(inner.Type())`, `_gGoType(inner.Type()) + "X"`,
			func(d observe.Document) bool {
				ok := false
				walkValues(d, func(v observe.Value) {
					if v.Kind == "interface" && v.Payload != nil {
						ok = true
					}
				})
				return ok
			}},
		{"event-order",
			`_gEvents = append(_gEvents, map[string]any{"at": at, "value": v})`,
			`_gEvents = append([]map[string]any{{"at": at, "value": v}}, _gEvents...)`,
			func(d observe.Document) bool {
				n := 0
				for _, e := range d.Events {
					if e.Value != nil {
						n++
					}
				}
				return n >= 2
			}},
		{"panic-kind", `return "divide"`, `return "other"`,
			func(d observe.Document) bool {
				return d.Panic != nil && d.Panic.Kind == observe.PanicDivide
			}},
		// Width erasure: int8 reported as int16. (ReplaceAll also turns
		// uint8 into uint16 — a broader goType perturbation, same shape.)
		{"width",
			`return _gstrings.ReplaceAll(t.String(), "main.", "")`,
			`return _gstrings.ReplaceAll(_gstrings.ReplaceAll(t.String(), "main.", ""), "int8", "int16")`,
			func(d observe.Document) bool {
				ok := false
				walkValues(d, func(v observe.Value) {
					if v.GoType == "int8" {
						ok = true
					}
				})
				return ok
			}},
	}
}

// TestSensitivityMatrix is Phase 2's per-shape positive control (audit:
// "every supported observation shape has a targeted unequal-state
// witness"): for each observation shape, a clone doctored in exactly
// that shape must produce observation-mismatch on a case exercising it,
// and must never leak an infra or harness verdict.
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
		mismatches := 0
		for _, pc := range pool {
			v, d := Judge(pc.ref, doctored.Run(ctx, pc.dir), observe.PanicExact)
			switch v {
			case VerdictMatch:
			case VerdictMismatch:
				mismatches++
			default:
				t.Fatalf("control %s on %s: verdict %s leaked out of the semantic pair: %s",
					ctl.name, pc.dir, v, d)
			}
		}
		if mismatches == 0 {
			t.Errorf("control %s: no case diverged — shape not proven injective end-to-end", ctl.name)
		} else {
			t.Logf("control %-12s %d/%d cases diverged", ctl.name, mismatches, len(pool))
		}
	}
}
