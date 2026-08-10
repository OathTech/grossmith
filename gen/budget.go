package gen

// The execution budget (evidence arc E6; design-note option B, the
// user's call). HALTS' executed-statement half is enforced HERE, at
// emission, by construction — not by a closed-form formula over the
// grammar. Three bound defects shared one shape ("a static figure read
// at emission describing state that execution changes": W4's original
// writeBound, E4's range gate, E4's unpriced calls); this mechanism
// retires the shape, because the generator charges each emission its
// EXACT worst-case executions at the moment it knows them:
//
//   - every trip count is a literal (or a gated bound the loop-nest
//     freeze holds still), so the multiplier over enclosing loops —
//     execMul — is exact at every emission point;
//   - every callee body is fully generated before its first call site,
//     so a call charges a recorded per-call cost;
//   - both branches of a conditional are charged (bounded overcharge,
//     never an undercharge).
//
// Arms consult affordability BEFORE emitting (budgetLeft minus the
// floor liability of already-committed constructs), so nothing emits
// unless payable and every committed construct can finish on 1-cost
// statements. Extreme tapes degrade to cheap arms; no config is
// refused for cost, and total executed statements <= ExecBudget holds
// for every tape.
//
// The "executed statement" unit matches the instrumentation witness
// (gen/execmeasure_test.go): one count per statement position in a
// block, per execution. Observation folds at the subject's end are
// PRE-PAID: each declaration charges a per-variable constant covering
// its observation shape, and each append charges one extra execution
// for the element the final fold will visit.

const (
	// ExecBudget is the ceiling on one subject's worst-case executed
	// statements — E4's memory derivation, unchanged: a defer record or
	// append can retain ~56B per executed statement, so 4e6 bounds
	// retained memory to ~224MB.
	ExecBudget = 4_000_000

	// perVarReserve pre-pays one binding's declaration and observation:
	// the decl line, the discharge or obs line, and the worst aggregate
	// fold base (len line + loop over <= 4 initial elements or the
	// 4-key map alphabet, plus the close). Appends pre-pay their own
	// fold visits on top.
	perVarReserve = 16
	// fixedReserve pre-pays per-subject scaffolding outside the
	// statement tree: the return line, the order-witness declaration
	// and helper, and slack for the trailing observed slots.
	fixedReserve = 128
	// wrapperReserve pre-pays the recover wrapper's prologue and defer
	// body (psite lines are charged per top-level statement instead —
	// they scale with Stmts).
	wrapperReserve = 32

	// budgetCap keeps saturating budget arithmetic away from overflow.
	budgetCap = int64(1) << 62
)

// satAdd/satMul are plain saturating arithmetic for budget accounting.
// NOT boundAdd/boundMul: those treat 0 as "unknown" (the W4 magnitude
// convention); here 0 is a real zero.
func satAdd(a, b int64) int64 {
	if a > budgetCap-b {
		return budgetCap
	}
	return a + b
}

func satMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > budgetCap/b {
		return budgetCap
	}
	return a * b
}

// charge records n worst-case executed statements: against the pricing
// sink while a pure body (helper/method) is being generated, against
// the subject's pool otherwise. Mandatory lines charge unconditionally;
// the floor-liability protocol is what guarantees they were payable —
// budgetBreached going true means the accounting itself has a bug, and
// the white-box witness asserts it stays false across sweeps.
func (g *Generator) charge(n int64) {
	if g.costSink != nil {
		*g.costSink = satAdd(*g.costSink, n)
		return
	}
	if n > g.budgetLeft {
		g.budgetLeft = 0
		g.budgetBreached = true
		return
	}
	g.budgetLeft -= n
}

// afford reports whether an OPTIONAL emission costing n fits above the
// committed floors. Pure bodies are priced, not budgeted: their cost is
// weighed by call sites instead.
func (g *Generator) afford(n int64) bool {
	if g.costSink != nil {
		return true
	}
	return g.budgetLeft-g.floorLiability >= n
}

// commitFloor reserves the worst-case MANDATORY cost of a construct the
// generator just committed to (a block's statement slots, a loop's
// per-iteration index fold). The construct's actual lines charge() as
// they emit while the floor stays reserved — a deliberate double-count
// that makes afford() conservative mid-construct — and the caller
// releases the same figure on exit. Balanced by construction:
//
//	release := g.commitFloor(f)
//	... emit ...
//	release()
func (g *Generator) commitFloor(f int64) func() {
	if g.costSink != nil {
		return func() {}
	}
	g.floorLiability = satAdd(g.floorLiability, f)
	return func() { g.floorLiability -= f }
}

// priceBody prices a pure body (helper, method): charges accrue to a
// local sink at execMul 1 with no budget constraint, and the recorded
// per-call cost is what call sites weigh against the pool. An expensive
// body is legal — its call sites simply stop being affordable.
func (g *Generator) priceBody(f func()) int64 {
	var cost int64
	savedSink, savedMul, savedLiab := g.costSink, g.execMul, g.floorLiability
	g.costSink, g.execMul, g.floorLiability = &cost, 1, 0
	f()
	g.costSink, g.execMul, g.floorLiability = savedSink, savedMul, savedLiab
	// The call itself plus the return line.
	return satAdd(cost, 2)
}

// Worst-case mandatory floors, sized to the emitters they cover (the
// white-box accounting witness pins these to the code):
//
//	blockFloor: block() emits 1+draw(2) <= 2 statement slots plus the
//	inner declaration and its projection (= 4; the constant carries one
//	slot of slack, in the safe direction).
const blockFloorStmts = 5

// loopBodyFloor adds consumeIndex's one line per iteration.
const loopBodyFloor = blockFloorStmts + 1

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// affordableHelpers filters the helper pool by per-call cost at the
// current multiplier — an expensive helper is legal, it just stops
// being callable where the budget cannot carry its executions.
func (g *Generator) affordableHelpers() []int {
	var out []int
	for i := range g.helpers {
		if g.afford(satMul(g.helpers[i].cost, g.execMul)) {
			out = append(out, i)
		}
	}
	return out
}

// affordableSingleResultHelpers is singleResultHelpers under the same
// budget mask.
func (g *Generator) affordableSingleResultHelpers(t Type) []int {
	var out []int
	for _, i := range g.singleResultHelpers(t) {
		if g.afford(satMul(g.helpers[i].cost, g.execMul)) {
			out = append(out, i)
		}
	}
	return out
}

// affordableDispatchSites / affordableMethodsWithResult: the method-call
// expression arms under the budget mask.
func (g *Generator) affordableDispatchSites(t Type) []dispatchSite {
	var out []dispatchSite
	for _, s := range g.dispatchSites(t) {
		if g.afford(satMul(g.defined[s.di].methods[s.mi].cost, g.execMul)) {
			out = append(out, s)
		}
	}
	return out
}

func (g *Generator) affordableMethodsWithResult(t Type) []methodRef {
	var out []methodRef
	for _, mr := range g.methodsWithResult(t) {
		if g.afford(satMul(g.defined[mr.di].methods[mr.mi].cost, g.execMul)) {
			out = append(out, mr)
		}
	}
	return out
}
