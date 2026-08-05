# grossmith — a Go program generator for differential conformance testing

**The charter:**

1. Generate lots of small Go programs.
2. Make every program outcome-deterministic, so it can be differentially tested.
3. Cover as much of the language as possible, and as many edge cases as possible.
4. Build iteratively: an MVP that works, then grow it.
5. Keep the number of non-compiling programs low.

**The one process rule** (the antibody, inherited from the prototype's
`deps/grossmith-proto/LESSONS.md` §6, which records why it exists):

> No gate, meter, audit layer, or process rule may be added without a named
> incident that demanded it. Machinery petitions for existence; product does
> not.

We are building software, not bureaucracy. Tests are the gate; work happens on
a branch; the maintainer reviews before merge. That is the entire process.

## What this tool is for: clone conformance

The use pattern is validating a *reimplementation* of Go (an interpreter, a
formal semantics, an alternative backend) against the reference
implementation. That shapes everything:

- **The oracle is asymmetric.** A pinned `gc` toolchain is truth by
  definition; the clone must match. Any divergence is a clone bug, a declared
  quotient, or our generator bug — no voting, no trusted-reference problem.
  The runtime abstraction stays symmetric (two implementations in, verdict
  out) so the roles can flip later.
- **The conformance equivalence is byte equality of observations.** Programs
  are outcome-deterministic by construction, so no fuzzy matching is ever
  needed. Only two declared quotients exist: platform width (pin `GOARCH` or
  declare the dependency) and panic identity (policy knob: match panic *kind*
  or full `gc` message text — the prose is implementation detail a legitimate
  clone may not reproduce).
- **"Cover the interesting behaviors" is a measured claim.** The Go spec
  surface is enumerated as tags; every program carries the tags it exercised;
  the batch report shows the histogram. The uncovered remainder *is* the
  roadmap.
- **The conformance statement is the product**: reference version, `GOARCH`,
  equivalence policy, N programs, compile/run rate, coverage histogram.

## Design: csmith's skeleton, minus what Go obsoletes, plus the free wins

Baseline is the csmith architecture — it works (survey:
`deps/grossmith-proto/docs/2026-08-03_generator-survey-and-design.md`, which
reads csmith, YARPGen, gosmith, and rustlantis against their actual trees).
Go deletes most of csmith's bulk: no UB means no safe-math wrappers, no
points-to analysis, almost no effect system. What replaces it is the **Go
trap catalogue** (`LESSONS.md` §3) — compile-time constant-overflow
rejection, unused-anything-is-fatal, constant contexts, duplicate case
labels — every entry paid for with a prototype debugging session.

Departures from csmith, each clearly dominating:

1. **Termination by construction**, not timeouts. Literal loop bounds, +1
   steps, index never assigned in the body; later: `range` over fixed-size
   data, acyclic call graphs, recursion fuel. A timeout is not a comparable
   outcome.
2. **One choice primitive: weights × legality mask → renormalize → one
   draw.** Never rejection loops, never silent fallbacks (csmith and gosmith
   both retry-sample and cannot state their realized distribution; Xsmith —
   csmith's authors' own generalization — switched to mask-and-draw). An
   emptied choice space is a reportable error. Minting is an arm: no variable
   of type T ⇒ "declare one" is a weighted choice, so type demands never
   fail. Every type carries a cheap total literal, so fuel exhaustion is safe
   anywhere.
3. **Injective observation over a drawn liveness mix.** The subject function
   returns its *observed* variables, each with its own type; the driver
   prints the full tuple. No hashing, no collapsing — two divergences cannot
   cancel, and triage reads values directly. Which variables are observed is
   itself a weighted, recorded draw (see "Liveness" below). (Csmith's CRC-32
   exists because printing C state is painful; Go has no such excuse.)
4. **Panic paths are test content.** Division by zero, bounds, nil deref are
   *defined, deterministic* outcomes in Go — a class csmith structurally
   cannot generate. Panic/no-panic is decided on purpose at each site,
   tagged, and the driver recovers and prints the panic as an ordinary
   observation. Message fidelity is a real clone defect class.
5. **Small programs.** Divergence value comes from construct composition,
   not length; minimization is nearly free. (Csmith's multi-KB outputs
   mandate external C-Reduce plus dedup clustering.)
6. **Everything the generator knows survives as data.** Feature tags, panic
   paths, dead regions, width dependence — recorded at emission, never
   thrown away for a consumer to re-infer (csmith computes width-dependence
   and emits it as a *comment*).

## Legality vs. weights: what is banned vs. what is rare

Exactly three properties are enforced by construction (the legality mask):

1. **Compiles** — the trap catalogue (charter 5).
2. **Halts** — non-termination is legal Go but not differentially testable
   (charter 2). Includes resource bounds: string/data growth linear by
   construction.
3. **Outcome-deterministic** — no unordered iteration reaching output, side
   effects in statement position until an effect discipline exists, panics
   deliberate (charter 2; gosmith admitted nondeterminism on day one and was
   crash-only forever — this cannot be retrofitted).

Everything else legal is *generated*, controlled by weight and recorded by
tag: dead code after a terminal statement, unreachable switch arms,
degenerate outputs. These are legitimate clone tests (an implementation that
lets dead code affect live results is buggy); it is only bad if they
dominate. The prototype's hard bans on them protected a metric, not the
corpus — measurements stratify by tag instead. Batch reports show the
composition so dominance is a visible number and a turnable weight, never a
gate.

## Liveness: observation is a drawn tier, not a constant

Returning *every* variable would keep every value live — an optimizing
implementation's dead-value elimination would be structurally unexercisable
(rustlantis deliberately dumps only half its live locals for this reason).
So each variable draws an observation tier, recorded as a tag:

- **observed** — in the returned tuple; live to the end. The discriminating
  core; heavily weighted by default, with a floor of at least one observed
  variable per program (a program observing nothing tests nothing).
- **feeder** — read by the computation but not returned; a miscomputation of
  it is visible through what it feeds.
- **dead** — discharged with `_ = v` (satisfies Go's unused-variable rule,
  eliminable by an optimizer). Its miscomputation is invisible by
  definition; its value is testing that deadness is *handled correctly* —
  elimination must not corrupt live results or change panic behavior, which
  is a clone-bug class an all-observed corpus can never trigger.

A dead-rich mix joins the named-corner list for optimizing consumers; for a
pure semantics clone all-live is harmless — which is why this is a knob, not
a constant. Future levers on the same axis: multiple return sites
(path-dependent liveness) and interleaved `println` observation points (pin
intermediate state, create mid-function liveness ranges, localize *where*
implementations diverge).

## The choice tape

All randomness flows through the choice primitive, and the primitive records
its draws. The generator is then a decoder: choice sequence → valid program.
This buys, in order of adoption:

1. **Reproducibility**: seed ⇒ byte-identical program (ordered containers
   everywhere; no map-range dependence).
2. **Shrinking by regeneration** (when the first real finding needs
   minimizing): reduce the *choice sequence* and re-decode — every candidate
   is valid/halting/deterministic by construction. No C-Reduce, nothing goes
   stale. (Literature: "Test-Case Reduction via Test-Case Generation" — the
   Hypothesis reducer; Xsmith builds on Clotho, the same idea as a library.)
3. **Coverage-guided search over tapes** (if blind generation ever plateaus):
   mutate bytes, decode to valid programs — libFuzzer-style feedback without
   sacrificing any by-construction guarantee.

Only (1) ships in the MVP; (2) and (3) are named seams, not scheduled work.

## Population design: swarm mixes and named corners

- **Per-seed swarm mixes**: each seed enables a random subset of constructs;
  diversity comes from varying WHICH features a small program draws on, not
  from cramming (validated by csmith's drivers).
- **Named corners**: edge cases are hunted, not hoped for (YARPGen, 9.14×
  measured). Each corner — boundary literals (MIN/MAX/bit patterns ±k),
  division/modulo signs, conversion truncation, dead-code-rich, … — is a
  narrowed sub-config drawn per seed. The trap catalogue doubles as the
  corner list: whatever trips a naive generator is a divergence hypothesis
  about a clone.
- **Composition coverage**: clone bugs cluster at feature interactions
  (defer+recover+returns, break-in-switch-in-loop). The batch report tracks
  pairwise tag co-occurrence; mixes can bias toward under-covered pairs.
- **Choice-frequency report** (from reading Xsmith): per hole, how often
  each construct was legal vs chosen — so the weights demonstrably mean what
  they say.

## Program shape

Self-contained, `go run`-able, no imports:

```go
package main

func fuzzSubject() (int8, uint32, bool, string) {
        // declarations (one per type floor, so every demand is satisfiable)
        // statements (the computation under test)
        return v0, v1, v2, v3 // the observed tuple (drawn subset; the rest feed or are discharged)
}

func main() {
        // recovers any panic, prints "panic: <kind/message>"
        // otherwise prints each result on its own line
}
```

Any Go implementation that can run a program and capture output can be
diffed. No harness coupling, no imports to support, deterministic output
stream.

## Growth ladder (value order, from LESSONS.md §8)

MVP (slice 0): salvaged scalar core — all int kinds + bool; arithmetic /
bitwise / shifts / conversions / comparisons; if / for / switch /
break / continue; panic paths (division, modulo). Then:

1. **Strings** (observable, no structural prerequisites; growth bounded
   linear by construction).
2. **Boundary literals** — the first named corner (charter 3's edge cases).
3. **Arrays** — plus `range` loops over fixed-size data (termination free).
4. **Structs**.
5. **Block-tree emitter + real scopes** — the priced rewrite (gosmith's
   pending-block-tree with retroactive declaration; Xsmith's ~200-line
   scope-graph resolution is the reference design), prerequisite for
   helpers, closures, defer.
6. Onward per the trap catalogue: slices, maps (determinism via
   construction or declared quotient — decided then, knowledge never
   punted), defer/recover, methods, interfaces. Observation levers ride
   along when wanted: multiple return sites, interleaved observation
   points.

Each rung: emission code gated on new tags + witness test + the conformance
rate watched (expect a dip, fix by construction).

## Repo layout

```
BRIEF.md        this file
gen/            the generator (salvaged core: deps/grossmith-proto/internal/gen)
conform/        build + run + compare against the pinned reference
cmd/gengo/      CLI: gengo -n N -seed S -out DIR [-conformance]
deps/           read-only reference checkouts (gitignored): grossmith-proto, xsmith
```

MVP acceptance, mechanically checkable: `go test ./...` green (witness
tests: reproducibility, typecheck-in-process over hundreds of seeds,
loops-bounded, every-declaration-observed); `gengo -n 1000 -conformance`
reports ≥99% compile+run on the pinned toolchain with the composition
histogram; a deliberately-broken "clone" (e.g. the same toolchain at a
different `GOARCH`, or a mutated binary) shows divergences — proof the
observation discriminates.

## Sources

- `deps/grossmith-proto/LESSONS.md` — trap catalogue (§3), salvage map (§7),
  process post-mortem (§6). The prototype held 100% compile/run and zero
  nondeterminism over ~877k programs; its generator core is this MVP's seed.
- `deps/grossmith-proto/docs/2026-08-03_generator-survey-and-design.md` —
  the four-generator survey, adversarially reviewed.
- `deps/xsmith` — the "generalized csmith" (BSD, Racket; read for
  architecture): mask-and-draw choice, scope graphs, Clotho tape,
  choice-frequency logging. Its canned layer generates conservative subsets
  (no general loops, wrapped division); the gap it leaves — general loops
  with termination, deliberate panics, wide coverage — is this project.
- Hypothesis reducer paper (choice-sequence reduction); Zest / "fuzzing as
  parsing a stream of choices" (tape mutation), for the named seams above.
