# Phase 2 slice 2: historical-defect campaigns (2026-08-07)

Method (design note, slice 2): differential of differentials. The SAME
batch (n=100, seed base 42000, identical subject hashes verified) runs
against the pre-fix and post-fix checkouts of a bug's fix commit in a
scratch clone of GoLean; any per-case verdict change between the two
runs is attributable to that fix. "Detection" = at least one flip.

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
  pair). Cases-to-first-detection: **4**. Of the 18 bare-call cases at
  the pre-fix rev, 9 flip and 9 stay clone-infra because they
  independently trip the (then-broader) mismatched-integer-kinds stuck
  family — consistent, since the fix commit touches only the bare-call
  lowering.

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

## Reading

The purpose of slice 2 was never "grossmith detects old bugs" — it was
to measure sensitivity honestly. Score: one bug detectable only after a
measured gap was closed (the loop working end to end), one undetectable
by an explicit design quotient with the relaxation already backlogged,
two unmeasurable for infrastructure reasons. The defined-type ++/--
natural control (grossmith's own find) joins this table when GoLean's
fix lands.
