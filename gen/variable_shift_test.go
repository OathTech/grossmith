package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func variableShiftConfig(seed int64) Config {
	cfg := batteryConfig(seed, FunctionPanicsNone)
	cfg.Include = []string{"variable_shift"}
	return cfg
}

// countVariableShifts counts shifts whose count is not a constant
// literal.
func countVariableShifts(t *testing.T, src []byte) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(x ast.Node) bool {
		if b, ok := x.(*ast.BinaryExpr); ok && (b.Op == token.SHL || b.Op == token.SHR) {
			if _, lit := b.Y.(*ast.BasicLit); !lit {
				n++
			}
		}
		return true
	})
	return n
}

// TestVariableShiftIsOptIn: off unless requested (no draws, no emission,
// even with a nil construct map), on when included, refused by Exclude.
func TestVariableShiftIsOptIn(t *testing.T) {
	sawSubject, sawBattery, noShift := 0, 0, 0
	for seed := int64(1); seed <= 200; seed++ {
		all := DefaultConfig(seed)
		all.Swarm = false // nil construct map: every optional tag enabled
		off, err := New(all).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if countVariableShifts(t, off.Source) != 0 || off.FeatureCounts["variable_shift"] != 0 {
			t.Fatalf("seed %d: variable shift emitted without being requested", seed)
		}
		on, err := New(variableShiftConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := countVariableShifts(t, on.Source), on.FeatureCounts["variable_shift"]; got != want {
			t.Fatalf("seed %d: %d variable shifts in the subject, %d recorded", seed, got, want)
		}
		sawSubject += on.FeatureCounts["variable_shift"]
		sawBattery += countVariableShifts(t, on.FunctionsSource)
		if on.FeatureCounts["shifts"] == 0 && on.FeatureCounts["variable_shift"] == 0 {
			// Requesting the tag force-enables shifts; a case without a
			// shift site at all is still possible, but rare.
			noShift++
		}
	}
	if noShift > 20 {
		t.Fatalf("%d of 200 requested cases had no shift site: the request is not forcing shifts on", noShift)
	}
	if sawSubject == 0 || sawBattery == 0 {
		t.Fatalf("variable shifts never realised (subject %d, battery %d)", sawSubject, sawBattery)
	}
}

// TestVariableShiftRuns: subjects with variable shifts build and run, and
// battery functions with them never panic at FunctionPanics none (counts
// are unsigned; counts at and past the width occur in the inputs).
func TestVariableShiftRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	ran := 0
	for seed := int64(1); seed <= 40 && ran < 8; seed++ {
		c, err := New(variableShiftConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if c.FeatureCounts["variable_shift"] == 0 && countVariableShifts(t, c.FunctionsSource) == 0 {
			continue
		}
		ran++
		g := New(variableShiftConfig(seed))
		if _, err := g.Generate(); err != nil {
			t.Fatal(err)
		}
		if measured, charged := measureExec(t, c), int64(ExecBudget)-g.budgetLeft; measured > charged {
			t.Fatalf("seed %d: measured %d executed statements, charged %d", seed, measured, charged)
		}
		rows, end, _ := runBattery(t, c, false)
		if len(rows) != end {
			t.Fatalf("seed %d: %d rows, END %d", seed, len(rows), end)
		}
		for _, r := range rows {
			if r[3] == "panic" {
				t.Fatalf("seed %d: panic with FunctionPanics none: %v", seed, r)
			}
		}
	}
	if ran == 0 {
		t.Fatal("no case realised a variable shift")
	}
}

// TestOptInRequestsAreNeverVoid pins the variable shift review's
// plumbing findings: contradictory or void requests are refused.
func TestOptInRequestsAreNeverVoid(t *testing.T) {
	bad := map[string]func(*Config){
		"included and excluded": func(c *Config) { c.Include, c.Exclude = []string{"variable_shift"}, []string{"variable_shift"} },
		"prerequisite excluded": func(c *Config) { c.Include, c.Exclude = []string{"variable_shift"}, []string{"shifts"} },
		"prerequisite off in Constructs": func(c *Config) {
			c.Include, c.Constructs = []string{"variable_shift"}, map[string]bool{"if": true}
		},
		"requested through Constructs": func(c *Config) { c.Constructs = map[string]bool{"shifts": true, "variable_shift": true} },
	}
	for name, m := range bad {
		cfg := DefaultConfig(1)
		m(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"s","constructs":["variable_shift"]}`)); err == nil {
		t.Error("a profile listing variable_shift without shifts was accepted")
	}
	// Apply copies Include rather than writing the caller's array.
	p, _ := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"s","constructs":["loops","shifts","variable_shift"]}`))
	inc := make([]string, 1, 4)
	inc[0] = "loops"
	cfg := DefaultConfig(1)
	cfg.Include = inc
	p.Apply(cfg)
	if inc[:2][1] != "" {
		t.Fatal("Apply wrote into the caller's Include array")
	}
}

func TestProfileRequestsOptIn(t *testing.T) {
	p, err := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"s","constructs":["shifts","variable_shift"]}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := p.Apply(DefaultConfig(1))
	if !containsTag(cfg.Include, "variable_shift") || containsTag(cfg.Exclude, "variable_shift") {
		t.Fatalf("Apply: include %v exclude %v", cfg.Include, cfg.Exclude)
	}
	q, _ := ParseProfile([]byte(`{"schema":"grossmith-profile-v1","name":"s","constructs":["shifts"]}`))
	if err := q.CheckFeatures(Case{FeatureCounts: map[string]int{"variable_shift": 1}}); err == nil {
		t.Fatal("a profile without variable_shift accepted a case that recorded it")
	}
}
