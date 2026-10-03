# grossmith

[![ci](https://github.com/OathTech/grossmith/actions/workflows/ci.yml/badge.svg)](https://github.com/OathTech/grossmith/actions/workflows/ci.yml)

A generator of small, valid Go programs, built for differential
conformance testing of Go reimplementations ("clones" — interpreters,
formal semantics, alternative backends) against the reference `gc`
toolchain. Generated programs in the **STRICT lane** — today the entire
corpus — are outcome-deterministic by construction; other lanes
(designed, not yet emitted) carry explicit lane-specific oracles.

**Current status (2026-10-03):** all four phases of the 2026-08-06 audit
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
(`gen/budget.go`). The containment arc closed on August 10 at `e907c22`:
descriptor paths and output ownership are checked, adapter results fail closed,
and saved reports are checked for consistency. The current improvements add
lossless string bytes, observations at every returning exit, compiler
comparisons, and checks of saved or edited source, described below.
`docs/roadmap.md` is the one living roadmap.

## What works today

```sh
go test ./...                                          # the witness suite
go run ./cmd/gengo -n 1000 -seed 1 -out out            # generate a batch
go run ./cmd/gengo -n 1000 -seed 1 -out out -judge     # + gc reference pass
go run ./cmd/gengo -n 300 -out out-opt -clone gc -clone-gcflags='-N -l' # compare optimization
go run ./cmd/gengo -n 300 -out out-next -clone gc -clone-go /path/to/other/go # compare toolchains
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

- **`gc`** — a Go toolchain on the host architecture. `-clone-go` selects
  its executable (defaults to the reference's `-go`), and `-clone-gcflags`
  passes a `go build -gcflags` value to the clone only. For example,
  `-N -l` compares the reference's default optimization against a build
  with optimization and inlining disabled. Both toolchains must accept
  the generated module's Go 1.26 language version. Paths, binary digests,
  versions, architectures, and compiler flags are recorded in `batch.json`.
- **`gc-386`** — by default the same toolchain at GOARCH=386, a degenerate clone that
  proves the harness discriminates: divergences must fall inside the
  declared `width_dependent` tag (reported as tag yield). It also accepts
  `-clone-go` and `-clone-gcflags`; differences under custom toolchains or
  flags may include compiler defects beyond platform-width behavior.
- **`golean[:checkout]`** — [GoLean](../golean) (default checkout
  `deps/golean`): cases are translated into GoLean's differential-coverage
  corpus format and judged by their own `scripts/diff-coverage` harness
  against `go run`; grossmith maps their result stages back onto the
  verdict taxonomy. The generator applies GoLean's capability profile
  automatically (slices/maps leave the observed tier, observation-event
  constructs are excluded). Frontend coverage gaps surface as
  `clone-infra-failure` with the stage preserved — visibly, never as a
  false match.

Use `go run ./cmd/gengo -verify <batch-dir>` to check a saved campaign's
artifact integrity and report consistency offline. Direct gc comparisons
build the same manifested source files; GoLean additionally records its
translated work tree. Verification requires the per-case records and complete
comparison results, recomputes verdict and coverage totals, and checks each
wrapper counter against the individual verdicts. GoLean verdicts remain
attestations by its external adapter; its clone observations are not stored
for offline re-judging.

Use `-check` to rerun existing source without regenerating it:

```sh
go run ./cmd/gengo -check out3/case_00063 -clone golean -out checked-case
go run ./cmd/gengo -check reduced/subject.go -clone gc -clone-gcflags='-N -l' -out checked-source
go run ./cmd/gengo -verify checked-source
```

A case-directory input copies its `subject.go` and `driver.go` exactly. A
single-file input gets a current driver for its parameterless `fuzzSubject`
function, which must return at least one value in package `main`. Each check
requires an explicit empty or absent output directory with no previous batch
at `<out>.prev`, and runs the reference
even without `-judge`. Its record identifies the input and driver choice;
coverage tags and generation history are not inferred, and the recorded seed
is a zero placeholder. These records use `-check` again, rather than draw-tape
`-replay`. The usual clone, toolchain, and timeout options apply. Existing source
is used as supplied; generator capability profiles do not rewrite it.
For GoLean, directory mode requires the saved driver to be byte-identical to
the current observation driver: GoLean's nested comparison runs its own driver
over the subject alone. Edited or older drivers are refused. Selecting the
`subject.go` file explicitly opts into the current driver instead.

GoLean manifest rows request `depth=1024`: three seeded choice streams
supplement its fixed adversarial streams. GoLean checks that observations
agree and that the seeded streams cover every choice with multiple options;
exhausting that budget still fails the campaign. This is sampled invariance,
not exhaustive schedule enumeration. The depth is recorded in the
digest-bound `golean-work/manifest.tsv`.

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

String slicing can split UTF-8 sequences. The reference observation format
preserves the resulting bytes with a base64 `strBytes` payload, including
strings in containers and events; ordinary UTF-8 strings remain readable.
See [string observations](docs/observation-format.md). GoLean also supports
these cases through its byte-array string channel.

For adapters that cannot directly observe maps and slices, selected containers
contribute scalar fingerprints. These capture current state on normal returns,
early returns, and recovered panics. Fingerprints can collide; they offer less
sensitivity than the full typed observations used by the reference driver.
An unrecovered panic still reports panic evidence without a returned value tuple.

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
