package gen

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Battery functions (gc-verified arc G1; the request's R3). A case can carry
// parameterised top-level functions bf0..bfN-1, each with a seeded input
// battery. They live in their OWN file, functions.go: that file compiles by
// itself (package main, no main, no init, no package variables, no
// imports), so a consumer can call every function directly with its own
// driver. The subject file is untouched.
//
// Invariants, by construction:
//
//   - SEED STABILITY. Functions are generated after every subject draw, and
//     only when Config.Functions > 0. A case's subject source and the
//     subject's tape prefix are identical with and without functions.
//   - HALTS. Bodies are generated against a real per-call budget:
//     (ExecBudget / (Functions x BatteryInputs)) minus the driver's per-call
//     overhead. Arms degrade to cheap ones as the budget runs out, as in
//     the subject, so the whole battery executes at most ExecBudget
//     statements.
//   - DETERMINISM. Bodies are generated in the helpers' pure mode (no
//     observation points, no package state, no order witness, no recover,
//     no early return), so each call's outcome is a function of its
//     arguments. Unlike helpers, panic-capable arms are allowed when
//     Config.FunctionPanics > 0, under the subject's rule of at most one
//     panic-capable site per statement. With FunctionPanics == 0 a body
//     cannot panic: it is exactly as panic-free as a helper.
//   - 64-BIT INPUTS. Platform int and uint inputs use their 64-bit ranges.
//     On a 32-bit target the battery driver does not compile (its 64-bit
//     literals overflow int), so no results are reported for inputs the
//     target cannot represent; the run-time IntSize check is a second
//     line for drivers built some other way.

// Function-panic densities (Config.FunctionPanics).
const (
	// FunctionPanicsNone: no panic-capable sites; bodies cannot panic.
	FunctionPanicsNone = 0
	// FunctionPanicsOrdinary: the subject's ordinary hot-arm rates.
	FunctionPanicsOrdinary = 1
	// FunctionPanicsBiased: the recover wrapper's raised hot-arm rates.
	FunctionPanicsBiased = 2
)

const (
	// MaxFunctions and MaxBatteryInputs bound the battery so the per-call
	// budget never falls below batteryMinPerCall.
	MaxFunctions     = 64
	MaxBatteryInputs = 1024
	// batteryCallOverhead pre-pays the driver's own executed statements per
	// call: the call wrapper's defer, the call, and the result encoding.
	batteryCallOverhead = 16
	// batteryMinPerCall is the smallest per-call budget at which a body's
	// mandatory floor (<= 3 statement slots, the return) stays payable.
	batteryMinPerCall = 32
	// FunctionsFile and BatteryDriverFile are the case files G1 adds.
	FunctionsFile     = "functions.go"
	BatteryDriverFile = "battery_driver.go"
)

// BatteryFunction describes one generated battery function and its inputs.
type BatteryFunction struct {
	Name string
	// Params and Results are the Go spellings of the parameter and result
	// types: integer kinds and bool.
	Params  []string
	Results []string
	// Inputs are the battery, one argument list per call. Each argument is
	// its exact literal text: a decimal integer or true/false (both are
	// valid JSON tokens).
	Inputs [][]string
	// Features are the construct and info tags recorded while generating
	// this function's body, with counts.
	Features map[string]int
	// Cost is the body's worst-case executed statements per call.
	Cost int64
}

// battery is the state threaded through function generation.
type battery struct {
	funcs []BatteryFunction
	src   []string
}

// generateFunctions builds the battery functions and their inputs. It runs
// after every subject draw (seed stability), and only when enabled.
func (g *Generator) generateFunctions() error {
	n := g.cfg.Functions
	if n == 0 {
		return nil
	}
	// Case.Stats describes the subject's choices (G1 review finding 3):
	// battery draws stay on the tape but out of the per-site statistics.
	saved := make(map[string]*SiteStats, len(g.c.stats))
	for site, s := range g.c.stats {
		cp := &SiteStats{Valid: map[string]int{}, Chosen: map[string]int{}}
		for k, v := range s.Valid {
			cp.Valid[k] = v
		}
		for k, v := range s.Chosen {
			cp.Chosen[k] = v
		}
		saved[site] = cp
	}
	defer func() { g.c.stats = saved }()
	perCall := int64(ExecBudget)/int64(n*g.cfg.BatteryInputs) - batteryCallOverhead
	for i := 0; i < n; i++ {
		f, src, err := g.generateFunction(i, perCall)
		if err != nil {
			return err
		}
		f.Inputs = g.batteryInputs(f.Params)
		g.battery.funcs = append(g.battery.funcs, f)
		g.battery.src = append(g.battery.src, src)
	}
	return nil
}

func (g *Generator) generateFunction(idx int, perCall int64) (BatteryFunction, string, error) {
	paramPool := scalarTypes()
	if g.cfg.FunctionIntParams {
		paramPool = intTypes()
	}
	params := []Type{Int(0, false)}
	for j := g.c.draw(4); j > 0; j-- {
		params = append(params, pick(g.c, paramPool))
	}
	var results []Type
	for j := 1 + g.c.draw(2); j > 0; j-- {
		results = append(results, pick(g.c, scalarTypes()))
	}

	usedBefore := make(map[string]int, len(g.used))
	for k, v := range g.used {
		usedBefore[k] = v
	}
	saved := struct {
		vars                         []binding
		risk, pure, fnRisk, bias, br bool
		base                         string
		left, liab, mul              int64
		sink                         *int64
	}{g.vars, g.riskSpent, g.pureMode, g.fnRisk, g.guardBias, g.budgetBreached,
		g.pureBase, g.budgetLeft, g.floorLiability, g.execMul, g.costSink}

	g.vars = nil
	g.pureMode, g.pureBase = true, "p0"
	g.fnRisk = g.cfg.FunctionPanics != FunctionPanicsNone
	g.guardBias = g.cfg.FunctionPanics == FunctionPanicsBiased
	// A real budget, not priceBody's unconstrained sink: arms consult it
	// and degrade, so no body can outgrow its share of the battery.
	g.costSink, g.budgetLeft, g.floorLiability, g.execMul, g.budgetBreached = nil, perCall, 0, 1, false
	for j, pt := range params {
		g.vars = append(g.vars, binding{name: fmt.Sprintf("p%d", j), typ: pt})
	}

	body := &emitter{indent: 1}
	stmts := 1 + g.c.draw(3)
	// The statement slots plus the return line are mandatory.
	release := g.commitFloor(int64(stmts) + 1)
	for j := 0; j < stmts; j++ {
		g.stmtIn(body, 1, false, false)
	}
	release()
	// The return is one statement: one panic-capable site at most across
	// all of its result expressions.
	g.resetRisk()
	g.charge(1)
	rs := make([]string, len(results))
	for j, rt := range results {
		rs[j] = g.resultExpr(rt)
	}
	body.line("return %s", strings.Join(rs, ", "))
	// The return line is already charged; add the call itself.
	cost := satAdd(perCall-g.budgetLeft, 1)
	breached := g.budgetBreached

	g.vars, g.riskSpent, g.pureMode, g.fnRisk, g.guardBias, g.budgetBreached = saved.vars, saved.risk, saved.pure, saved.fnRisk, saved.bias, saved.br
	g.pureBase, g.budgetLeft, g.floorLiability, g.execMul, g.costSink = saved.base, saved.left, saved.liab, saved.mul, saved.sink
	if breached {
		return BatteryFunction{}, "", fmt.Errorf("gen: battery function bf%d breached its per-call budget %d (generator bug; seed %d)", idx, perCall, g.cfg.Seed)
	}

	// The function's tags are its own: the case's Features keep describing
	// the subject alone, so subject-level accounting (composition, ledger
	// gates) is unchanged by enabling functions.
	features := map[string]int{}
	for k, v := range g.used {
		if d := v - usedBefore[k]; d > 0 {
			features[k] = d
		}
	}
	g.used = usedBefore
	name := fmt.Sprintf("bf%d", idx)
	ps := make([]string, len(params))
	pn := make([]string, len(params))
	for j, pt := range params {
		ps[j] = fmt.Sprintf("p%d %s", j, pt.GoName())
		pn[j] = pt.GoName()
	}
	rn := make([]string, len(results))
	for j, r := range results {
		rn[j] = r.GoName()
	}
	src := fmt.Sprintf("func %s(%s) (%s) {\n%s}\n\n",
		name, strings.Join(ps, ", "), strings.Join(rn, ", "), body.buf.String())
	return BatteryFunction{Name: name, Params: pn, Results: rn, Features: features, Cost: cost}, src, nil
}

// resultExpr draws one return expression. A constant result would make
// every battery row identical for that slot, so a constant draw is
// combined with a non-constant base. The fallback makes no draws, and
// emits only what is enabled and recorded (G2 review finding 1):
//
//   - integers add the first parameter of exactly that type, or else a
//     conversion of p0 (noted p0_conversion, like every pure-mode
//     fallback); the constant is representable in the result type, so this
//     compiles, and the sum wraps at run time;
//   - bools become p0 > 0 when `comparisons` is enabled (marked), and stay
//     the constant otherwise.
func (g *Generator) resultExpr(t Type) string {
	e := g.expr(t, g.cfg.ExprFuel)
	if !e.constant {
		return e.text
	}
	if t.Shape == ShapeBool {
		if !g.enabled("comparisons") {
			return e.text
		}
		g.mark("comparisons")
		return fmt.Sprintf("%s > 0", g.pureBase)
	}
	for _, v := range g.vars {
		if v.typ.Equal(t) && v.typ.Shape == ShapeInt {
			return fmt.Sprintf("%s + %s", v.name, e.text)
		}
	}
	g.note(tagPureConversion)
	return fmt.Sprintf("%s(%s) + %s", t.GoName(), g.pureBase, e.text)
}

// hotVariable is the operand of a panic-capable arm that needs a variable
// (a divisor, today). In the subject the one-per-type floor makes
// variable() total. In a battery function only the parameters are in
// scope, and variable()'s literal fallback could put a constant zero
// divisor in the source, which does not compile; the fallback here
// converts the guaranteed plain-int p0 instead, which is never constant.
func (g *Generator) hotVariable(t Type) value {
	if !g.pureMode {
		return g.variable(t)
	}
	if i, ok := g.pickVar(t); ok {
		g.vars[i].reads++
		return value{text: g.vars[i].name, bound: g.vars[i].bound}
	}
	g.note(tagPureConversion)
	return value{text: fmt.Sprintf("%s(%s)", t.GoName(), g.pureBase)}
}

// batteryPowers are the exponents whose powers of two (+-1) join the edge
// alphabet: the byte, half-word, word and double-word boundaries, plus 1
// and 2 for small shifts and masks.
var batteryPowers = []int{1, 2, 7, 8, 15, 16, 31, 32, 63}

// batteryEdges is a type's edge-value alphabet for battery inputs: 0, 1,
// -1, the extremes and their neighbours, and powers of two +-1 inside the
// range. Platform int/uint use the 64-bit range (see the file comment).
func batteryEdges(goType string) []string {
	if goType == "bool" {
		return []string{"false", "true"}
	}
	bits, unsigned := intKind(goType)
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if unsigned {
		max := ^uint64(0) >> (64 - bits)
		add("0")
		add("1")
		add(strconv.FormatUint(max, 10))
		add(strconv.FormatUint(max-1, 10))
		for _, k := range batteryPowers {
			if k >= bits {
				continue
			}
			p := uint64(1) << k
			add(strconv.FormatUint(p-1, 10))
			add(strconv.FormatUint(p, 10))
			add(strconv.FormatUint(p+1, 10))
		}
		return out
	}
	max := int64(^uint64(0) >> (65 - bits))
	min := -max - 1
	add("0")
	add("1")
	add("-1")
	add(strconv.FormatInt(min, 10))
	add(strconv.FormatInt(max, 10))
	add(strconv.FormatInt(max-1, 10))
	add(strconv.FormatInt(min+1, 10))
	for _, k := range batteryPowers {
		if k >= bits-1 {
			continue
		}
		p := int64(1) << k
		add(strconv.FormatInt(p-1, 10))
		add(strconv.FormatInt(p, 10))
		add(strconv.FormatInt(p+1, 10))
		add(strconv.FormatInt(-p, 10))
	}
	return out
}

// intKind decodes an integer kind's spelling. Platform int/uint are 64-bit
// for battery purposes.
func intKind(goType string) (bits int, unsigned bool) {
	switch goType {
	case "int", "int64":
		return 64, false
	case "int8":
		return 8, false
	case "int16":
		return 16, false
	case "int32":
		return 32, false
	case "uint", "uint64":
		return 64, true
	case "uint8":
		return 8, true
	case "uint16":
		return 16, true
	case "uint32":
		return 32, true
	}
	panic("gen: battery input for unsupported type " + goType)
}

// edgeIndex picks parameter i's edge value for edge row j. Each parameter
// walks its alphabet with stride 1, starting i places along, over a window
// of min(len, rows) entries: every entry in the window appears in at
// least one row whatever the offset, and the alphabets list the core
// values (0, +-1, the extremes and their neighbours) first, so those are
// always reached. The offsets vary the combinations between parameters.
// (G1 review finding 1: the earlier strides j*(i+1) skipped whole edge
// classes when the stride shared a factor with the alphabet length.)
func edgeIndex(j, i, alphabet, rows int) int {
	window := alphabet
	if rows < window {
		window = rows
	}
	return (j + i) % window
}

// batteryInputs draws one function's battery. The first inputs walk the
// edge alphabets (edgeIndex).
// The edge rows are the leading max(1, BatteryInputs/2) inputs, capped
// at the longest alphabet; the rest are random. A random argument is an edge value, a small
// value in [-60,60] (clamped to the type), or uniform over the type, a
// third each. Every draw goes through the chooser, so the battery is on
// the tape and replays.
func (g *Generator) batteryInputs(params []string) [][]string {
	k := g.cfg.BatteryInputs
	edges := make([][]string, len(params))
	longest := 0
	for i, p := range params {
		edges[i] = batteryEdges(p)
		if len(edges[i]) > longest {
			longest = len(edges[i])
		}
	}
	// Half the battery walks the edges (at least one row), the rest is
	// random.
	edgeRows := k / 2
	if edgeRows < 1 {
		edgeRows = 1
	}
	if edgeRows > longest {
		edgeRows = longest
	}
	inputs := make([][]string, 0, k)
	for j := 0; j < edgeRows; j++ {
		row := make([]string, len(params))
		for i := range params {
			row[i] = edges[i][edgeIndex(j, i, len(edges[i]), edgeRows)]
		}
		inputs = append(inputs, row)
	}
	for j := edgeRows; j < k; j++ {
		row := make([]string, len(params))
		for i, p := range params {
			row[i] = g.randomArg(p, edges[i])
		}
		inputs = append(inputs, row)
	}
	return inputs
}

func (g *Generator) randomArg(goType string, edges []string) string {
	if goType == "bool" {
		return edges[g.c.draw(2)]
	}
	bits, unsigned := intKind(goType)
	switch g.c.draw(3) {
	case 0:
		return edges[g.c.draw(len(edges))]
	case 1:
		lo := -60
		if unsigned {
			lo = 0
		}
		return strconv.Itoa(lo + g.c.draw(61-lo))
	}
	// Uniform: four 16-bit draws, truncated to the width.
	var u uint64
	for i := 0; i < 4; i++ {
		u = u<<16 | uint64(g.c.draw(1<<16))
	}
	u &= ^uint64(0) >> (64 - bits)
	if unsigned {
		return strconv.FormatUint(u, 10)
	}
	// Sign-extend from the width.
	shift := 64 - bits
	return strconv.FormatInt(int64(u<<shift)>>shift, 10)
}

// functionsSource assembles functions.go: the declarations the battery
// functions can reach (helpers, struct and defined types with methods,
// interfaces) and the functions themselves. Unused declarations are legal
// Go. The file is type-checked here, and its top-level declarations are
// checked against the standalone contract, so a violation is a generator
// error rather than a case.
func (g *Generator) functionsSource() ([]byte, error) {
	var out strings.Builder
	out.WriteString("package main\n\n")
	for _, st := range g.structs {
		out.WriteString(st.decl())
	}
	for _, h := range g.helpers {
		out.WriteString(h.src)
	}
	g.writeDefinedAndInterfaces(&out)
	for _, s := range g.battery.src {
		out.WriteString(s)
	}
	src, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("generated %s does not parse (generator bug): %w\n%s", FunctionsFile, err, out.String())
	}
	if err := checkStandalone(src); err != nil {
		return nil, fmt.Errorf("generated %s (generator bug): %w\n%s", FunctionsFile, err, src)
	}
	return src, nil
}

// checkStandalone enforces the functions.go contract: it type-checks alone
// with no importer, and declares no imports, no package-level variables, no
// main and no init.
func checkStandalone(src []byte) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, FunctionsFile, src, 0)
	if err != nil {
		return err
	}
	if len(file.Imports) > 0 {
		return fmt.Errorf("declares imports")
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok == token.VAR {
				return fmt.Errorf("declares a package-level variable")
			}
		case *ast.FuncDecl:
			if d.Recv == nil && (d.Name.Name == "main" || d.Name.Name == "init") {
				return fmt.Errorf("declares func %s", d.Name.Name)
			}
		}
	}
	conf := types.Config{Error: func(error) {}}
	if _, err := conf.Check("main", fset, []*ast.File{file}, nil); err != nil {
		return fmt.Errorf("does not type-check alone: %w", err)
	}
	return nil
}

// batteryDriverSource is the gc reference driver for the battery: it calls
// every (function, input) pair under recover and writes one line per call
// to stdout, then an END line with the call count:
//
//	R <fn> <input> ok <result>...
//	R <fn> <input> panic <strconv-quoted message>
//	END <calls>
//
// Fields are tab-separated. Only runtime.Error panic values are expected
// (bodies emit no explicit panic); any other value exits 3, as does a
// 32-bit target.
func (g *Generator) batteryDriverSource() ([]byte, error) {
	var out strings.Builder
	out.WriteString("package main\n\nimport (\n\t\"os\"\n\t\"runtime\"\n\t\"strconv\"\n)\n\n")
	out.WriteString(`func batteryFail(msg string) {
	os.Stderr.WriteString("battery: " + msg + "\n")
	os.Exit(3)
}

func batteryOK(fn, in int, vals ...string) {
	line := "R\t" + strconv.Itoa(fn) + "\t" + strconv.Itoa(in) + "\tok"
	for _, v := range vals {
		line += "\t" + v
	}
	os.Stdout.WriteString(line + "\n")
}

func batteryRecover(fn, in int) {
	r := recover()
	if r == nil {
		return
	}
	e, ok := r.(runtime.Error)
	if !ok {
		batteryFail("panic value is not a runtime.Error")
	}
	os.Stdout.WriteString("R\t" + strconv.Itoa(fn) + "\t" + strconv.Itoa(in) + "\tpanic\t" + strconv.Quote(e.Error()) + "\n")
}

`)
	calls := 0
	for fi, f := range g.battery.funcs {
		ps := make([]string, len(f.Params))
		args := make([]string, len(f.Params))
		for j, p := range f.Params {
			ps[j] = fmt.Sprintf("a%d %s", j, p)
			args[j] = fmt.Sprintf("a%d", j)
		}
		rs := make([]string, len(f.Results))
		enc := make([]string, len(f.Results))
		for j, r := range f.Results {
			rs[j] = fmt.Sprintf("r%d", j)
			enc[j] = encodeResult(r, rs[j])
		}
		fmt.Fprintf(&out, "func call%d(in int, %s) {\n\tdefer batteryRecover(%d, in)\n\t%s := %s(%s)\n\tbatteryOK(%d, in, %s)\n}\n\n",
			fi, strings.Join(ps, ", "), fi, strings.Join(rs, ", "), f.Name, strings.Join(args, ", "), fi, strings.Join(enc, ", "))
		calls += len(f.Inputs)
	}
	out.WriteString("func main() {\n\tif strconv.IntSize != 64 {\n\t\tbatteryFail(\"needs a 64-bit target\")\n\t}\n")
	for fi, f := range g.battery.funcs {
		for ii, in := range f.Inputs {
			args := make([]string, len(in))
			for j, a := range in {
				args[j] = fmt.Sprintf("%s(%s)", f.Params[j], a)
				if f.Params[j] == "bool" {
					args[j] = a
				}
			}
			fmt.Fprintf(&out, "\tcall%d(%d, %s)\n", fi, ii, strings.Join(args, ", "))
		}
	}
	fmt.Fprintf(&out, "\tos.Stdout.WriteString(\"END\\t%d\\n\")\n}\n", calls)
	return format.Source([]byte(out.String()))
}

func encodeResult(goType, v string) string {
	if goType == "bool" {
		return "strconv.FormatBool(" + v + ")"
	}
	if _, unsigned := intKind(goType); unsigned {
		return "strconv.FormatUint(uint64(" + v + "), 10)"
	}
	return "strconv.FormatInt(int64(" + v + "), 10)"
}
