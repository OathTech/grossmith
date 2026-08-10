# Roadmap

The ONE living roadmap. Dated documents under `docs/` are designs,
audits, and closing records; none of them governs ordering — this file
does, and it changes in the same commit that opens or closes an arc.
It is an index with pointers, not a plan document.

## What grossmith is

A generator of small, valid Go programs for differential conformance
testing: programs run under the reference `gc` toolchain and under a
Go reimplementation (today GoLean, `deps/golean`), and every
disagreement is classified by a closed verdict taxonomy —
infrastructure failure is never conflated with semantic divergence.
Every program compiles and halts by construction; generated programs
in the STRICT lane — today the entire corpus — are
outcome-deterministic by construction; other lanes (designed, not yet
emitted) carry explicit lane-specific oracles.

## Current capability state (2026-08-09)

Delivered and merged: the generator with swarm mixes, named corners,
and capability profiles (`gen`); the `grossmith-observation-v2`
protocol (`observe`); the adapter harness with durable per-case and
per-batch artifacts (`harness`); the GoLean campaign adapter
(`golean`); measured observation sensitivity (positive controls,
historical fix-pair campaigns); draw-trace replay (`gengo -replay`);
the spec-surface ledger with its honesty gate; the request-driven
ladder (GoLean's R1, R2a, R3, R4, R5, then R2b order witnessing and
tuple forwarding); tier-1 push CI and the golean-nightly heartbeat.
The witness arc (W0-W5) is complete. Ground truth:

- `docs/spec-ledger.md` — what is generated, quotiented, deferred and
  WHY (the honesty gate `TestLedgerNamesEveryTag` enforces it).
- `docs/2026-08-09_witness-arc-closing.md` — the witness-arc record.

## Closed arc: EVIDENCE (merged 2026-08-09 at 01fab3f)

`docs/2026-08-09_evidence-arc-charter.md` was its charter; the record
of what held and what did not is
`docs/2026-08-09_evidence-arc-status.md`. The 2026-08-09 comprehensive
audit found the EVIDENCE BOUNDARY — not the generator — was the weak
layer. Delivered: E0 claims/lifecycle reconciliation (this file is its
product), E1 fail-closed inputs, E2 one pinned Go oracle, E3
experiment identity and atomicity, E4 resource guarantees, E5 the
arc-end review's seven blockers, E6 the execution budget (HALTS
enforced at emission for every tape;
`docs/2026-08-09_execution-bound-design-note.md` records why a closed
form was abandoned). Two re-reviews, three clean campaigns.

## Current arc: CONTAINMENT (in progress)

The 2026-08-10 audit
(`docs/2026-08-10_comprehensive-technical-audit.md`, committed
verbatim) found that the evidence arc's closure did not bottom out the
evidence boundary. Two of its findings were replicated here before any
fix: a descriptor naming case ID `../outside` validated and would be
judged, and a directory holding any file merely NAMED `manifest.tsv`
was accepted as `-out` and deleted on publish. Rungs:

- **C0 — containment and lifecycle.** Descriptor names are contained
  before any path is joined; `-out` ownership is a content test;
  Depth/LoopCap get upper bounds so derived arithmetic stays exact;
  these documents match the merged state. DONE.
- **C1 — evidence correctness.** The GoLean boundary fails closed
  (prevalidated case slice, no duplicate-ID overwrite, exported
  documents validated); `Judge` validates before classifying;
  free-text suffix matching is replaced by a typed clone status;
  `batch.json` gains strict decode plus self-consistency validation, so
  `-verify` states input integrity and report consistency separately.

One finding is REFUTED and stays refuted: the audit's "shared
diagnostic buffers have data races" P1. Both cited sites assign the
SAME buffer to `Stdout` and `Stderr`, and `os/exec` documents that
case — "If Stdout and Stderr are the same writer, and have a type that
can be compared with ==, at most one goroutine at a time will call
Write." The subject-run path uses two distinct buffers. No race; no
change.

Deferred to their own charter (the audit's R2 onward): aggregate-fold
lossiness and path-valid aggregates, whole-case resource limits
(driver reflection tree, JSON, parser), order-witness collision
bounds, real build/run matrices, and the incidence/sensitivity
accounting.

## House convention: wording

Grossmith checks that a MEASUREMENT is honest; it does not defend
against an adversary. Describe failures by their ordinary cause — a
leftover file, a second toolchain on the PATH, a hand-edit, a stalled
compiler, unbounded output — not in security-incident vocabulary
(injection, tampering, decoys, hostility). This was learned twice: the
witness arc's charter and the evidence arc's first draft both drifted
into that register and were rewritten. New rungs and delegated briefs
inherit the convention.

## Deferred queue, in order (the audit's R4-R7)

1. **R4 — durable evidence corpus and operational CI.** Checked-in
   compact fix-pair transitions (BUG-012/042/043/049) and fake-clone
   contract tests; deterministic 386 canaries; nightly last-green
   identities published, red case trees retained; minimum/current Go
   and a second-OS lane; pinned workflow inputs. Exit: a clean clone
   reproduces representative historical detections.
2. **R5 — machine-readable ledger and pair accounting.** Structured
   ledger with witness linkage, profile support, language-version
   scope; pair-compatibility denominator and generated/judged
   realization matrices; CI rejects phantom tags, absent witnesses,
   impossible pairs, unexplained coverage regressions.
3. **R6 — membership-lane implementation.** The first non-strict
   lane: raw map iteration order under GoLean's membership oracle.
   Design: `docs/2026-08-09_membership-lane-emission-design.md` —
   BLOCKED on a stable machine-readable GoLean reason-code contract
   for their membership stages. Then: bounded, modularly proven
   multiplicity, explicit width, `verdictsByLane`, a forced
   all-verdict-classes canary. Strict-lane headlines never mix in
   membership cases.
4. **R7 — sensitivity and capability growth.** In order: the
   recovered-event coverage rung (guarded-statement positive
   control); the pointer-parameter witness (effect-discipline
   mechanism 2, design note first —
   `docs/2026-08-07_effect-discipline-design.md`); per-type `wit`
   helpers beyond plain int; then embedding/promotion and
   floats/complex per the ledger's deferrals (floats enter only with
   the explicit bit/NaN equivalence policy). Generics and concurrency
   remain later, dependency-driven.

Beyond the queue, the ladder is the ledger: every `deferred(reason)`
row in `docs/spec-ledger.md` is backlog, and `TODO.md` is the issue
inventory (unordered; this file owns ordering).
