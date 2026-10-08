package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseProfileRefusals(t *testing.T) {
	bad := map[string]string{
		"schema":         `{"schema":"x","name":"a","constructs":[]}`,
		"empty name":     `{"schema":"grossmith-profile-v1","name":"","constructs":[]}`,
		"name with path": `{"schema":"grossmith-profile-v1","name":"../a","constructs":[]}`,
		"dot name":       `{"schema":"grossmith-profile-v1","name":"..","constructs":[]}`,
		"missing list":   `{"schema":"grossmith-profile-v1","name":"a"}`,
		"unknown tag":    `{"schema":"grossmith-profile-v1","name":"a","constructs":["loop"]}`,
		"core tag":       `{"schema":"grossmith-profile-v1","name":"a","constructs":["ints"]}`,
		"duplicate tag":  `{"schema":"grossmith-profile-v1","name":"a","constructs":["if","if"]}`,
		"unknown field":  `{"schema":"grossmith-profile-v1","name":"a","constructs":[],"corner":"none"}`,
		"duplicate key":  `{"schema":"grossmith-profile-v1","name":"a","name":"b","constructs":[]}`,
		"trailing":       `{"schema":"grossmith-profile-v1","name":"a","constructs":[]} {}`,
	}
	for name, src := range bad {
		if _, err := ParseProfile([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p, err := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"core-only","constructs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Excluded(), Optional()) {
		t.Fatal("an empty allow-list must exclude every optional construct")
	}
}

func TestProfileApplyUnionsAndOrders(t *testing.T) {
	p, err := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"a","constructs":["if","defer","loops"]}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(1)
	cfg.Exclude = []string{"defer"} // an adapter's own exclusion survives
	got := p.Apply(cfg).Exclude
	if containsTag(got, "if") || containsTag(got, "loops") || !containsTag(got, "defer") {
		t.Fatalf("Apply: %v", got)
	}
	if len(got) != len(Optional())-2 {
		t.Fatalf("Apply excluded %d tags, want %d", len(got), len(Optional())-2)
	}
	// Optional() order, independent of the inputs' order.
	idx := map[string]int{}
	for i, t := range Optional() {
		idx[t] = i
	}
	for i := 1; i < len(got); i++ {
		if idx[got[i-1]] > idx[got[i]] {
			t.Fatalf("Apply's exclusions are not in Optional() order: %v", got)
		}
	}
}

func loadFixtureProfile(t *testing.T, name string) Profile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "profiles", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestProfilesConfineEmission: across the fixture profiles, no subject or
// battery function records an optional construct outside the allow-list,
// and swarm sampling still varies the mix within it.
func TestProfilesConfineEmission(t *testing.T) {
	n := int64(300)
	if testing.Short() {
		n = 60
	}
	for _, name := range []string{"f0", "f1", "f2", "f3"} {
		p := loadFixtureProfile(t, name)
		seen := map[string]bool{}
		for seed := int64(1); seed <= n; seed++ {
			cfg := p.Apply(batteryConfig(seed, FunctionPanicsBiased))
			c, err := New(cfg).Generate()
			if err != nil {
				t.Fatalf("%s seed %d: %v", name, seed, err)
			}
			if err := p.CheckFeatures(c); err != nil {
				t.Fatalf("%s seed %d: %v", name, seed, err)
			}
			for tag := range c.FeatureCounts {
				seen[tag] = true
			}
		}
		// Not vacuous: most allowed constructs are realised somewhere.
		realised := 0
		for _, tag := range p.Constructs {
			if seen[tag] {
				realised++
			}
		}
		if realised*2 < len(p.Constructs) {
			t.Fatalf("%s: only %d of %d allowed constructs realised over %d seeds", name, realised, len(p.Constructs), n)
		}
	}
}

func TestCheckFeaturesRefuses(t *testing.T) {
	p := loadFixtureProfile(t, "f0")
	if err := p.CheckFeatures(Case{FeatureCounts: map[string]int{"loops": 1}}); err == nil || !strings.Contains(err.Error(), "loops") {
		t.Fatalf("subject: %v", err)
	}
	c := Case{FeatureCounts: map[string]int{"if": 1, "panic_risk": 2},
		Functions: []BatteryFunction{{Name: "bf0", Features: map[string]int{"division": 1}}}}
	if err := p.CheckFeatures(c); err == nil || !strings.Contains(err.Error(), "bf0") {
		t.Fatalf("battery function: %v", err)
	}
	// Info tags and core constructs are not gated.
	if err := p.CheckFeatures(Case{FeatureCounts: map[string]int{"panic_risk": 1, "ints": 3, "corner_kinds": 1}}); err != nil {
		t.Fatal(err)
	}
}

// TestCoreOnlyProfileEmitsNoComparisons pins G2 review finding 1: under
// a profile allowing no optional constructs, neither the subject nor the
// battery functions contain a comparison or equality operator (the
// battery's constant-bool fallback used to add one unmarked).
func TestCoreOnlyProfileEmitsNoComparisons(t *testing.T) {
	p, err := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"core","constructs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	n := int64(300)
	if testing.Short() {
		n = 60
	}
	for seed := int64(1); seed <= n; seed++ {
		c, err := New(p.Apply(batteryConfig(seed, FunctionPanicsBiased))).Generate()
		if err != nil {
			t.Fatal(err)
		}
		for name, src := range map[string][]byte{"subject": c.Source, "functions.go": c.FunctionsSource} {
			f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if b, ok := n.(*ast.BinaryExpr); ok {
					switch b.Op {
					case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
						t.Fatalf("seed %d %s: %s operator emitted under a core-only profile", seed, name, b.Op)
					}
				}
				return true
			})
		}
	}
}
