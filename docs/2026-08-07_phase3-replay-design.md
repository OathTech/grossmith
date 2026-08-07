# Phase 3: replay (2026-08-07)

Audit Phase 3, done-when: "a report artifact reproduces byte-identical
source and observation without relying on an implicit seed-range
convention." Prerequisites were already banked (case.json carries seed,
resolved config, subject hash, and draw trace since Phases 1-2); this
arc delivers the decode path.

## The choice-source seam

`chooser` gains a `drawSource`: seeded (the PRNG, unchanged behavior) or
replay (a recorded trace). `gen.NewReplay(config, trace)` decodes; the
setup draws (swarm mix, corner) moved from `New` into `Generate`'s top —
same draw order, so pre-existing traces replay unchanged (the
reproducibility witnesses pin this) — giving Generate a single recovery
point for violations.

## Violation semantics — all fail closed, all typed

`*ReplayError{Pos, Bound, Value, Reason}` from Generate; never a silent
fallback to randomness, never a partial case:

- **exhausted**: the trace ran out at draw Pos while the generator
  requested [0,Bound). Config or generator-revision mismatch — or a
  shrinker candidate to reject.
- **out-of-range**: trace value Value at Pos does not fit [0,Bound).
- **surplus**: generation completed with unconsumed draws — the decode
  produced a program the trace does not describe, refused even though a
  source exists.

## The command

`gengo -replay <case-dir>`: decode case.json (schema-checked; config
read back typed), verify `subjectSha256`, run the gc reference on the
regenerated pair, and — when `batch.json` sits next to the case —
verify the observation document equal to the recorded reference
outcome. Failures name both the recording generator revision and the
current one. Witness: full round trip from artifacts on disk plus a
tampered-record refusal.

## Witnesses

- Round trip byte-identical (source, driver, re-recorded trace) over
  40 seeds x 5 configs (swarm on/off, boundary corner, capability
  profile, explicit construct set).
- Each violation class → its typed error with position.
- **The shrinking precondition**, measured: single-draw mutations either
  decode to type-checking programs or fail closed (200 valid / 70
  rejected over the sweep); no third outcome exists.

## Deliberately not built

The shrinker itself — the audit gates it on the first real divergence
that needs minimizing, and the mutation witness is the proof the seam is
ready. Trace-format versioning beyond generatorRev is also deferred: a
trace is only ever replayed against the revision recorded beside it.
