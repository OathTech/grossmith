# grossmith

A generator of small, valid, **outcome-deterministic** Go programs, built
for differential conformance testing of Go reimplementations ("clones" —
interpreters, formal semantics, alternative backends) against the reference
`gc` toolchain.

**Current status (2026-08-06):** an advanced deterministic Go program
generator with a gc-only batch harness. It is **not yet** the
clone-conformance product its design describes — the runtime-adapter seam,
portable observation protocol, explicit equivalence policies, and durable
conformance artifacts are the current work (see
`docs/2026-08-06_project-charter-and-engineering-audit.md`, whose phase
plan governs). GoLean is the first intended clone consumer.

## What works today

```sh
go test ./...                 # the witness suite (~46 structural/semantic witnesses)
go run ./cmd/gengo -n 1000 -seed 1 -out out -conformance
go run ./cmd/gengo -n 300 -seed 9000 -out out2 -conformance -cross-arch 386
```

`gengo` generates N self-contained, import-free, `go run`-able programs
(one directory each, plus `manifest.tsv` with per-program feature counts),
builds and runs them against the local `go` toolchain, and reports the
conformance rate, composition histogram, and (with `-stats`) the realized
choice distribution. `-cross-arch 386` additionally builds at GOARCH=386
and byte-compares observations — a width-divergence experiment, not a
clone comparison.

Generated programs cover: all integer kinds, bool, string, arrays, slices,
maps (no map-range except an order-invariant fold), named structs, defined
integer types, pure value-receiver methods, interfaces (derived and
`interface{}`), pure helper functions, `if`/`for`/`range`/`switch`,
`break`/`continue`, block-scoped declarations, multiple return sites,
interleaved observation points, `defer`, and statement-level
`recover` — with deliberate, budgeted, tagged panic paths. Three properties
hold **by construction** (never by filtering): every program compiles,
halts (time and memory), and produces byte-identical output on every run.

## Design

`BRIEF.md` is the founding design document: the charter, the
legality-vs-weights taxonomy, the observation model, and the growth
ladder. `docs/2026-08-06_prototype-salvage-notes.md` records the Go trap
catalogue and generator-survey conclusions this design is built on.

## License

Apache-2.0 (see `LICENSE`, `NOTICE`). Parts of the generator core are
salvaged from the frozen `grossmith-proto` prototype (same authors, same
license).
