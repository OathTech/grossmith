*DECIDED (2026-08-09): the user chose OPTION B — the emission-time cost
budget — and rolled it into the evidence arc as rung E6 (charter:
`docs/2026-08-09_evidence-arc-charter.md`). The analysis below is the
record of why. Status: `docs/2026-08-09_evidence-arc-status.md`.*

# The executed-statement bound: closed form vs emission-time budget

## Where E5 got to

The arc-end review refuted E4's bound by measurement. E5 closed the
measured MECHANISMS, each with its witness in the suite:

- **The loop-nest freeze** (`loopFrozenSlices`): a slice ranged inside a
  loop nest cannot be appended to until the nest closes, so a range's
  emission-time gate can no longer be outrun by an append emitted later
  in the same nest. The review's counterexample — seed 174813, its one
  occurrence in seeds 150,000-210,000, reproduced byte-for-byte by our
  own scan — is caught by `TestNoAppendAfterRangeInLoopNest` when the
  freeze is disabled and refused by construction when it is on.
- **String gates** (`stringRangeableVars`, the literal-operand fold):
  string ranges take variable operands only at the subject's top level,
  under a byte-length bound tracked through every write site (with a
  fail-safe unknown default at the `writeBound` chokepoint); inside
  loops and pure bodies the operand is a literal. String growth is
  charged like slice growth.
- **Per-site `maxExec`** re-derived (trip-product with a one-level
  floor for helper bodies), fixing two honest defects along the way:
  the map-fold's conflated iteration factor, and `projectInner` reading
  declaration-time bounds for a variable the block may have written.

Instrumented replay (`TestExecutedStatementsMeasured`): the
regenerated counterexample neighborhood now peaks at 123,825 executed
statements against the review's 14,372,767. DefaultConfig reality:
median 54, max 288 over 60 seeds.

## What is still open — and why it stopped here

`Config.Validate`'s formula `Stmts * (8*LoopCap)^Depth <= 4e6` remains
a **plausibility screen, not a proof**, because a closed form over the
grammar must price three things it currently prices at zero or one:

1. **Block branching.** A loop body holds 3-5 statements plus the inner
   declaration and its projection; the formula multiplies by trips
   only.
2. **Fold trips.** Map and string folds are loops that consume no depth
   budget.
3. **Calls.** This is the deep one. Helper and method bodies generate
   at depth 1, so they carry loops (15.9% of DefaultConfig programs);
   helpers call other helpers (15.2%); calls nest in argument position
   (~7.5 sites per program, up to `ExprFuel` deep); and a call can sit
   inside any expression at any nesting level. Priced honestly,
   helper-calls-helper-in-a-loop compounds per helper generation, and
   the true worst-case tape at DefaultConfig sits orders of magnitude
   above 4e6 — while the measured typical program is ~54 statements.

The charter's pre-made A1 decision ("widen `maxLenBound`, else a
post-pass") cannot reach this: widening addresses only mechanism 1 of
the refutation, and a post-pass that REJECTS finished programs would be
filtering — the invariants are by construction, never by filtering.
Making a closed form honest therefore forces grammar restrictions
nowhere pre-authorized (loop-free and call-free pure bodies, a fuel
floor on call nesting), which would delete exactly the populations that
matter to GoLean: nested calls are argument-evaluation-order surface
(R2b's home turf), and calls under loops exercise repeated call
lowering.

## The options

**A. Restrict the grammar to fit a closed form.** Pure bodies become
loop-free and call-free; call arms require a fuel floor. Arithmetic
fits under 4e6 with ~2-3x margin at DefaultConfig. Cost: deletes the
populations above; and every future emitter change re-derives the
formula by hand — the exact failure class that produced both the W4
`writeBound` bug and the E4 range-gate bug.

**B. Emission-time cost budget (recommended).** The generator already
knows, at every emission point, the exact worst-case cost of what it
emits: every trip count is a literal, every helper body is fully
generated before its first call site, and the freeze pins range trip
counts. Maintain a running cost accumulator (statement cost x the
product of enclosing literal trips) and let arms consult the remaining
budget in their `ok` masks — a loop needs its floor cost affordable, a
call needs its callee's known per-call cost, everything else costs its
multiplier. Nothing emits unless affordable, so `total <= budget` holds
BY CONSTRUCTION, for every tape, with no formula to maintain. At the
4e6 budget nothing is ever masked at DefaultConfig (measured max 288),
so the corpus is untouched; extreme tapes degrade gracefully to cheap
arms instead of the config being refused. Validate keeps only sanity
caps. The witness is direct: instrumented execution vs the budget
constant, plus a budget-exhaustion program that still compiles and
halts.

**C. Split ceilings (time vs retention), keep the grammar.** Honest
only if the TIME ceiling is set from measured statement rates on BOTH
adapters — GoLean's execution rate is unknown to us and may be orders
slower than gc. Rejected until someone measures it; and it still keeps
a hand-maintained formula.

## Recommendation

Option B, as its own rung after the current E5 items land. It is more
work than a constant (cost accounting through the statement and
expression emitters, per-helper cost records, budget-aware `ok` masks),
but it retires the entire "formula forgot an emitter" failure class —
which has now bitten three times (W4 `writeBound`, E4 range gate, E4
call pricing) — and it is the only option that is exact, by
construction, and corpus-neutral for every config anyone actually runs.

Until the decision: the freeze, gates, and measurements above stand on
their own; `Validate`'s comment states plainly that its formula is a
screen; and the arc's E4 exit condition remains UNMET on the
universal-bound half.
