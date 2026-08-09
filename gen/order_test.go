package gen

import (
	"bytes"
	"go/ast"
	"reflect"
	"strconv"
	"testing"

	"grossmith/observe"
)

// The R2b order-witness witnesses (witness arc W2). The construct under
// test is an INSTRUMENT: a package accumulator wOrd folded by the sole
// designated impure helper wit(x, tag), wrapped around drawn operands at
// evaluation-order-rich sites, order-corner only. What must hold:
// text-iff-tag both ways, corner containment, tag uniqueness, purity of
// every other function, a live trailing slot, run-to-run determinism,
// and the E3 panic-truncation composition under the recover wrapper.

// witCalls returns the tag literal of every wit(...) call in the file,
// per enclosing function name.
func witCalls(t *testing.T, file *ast.File) map[string][]int {
	t.Helper()
	out := map[string][]int{}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "wit" {
				return true
			}
			if len(call.Args) != 2 {
				t.Fatalf("wit call with %d args in %s", len(call.Args), fd.Name.Name)
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok {
				t.Fatalf("wit tag is not a literal in %s", fd.Name.Name)
			}
			tag, err := strconv.Atoi(lit.Value)
			if err != nil {
				t.Fatalf("wit tag %q: %v", lit.Value, err)
			}
			out[fd.Name.Name] = append(out[fd.Name.Name], tag)
			return true
		})
	}
	return out
}

// TestOrderWitnessEmitted: forced-corner sweep for structure and honesty.
func TestOrderWitnessEmitted(t *testing.T) {
	witnessed := 0
	for seed := int64(72000); seed < 72200; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "order"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		hasText := bytes.Contains(c.Source, []byte("wit("))
		if hasText != hasFeature(c, "order_witness") {
			t.Fatalf("seed %d: wit text %v but tag %v — text-iff-tag broken\n%s",
				seed, hasText, !hasText, c.Source)
		}
		if !hasText {
			// A corner subject may legitimately draw no eligible site;
			// then NO witness machinery may exist either.
			if bytes.Contains(c.Source, []byte("wOrd")) {
				t.Fatalf("seed %d: wOrd machinery without a wit call\n%s", seed, c.Source)
			}
			continue
		}
		witnessed++
		for _, want := range []string{"var wOrd int", "func wit(x int, tag int) int"} {
			if !bytes.Contains(c.Source, []byte(want)) {
				t.Fatalf("seed %d: wit calls but no %q\n%s", seed, want, c.Source)
			}
		}
		// The accumulator must reach the observation: wOrd in a return.
		if !bytes.Contains(c.Source, []byte(", wOrd")) && !bytes.Contains(c.Source, []byte("return wOrd")) {
			t.Fatalf("seed %d: wOrd never returned\n%s", seed, c.Source)
		}
		_, file := parseCase(t, c.Source, seed)
		calls := witCalls(t, file)
		// Purity (E4): wit calls may appear in the subject ONLY. Every
		// helper and method stays pure — a wit call inside one would make
		// its callers' evaluation order observable outside the discipline.
		for fn, tags := range calls {
			if fn != Subject {
				t.Fatalf("seed %d: wit call inside %s — E4 violated\n%s", seed, fn, c.Source)
			}
			// Tags are exactly {1..n}, no gaps, no repeats (draw order,
			// not lexical order, so only the SET is pinned).
			seen := map[int]bool{}
			for _, tag := range tags {
				if tag < 1 || tag > len(tags) || seen[tag] {
					t.Fatalf("seed %d: wit tags %v are not exactly 1..%d", seed, tags, len(tags))
				}
				seen[tag] = true
			}
		}
	}
	if witnessed < 100 {
		t.Fatalf("only %d/200 forced-corner subjects carry witnesses — density collapsed", witnessed)
	}
	t.Logf("witnessed subjects: %d/200", witnessed)
}

// TestOrderWitnessContained: outside the order corner the witness text
// must not exist — instruments are minorities, and the corner IS the
// minority mechanism (mid-arc review bar).
func TestOrderWitnessContained(t *testing.T) {
	corner := 0
	for seed := int64(73000); seed < 73400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if hasFeature(c, "corner_order") {
			corner++
			continue
		}
		for _, kw := range []string{"wit(", "wOrd"} {
			if bytes.Contains(c.Source, []byte(kw)) {
				t.Fatalf("seed %d: %q outside the order corner\n%s", seed, kw, c.Source)
			}
		}
	}
	if corner == 0 {
		t.Fatal("no natural order-corner subject in 400 seeds — swarm arm starved")
	}
	t.Logf("natural order-corner rate: %d/400", corner)
}

// TestOrderWitnessDeterministic: the accumulator is a deterministic
// fingerprint — same subject, two runs, byte-identical documents, and the
// witness slot actually accumulated (nonzero) in at least one subject.
func TestOrderWitnessDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	sawLive := false
	checked := 0
	for seed := int64(74000); seed < 74200 && checked < 3; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "order"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		// Unwrapped witnessed subjects only: there the witness slot is the
		// LAST observed value, so the assertion needs no arity bookkeeping.
		if !hasFeature(c, "order_witness") || hasFeature(c, "recover_wrapper") {
			continue
		}
		checked++
		doc1 := runCase(t, c)
		doc2 := runCase(t, c)
		if !reflect.DeepEqual(doc1, doc2) {
			t.Fatalf("seed %d: two runs disagree\n%+v\n%+v", seed, doc1, doc2)
		}
		if doc1.Status != observe.StatusOK {
			continue // a panicking unwrapped subject reports panic; fine
		}
		slot := doc1.Values[len(doc1.Values)-1]
		if slot.Kind != "int" || slot.GoType != "int" {
			t.Fatalf("seed %d: trailing slot is %s/%s, want int wOrd", seed, slot.Kind, slot.GoType)
		}
		if slot.Int != 0 {
			sawLive = true
		}
	}
	if checked == 0 {
		t.Fatal("no unwrapped witnessed subject in the sweep")
	}
	if !sawLive {
		t.Fatal("every checked witness accumulator was zero — instrument never ran")
	}
}

// truncationSubject is HANDCRAFTED in the emitter's exact shape: a
// wrapped, witnessed subject whose statement 2 panics AFTER both of its
// wit operands accumulated, with statement 3's witness never running.
// E3 as intended behavior: order-before-panic (wOrd = ((0*31+1)*31+2)*31+3
// = 1026), site (2), and store-before-panic partial state (v0 = 1)
// compose in one document.
const truncationSubject = `package main

var wOrd int

func wit(x int, tag int) int {
	wOrd = wOrd*31 + tag
	return x
}

func fuzzSubject() (q0 int, q1 int, qP int) {
	v0 := 0
	psite := 0
	defer func() {
		if recover() != nil {
			qP = psite
			q1 = wOrd
			q0 = v0
		}
	}()
	psite = 1
	v0 = wit(1, 1)
	psite = 2
	v0 = wit(2, 2) / wit(v0-v0, 3)
	psite = 3
	v0 = wit(4, 4)
	return v0, wOrd, 0
}
`

func TestOrderWitnessPanicTruncation(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	var g Generator
	c := Case{Source: []byte(truncationSubject), Driver: []byte(g.driverSource(make([]binding, 3)))}
	doc := runCase(t, c)
	if doc.Status != observe.StatusOK {
		t.Fatalf("status %s", doc.Status)
	}
	if got := doc.Values[2].Int; got != 2 {
		t.Fatalf("site = %d, want 2 (the dividing statement)", got)
	}
	if got := doc.Values[1].Int; got != 1026 {
		t.Fatalf("wOrd = %d, want 1026 (tags 1,2,3 accumulated; 4 truncated)", got)
	}
	if got := doc.Values[0].Int; got != 1 {
		t.Fatalf("partial state = %d, want 1 (statement 2's store never landed)", got)
	}
}
