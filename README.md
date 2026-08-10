# grossmith

[![ci](https://github.com/OathTech/grossmith/actions/workflows/ci.yml/badge.svg)](https://github.com/OathTech/grossmith/actions/workflows/ci.yml)

A generator of small, valid Go programs, built for differential
conformance testing of Go reimplementations ("clones" — interpreters,
formal semantics, alternative backends) against the reference `gc`
toolchain. Generated programs in the **STRICT lane** — today the entire
corpus — are outcome-deterministic by construction; other lanes
(designed, not yet emitted) carry explicit lane-specific oracles.

**Current status (2026-08-09):** all four phases of the 2026-08-06 audit
plan are complete and merged: a portable observation protocol
(`observe`), a runtime-adapter harness with a closed verdict taxonomy
(`harness`), durable per-case and per-batch artifacts, a working first
clone integration (GoLean, `golean`), measured observation sensitivity
(per-shape positive controls; planted and historical defect campaigns),
draw-trace replay (`gengo -replay` reproduces a case byte-identically
from its record plus the compatible generator revision), the
spec-surface ledger (`docs/spec-ledger.md`, live, with its honesty
gate), and the request-driven ladder (GoLean's R1-R5 plus R2b order
witnessing and tuple forwarding delivered). The witness arc
(`docs/2026-08-09_witness-arc-closing.md`) and the evidence arc
(`docs/2026-08-09_evidence-arc-charter.md`) are both complete and
merged: a campaign is an immutable, self-consistent experiment whose
descriptor, report, and clone tree are all digest-bound, and HALTS is
enforced at emission by the execution budget
(`gen/budget.go`). In progress: the containment arc, closing the
2026-08-10 audit's findings
(`docs/2026-08-10_comprehensive-technical-audit.md`).
`docs/roadmap.md` is the one living roadmap.

## What works today

```sh
go test ./...                                          # the witness suite
go run ./cmd/gengo -n 1000 -seed 1 -out out            # generate a batch
go run ./cmd/gengo -n 1000 -seed 1 -out out -judge     # + gc reference pass
go run ./cmd/gengo -n 300 -seed 9000 -out out2 -clone gc-386   # cross-arch clone
go run ./cmd/gengo -n 300 -seed 4242 -out out3 -clone golean   # GoLean campaign
```

`gengo` generates N self-contained programs — per case a `subject.go`
(import-free, no I/O of its own), a `driver.go` (the gc reference driver:
runs the subject, emits a `grossmith-observation-v2` JSON document), and a
`case.json` replay record (seed, generator revision, subject hash, draw
trace). Judging compares the reference against a clone per case and writes
`batch.json` — the conformance statement of record: both identities, the
panic-equivalence policy (`-panic-policy exact|kind`), and a verdict per
case from the closed taxonomy `match | observation-mismatch |
reference-infra-failure | clone-infra-failure | both-infra-failure |
harness-error`. Infrastructure failure is never conflated with semantic
divergence.

Clones:

- **`gc-386`** — the same toolchain at GOARCH=386, a degenerate clone that
  proves the harness discriminates: divergences must fall inside the
  declared `width_dependent` tag (reported as tag yield).
- **`golean[:checkout]`** — [GoLean](../golean) (default checkout
  `deps/golean`): cases are translated into GoLean's differential-coverage
  corpus format and judged by their own `scripts/diff-coverage` harness
  against `go run`; grossmith maps their result stages back onto the
  verdict taxonomy. The generator applies GoLean's capability profile
  automatically (slices/maps leave the observed tier, observation-event
  constructs are excluded). Frontend coverage gaps surface as
  `clone-infra-failure` with the stage preserved — visibly, never as a
  false match.

Generated programs cover: all integer kinds, bool, string, arrays, slices,
maps (no map-range except an order-invariant fold), named structs, defined
integer types, pure value-receiver methods, interfaces (derived and
`interface{}`), pure helper functions, `if`/`for`/`range`/`switch`,
`break`/`continue`, block-scoped declarations, multiple return sites,
interleaved observation points, `defer`, and statement-level
`recover` — with deliberate, budgeted, tagged panic paths. Three properties
hold **by construction** (never by filtering): every program compiles,
halts (time and memory), and — in the STRICT lane, today the whole
corpus — produces byte-identical output on every run; other lanes
(designed, not yet emitted — membership first) carry explicit
lane-specific oracles (`docs/2026-08-09_membership-lane-emission-design.md`).

## Packages

- `gen` — the generator: one weighted-choice primitive, construct swarm,
  capability profiles (`NoObserve` shapes, `Exclude` constructs).
- `observe` — the `grossmith-observation-v2` document: typed values with
  width-preserving `goType`, ordered events, closed panic-kind taxonomy,
  fail-closed parsing, policy-parameterized equality.
- `harness` — the product boundary: `Adapter` (name, pinned identity, run),
  the verdict taxonomy, batch running, durable artifacts.
- `golean` — the quarantined GoLean integration (nothing else imports it,
  it imports nothing GoLean-shaped into the rest).
- `cmd/gengo` — the CLI; validates every input before writing anything.

## Design

`BRIEF.md` is the founding design document: the charter, the
legality-vs-weights taxonomy, the observation model, and the growth
ladder. `docs/roadmap.md` is the living roadmap; `docs/spec-ledger.md`
is the spec-surface ledger (what is generated, quotiented, deferred and
why). `docs/2026-08-06_observation-protocol-and-adapters.md` is the
Phase 1 protocol/adapter design; `docs/2026-08-06_prototype-salvage-notes.md`
records the Go trap catalogue and generator-survey conclusions.

## License

Apache-2.0 (see `LICENSE`, `NOTICE`). Parts of the generator core are
salvaged from the frozen `grossmith-proto` prototype (same authors, same
license).
