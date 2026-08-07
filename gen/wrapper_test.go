package gen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"grossmith/observe"
)

func hasFeature(c Case, tag string) bool {
	for _, f := range c.Features {
		if f == tag {
			return true
		}
	}
	return false
}

// runCase builds and runs one generated case, returning the parsed
// observation document (driver_test pattern; go.mod per dir per the
// module-mode fix).
func runCase(t *testing.T, c Case) observe.Document {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"subject.go": c.Source,
		"driver.go":  c.Driver,
		"go.mod":     []byte("module grossmith-cases\n\ngo 1.26\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-o", "case.bin", ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	run := exec.Command(filepath.Join(dir, "case.bin"))
	run.Env = []string{}
	var stdout bytes.Buffer
	run.Stdout = &stdout
	if err := run.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	doc, err := observe.Parse(bytes.TrimSpace(stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestMultiAssignEmitted (Phase 4 rung 2, GoLean R2a): the grammar emits
// multi-target assignment in its three shapes — swap, aliased index, and
// mixed targets — across a seed sweep.
func TestMultiAssignEmitted(t *testing.T) {
	swapLine := regexp.MustCompile(`(?m)^\t+(v\d+), (v\d+) = (v\d+), (v\d+)$`)
	isSwap := func(src []byte) bool {
		for _, m := range swapLine.FindAllSubmatch(src, -1) {
			if string(m[1]) == string(m[4]) && string(m[2]) == string(m[3]) {
				return true
			}
		}
		return false
	}
	alias := regexp.MustCompile(`(?m)^\t+v\d+, v\d+\[int\(v\d+%`)
	multi := regexp.MustCompile(`(?m)^\t+[^=\n]+, [^=\n]+ = [^\n]+, `)
	swaps, aliases, tagged := 0, 0, 0
	for seed := int64(500); seed < 900; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "multi_assign") {
			continue
		}
		tagged++
		if !multi.Match(c.Source) {
			t.Fatalf("seed %d: multi_assign tagged but no multi-target line\n%s", seed, c.Source)
		}
		if isSwap(c.Source) {
			swaps++
		}
		if alias.Match(c.Source) {
			aliases++
		}
	}
	if tagged == 0 || swaps == 0 || aliases == 0 {
		t.Fatalf("multi-assign shapes starved: %d tagged, %d swaps, %d aliased", tagged, swaps, aliases)
	}
	t.Logf("multi_assign: %d tagged, %d with swaps, %d with aliased indexes", tagged, swaps, aliases)
}

// TestKindsCorner (Phase 4 rung 3, GoLean R3): the kinds corner is
// conversion-FREE (their constraint: int(x) laundering masks the
// kind-defaulting bug class), densifies inc/dec and compound sites, and
// reaches defined-type targets — the BUG-042/043 family's habitat.
func TestKindsCorner(t *testing.T) {
	incdec := regexp.MustCompile(`(?m)^\t+v\d+(\+\+|--)$`)
	sites, definedTyped := 0, 0
	for seed := int64(4000); seed < 4060; seed++ {
		cfg := DefaultConfig(seed)
		cfg.Corner = "kinds"
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "corner_kinds") {
			t.Fatalf("seed %d: kinds corner not noted", seed)
		}
		if hasFeature(c, "conversions") {
			t.Fatalf("seed %d: conversions tagged inside the kinds corner\n%s", seed, c.Source)
		}
		sites += len(incdec.FindAll(c.Source, -1))
		if hasFeature(c, "defined_types") && incdec.Match(c.Source) {
			definedTyped++
		}
	}
	if sites < 60 {
		t.Fatalf("kinds corner emitted only %d inc/dec sites over 60 seeds — density lever broken", sites)
	}
	if definedTyped == 0 {
		t.Fatal("no kinds-corner case combines defined types with inc/dec sites")
	}
	t.Logf("kinds corner: %d inc/dec sites over 60 seeds, %d cases with defined types present", sites, definedTyped)
}

// TestRecoverWrapperObservesPanics (Phase 4 rung 1, GoLean R1): wrapped
// subjects return status OK with the panic ENCODED in the trailing int
// result and partial state in the others — panic identity and
// store-before-panic state as ordinary observed values, no obs* events.
// Sweeps seeds until both outcomes are seen: a wrapped subject that
// caught a panic (code != 0) and one that exited normally (code == 0).
func TestRecoverWrapperObservesPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	shape := regexp.MustCompile(`(?s)defer func\(\) \{\n\s*if p := recover\(\); p != nil \{`)
	caught, clean, wrapped := 0, 0, 0
	for seed := int64(95000); seed < 95400 && (caught == 0 || clean == 0); seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !hasFeature(c, "recover_wrapper") {
			continue
		}
		wrapped++
		if !shape.Match(c.Source) {
			t.Fatalf("seed %d: wrapper tag without wrapper shape\n%s", seed, c.Source)
		}
		doc := runCase(t, c)
		if doc.Status != observe.StatusOK {
			// A wrapped subject can still panic-terminate only via a
			// DRIVER-visible escape, which the wrapper should prevent.
			t.Fatalf("seed %d: wrapped subject status %s", seed, doc.Status)
		}
		if len(doc.Values) == 0 {
			t.Fatalf("seed %d: no observed values", seed)
		}
		code := doc.Values[len(doc.Values)-1]
		if code.Kind != "int" || code.GoType != "int" {
			t.Fatalf("seed %d: trailing result is %s/%s, want the int panic code", seed, code.Kind, code.GoType)
		}
		if code.Int != 0 {
			caught++
			if code.Int < 1 || code.Int > 5 {
				t.Fatalf("seed %d: panic code %d outside the table", seed, code.Int)
			}
		} else {
			clean++
		}
	}
	if caught == 0 || clean == 0 {
		t.Fatalf("wrapper sweep did not see both outcomes: %d caught, %d clean over %d wrapped subjects",
			caught, clean, wrapped)
	}
	t.Logf("wrapped subjects: %d caught / %d clean (of %d run before both seen)", caught, clean, caught+clean)
}
