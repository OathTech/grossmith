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

## Current arc: EVIDENCE (in progress)

`docs/2026-08-09_evidence-arc-charter.md` governs. The 2026-08-09
comprehensive audit (`docs/2026-08-09_comprehensive-technical-audit.md`)
found the EVIDENCE BOUNDARY — not the generator — is the weak layer;
this arc makes a campaign an immutable, self-consistent, reproducible
experiment. Rungs: E0 claims/lifecycle reconciliation (this file is
its product), E1 fail-closed inputs, E2 one pinned Go oracle, E3
experiment identity and atomicity, E4 resource guarantees made true.
No language surface. Branch `evidence-arc`; merge waits for arc-end
sign-off.

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
