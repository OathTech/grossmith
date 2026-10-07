package harness

// Battery expected outcomes (gc-verified arc G1). A case generated with
// battery functions carries battery/functions.go (the functions and the
// declarations they reach, compilable alone) and battery/battery_driver.go
// (the reference driver). gengo runs the battery under the pinned gc AT
// GENERATION and writes functions.json beside case.json:
//
//   - per function: signature, construct tags, inputs, and per input the
//     expected results or the panic (closed kind + exact message);
//   - the oracle identity that produced it, and the sha256 of both battery
//     sources.
//
// functions.json is written before the manifest, so the manifest digests
// it with every other case file and -verify covers it; ValidateBatch also
// checks it is internally consistent and names the battery sources on
// disk. The battery files live in their own subdirectory because gc
// builds every .go file in a directory together: beside subject.go,
// functions.go would redeclare the helpers.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"grossmith/internal/strictjson"
	"grossmith/observe"
)

const (
	FunctionsSchema = "grossmith-functions-v1"
	// BatteryDir is the case subdirectory holding the battery sources.
	BatteryDir        = "battery"
	FunctionsJSON     = "functions.json"
	FunctionsGo       = "functions.go"
	BatteryDriverGo   = "battery_driver.go"
	batteryOutputCap  = 64 << 20
	batteryEndPrefix  = "END\t"
	batteryRowPrefix  = "R\t"
	batteryRowOK      = "ok"
	batteryRowPanic   = "panic"
	maxBatteryResults = 2
)

// BatterySpec is one battery function as generated: the harness does not
// import the generator, so the CLI hands it over in this shape.
type BatterySpec struct {
	Name     string
	Params   []string
	Results  []string
	Inputs   [][]string
	Features map[string]int
}

// FunctionsDoc is functions.json.
type FunctionsDoc struct {
	Schema              string          `json:"schema"`
	Case                string          `json:"case"`
	FunctionsSHA256     string          `json:"functionsSha256"`
	BatteryDriverSHA256 string          `json:"batteryDriverSha256"`
	Oracle              OracleIdentity  `json:"oracle"`
	Functions           []FunctionEntry `json:"functions"`
}

// FunctionEntry is one function with its battery and expected outcomes.
type FunctionEntry struct {
	Function string         `json:"function"`
	Params   []string       `json:"params"`
	Results  []string       `json:"results"`
	Features map[string]int `json:"features"`
	// Inputs and the ok values are bare JSON integers and booleans, typed
	// by Params and Results.
	Inputs   [][]json.RawMessage `json:"inputs"`
	Expected []Expectation       `json:"expected"`
}

// Expectation is one call's outcome: exactly one of OK or Panic.
type Expectation struct {
	OK    []json.RawMessage  `json:"ok,omitempty"`
	Panic *observe.PanicInfo `json:"panic,omitempty"`
}

// RunBattery builds and runs a case's battery under the adapter's pinned
// toolchain and returns functions.json. Any deviation from the driver's
// line protocol (a missing, repeated or out-of-order row, a result that
// does not parse at its type, a missing END) is an error: the battery
// produced no expected outcomes rather than partial ones.
func (a *GcAdapter) RunBattery(ctx context.Context, caseDir, caseID string, specs []BatterySpec, oracle OracleIdentity) ([]byte, error) {
	bdir := filepath.Join(caseDir, BatteryDir)
	fsum, err := FileSHA256(filepath.Join(bdir, FunctionsGo))
	if err != nil {
		return nil, err
	}
	dsum, err := FileSHA256(filepath.Join(bdir, BatteryDriverGo))
	if err != nil {
		return nil, err
	}
	stdout, o := a.buildRun(ctx, bdir, "battery", batteryOutputCap)
	if o.Status != "" {
		return nil, fmt.Errorf("%s battery: %s: %s", caseID, o.Status, o.Detail)
	}
	expected, err := parseBattery(stdout, specs)
	if err != nil {
		return nil, fmt.Errorf("%s battery: %w", caseID, err)
	}
	doc := FunctionsDoc{Schema: FunctionsSchema, Case: caseID, FunctionsSHA256: fsum,
		BatteryDriverSHA256: dsum, Oracle: oracle}
	for i, s := range specs {
		e := FunctionEntry{Function: s.Name, Params: s.Params, Results: s.Results,
			Features: s.Features, Expected: expected[i]}
		if e.Features == nil {
			e.Features = map[string]int{}
		}
		for _, in := range s.Inputs {
			row := make([]json.RawMessage, len(in))
			for j, a := range in {
				row[j] = json.RawMessage(a)
			}
			e.Inputs = append(e.Inputs, row)
		}
		doc.Functions = append(doc.Functions, e)
	}
	if err := doc.validate(); err != nil {
		return nil, fmt.Errorf("%s: produced an invalid %s (harness bug): %w", caseID, FunctionsJSON, err)
	}
	b, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// parseBattery reads the driver's line protocol into expectations, one
// slice per function.
func parseBattery(stdout []byte, specs []BatterySpec) ([][]Expectation, error) {
	lines := strings.Split(string(stdout), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		return nil, fmt.Errorf("output does not end in a newline")
	}
	lines = lines[:len(lines)-1]
	out := make([][]Expectation, len(specs))
	pos := 0
	calls := 0
	for fi, s := range specs {
		for ii := range s.Inputs {
			if pos >= len(lines) {
				return nil, fmt.Errorf("output ends before %s input %d", s.Name, ii)
			}
			line := lines[pos]
			pos++
			f := strings.Split(line, "\t")
			if len(f) < 5 || f[0] != "R" || f[1] != strconv.Itoa(fi) || f[2] != strconv.Itoa(ii) {
				return nil, fmt.Errorf("line %d is %q, want the row for function %d input %d", pos, line, fi, ii)
			}
			var e Expectation
			switch f[3] {
			case batteryRowOK:
				vals := f[4:]
				if len(vals) != len(s.Results) {
					return nil, fmt.Errorf("line %d: %d results, %s has %d", pos, len(vals), s.Name, len(s.Results))
				}
				for j, v := range vals {
					if err := checkTypedValue(s.Results[j], v); err != nil {
						return nil, fmt.Errorf("line %d: result %d: %w", pos, j, err)
					}
					e.OK = append(e.OK, json.RawMessage(v))
				}
			case batteryRowPanic:
				if len(f) != 5 {
					return nil, fmt.Errorf("line %d: malformed panic row %q", pos, line)
				}
				msg, err := strconv.Unquote(f[4])
				if err != nil || strconv.Quote(msg) != f[4] {
					// Exactly the driver's strconv.Quote spelling: one
					// message, one spelling.
					return nil, fmt.Errorf("line %d: panic message %s is not a strconv.Quote string", pos, f[4])
				}
				p := observe.NewPanicInfo(observe.KindFromMessage(msg), msg)
				e.Panic = &p
			default:
				return nil, fmt.Errorf("line %d: unknown row kind %q", pos, f[3])
			}
			out[fi] = append(out[fi], e)
			calls++
		}
	}
	if pos != len(lines)-1 || lines[pos] != batteryEndPrefix+strconv.Itoa(calls) {
		return nil, fmt.Errorf("want exactly one final line %q after %d rows, got %d trailing lines", batteryEndPrefix+strconv.Itoa(calls), calls, len(lines)-pos)
	}
	return out, nil
}

// checkTypedValue checks a bare JSON token against a battery Go type.
// Platform int and uint are 64-bit for batteries.
func checkTypedValue(goType, v string) error {
	var err error
	switch goType {
	case "bool":
		if v != "true" && v != "false" {
			err = fmt.Errorf("not a bool")
		}
	case "int", "int64":
		_, err = strconv.ParseInt(v, 10, 64)
	case "int8":
		_, err = strconv.ParseInt(v, 10, 8)
	case "int16":
		_, err = strconv.ParseInt(v, 10, 16)
	case "int32":
		_, err = strconv.ParseInt(v, 10, 32)
	case "uint", "uint64":
		_, err = strconv.ParseUint(v, 10, 64)
	case "uint8":
		_, err = strconv.ParseUint(v, 10, 8)
	case "uint16":
		_, err = strconv.ParseUint(v, 10, 16)
	case "uint32":
		_, err = strconv.ParseUint(v, 10, 32)
	default:
		return fmt.Errorf("type %q is outside the battery types", goType)
	}
	if err != nil {
		return fmt.Errorf("%q is not a %s: %v", v, goType, err)
	}
	// Canonical spelling only: "+1", "01" and "-0" parse but are not what
	// strconv's formatter writes, and a value has exactly one spelling.
	if goType != "bool" && v != canonicalInt(v) {
		return fmt.Errorf("%q is not the canonical decimal spelling", v)
	}
	return nil
}

func canonicalInt(v string) string {
	if strings.HasPrefix(v, "-") {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return ""
		}
		return strconv.FormatInt(n, 10)
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(n, 10)
}

// validate checks a functions document's internal consistency.
func (d FunctionsDoc) validate() error {
	if d.Schema != FunctionsSchema {
		return fmt.Errorf("schema %q, want %q", d.Schema, FunctionsSchema)
	}
	if !caseDirRe.MatchString(d.Case) {
		return fmt.Errorf("case %q is not a case ID", d.Case)
	}
	if !digestRe.MatchString(d.FunctionsSHA256) || !digestRe.MatchString(d.BatteryDriverSHA256) || !digestRe.MatchString(d.Oracle.SHA256) {
		return fmt.Errorf("a digest is not a sha256 digest")
	}
	if d.Oracle.Path == "" || d.Oracle.Version == "" || d.Oracle.GOOS == "" || d.Oracle.GOARCH == "" || d.Oracle.ModuleMode == "" {
		return fmt.Errorf("oracle identity is incomplete")
	}
	if len(d.Functions) == 0 {
		return fmt.Errorf("no functions")
	}
	names := map[string]bool{}
	for _, f := range d.Functions {
		if want := "bf" + strconv.Itoa(len(names)); f.Function != want {
			return fmt.Errorf("function %d is named %q, want %q", len(names), f.Function, want)
		}
		names[f.Function] = true
		for _, t := range append(append([]string(nil), f.Params...), f.Results...) {
			if !batteryType(t) {
				return fmt.Errorf("%s: type %q is outside the battery types", f.Function, t)
			}
		}
		for tag, n := range f.Features {
			if tag == "" || n < 1 {
				return fmt.Errorf("%s: feature %q=%d (tags are nonempty, counts positive)", f.Function, tag, n)
			}
		}
		if len(f.Params) == 0 || f.Params[0] != "int" || len(f.Params) > 4 {
			return fmt.Errorf("%s: parameters %v outside the contract (first plain int, at most 4)", f.Function, f.Params)
		}
		if len(f.Results) == 0 || len(f.Results) > maxBatteryResults {
			return fmt.Errorf("%s: %d results, want 1 or 2", f.Function, len(f.Results))
		}
		if f.Features == nil {
			return fmt.Errorf("%s: features missing", f.Function)
		}
		if len(f.Inputs) == 0 || len(f.Inputs) != len(f.Expected) {
			return fmt.Errorf("%s: %d inputs and %d expectations", f.Function, len(f.Inputs), len(f.Expected))
		}
		for i, in := range f.Inputs {
			if len(in) != len(f.Params) {
				return fmt.Errorf("%s input %d: %d arguments for %d parameters", f.Function, i, len(in), len(f.Params))
			}
			for j, a := range in {
				if err := checkTypedValue(f.Params[j], string(a)); err != nil {
					return fmt.Errorf("%s input %d argument %d: %w", f.Function, i, j, err)
				}
			}
			e := f.Expected[i]
			switch {
			case (e.OK == nil) == (e.Panic == nil):
				return fmt.Errorf("%s input %d: expectation must hold exactly one of ok and panic", f.Function, i)
			case e.Panic != nil:
				// The driver accepts only runtime.Error panic values, and
				// gc's runtime errors all carry this prefix.
				if !strings.HasPrefix(e.Panic.MessageData(), "runtime error: ") {
					return fmt.Errorf("%s input %d: panic message %q is not a runtime error", f.Function, i, e.Panic.MessageData())
				}
				if want := observe.KindFromMessage(e.Panic.MessageData()); e.Panic.Kind != want {
					return fmt.Errorf("%s input %d: panic kind %q, but the message classifies as %q", f.Function, i, e.Panic.Kind, want)
				}
			default:
				if len(e.OK) != len(f.Results) {
					return fmt.Errorf("%s input %d: %d results for %d result types", f.Function, i, len(e.OK), len(f.Results))
				}
				for j, v := range e.OK {
					if err := checkTypedValue(f.Results[j], string(v)); err != nil {
						return fmt.Errorf("%s input %d result %d: %w", f.Function, i, j, err)
					}
				}
			}
		}
	}
	return nil
}

func batteryType(t string) bool {
	switch t {
	case "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	}
	return false
}

// ReadFunctionsDoc strict-decodes and validates a functions.json.
func ReadFunctionsDoc(path string) (FunctionsDoc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FunctionsDoc{}, err
	}
	var d FunctionsDoc
	if err := strictjson.Unmarshal(b, &d); err != nil {
		return FunctionsDoc{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := d.validate(); err != nil {
		return FunctionsDoc{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// checkFunctionsDoc is ValidateBatch's check for a case carrying a
// battery: functions.json is valid, names its own case, and binds the
// battery sources the manifest digested.
func checkFunctionsDoc(caseDir string, mc ManifestCase) error {
	d, err := ReadFunctionsDoc(filepath.Join(caseDir, FunctionsJSON))
	if err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	if d.Case != mc.ID {
		return fmt.Errorf("batch: %s/%s names case %s", mc.ID, FunctionsJSON, d.Case)
	}
	if d.FunctionsSHA256 != mc.Files[BatteryDir+"/"+FunctionsGo] || d.BatteryDriverSHA256 != mc.Files[BatteryDir+"/"+BatteryDriverGo] {
		return fmt.Errorf("batch: %s/%s binds battery sources that differ from the manifest's", mc.ID, FunctionsJSON)
	}
	if err := checkAgainstRecord(caseDir, d); err != nil {
		return fmt.Errorf("batch: %s/%s: %w", mc.ID, FunctionsJSON, err)
	}
	return nil
}

// checkAgainstRecord holds functions.json to the battery configuration
// case.json records: the function count, the inputs per function, and the
// integer-only parameter rule. (The functions' bodies are not re-derived
// here; -replay regenerates them.)
func checkAgainstRecord(caseDir string, d FunctionsDoc) error {
	b, err := os.ReadFile(filepath.Join(caseDir, "case.json"))
	if err != nil {
		return err
	}
	var rec struct {
		Config *struct {
			Functions         int
			BatteryInputs     int
			FunctionIntParams bool
		} `json:"config"`
	}
	// Extensible: the config's other fields belong to the generator.
	if err := strictjson.UnmarshalExtensible(b, &rec); err != nil {
		return fmt.Errorf("case.json: %w", err)
	}
	if rec.Config == nil || rec.Config.Functions == 0 {
		return fmt.Errorf("case.json records no battery functions")
	}
	if len(d.Functions) != rec.Config.Functions {
		return fmt.Errorf("%d functions, case.json records %d", len(d.Functions), rec.Config.Functions)
	}
	for _, f := range d.Functions {
		if len(f.Inputs) != rec.Config.BatteryInputs {
			return fmt.Errorf("%s has %d inputs, case.json records %d", f.Function, len(f.Inputs), rec.Config.BatteryInputs)
		}
		if rec.Config.FunctionIntParams {
			for _, p := range f.Params {
				if p == "bool" {
					return fmt.Errorf("%s has a bool parameter, but case.json records integer-only parameters", f.Function)
				}
			}
		}
	}
	return nil
}

// SameBattery reports whether two functions documents carry the same
// functions, inputs and outcomes (replay's comparison; oracle identity and
// source digests are compared by the caller as needed).
func SameBattery(a, b FunctionsDoc) bool {
	ja, err1 := json.Marshal(a.Functions)
	jb, err2 := json.Marshal(b.Functions)
	return err1 == nil && err2 == nil && bytes.Equal(ja, jb)
}
