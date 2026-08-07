# grossmith

A generator of small, valid, **outcome-deterministic** Go programs, built
for differential conformance testing of Go reimplementations ("clones" —
interpreters, formal semantics, alternative backends) against the reference
`gc` toolchain.

**Current status (2026-08-07):** Phase 1 of the audit plan
(`docs/2026-08-06_project-charter-and-engineering-audit.md`) is complete:
a portable observation protocol (`observe`), a runtime-adapter harness with
a closed verdict taxonomy (`harness`), durable per-case and per-batch
artifacts, and a working first clone integration (GoLean, `golean`).
Planted-defect positive controls, replay, and the spec-surface ledger are
the next phases.

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
halts (time and memory), and produces byte-identical output on every run.

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
ladder. `docs/2026-08-06_observation-protocol-and-adapters.md` is the
Phase 1 protocol/adapter design; `docs/2026-08-06_prototype-salvage-notes.md`
records the Go trap catalogue and generator-survey conclusions.

## License

Apache-2.0 (see `LICENSE`, `NOTICE`). Parts of the generator core are
salvaged from the frozen `grossmith-proto` prototype (same authors, same
license).
