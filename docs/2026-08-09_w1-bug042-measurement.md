# W1: BUG-042/043 historical-defect measurement (2026-08-09)

PURPOSE. Measure whether grossmith's generated corpus detects a real,
historical GoLean defect — the BUG-042/043 integer-kind family fixed in
their 2026-08-07 channels-arc-maint work — using the fix-pair method:
run the same seeds against the commit before the fix and the commit
after it, and attribute every verdict change to the fix. This is the
Phase-2 debt the witness-arc charter names as rung W1. Measurement
only; no generator changes.

## Setup

- Generator: `e295567` (witness-arc branch, pre-W2).
- Seeds 61000–61299 (n=300), standard swarm mix, golean capability
  profile (`NoObserve` slices/maps, `Exclude` observe_point/defer/
  recover). No forced corner: the kinds corner participates at its
  usual 1-in-8 swarm share.
- Fix pair, campaigned in a scratch clone under `.tmp/w1-scratch`
  (deps/golean untouched):
  - pre-fix `6840673` (parent of the fix)
  - post-fix `3700bd9`
- Subject hashes verified identical across the pair for all 300 cases,
  so every verdict change is attributable to the clone-side change.

## Result

| | match | clone-infra-failure |
|---|---|---|
| pre-fix `6840673` | 266 | 34 |
| post-fix `3700bd9` | 293 | 7 |

**27 flips, all `clone-infra-failure → match`, zero flips in any other
direction.** Every one of the 27 pre-fix failures carries the family's
signature — `mismatched +/- integer kinds: <kind> and int` — across
ten distinct kind pairings (uint64, int64, int32, uint32, int16,
uint16, int8, uint8, uint, each against int).

- **Cases to first detection: 36** (case_00035, seed 61035).
- Detection density: 27/300 = 9% of the corpus hits the family.
- Corner contribution: 9 of the 27 flipped cases had the kinds corner
  active. At a 1-in-8 share of the mix that is roughly a 3x
  densification over the base rate — but 18 flips came from the
  ordinary mix (4 of them under the boundary corner), so the family
  was reachable without the corner. The corner buys speed-to-first-
  detection, not reachability, for this defect.

## The residual 7

All 7 post-fix failures are the known short-circuit-operand class
("call/allocation in short-circuit operand (would change evaluation
order)"), and all 7 also failed pre-fix. Six report through the
quarantine document; one (case_00166) reports the identical cause
through the native-frontend error path — a reporting-surface
difference at these older commits, not a second failure class. The
charter's residual-failure floor holds: one class, no exit condition
fires.

## What this banks

The fix-pair method now has two clean data points on real GoLean
defects: BUG-012 (Phase 2: 9 flips, first at 4) and BUG-042/043
(this note: 27 flips, first at 36). Both defects are detected by the
standard mix at n=300 with full attribution and zero contamination
from unrelated verdict movement. Artifacts under `.tmp/w1-pre` and
`.tmp/w1-post` (scratch, not tracked).
