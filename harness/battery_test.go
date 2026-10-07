package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grossmith/gen"
)

var twoSpecs = []BatterySpec{
	{Name: "bf0", Params: []string{"int", "uint8"}, Results: []string{"int8"}, Inputs: [][]string{{"0", "255"}, {"-1", "3"}}},
	{Name: "bf1", Params: []string{"int"}, Results: []string{"bool", "uint64"}, Inputs: [][]string{{"7"}}},
}

const goodBattery = "R\t0\t0\tok\t-128\n" +
	"R\t0\t1\tpanic\t\"runtime error: integer divide by zero\"\n" +
	"R\t1\t0\tok\ttrue\t18446744073709551615\n" +
	"END\t3\n"

func TestParseBattery(t *testing.T) {
	got, err := parseBattery([]byte(goodBattery), twoSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[0][0].OK[0]) != "-128" || got[0][1].Panic == nil || got[0][1].Panic.Kind != "divide" ||
		string(got[1][0].OK[1]) != "18446744073709551615" {
		t.Fatalf("parsed %+v", got)
	}
	// Every deviation from the line protocol refuses: the battery then
	// has no expected outcomes rather than partial ones.
	bad := map[string]string{
		"missing row":         strings.Replace(goodBattery, "R\t1\t0\tok\ttrue\t18446744073709551615\n", "", 1),
		"rows out of order":   "R\t0\t1\tok\t1\nR\t0\t0\tok\t1\nR\t1\t0\tok\ttrue\t1\nEND\t3\n",
		"extra row":           strings.Replace(goodBattery, "END", "R\t1\t1\tok\ttrue\t1\nEND", 1),
		"END count wrong":     strings.Replace(goodBattery, "END\t3", "END\t4", 1),
		"no END":              strings.Replace(goodBattery, "END\t3\n", "", 1),
		"no final newline":    strings.TrimSuffix(goodBattery, "\n"),
		"trailing line":       goodBattery + "x\n",
		"result out of range": strings.Replace(goodBattery, "-128", "-129", 1),
		"non-canonical":       strings.Replace(goodBattery, "-128", "-0128", 1),
		"wrong result count":  strings.Replace(goodBattery, "ok\t-128", "ok\t1\t2", 1),
		"bad bool":            strings.Replace(goodBattery, "true", "1", 1),
		"unquoted panic":      strings.Replace(goodBattery, "\"runtime error: integer divide by zero\"", "runtime error", 1),
		"unknown row kind":    strings.Replace(goodBattery, "panic\t", "oops\t", 1),
	}
	for name, out := range bad {
		if _, err := parseBattery([]byte(out), twoSpecs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func validDoc(t *testing.T) FunctionsDoc {
	t.Helper()
	exp, err := parseBattery([]byte(goodBattery), twoSpecs)
	if err != nil {
		t.Fatal(err)
	}
	d := FunctionsDoc{Schema: FunctionsSchema, Case: "case_00000",
		FunctionsSHA256: strings.Repeat("a", 64), BatteryDriverSHA256: strings.Repeat("b", 64),
		Oracle: OracleIdentity{Path: "/go", SHA256: strings.Repeat("c", 64), Version: "go version x", GOARCH: "amd64"}}
	for i, s := range twoSpecs {
		// Copies: mutations must not reach the shared specs.
		e := FunctionEntry{Function: s.Name, Params: append([]string(nil), s.Params...),
			Results: append([]string(nil), s.Results...), Features: map[string]int{}, Expected: exp[i]}
		for _, in := range s.Inputs {
			var row []json.RawMessage
			for _, a := range in {
				row = append(row, json.RawMessage(a))
			}
			e.Inputs = append(e.Inputs, row)
		}
		d.Functions = append(d.Functions, e)
	}
	if err := d.validate(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFunctionsDocValidate(t *testing.T) {
	mutations := map[string]func(*FunctionsDoc){
		"schema":              func(d *FunctionsDoc) { d.Schema = "x" },
		"case":                func(d *FunctionsDoc) { d.Case = "../x" },
		"digest":              func(d *FunctionsDoc) { d.FunctionsSHA256 = "abc" },
		"oracle":              func(d *FunctionsDoc) { d.Oracle.Version = "" },
		"no functions":        func(d *FunctionsDoc) { d.Functions = nil },
		"repeated name":       func(d *FunctionsDoc) { d.Functions[1].Function = "bf0" },
		"first param":         func(d *FunctionsDoc) { d.Functions[0].Params[0] = "int8" },
		"no features":         func(d *FunctionsDoc) { d.Functions[0].Features = nil },
		"input count":         func(d *FunctionsDoc) { d.Functions[0].Inputs = d.Functions[0].Inputs[:1] },
		"argument type":       func(d *FunctionsDoc) { d.Functions[0].Inputs[0][1] = json.RawMessage("256") },
		"argument spelling":   func(d *FunctionsDoc) { d.Functions[0].Inputs[0][0] = json.RawMessage("1e0") },
		"both ok and panic":   func(d *FunctionsDoc) { d.Functions[0].Expected[1].OK = d.Functions[0].Expected[0].OK },
		"neither":             func(d *FunctionsDoc) { d.Functions[0].Expected[0].OK = nil },
		"panic kind mismatch": func(d *FunctionsDoc) { d.Functions[0].Expected[1].Panic.Kind = "index-out-of-range" },
		"result type":         func(d *FunctionsDoc) { d.Functions[1].Expected[0].OK[0] = json.RawMessage("2") },
	}
	for name, m := range mutations {
		d := validDoc(t)
		m(&d)
		if err := d.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// writeBatteryBatch writes a one-case batch with a real battery,
// running it under the host toolchain.
func writeBatteryBatch(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := gen.DefaultConfig(3)
	cfg.Functions, cfg.BatteryInputs, cfg.FunctionPanics = 2, 8, gen.FunctionPanicsBiased
	c, err := gen.New(cfg).Generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "case_00000")
	if err := os.MkdirAll(filepath.Join(dir, BatteryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"subject.go": c.Source, "driver.go": c.Driver,
		BatteryDir + "/" + FunctionsGo: c.FunctionsSource, BatteryDir + "/" + BatteryDriverGo: c.BatteryDriver}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var specs []BatterySpec
	for _, f := range c.Functions {
		specs = append(specs, BatterySpec{Name: f.Name, Params: f.Params, Results: f.Results, Inputs: f.Inputs, Features: f.Features})
	}
	ref := &GcAdapter{}
	oracle, err := ref.Oracle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ref.RunBattery(context.Background(), dir, "case_00000", specs, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FunctionsJSON), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteManifest(root, "test", "go 1.26", []string{"case_00000"}, map[string]int64{"case_00000": 3}); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestBatteryBatchValidation: a battery case validates; the battery
// layout stays closed (no extra or missing battery file, no other
// subdirectory, all three files or none), and functions.json must bind
// the battery sources the manifest digested.
func TestBatteryBatchValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a battery")
	}
	root := writeBatteryBatch(t)
	if _, err := ValidateBatch(root); err != nil {
		t.Fatalf("valid battery batch refused: %v", err)
	}
	m, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Cases[0].Files) != 5 {
		t.Fatalf("manifest lists %v, want the 5 case files", m.Cases[0].Files)
	}
	dir := filepath.Join(root, "case_00000")

	refusals := map[string]func(t *testing.T){
		"extra battery file": func(t *testing.T) {
			os.WriteFile(filepath.Join(dir, BatteryDir, "extra.go"), []byte("package main\n"), 0o644)
		},
		"other subdirectory": func(t *testing.T) {
			os.Mkdir(filepath.Join(dir, "other"), 0o755)
		},
		"missing battery source": func(t *testing.T) {
			os.Remove(filepath.Join(dir, BatteryDir, BatteryDriverGo))
		},
		"battery dir is a symlink": func(t *testing.T) {
			real := filepath.Join(t.TempDir(), "b")
			os.Rename(filepath.Join(dir, BatteryDir), real)
			os.Symlink(real, filepath.Join(dir, BatteryDir))
		},
	}
	for name, mutate := range refusals {
		t.Run(name, func(t *testing.T) {
			root := writeBatteryBatch(t)
			dir = filepath.Join(root, "case_00000")
			mutate(t)
			if _, err := ValidateBatch(root); err == nil {
				t.Fatal("accepted")
			}
		})
	}

	t.Run("functions.json names other sources", func(t *testing.T) {
		root := writeBatteryBatch(t)
		dir := filepath.Join(root, "case_00000")
		d, err := ReadFunctionsDoc(filepath.Join(dir, FunctionsJSON))
		if err != nil {
			t.Fatal(err)
		}
		d.FunctionsSHA256 = strings.Repeat("0", 64)
		b, _ := json.MarshalIndent(d, "", " ")
		os.WriteFile(filepath.Join(dir, FunctionsJSON), b, 0o644)
		// Re-bind the manifest so only the internal binding is wrong.
		if _, err := WriteManifest(root, "test", "go 1.26", []string{"case_00000"}, map[string]int64{"case_00000": 3}); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateBatch(root); err == nil || !strings.Contains(err.Error(), "binds battery sources") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("partial battery cannot be described", func(t *testing.T) {
		root := writeBatteryBatch(t)
		os.Remove(filepath.Join(root, "case_00000", FunctionsJSON))
		if _, err := WriteManifest(root, "test", "go 1.26", []string{"case_00000"}, map[string]int64{"case_00000": 3}); err == nil {
			t.Fatal("WriteManifest described a battery without functions.json")
		}
	})
}

func TestCaseFileNames(t *testing.T) {
	ok := []string{"subject.go", "driver.go", "case.json", "functions.json", "battery/functions.go", "battery/battery_driver.go"}
	bad := []string{"battery", "battery/", "battery/x.go", "battery/../subject.go", "other/functions.go", "battery/functions.go/x", "/battery/functions.go", "functions.go"}
	for _, n := range ok {
		if !caseFileName(n) {
			t.Errorf("%q refused", n)
		}
	}
	for _, n := range bad {
		if caseFileName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}
