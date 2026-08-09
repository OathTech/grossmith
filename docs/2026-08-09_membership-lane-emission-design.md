*ACTIVE, BLOCKED-ON (2026-08-09): implementation (roadmap R6) is blocked on a
stable machine-readable GoLean reason-code contract for their membership
stages — their `diff-coverage` today emits one `membership` stage with
free-text detail for five distinct failure kinds, which cannot map to the
verdict taxonomy in §3. Sequencing: `docs/roadmap.md`.*

# Membership-lane emission — design note (witness arc W5, 2026-08-09)

PURPOSE. grossmith generates outcome-deterministic Go programs for
differential conformance testing. This note designs the FIRST sanctioned
relaxation of that determinism: emitting a tagged minority of programs
whose one order-sensitive observable is judged by GoLean's MEMBERSHIP
oracle (every reference-observed behavior lies inside the machine's
enumerated set) instead of byte equality. Design only — implementation
is the next arc; this rung ends at this note.

Doctrine of record: the effect-discipline design §4 (the lanes model,
user direction 2026-08-07 — lanes beside the strict invariant, never a
loosened invariant), and GoLean's
`docs/2026-08-04_membership-lane-design.md` +
`docs/2026-08-04_nondeterminism-doctrine.md` (their lane gates and
epistemic captions), which this note treats as the consumer contract.

## 1. What becomes observable

Exactly one observable class in v1: **raw map iteration order**, through
a POSITION-WEIGHTED fold over a map range —

```go
mo := 0
for k, e := range m {
    mo = mo*31 + (kTerm*31 + eTerm)
}
```

— the fold shape the strict lane deliberately bans (our map observation
is commutative BY DESIGN; ledger row "Maps", quotient "iteration
order"). The accumulator observes WHICH order gc realized this run; gc
randomizes map iteration, so the program is genuinely
choice-dependent — the strict lane cannot hold it, and the membership
lane is built for it.

The append-capacity envelope (the BUG-021 class) is the second
candidate the lanes model names; it stays OUT of v1 — one observable
class keeps the first lane campaign attributable.

## 2. Emission mechanics (all by construction)

- **A per-seed lane draw**, minority, through the chooser — the corner
  precedent. A membership seed arms the position-weighted map fold as
  a statement form; everything else generates as usual.
- **Lane honesty, text-iff-use**: the case is DECLARED
  `lane=membership` iff the order-sensitive fold was actually emitted
  (tag `membership_fold`, ledger row in the same commit). A membership
  seed whose draws never emitted the fold demotes to strict — their
  gate fails singleton/deterministic membership cases loudly, and
  demotion-by-honesty is the correct response, mirrored on our side by
  batch.json carrying the realized lane per case.
- **Guaranteed multiplicity** (their singleton-rejection gate): the
  emitter writes TWO known keys into the map immediately before the
  fold, unconditionally — so len >= 2 at range time whatever earlier
  deletes did. CORRECTED 2026-08-09 (evidence arc E0; the 2026-08-09
  comprehensive audit caught it): this note originally argued the two
  orders differ "because a*31 + b == b*31 + a iff a == b", which is
  FALSE in modular arithmetic — the two folds differ by 30*(a-b), and
  30*(a-b) ≡ 0 mod 2^w whenever a-b ≡ 0 mod 2^(w-1), reachable at
  w=32 with in-range values. The repaired argument: both fold terms
  come from literals the emitter chooses, so it controls their
  difference d outright; it emits terms with 0 < |d| < 2^31, and then
  30*d is nonzero mod 2^32 AND mod 2^64 (30 = 2*15 with 15 odd, so
  30*d ≡ 0 mod 2^w iff d ≡ 0 mod 2^(w-1)) — the two orders provably
  differ at both widths. That bounded-difference argument (2
  guaranteed entries, term difference in (0, 2^31) => >= 2 enumerable
  observations at either width) goes verbatim into the manifest's
  `why` column.
- **Width metadata, explicit** (their audit F2a: no silent defaults):
  the map-range consumption site's bound is the map's length at range
  time. The generator emits maps from bounded key alphabets and
  bounded literal inserts, so a static upper bound L on len(m) is
  computable at emission; the manifest row declares `width = L` and the
  `why` states the bound argument. Their alias-guard ladder
  cross-checks it; a "raise width" refutation means OUR static bound
  was wrong — a generator bug, judged harness-error, never semantic.
- **One order-sensitive observable per case**: a second raw-order fold
  would multiply enumerated sets and blur attribution — same rule as
  one hot panic site per statement. The fold is the FINAL observation
  only in v1: no composition with the recover wrapper (panic-path ×
  enumeration interplay deferred with the arity note), no interaction
  with the order corner needed (wOrd is deterministic and orthogonal).

## 3. The judge mapping (golean.judge's deliberate gap, closed)

Today `translate` pins `lane=strict` and the membership stages are
known-unreachable, deliberately unmapped. Implementation order:

- translate emits `lane`/`why`/`width` from the case record; strict
  cases unchanged (10-field manifest, their `scripts/diff-coverage`
  validation is the contract).
- PASS rows with stage `membership` → match (the membership PASS: every
  gc sample, plain and -race, inside the enumerated set).
- Their sample-outside-set failure → observation-mismatch (the lane's
  semantic signal).
- Enumeration infrastructure failures (cap exceeded, enumerator error)
  → clone-infra-failure.
- Width refuted by the alias guard → harness-error (our declared bound
  was wrong; fail closed, never a semantic claim).
- Any other new stage keeps the existing unknown-stage rule:
  harness-error with the stage preserved.

Exact stage strings are read off their `diff-coverage` at
implementation time (the suffix-match discipline from the refusal
detection applies; their vocabulary may grow between now and then).

## 4. What does NOT change

COMPILES and HALTS are untouched (the fold is a bounded range). The
strict lane remains the default and the bulk of the corpus; the
membership draw is a tagged minority. Verdict taxonomy is unchanged —
the lane reuses match / observation-mismatch / clone-infra-failure /
harness-error; "lane" is a per-case declaration in the record, never a
global mode. Confluent and racy lanes, concurrency, and the capacity
envelope stay out. Replay is unaffected: the lane draw and the fold's
draws ride the same tape as everything else.

## 5. Open questions for the implementing arc

1. Whether the observed accumulator should also flow into a trailing
   int slot (aggregate-style) for the gc-only profile, so gc-vs-gc-386
   cross-arch runs can at least CARRY the value (they compare equal
   only per-run — cross-arch comparison of a choice-dependent value is
   meaningless; probably observe-and-ignore with a tag).
2. Whether the membership seed should suppress the boundary/kinds/order
   corners for attribution cleanliness, or let them compose (lean:
   compose — the fold's enumerated set does not depend on them).
3. Campaign accounting: membership matches should report separately in
   batch.json (`verdictsByLane`) so the strict lane's zero-mismatch
   claim stays crisp.
