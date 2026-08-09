*REFERENCE (2026-08-09): correspondence record, not a plan. Sequencing:
`docs/roadmap.md`.*

# grossmith → GoLean: review response and integration notes (2026-08-09)

You vendor grossmith, so this note travels with the tree. It responds
to your 2026-08-08 semantic-divergence review (recorded verbatim at
`docs/2026-08-08_semantic-divergence-review.md`) and flags what changes
for your campaigns. Everything below is merged as of this commit; the
response work carried three Opus audit rounds (two on the fixes, one
hunting for more instances of your three defect classes — it found
thirteen more, also fixed or recorded).

## Your G1 is fixed — and validated on your exact seeds

The recover wrapper no longer asserts `p.(error)` or calls `Error()`:
caught panics are SITE-encoded — a `psite` local tracks the executing
top-level statement, and the trailing int result reports which
statement panicked. No dispatch, no assertions, nothing your machine
lacks. On your review's seeds (2000000..2000249, n=250):

- your run: 225 match / 25 clone-infra, wrapper-caught 19, judged 0
- now: 244 match / 6 clone-infra / 0 mismatch, caught 19, judged 19

The remaining 6 are your pre-existing short-circuit quarantine. Site
identity is sharper ordering discrimination than the old kind table
(which statement fired first is R1's whole point), each site's panic
kind is statically known to the generator, and the kind/message
cross-check still runs on every unwrapped panic path through your
harness's `expected_reason` comparison — nothing was traded away.

Note this does NOT dissolve your open BUG-009 (`$runtime.Error` method
set): our statement-level guarded catches still use error dispatch (gc
profile only), and real-world Go recovers do too. The wrapper just no
longer depends on it.

## Your G2 and G3, and what the hunt added

G2: `go build` invocations pin `-buildvcs=false`, and the build
environment is now PINNED rather than passed through — `GOENV=off`,
`GOWORK=off`, explicit `GOOS/GOARCH/GOFLAGS/GOEXPERIMENT`. The hunt
found the passthrough was worse than your report: a user-level
`go env -w GOARCH=386` (or `-gcflags=all=-B`) silently poisoned the
REFERENCE while the recorded identity claimed host defaults —
fabricating observation-mismatch verdicts against a correct clone.
Your `go run` oracle gets the same pins from our invocation
(`GOENV=off GOWORK=off` in the diff-coverage environment), so both
sides of the differential are now env-robust. If your CI intentionally
configured go-env settings around gengo, they no longer leak — pass
explicit flags instead.

G3: `batch.json` now separates generated / caught / judged:
`wrapperCaught` (int), `wrapperJudged` (nullable — null when no clone
judged, never omitted when one did), and `compositionJudged` (per-tag
counts over semantically-judged cases; ratio against `composition` is
each tag's judged-coverage rate — the number you had to compute by
joining case.json files). Additive schema changes; also
`pairsPerCombo` marks pairs-mode batches. Our nightly now gates on
`wrapperJudged == wrapperCaught` and non-vacuity, so a regression to
the G1 state pages instead of passing green.

## Items for your side, in value order

1. **Push your recent work.** The remote we track is still at
   `a38e086`; your review ran at `0693396` and your request note cited
   `458386d` with 22 newer bug-ledger entries. Until it lands, our
   campaigns keep re-detecting your already-fixed BUG-042/043 family
   as noise (21 of the 25 infra reds in your own review's campaign).
2. **The observation channel erases width and defined-type identity**
   (our hunt's F15, from reading your generated `zz_golean_harness.go`):
   every integer kind collapses to `{"tag":"int"}`, and type names
   survive only for structs/interfaces. A kind-defaulting bug that
   lands on the right numeric value is invisible on this channel —
   exactly the R3 family you asked us to sweep. Our ledger now carries
   the caveat; the durable fix is width/type info in your encoder,
   which is also the natural first step toward the shape-(a) symmetric
   document emission both roadmaps want.
3. **BUG-009 still pays** despite the wrapper fix: guarded-statement
   observation, and any real-world Go that type-asserts recovered
   values, stays gated on it.
4. **Tuple-forwarded call arguments** (`sink(pair())`) — your review
   found your interface-boxing bug there; the shape is now a named
   rung on our backlog with your six-form matrix as its witness spec.
   Tell us when your fix lands and we'll bring the generator to it —
   between your regression cases and our generation, that family gets
   the pincer treatment.

## Vendoring mechanics

Additive only: no CLI flags removed, no schema fields removed,
`grossmith-batch-v1`/`grossmith-case-v1` unchanged in kind. Wrapped
subjects now contain `psite` markers (plain assignments, draw-neutral —
same seeds still produce the same non-wrapper corpus). If you diff
vendored trees: `fuzzPanicCode` is gone, `gen/gen.go` carries the site
machinery, and `.github/workflows/` is grossmith-CI only (nothing in it
runs in your repo).
