package gen

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func batteryConfig(seed int64, panics int) Config {
	cfg := DefaultConfig(seed)
	cfg.Functions, cfg.BatteryInputs, cfg.FunctionPanics = 4, 32, panics
	return cfg
}

// TestFunctionsLeaveSubjectUnchanged is the seed-stability witness:
// enabling battery functions changes nothing about the subject (source,
// driver, features) and only appends to the tape.
func TestFunctionsLeaveSubjectUnchanged(t *testing.T) {
	n := int64(300)
	if testing.Short() {
		n = 60
	}
	for seed := int64(1); seed <= n; seed++ {
		for _, panics := range []int{FunctionPanicsNone, FunctionPanicsBiased} {
			off, err := New(DefaultConfig(seed)).Generate()
			if err != nil {
				t.Fatal(err)
			}
			on, err := New(batteryConfig(seed, panics)).Generate()
			if err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
			if !bytes.Equal(off.Source, on.Source) || !bytes.Equal(off.Driver, on.Driver) {
				t.Fatalf("seed %d: enabling functions changed the subject", seed)
			}
			if !reflect.DeepEqual(off.FeatureCounts, on.FeatureCounts) {
				t.Fatalf("seed %d: enabling functions changed the subject's features:\noff %v\non  %v", seed, off.FeatureCounts, on.FeatureCounts)
			}
			if len(on.Tape) <= len(off.Tape) || !reflect.DeepEqual(off.Tape, on.Tape[:len(off.Tape)]) {
				t.Fatalf("seed %d: the subject's tape is not a prefix of the tape with functions", seed)
			}
			if off.Functions != nil || off.FunctionsSource != nil || off.BatteryDriver != nil {
				t.Fatalf("seed %d: functions emitted with Functions 0", seed)
			}
			if len(on.Functions) != 4 {
				t.Fatalf("seed %d: %d functions, want 4", seed, len(on.Functions))
			}
			for _, f := range on.Functions {
				if len(f.Inputs) != 32 {
					t.Fatalf("seed %d %s: %d inputs, want 32", seed, f.Name, len(f.Inputs))
				}
			}
		}
	}
}

// TestFunctionsConfigJSONUnchanged: a record written with functions off
// carries no new config keys, so existing case.json bytes are unchanged.
func TestFunctionsConfigJSONUnchanged(t *testing.T) {
	b, err := json.Marshal(DefaultConfig(7))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Functions", "BatteryInputs", "FunctionPanics"} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("config JSON with functions off contains %q: %s", key, b)
		}
	}
}

func TestFunctionsReplay(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		cfg := batteryConfig(seed, FunctionPanicsOrdinary)
		c, err := New(cfg).Generate()
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReplay(cfg, c.Tape).Generate()
		if err != nil {
			t.Fatalf("seed %d: replay: %v", seed, err)
		}
		if !bytes.Equal(c.FunctionsSource, r.FunctionsSource) || !bytes.Equal(c.BatteryDriver, r.BatteryDriver) ||
			!reflect.DeepEqual(c.Functions, r.Functions) {
			t.Fatalf("seed %d: replay did not reproduce the battery", seed)
		}
	}
}

func TestFunctionsConfigValidation(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Functions = -1 },
		func(c *Config) { c.Functions = MaxFunctions + 1; c.BatteryInputs = 1 },
		func(c *Config) { c.BatteryInputs = 32 },       // knob without functions
		func(c *Config) { c.FunctionPanics = 1 },       // knob without functions
		func(c *Config) { c.FunctionIntParams = true }, // knob without functions
		func(c *Config) { c.Functions = 1 },            // no inputs
		func(c *Config) { c.Functions, c.BatteryInputs = 1, MaxBatteryInputs+1 },
		func(c *Config) { c.Functions, c.BatteryInputs, c.FunctionPanics = 1, 1, 3 },
	}
	for i, mutate := range bad {
		cfg := DefaultConfig(1)
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("case %d: config %+v accepted", i, cfg)
		}
	}
	cfg := DefaultConfig(1)
	cfg.Functions, cfg.BatteryInputs = MaxFunctions, MaxBatteryInputs
	if err := cfg.Validate(); err != nil {
		t.Errorf("largest battery refused: %v", err)
	}
}

// TestFunctionsSourceContract: every functions.go is standalone (checked
// inside Generate) and every battery is well-formed against its
// signature.
func TestFunctionsSourceContract(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		c, err := New(batteryConfig(seed, FunctionPanicsBiased)).Generate()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if err := checkStandalone(c.FunctionsSource); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		for _, f := range c.Functions {
			if f.Params[0] != "int" || len(f.Params) > 4 || len(f.Results) < 1 || len(f.Results) > 2 {
				t.Fatalf("seed %d %s: signature %v -> %v outside the contract", seed, f.Name, f.Params, f.Results)
			}
			for _, in := range f.Inputs {
				for j, a := range in {
					if err := checkArg(f.Params[j], a); err != nil {
						t.Fatalf("seed %d %s: %v", seed, f.Name, err)
					}
				}
			}
		}
	}
}

func checkArg(goType, a string) error {
	if goType == "bool" {
		if a != "true" && a != "false" {
			return &strconv.NumError{Func: "bool", Num: a, Err: strconv.ErrSyntax}
		}
		return nil
	}
	bits, unsigned := intKind(goType)
	if unsigned {
		_, err := strconv.ParseUint(a, 10, bits)
		return err
	}
	_, err := strconv.ParseInt(a, 10, bits)
	return err
}

// runBattery builds and runs a case's battery, optionally with
// functions.go instrumented by the statement counter; it returns the R
// lines, the END count and the executed-statement count (-1 when not
// instrumented).
func runBattery(t *testing.T, c Case, instrument bool) ([][]string, int, int64) {
	t.Helper()
	dir := t.TempDir()
	fsrc, driver := c.FunctionsSource, c.BatteryDriver
	files := map[string][]byte{"go.mod": []byte("module battery\n\ngo 1.26\n")}
	if instrument {
		fsrc = instrumentExec(t, fsrc)
		driver = bytes.Replace(driver, []byte(`os.Stdout.WriteString("END`), []byte(`reportExeCtr()
	os.Stdout.WriteString("END`), 1)
		files["count.go"] = []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar exeCtr int64\n\nfunc reportExeCtr() { fmt.Fprintf(os.Stderr, \"EXECSTMTS %d\\n\", exeCtr) }\n")
	}
	files[FunctionsFile], files[BatteryDriverFile] = fsrc, driver
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", "battery.bin", ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	run := exec.Command(filepath.Join(dir, "battery.bin"))
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("battery run: %v: %s", err, stderr.String())
	}
	var rows [][]string
	end := -1
	for _, l := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		f := strings.Split(l, "\t")
		switch f[0] {
		case "R":
			rows = append(rows, f)
		case "END":
			end, _ = strconv.Atoi(f[1])
		default:
			t.Fatalf("unexpected battery line %q", l)
		}
	}
	executed := int64(-1)
	if instrument {
		for _, line := range strings.Split(stderr.String(), "\n") {
			if rest, ok := strings.CutPrefix(line, "EXECSTMTS "); ok {
				executed, _ = strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			}
		}
	}
	return rows, end, executed
}

// TestBatteryRuns builds and runs batteries: every call reports exactly
// once, FunctionPanics 0 never panics, and the biased density does panic
// somewhere in the sweep.
func TestBatteryRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	panicked := 0
	for seed := int64(1); seed <= 12; seed++ {
		for _, density := range []int{FunctionPanicsNone, FunctionPanicsBiased} {
			c, err := New(batteryConfig(seed, density)).Generate()
			if err != nil {
				t.Fatal(err)
			}
			rows, end, _ := runBattery(t, c, false)
			if end != 4*32 || len(rows) != end {
				t.Fatalf("seed %d: %d rows, END %d, want 128", seed, len(rows), end)
			}
			for i, r := range rows {
				fn, in := i/32, i%32
				if r[1] != strconv.Itoa(fn) || r[2] != strconv.Itoa(in) {
					t.Fatalf("seed %d: row %d is %v, want function %d input %d", seed, i, r, fn, in)
				}
				switch r[3] {
				case "ok":
					if len(r)-4 != len(c.Functions[fn].Results) {
						t.Fatalf("seed %d: row %v has the wrong result count", seed, r)
					}
				case "panic":
					if density == FunctionPanicsNone {
						t.Fatalf("seed %d: FunctionPanics 0 but %s panicked: %v", seed, c.Functions[fn].Name, r)
					}
					panicked++
				default:
					t.Fatalf("seed %d: row %v", seed, r)
				}
			}
		}
	}
	if panicked == 0 {
		t.Fatal("the biased density produced no panics over the sweep")
	}
}

// TestBatteryChargedCoversMeasured is the halting witness for batteries:
// measured executions of functions.go never exceed the summed per-call
// charges, which never exceed ExecBudget. The configurations include the
// extremes: one function with one input (the whole budget on one call,
// deep loops) and the densest battery (the smallest per-call budget).
func TestBatteryChargedCoversMeasured(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs binaries")
	}
	configs := []struct {
		name  string
		cfg   func(int64) Config
		seeds []int64
	}{
		{"default", func(s int64) Config { return batteryConfig(s, FunctionPanicsOrdinary) }, []int64{1, 2, 3, 4}},
		{"one call, deep loops", func(s int64) Config {
			cfg := DefaultConfig(s)
			cfg.Depth, cfg.LoopCap = 4, 4096
			cfg.Functions, cfg.BatteryInputs = 1, 1
			return cfg
		}, []int64{1, 2, 3, 4, 5, 6}},
		{"densest", func(s int64) Config {
			cfg := DefaultConfig(s)
			cfg.Depth, cfg.LoopCap = 3, 4096
			cfg.Functions, cfg.BatteryInputs = 16, 1024
			return cfg
		}, []int64{1, 2}},
	}
	for _, tc := range configs {
		for _, seed := range tc.seeds {
			cfg := tc.cfg(seed)
			c, err := New(cfg).Generate()
			if err != nil {
				t.Fatalf("%s seed %d: %v", tc.name, seed, err)
			}
			var charged int64
			for _, f := range c.Functions {
				charged += f.Cost * int64(len(f.Inputs))
			}
			if charged > ExecBudget {
				t.Fatalf("%s seed %d: charged %d past ExecBudget", tc.name, seed, charged)
			}
			_, _, measured := runBattery(t, c, true)
			if measured < 0 || measured > charged {
				t.Fatalf("%s seed %d: measured %d executed statements, charged %d\n%s", tc.name, seed, measured, charged, c.FunctionsSource)
			}
			t.Logf("%s seed %d: measured %d / charged %d", tc.name, seed, measured, charged)
		}
	}
}

// TestFunctionIntParams: with the flag, no battery parameter is bool;
// without it, bool parameters do occur (the flag is not vacuous).
func TestFunctionIntParams(t *testing.T) {
	sawBool := false
	for seed := int64(1); seed <= 200; seed++ {
		for _, ints := range []bool{true, false} {
			cfg := batteryConfig(seed, FunctionPanicsOrdinary)
			cfg.FunctionIntParams = ints
			c, err := New(cfg).Generate()
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range c.Functions {
				for _, p := range f.Params {
					if p == "bool" {
						if ints {
							t.Fatalf("seed %d %s: bool parameter with FunctionIntParams", seed, f.Name)
						}
						sawBool = true
					}
				}
			}
		}
	}
	if !sawBool {
		t.Fatal("no bool parameter without the flag: the test cannot tell the flag works")
	}
}

// TestBatteryEdgeCoverage pins G1 review finding 1: in the edge rows
// every parameter takes each of the first min(alphabet, rows) edge values
// at least once, whatever its position and type.
func TestBatteryEdgeCoverage(t *testing.T) {
	for _, k := range []int{1, 2, 3, 8, 32, 64, 1024} {
		for seed := int64(1); seed <= 60; seed++ {
			cfg := batteryConfig(seed, FunctionPanicsOrdinary)
			cfg.BatteryInputs = k
			c, err := New(cfg).Generate()
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range c.Functions {
				longest := 0
				for _, p := range f.Params {
					if n := len(batteryEdges(p)); n > longest {
						longest = n
					}
				}
				rows := max(k/2, 1)
				rows = min(rows, longest)
				for i, p := range f.Params {
					edges := batteryEdges(p)
					window := min(len(edges), rows)
					seen := map[string]int{}
					for j := 0; j < rows; j++ {
						seen[f.Inputs[j][i]]++
					}
					for _, e := range edges[:window] {
						if seen[e] == 0 {
							t.Fatalf("K=%d seed %d %s p%d %s: edge %s never in the %d edge rows", k, seed, f.Name, i, p, e, rows)
						}
					}
				}
			}
		}
	}
}

// TestFunctionsLeaveStatsUnchanged pins G1 review finding 3: Case.Stats
// describes the subject's choices only.
func TestFunctionsLeaveStatsUnchanged(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		off, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		on, err := New(batteryConfig(seed, FunctionPanicsBiased)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(off.Stats, on.Stats) {
			t.Fatalf("seed %d: enabling functions changed Case.Stats", seed)
		}
	}
}
