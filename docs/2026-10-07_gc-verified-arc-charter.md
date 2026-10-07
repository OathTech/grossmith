*ACTIVE (2026-10-07): charter for the gc-verified arc; ordering lives in
`docs/roadmap.md`.*

# The gc-verified arc: charter (2026-10-07)

PURPOSE. grossmith is a Go program generator used for differential
conformance testing. This charter plans the capability work requested by
its second customer, gc-verified (`~/projects/gc-verified`). gc-verified
proves, per compilation, that the amd64 code gc emits for a Go function
refines that function's meaning in GoLean. It grades its pipeline with
differential lanes (GoCore, gc `-N -l` and `-O`, the Kraken x86 model,
gccgo, TinyGo).

The request is `gc-verified/docs/ai/2026-10-08_grossmith-feature-request.md`
(their commit b24fa7c). Contact: the `gc-verified-e8` session.

## Decisions and their provenance

- [USER] 2026-10-07: this arc goes AHEAD of the roadmap's deferred queue
  (R4-R7 there) and the proposed sensitivity arc. Reason: a direct
  customer request.
- [AGENT] assessment 2026-10-07, agreed by gc-verified the same day:
  - R3 functions that can panic are generated OUTSIDE pure mode.
  - Generic named construct allow-lists live in grossmith. gc-verified
    owns its fragment definition files.
  - R1 starts with read-only pointers and non-escaping `&local`.
  - R4 tags mean "shape emitted"; gc-verified measures whether the
    optimisation fired.
  - R5 starts with a protocol note.
- [AGENT] correction delivered to gc-verified 2026-10-07: helpers are
  panic-free on every input by construction. So the E4 helper evidence
  (10,277 inputs) covers no panic paths. gc-verified confirmed it by
  measurement (0 panicking executions from grossmith) and narrowed its
  claims.

## Ground rules (unchanged)

- The by-construction invariants are non-negotiable: every program
  compiles; every program halts within `ExecBudget`; strict-lane
  outcomes are deterministic. The effect discipline's E1-E4 rules
  (`docs/2026-08-07_effect-discipline-design.md` §2) govern every
  effectful construct.
- Every emitter gates AND marks exactly what it emits. A ledger row and
  a witness land in the same commit as the construct.
- **Seed stability.** Every new capability is opt-in. With it off,
  generated sources and draw traces stay byte-identical to the previous
  generator, and a witness test proves it. (The `string_bytes` change
  re-mapped every seed; that cost is not repeated.)
- Fail closed. A new artifact is digest-bound and checked by `-verify`,
  or it is not evidence.
- The house wording convention (`docs/roadmap.md`) applies to code,
  docs, and delegated briefs.
- All work goes on branch `gc-verified-arc`. Each rung can merge
  separately, but only on Mike's sign-off for that merge, after a
  review. Pushing needs its own sign-off.

## Rungs, in order

The order is gc-verified's suggestion, adopted: R3 unlocks the evidence
they already consume; R2 makes it attributable; R1 is their largest gap.

### G1 — function-level emission with input batteries (their R3)

Decided here:

- **Opt-in.** `Config.Functions` (default 0) is the number of battery
  functions `tf0..tfN-1` per case. With 0, nothing changes (seed
  stability witness).
- **Generated after the subject.** Their draws come after every subject
  draw. The subject's source and its tape prefix are therefore
  identical with and without functions. A witness test checks this, so
  enabling functions never changes the main program.
- **Signature.** The first parameter is plain `int` (the non-constant
  base, as for helpers). Up to three more are drawn from integer kinds
  and `bool`. Results are one or two integer kinds or `bool`. No strings
  in v1: their lengths interact with the execution budget and need their
  own pricing first.
- **Body.** Statement generation as for the subject, but parameterised
  and NOT in pure mode. Panic-capable arms are allowed, one hot site per
  statement, under the existing risk rules. Excluded inside function
  bodies: observation points (`obs*`), package variables, the recover
  wrapper, order witnessing. The body may call helpers when `helpers` is
  enabled (their F2 gate is the profile's job, rung G2).
- **Battery.** For each function, K inputs (default 32). First the edge
  values per parameter type (0, 1, −1, min, max, max−1, powers of two
  ±1, in combination), then seeded random values. All drawn through the
  chooser, so the battery is on the tape and replays.
- **Budget.** Each function's per-call cost is priced at generation, as
  for helpers. The whole battery (K × cost, plus driver overhead) is
  charged against the case's `ExecBudget`. Generation refuses a breach.
- **Expected outcomes.** A separate battery driver calls every
  (function, input) under `recover`. The gc reference runs it. The
  result is `functions.json`:
  - per input, the results, or the panic kind and exact message;
  - in the observation-v2 value vocabulary;
  - written under the batch's staging and publish rules;
  - digest-bound in `complete.json` and checked by `-verify`.
  Its exact field shape is agreed with gc-verified before G1 merges.
- **Out of scope for G1.** Judging batteries under clone adapters
  (GoLean, `-clone gc`). The sidecar is reference-only. Clone judging is
  a recorded follow-up.

### G2 — named construct profiles (their R2)

`-profile <file>` loads a named allow-list of constructs (plus corner
and knob constraints). The profile name and the file's sha256 are
recorded in `case.json`, `manifest.json`, and `batch.json`. Strictness
is the existing mechanism: emitting a disabled construct is a generator
panic, and a witness covers profiles.

gc-verified owns its fragment files (F0 … S1). grossmith ships test
fixtures only. Where a fragment boundary is finer than our tags (for
example, "slices over fixed arrays" against our literal-born slices),
the tag is split in G2 or the gap is recorded in the ledger. Their
`srcclass` classifier cross-checks that cases land in the intended
fragment, and they report the result back.

### G3 — pointers, stage 0 (their R1, M-mem-0)

First, the mechanism-2 design note that the effect discipline requires
before pointers enter, amending
`docs/2026-08-07_effect-discipline-design.md`. Then:

- `&x` / `&s.f` / `&a[i]` of non-escaping locals;
- `*p` reads, and writes in statement position;
- read-only `*T` helper and battery-function parameters;
- comparisons with `nil`.

Observations never include addresses; only pointees and pointer
equality are observed. Equality between distinct zero-size variables is
kept out of the strict lane.

Pointer writes through helpers, escaping pointers (`new(T)`, returned
pointers), and nil dereference each need their own rung after the note.
Returning a pointer from `fuzzSubject` needs an observation-format
change coordinated with GoLean.

### G4 — optimisation-stressing shapes (their R4)

A named corner (or corners), kept a minority. It emits `bce_loop` (with
near misses), `cmov`, `magic_div`, `mixed_bitwise`, `byte_combine`,
`strength_reduce`, and `loop_rotate` shapes. A tag records that the
shape was emitted, never that gc optimised it.

### G5 — unrecovered panics and `println` (their R5)

A protocol note first. The observation is new: process-level exit status
plus the fd-2 byte stream. Only the first line of a panic is compared;
the rest carries addresses and goroutine traces. GoLean's support is
checked before any emission.

### G6 — size and shape knobs (their R6)

Target straight-line region length and branch density, within the
execution budget. `case.json` records the measured shape.

## Exit conditions

The arc closes when G1-G3 are merged and gc-verified has consumed a
G1+G2 batch end to end. G4-G6 may be re-chartered if the customer's
priorities move. Stop and ask Mike if any rung would need:

- a by-construction invariant relaxed;
- a seed-mapping change for existing configurations;
- an observation-format change visible to GoLean.
