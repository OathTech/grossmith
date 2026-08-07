# Phase 2 slice 2: historical-defect campaigns (2026-08-07)

Method (design note, slice 2): differential of differentials. The SAME
batch (n=100, seed base 42000, identical subject hashes verified) runs
against the pre-fix and post-fix checkouts of a bug's fix commit in a
scratch clone of GoLean; any per-case verdict change between the two
runs is attributable to that fix. "Detection" = at least one flip.

Provenance: campaign artifacts live in gitignored `.tmp/phase2/` and
are not committed; the corpus is anchored to this branch's tip instead
— the pre-merge audit verified HEAD regenerates the v1 campaigns'
subjects bit-for-bit (100/100 subject hashes) under
`golean.Profile(gen.DefaultConfig(seed))`, seeds 42000..42099. The v0
(pre-bare-call) generator is commit e475e85.

Candidate set: the four fixed, differential-pinned GoLean fidelity bugs
inside grossmith's generated grammar (of 15 fixed in their ledger).

## BUG-012 — bare value-returning calls went stuck (fix a8e2b3e)

The headline result: a measured generator gap, closed, with detection
proven on both sides of the fix.

| campaign | generator | rev | verdicts | flips vs pair |
|---|---|---|---|---|
| A | v0 (no bare calls) | pre-fix | 89 match / 11 clone-infra | — |
| B | v0 | post-fix | 89 match / 11 clone-infra | **0** |
| C | v1 (bare-call arm) | pre-fix | 82 match / 18 clone-infra | — |
| D | v1 | post-fix | 91 match / 9 clone-infra | **9** |

- **A→B: zero verdict flips.** grossmith's original grammar was
  PROVABLY blind to BUG-012 — `callStmt` always wrote assignment
  targets, so the bare-call lowering never executed. GoLean's own audit
  had found this shape their single most frequent novel-program failure
  (16 of 153 probes). A generator that "covers helpers" did not cover
  the ubiquitous way helpers are called.
- **The gap closure**: a `bare_call` optional construct (weight-1 arm
  emitting `hN(args)` with no targets — legal, ubiquitous, dead-by-
  purity and tagged as such). Witness: TestBareCallEmitted. The arm's
  draw-path shift also exposed a latent constant-context bug (len of a
  literal-fallback string marked non-constant), fixed separately.
- **C→D: 9 verdict flips, all clone-infra→match, all in cases
  containing a bare call** (attribution checked case-by-case: zero
  flips in bare-call-free cases; subject hashes identical across the
  pair). Cases-to-first-detection: **4**. Full accounting of the 18
  bare-call cases at the pre-fix rev (corrected by the pre-merge
  audit — the original text conflated "18 clone-infra in C" with "18
  bare-call cases", equal only by coincidence): 11 were clone-infra,
  of which 9 flip and 2 stay red for INDEPENDENT reasons (one
  mismatched-integer-kinds stuck, one short-circuit frontend
  quarantine); 7 matched even pre-fix — the bare-call shape does not
  deterministically trigger BUG-012 (their machine goes stuck only on
  specific lowering paths), which is why detection is probabilistic
  and cases-to-first-detection is the honest metric.

## BUG-021 — append-spill capacity envelope (fix 5c08df2)

**Undetectable by construction — no campaign run, argued instead.**
The bug is a capacity-envelope error, and grossmith's slice discipline
never observes `cap`: reallocation is value-invisible (elements are
copied), so no strict-lane observation can distinguish the envelopes.
GoLean's own strict lane showed "zero baseline drift outside the three
[membership] pins" for the same reason. This is direct evidence for two
backlog items: three-index slicing (g32 — pins cap deterministically,
making capacity behavior observable) and membership-lane emission.

## BUG-001 (field/element write lowering) and BUG-006 (raw interface slots)

**Blocked on manifest drift.** Their fix commits (2026-07-25 and
2026-07-30) predate the 10-field lane-column manifest; the pre-fix
`diff-coverage` readers take 7 fields and would reject every row the
adapter writes. Measuring these requires either a legacy-manifest mode
in the adapter (coupling to retired formats — not worth it) or
GoLean-side replay of the bugs at a modern rev. Recorded, not pursued.

## Planted defects (the remaining families)

The historical set left four families unmeasured, so defects were
PLANTED: one-line semantic breaks in a scratch clone of GoLean HEAD
(`a38e086`), each built and campaigned with the same 100 subjects (seed
42000) against the unplanted baseline (91 match / 9 clone-infra).
Machine-level plants must be made in BOTH layers where the relation and
stepFn are separate (the control-flow plant initially broke
`stepFn_sound` — their soundness architecture correctly rejecting an
inconsistent lie; the consistent two-layer plant builds), and in the
single shared op where they are not.

| family | plant (one line) | flips vs baseline | first detection |
|---|---|---|---|
| width/conversion | signed wrap dropped in `IntKind.normalize` | 78 | case 2 |
| control flow | `break` steps to `.next` (no-op) | 53 | case 1 |
| interface dispatch | concrete type assert always fails | 5 | case 19 |
| map semantics | found-key `delete` is a no-op | 4 | case 11 |

Every flip is match→observation-mismatch (or stuck-class movement) —
no plant ever leaked into a non-semantic verdict, and the scratch tree
was verified pristine after each revert. Two plants required
accompanying edits to keep the build honest rather than silent: the
control-flow plant had to be made consistently in BOTH the relation and
stepFn (their `stepFn_sound` correctly refused the one-layer lie), and
the map plant needed its `StateWf` preservation proof branch adjusted
to the planted identity semantics. The interface and width plants used
condition-level breaks (`&& false`) that leave both proof branches
intact.

Defer/recover is UNMEASURABLE against GoLean under the current profile:
both grossmith defer forms are obs* events, which the profile excludes
because their machine has no event model. Evidence for the g06/c54
(named-results defer) rung in TODO.md — the first non-obs defer
construct makes this family measurable.

## Reading — Phase 2 done-when, met

The audit's done-when: "every supported observation shape has a
targeted unequal-state witness (slice 1's eleven-control matrix) and
the end-to-end production path detects the selected defects" across
the named families. Final scoreboard, 100 cases per campaign:

| family | defect | flips | first detection |
|---|---|---|---|
| call lowering | historical BUG-012 (pre/post fix commit) | 9 | 4 |
| width/conversion | planted (signed wrap dropped) | 78 | 2 |
| control flow | planted (break no-op) | 53 | 1 |
| interface dispatch | planted (assert always fails) | 5 | 19 |
| map semantics | planted (delete no-op) | 4 | 11 |
| defer/recover | unmeasurable-by-profile (obs*-based; g06/c54 rung unblocks) | — | — |

Sensitivity ordering is itself information: width and control flow
saturate the corpus (protocol and grammar are strong there); interface
and map defects are detected but thinly — the assert/comma-ok and
delete-then-observe paths are minorities, a weight-tuning datum for the
ladder. One family became detectable only after a measured generator
gap was closed (bare calls) — the loop the audit asked Phase 2 to
prove, demonstrated end to end. The defined-type ++/-- natural control
(grossmith's own find) joins this table when GoLean's fix lands;
BUG-021 stands as by-construction evidence for the g32/membership
backlog items.
