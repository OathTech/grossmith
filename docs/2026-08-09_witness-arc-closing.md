# Witness arc: closing record (2026-08-09)

The arc ran to charter completion on branch `witness-arc` (head
`020a4bc`, 11 commits over `main`). Every rung either delivered or
recorded its deviation honestly; two Opus code reviews ran per the
charter's plan and every finding is fixed or recorded on-branch.

## Rungs

- **W0 (tuple forwarding)** — FOLDED at `d0f4126` on explicit sign-off,
  after two review passes (main session + the mid-arc review) and the
  review-response fixes (srcBoxed negative-control axis, call-shape
  guarantee stated true and witnessed by paired index, any_slot
  stratifier). Fix-pair cross-validation on BUG-049: 16 flips, all
  in-tag, first detection at case 22. The merge resolution reconciled
  W0 with W4's bound tracking (sink results unknown-bounded).
  Post-fold campaign (n=300, seed 94000): 287 match / 13 clone-infra /
  0 mismatch — all the one quarantine class; tuple_forward judged
  53/54, any_slot 16/16, wrapper 15 = 15 + 0.
- **W1 (BUG-042/043 measurement)** — 27/27 attributed flips, first
  detection at case 36 (`docs/2026-08-09_w1-bug042-measurement.md`).
- **W2 (R2b order witnessing)** — the order corner, the wit/wOrd
  instrument, E3 truncation witnessed at exact values.
- **W3 (element-inclusive range folds)** — F14 closed at the fold
  expression; the handcrafted shared-write witness observes the
  slice_triple alias write (185) where the index-only fold provably
  cannot (6).
- **W4 (width_dependent precision)** — static magnitude bounds;
  saturation 97.6% → 87.5% (n=2000), off-tag population 2.4% → 12.5%.
  TWO deviations recorded: (1) the charter's <50% target is missed FOR
  CAUSE — boundary-literal density makes lower rates under-tagging;
  (2) the first cut shipped three under-tag mechanisms (unsigned
  underflow, conditional-write replacement, loop staleness) that the
  ARC-END REVIEW caught with reproduced 32-vs-64 divergences — all
  closed in `020a4bc`, and the soundness screen now sweeps the exact
  breach range.
- **W5 (membership-lane design, stretch)** — reviewable note delivered
  (`docs/2026-08-09_membership-lane-emission-design.md`).

## Reviews

Mid-arc (after W2): 12 findings, all fixed — the headline was the
claims layer (nightly wrapper gate loosened to admit a 2/3 judgement
regression; two ledger overstatements on W0). Response: the exact
three-leg identity `wrapperCaught == wrapperJudged + wrapperCloneInfra`,
Validate-time corner-contract rejections, the witness_shortcircuit and
any_slot stratifiers.

Arc-end: 10 findings — three critical under-tags in W4's first cut
(fixed as above), the seed-lucky soundness screen (widened), the
mislabelled short-circuit tag (right-operand-only now), a silent tape
draw-order change (restored), stale headline numbers (re-measured from
a clean tree). The reviewer's clean list covers W3's in-bounds
argument, the wrapper identity, the corner contracts, replay, and
environment sensitivity.

## Closing campaign

n=300, seed 93000, generator `020a4bc` (clean tree), clone
golean@3d21582: **288 match / 12 clone-infra / 0 observation-mismatch**.
All 12 failures are the single short-circuit-operand class. Wrapper:
18 caught = 18 judged + 0 quarantined. Judged coverage: order_witness
22/29, element_fold 9/9, witness_shortcircuit 0/7 (the quarantined
population, now visible as such), width_dependent 260/272.

Every campaign in the arc (seeds 90000/91000/92000/93000, n=300 each)
held the residual-failure floor: one class, two reporting surfaces,
zero semantic divergence.

## Awaiting sign-off

1. ~~Fold `w0-tuple-forward` into `witness-arc`~~ — DONE at `d0f4126`.
2. Merge `witness-arc` to `main`.
3. Push (local `main` is ahead of origin; the note to the GoLean team
   only reaches them via the pushed tree).

## Deferred, recorded

Membership-lane implementation (next arc, note in place); pointer-
parameter witness (mechanism 2); per-type wit helpers (discard-path
dependency comments in place); recovered-event coverage rung; the
nonConstExpr pureMode conversions gate; the vacuous local cross-arch
printout; W6-candidate: splitting width_dependent by cause if sharper
stratification is ever needed.
