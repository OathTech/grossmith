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

## Current capability state (2026-10-03)

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

## Closed arc: EVIDENCE (merged 2026-08-10 at 01fab3f)

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

## Closed arc: CONTAINMENT (merged 2026-08-10 at e907c22)

The 2026-08-10 audit
(`docs/2026-08-10_comprehensive-technical-audit.md`, committed
verbatim) found that the evidence arc's closure did not bottom out the
evidence boundary. Two of its findings were replicated here before any
fix: a descriptor naming case ID `../outside` validated and would be
judged, and a directory holding any file merely NAMED `manifest.tsv`
was accepted as `-out` and deleted on publish. Rungs:

- **C0 — containment and lifecycle.** DONE. Descriptor names are
  contained before any path is joined (P0); `-out` ownership is a
  content test (P0); Depth/LoopCap have upper bounds so derived
  arithmetic stays exact; these documents match the merged state.
- **C1 — evidence correctness.** DONE. The GoLean boundary fails closed
  (whole-slice prevalidation, duplicate IDs fail the run instead of
  overwriting a verdict, exported documents validated, translated-case
  tree cleared under an ownership marker, `Profile` merges instead of
  replacing); `Judge` validates both documents before classifying;
  free-text suffix matching is replaced by a typed clone status with a
  closed vocabulary; `batch.json` gains strict decode plus
  self-consistency validation (membership, totals, histograms, wrapper
  accounting, subject digests, composition, and re-judged verdicts), so
  `-verify` states input integrity and report self-consistency as
  separate claims.

What C1 does NOT close, stated because `-verify` now invites the
question: the adapter identity strings and oracle version text are not
derivable from a batch, so they are structure-checked and
digest-bound, never recomputed. And a GoLean verdict cannot be
re-judged offline at all — their harness applies its own equivalence
and we record its conclusion, not the structured observation behind it.
That is the audit's "reports cannot be independently re-judged" P1,
which needs a versioned result document from their side; it is stated
in `harness/report.go` and `golean/workdigest.go` rather than implied
away, and belongs to the next arc with the sensitivity work.

One finding is REFUTED and stays refuted: the audit's "shared
diagnostic buffers have data races" P1. Both cited sites assign the
SAME buffer to `Stdout` and `Stderr`, and `os/exec` documents that
case — "If Stdout and Stderr are the same writer, and have a type that
can be compared with ==, at most one goroutine at a time will call
Write." The subject-run path uses two distinct buffers. No race; no
change.

Deferred to their own charter (the audit's R2 onward): aggregate-fold
lossiness, whole-case resource limits
(driver reflection tree, JSON, parser), order-witness collision
bounds, real build/run matrices, and the incidence/sensitivity
accounting.

## Current work: improvements awaiting landing

Branch work (2026-10-03, `codex/grossmith-improvements`): aggregate fingerprints
now snapshot state on early returns and wrapper recovery as well as normal
returns. Controlled mutation witnesses reproduce the former zero-slot blindness
and check all returning paths; generation sweeps and execution counters check
composition and budget preservation. Fingerprint collisions remain open.
The CLI also supports `-clone gc`, a separate `-clone-go` toolchain, and
`-clone-gcflags` for optimization comparisons. Direct gc clone identities
include compiler flags and binary digests, and offline verification recognizes
that gc clones build the same manifested inputs as the reference.
Outcome validation now precedes every comparison, including mixed build-failure
and document-error pairs. A 100-pair matrix checks classification and rejects
unknown policies, unknown statuses, and contradictory adapter payloads.
Saved-report validation requires complete comparison evidence, rejects dropped
histograms and unknown policies, checks case-record identities against the
manifest, and recomputes judged composition and both wrapper counters. These
checks are witnessed by report mutations with faithfully rebound file digests.
All JSON evidence readers now reject duplicate object names (including escaped
aliases) and trailing content. GoLean's external observation reader retains its
unknown-field compatibility while rejecting ambiguous status fields.
String observations now preserve arbitrary bytes through the additive
`strBytes` payload, and `string_bytes` enables slicing across UTF-8 boundaries.
The driver and comparator preserve scalar, nested and event values. GoLean
supports this coverage through its existing byte-array string channel from
revision `3bb8f4fc9cd7dab16571787140731b6d4c1f9d0e` onward.
A `utf8-split` string-slice arm, available only when `string_bytes` is in the
construct mix, raises realisation over seeds 1-1000 from 5 to 30 cases under
the default profile and from 10 to 36 under the GoLean profile. The eligible
populations are 52 and 56 cases: each reaches a string-slice site with the
construct enabled. With the construct excluded, generated sources and draw
traces for those seeds are byte-identical to the generator before this arm.

**Seed-to-program mapping changed on this branch.** Adding `string_bytes` to
the optional constructs adds a construct-mix draw. Every seed therefore
generates a different program than it does on `main`, and cases saved by
`main`'s `gengo` are refused by this branch's `-replay` rather than replayed
(for example `replay value 5 at draw 48 is outside the requested bound
[0,2)`). Old saved cases must be replayed with the generator revision that
wrote them; `case.json` records it as `generatorRev`. The `utf8-split` arm
changes programs again for seeds whose mix enables `string_bytes`. Seed-for-seed
comparisons across this boundary compare different programs. They do not
attribute behaviour to a change (the
[pre-landing review](2026-09-17_grossmith-improvements-prelanding-audit.md)
first recorded this).

Follow-ups for the CLI owner, not yet scheduled:

- The replay refusal already names both revisions ("recorded under generator
  X, this binary is Y"). The revisions can still be wrong. `generatorRev()`
  prefers Go's `vcs.revision` build setting, and Go stamps that from the
  nearest enclosing directory with a `.git` *directory*. A git worktree
  (whose `.git` is a file) or a `git archive` tree extracted inside another
  checkout therefore gets the enclosing checkout's HEAD and clean status. A
  binary built in a nested worktree at a later commit was observed stamping
  the outer checkout's `615498d`. A `main` build from an archive under
  `.tmp/` stamped the same revision, so the refusal printed identical
  revisions for two different generators. The stamp should be checked
  against the source actually built, or the mismatch should be reported as
  unknown provenance. Until then, `go build -buildvcs=false` from the source
  tree uses the cwd git probe instead.
- When decoding fails and the recorded revision differs from the current
  one, the refusal could say "this case was written by generator revision
  X; replay it with that revision" as its leading line. Today it prints a
  warning and then the decode error.
The CLI's `-check` mode now reruns saved cases or edited single-file subjects
into a separate verifiable batch. It records source provenance and driver
choice without claiming generated coverage or a replayable draw tape.
The [pre-landing review](2026-09-17_grossmith-improvements-prelanding-audit.md)
found four gaps in these additions. The
[review response](2026-09-17_grossmith-improvements-review-response.md) records
the driver contract, lossless panic messages, exact JSON field spelling, and
shared source-provenance validation that address them.

The [October 3 audit](2026-10-03_unlanded-work-audit.md) and its
[repair record](2026-10-03_improvements-repair-record.md) cover the remaining
panic-encoding, interrupted-publish and string-payload findings. The branch
also incorporates main's September 22 oracle-version and strict-depth fixes.
The saved assignment-before-panic divergence is checked in under
`golean/testdata/assignment-before-panic`; a current GoLean checkout matches it,
and the nightly runs its regression witness. The larger queue below remains
deferred until this branch lands.

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
