# Phase 4 scope: the ledger and the ladder (2026-08-07)

The audit's Phase 4: "resume capability scaling from a spec ledger" —
create the surface ledger, then prioritize capability slices by clone
relevance and composition value. Since the audit wrote that, clone
relevance stopped being hypothetical: GoLean filed six prioritized
requests grounded in their 43-entry bug ledger
(`docs/grossmith-requests-2026-08-07.md`). This arc runs long on one
branch; rungs land as individual commits with witnesses, and an n=300
GoLean campaign closes each rung (verdict drift examined — their
frontend-export gaps will GROW as the grammar widens; that is signal,
not failure).

## Rungs, in order

**Rung 0 — the spec-surface ledger** (the audit's precondition).
`docs/spec-ledger.md`: Go spec areas × status, reusing GoLean's status
vocabulary (`supported | partial | deferred(reason) | out-of-scope`) so
the two ledgers can be joined for campaign planning. Every `supported`
row names its emission tags and witness; every deferral names its
reason (determinism quotient, effect discipline, protocol). Seeded from
three sources: the grammar as built, GoLean's coverage ledger, and
their 98-entry semantic-edges catalogue (the mining backlog item rolls
in here — each edge classified generated-today / corner-candidate /
rung-blocked / quotiented-by-design / out-of-scope). An honesty gate
test pins ledger↔code: every optional construct tag appears in the
ledger. Rolled in: the size watch (audit M4, explicitly gated on ladder
resumption) — a witness asserting the subject-size distribution stays
inside a stated budget, so type-floor growth cannot be silent.

**Rung 1 — R1: the recover-observation wrapper** (their highest yield).
A canonical pure-Go idiom: named result + deferred recover encoding the
panic into an int by table. No obs* events, so it survives the golean
profile and DISSOLVES the recover exclusion (profile update included).
Expected side effect: recovered-path runtime coverage rises from ~1.5%
to a measured, witnessed level. Absorbs: the recovered-coverage rung,
the g06/c54 named-results-defer rung, the Phase 2 defer/recover gap.

**Rung 2 — R2a: multi-target assignment.** Tuple assignment with
aliased targets (`i, a[i] = ...`), nested chains, comma-ok into mixed
targets, multi-result helper calls into mixed targets. Spec-defined
assignment phases make the whole family deterministic and observable
via final state — no effect discipline needed. GoLean's most bug-dense
corner (8+ ledger entries).

**Rung 3 — R3: the kind/definedness matrix corner.** A named corner
sweeping {op site: arith, compound, incdec, index, shift} × {int kinds}
× {defined vs unnamed}, honoring their conversion-laundering constraint
(in-kind paths, no `int(x)` masking). The BUG-042/043 family generator.

**Rung 4 — R5: aggregate observation of maps/slices.** Order-independent
encodings — sums, min/max folds, len, membership bits — as observed-tier
forms, replacing the golean profile's NoObserve masking with
aggregation. Map/slice machinery reaches their machine in observed
positions for the first time, zero harness change on either side.

**Rung 5 — R4: pairwise swarm coverage.** A campaign mode generating n
cases per construct PAIR (forced-include mixes over the existing
Constructs override), with the batch report gaining the composition
histogram (backlog item rolls in — the charter lists it as part of the
conformance statement, and pair coverage needs it as measurement).

## Explicitly out of this arc

- **R2b order-witnessing** — requires deliberately reintroducing
  expression-position side effects; that is the audit's Phase 4 #3
  effect-discipline design (closures/pointer params), a design doc and
  its own arc. High yield, not a rung.
- **R6 embedding/promotion matrices** — blocked on embedding and
  pointer-receiver rungs (dependency order #3-4).
- Floats, membership-lane emission, the shrinker, concurrency —
  unchanged dispositions.

## Cadence

One rung per commit (or few), witness-gated; campaign after each rung;
pre-merge audit ask at arc end as always. If a rung's campaign surfaces
a real divergence, minimizing it outranks the next rung (that is the
shrinker's trigger condition arriving).
