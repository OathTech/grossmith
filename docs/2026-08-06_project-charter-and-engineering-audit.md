# Project charter and engineering audit

**Date:** 2026-08-06  
**Audited revision:** `3c23de7` (`main`, also `origin/main`)  
**Scope:** the tracked grossmith 2.0 tree, its local prototype/reference checkouts under `deps/`, generated batch artifacts, and the current test/conformance path.

## Executive judgment

grossmith has a promising generator core, unusually good comments, and a strong habit of turning concrete generator failures into construction rules. It is not yet the clone-conformance product described by its charter.

The generator is the strongest part. It emits a substantial deterministic subset of Go, its static witnesses are broad, and a fresh 300-case batch compiled and ran 300/300 on the local Go 1.26.5/amd64 reference. The implementation has already moved well beyond the scalar MVP: strings, arrays, structs, scopes, slices, maps including an order-invariant range fold, defer/recover, helper functions, defined integer types, methods, and interfaces are present. The tests are fast and deep: `gen` reached 98.1% statement coverage in this audit.

The product boundary is the weakest part. `conform.Runtime` is not a runtime abstraction: it hard-codes `go build` and direct execution and varies only `GOARCH`. Consequently grossmith cannot currently run the clone it says it exists to test. The supposedly pinned reference is merely whichever local `go` appears on `PATH`. The declared panic-equivalence knob does not exist. The observation protocol depends on Go's implementation-specific `print`/`println` builtins, which a conforming clone need not reproduce. The interface observation is lossy despite the brief's claim of an injective full tuple. The recorded choice tape cannot be replayed. Coverage tags do not enumerate the Go specification, so the uncovered remainder cannot yet be the roadmap.

These are not polish issues. They prevent the current system from making its advertised conformance statement. The next work should therefore pause grammar expansion and deliver one honest reference-versus-clone vertical slice with a specified observation protocol, executable runtime adapters, explicit equivalence policy, and a persistent case/run artifact. Once that works, the existing generator foundation is good enough to resume the language ladder.

## 1. Ground state

### Repository and maturity

The tracked project is small and focused: one charter/design document (`BRIEF.md`), a 3,398-line generator implementation plus 1,468 lines of generator tests, a 236-line conformance package, a 264-line CLI, and one conformance test. There is no checked-in README, license/notice, CI definition, release/version mechanism, fixture corpus, or prior audit in `docs/`. `deps/` is ignored and therefore the sources repeatedly cited by `BRIEF.md` are available in this checkout but not part of a clone of the repository.

The project history shows rapid capability growth, with recent commits explicitly remediating earlier reviews. The comments record several real incidents and their fixes: stale case directories, mutable configuration aliases, repeated generator use, unspecified multi-panic order, weak panic observation, helper effects, and a range-plus-append resource bomb. This is good use of the project's “named incident” process rule.

The ground-state feature set is:

- Integer kinds, booleans, strings, arrays, named structs, slices, maps, defined integer types, and named interfaces.
- Arithmetic, bitwise operations, shifts, conversions, comparisons, `min`/`max`, string concatenation and length, indexing, append, map updates/delete/comma-ok/range fold, fields, assertions, and method/interface dispatch.
- `if`, bounded `for`, array/slice `range`, `switch`, break/continue, nested declarations with projection, early returns, intermediate observations, defer/recover, pure acyclic helpers, and pure value-receiver methods.
- One named corner (`boundary`) and per-seed swarm gating.
- Per-case source, tag/count metadata, a recorded draw list, and per-choice-site legal/chosen counts in memory; the CLI persists source and a reduced manifest, but not the tape or choice statistics.

This is materially ahead of `BRIEF.md`'s growth ladder, which still lists methods, defined types, and interfaces as remaining (`BRIEF.md:231-234`). The plan is already stale at the point where future prioritization should begin.

### Verification performed for this audit

With `GOCACHE` and `GOTMPDIR` redirected to writable temporary storage:

- `go test ./...`: pass.
- `go vet ./...`: pass.
- `go test -race ./...`: pass.
- Coverage: `gen` 98.1%, `conform` 77.1%, `cmd/gengo` 0.0%.
- Fresh batch, seeds 260806..261105: 300/300 built and ran under `go version go1.26.5 linux/amd64`; zero timeouts; 49 terminal panic observations and 3 caught/recovered observations.

The fresh cross-architecture run did **not** prove discrimination. All 386 binaries were killed with exit status 159 in this execution environment, so `Diff` labeled 300/300 cases divergent and the CLI mislabeled 13 as untagged width divergences. This environmental limitation is not a generator defect. It exposed a harness defect: runtime failure is mixed with semantic divergence, the alternate runtime's compile/run rate is not printed, and the CLI can print quotient/tag conclusions from a completely non-runnable comparator (`conform/conform.go:193-211`, `cmd/gengo/main.go:148-190`). Earlier ignored logs report 135/1,000 genuine host/386 differences with zero outside the width tag, but ignored logs are not durable project evidence.

## 2. What is engineered well

### The core construction strategy is sound

The legality-mask/weighted-choice primitive is simple and consistently used (`gen/choice.go:24-79`). Type equality is structural where required and name-based where Go identity requires it. Constant contexts, safe/nonconstant arithmetic, termination bounds, map key alphabets, slice ownership, bounded data growth, switch-label uniqueness, and unused-variable discharge are treated as generation invariants rather than post-generation filters. This is exactly the right direction for charter items 2 and 5.

The generator validates configuration before generation, bounds worst-case executed statements, copies caller-owned configuration, is explicitly single-use, snapshots returned metadata, and formats generated code before returning it. Those choices make failure local and diagnosable.

The map-range fold is a good example of quotienting nondeterminism at the generated-program level: the loop body is restricted to a commutative accumulation (`gen/stmt.go:528-547`). The range/append restriction is also an excellent incident-driven fix: it addresses resource termination, not merely syntactic loop termination.

### Tests are unusually substantive for the project's age

The generator tests do more than snapshots. They parse and type-check hundreds of seeds, target 386 representability, walk ASTs for bounded-loop and panic-budget invariants, check construction gating, witness each major language family, and inspect purity/acyclicity. The end-to-end conformance test builds and executes generated programs twice. Test density and generator coverage are strengths.

### Empirical tuning is visible

The current batch report provides tag counts, choice legality versus selection, and top tag pairs. Recent comments cite measured starvation or masking rates and adjust weights in response. The fresh 300-case run reached every optional tag except no separate `modulo-sign`, `dead-rich`, or `conversion-truncation` corners exist yet; all currently implemented optional families appeared. This is a useful population-control foundation.

## 3. Critical findings

### C1. There is no clone runtime adapter, so the stated product cannot be used

`BRIEF.md` says the runtime layer is symmetric and accepts two implementations (`BRIEF.md:27-31`) and that any implementation able to run a program can be diffed (`BRIEF.md:200-202`). In code, `Runtime` contains only `Name` and `GOARCH`; `Check` always invokes the host `go build` and then directly executes the resulting binary (`conform/conform.go:35-69`). The CLI exposes only `-cross-arch`.

This supports a narrow gc cross-architecture experiment, not an interpreter, formal semantics, alternative backend, gccgo, TinyGo, another Go version, or the prototype's GoLean consumer. It also conflates build and execution into one gc-specific operation.

**Recommendation:** make the next deliverable a two-adapter vertical slice. Define a small adapter contract that consumes a case directory and returns a typed status plus observation bytes: reference gc adapter and one real clone adapter (the already proven prototype GoLean adapter is the lowest-risk candidate if it remains the intended consumer). Keep adapter-specific build/run commands behind that seam. Do not add a general plugin framework yet; two concrete adapters are enough to prove the abstraction.

### C2. The observation protocol is not a portable Go conformance protocol

Generated drivers use builtin `println` everywhere (`gen/gen.go:856-925`, `gen/stmt.go:429-479`). The Go specification reserves `print` and `println` for bootstrapping/debugging and leaves their behavior implementation-specific. Byte-equal `println` output therefore cannot be demanded from a conforming reimplementation. The brief partly notices this for defined types (`gen/gen.go:918-922`) but not for the protocol as a whole.

This directly undermines the charter's differential-conformance goal. Import-free convenience is not worth making the observation itself implementation-specific.

**Recommendation:** specify a versioned observation encoding and generate it using ordinary, specified language operations supported by the target fragment. The cleanest separation is a subject file plus an adapter/driver file: consumers that cannot support a standard-library serializer can provide their own driver while preserving the same typed observation schema. Until this is delivered, describe current output honestly as a gc-compatible debug channel, not a Go-conformance oracle.

### C3. “Pinned gc” and panic policy are descriptions, not implementations

The brief calls gc pinned (`BRIEF.md:27`, `BRIEF.md:42-43`), but `buildEnv` only sets `GOTOOLCHAIN=local`; the executable is whatever `go` resolves from inherited `PATH`. `GoVersion()` records the result after the fact. Recording is useful, but it is not pinning.

The brief also promises a panic-policy knob choosing kind versus full gc prose (`BRIEF.md:32-37`). There is no such knob. `Diff` performs unconditional byte equality, while generated recovery prints the full `error.Error()` string. Non-error panic payloads collapse to the single word `panic`. Thus current behavior is neither a documented full-payload equivalence nor an implemented panic-kind quotient.

**Recommendation:** accept an explicit reference adapter/toolchain path and persist its version/identity. Add exactly the two already-declared equivalence modes, with parsing/normalization performed on a structured observation rather than substring matching. Treat adapter failure, timeout, panic kind, panic payload, and normal result as distinct outcomes.

### C4. Interface observation violates the claimed injectivity

The brief claims that the returned full tuple is observed injectively and independent divergences cannot cancel (`BRIEF.md:70-75`). For an observed interface, the driver prints only comma-ok probes for possible dynamic types (`gen/gen.go:907-916`). It never prints the contained defined integer's value. Two executions holding `T0(1)` and `T0(2)` are indistinguishable if both have dynamic type `T0`.

The prototype plan explicitly required interfaces to carry a named dynamic type in a typed value observation. The current implementation preserves type identity but discards payload identity.

**Recommendation:** observe both a stable dynamic-type discriminator and the asserted underlying value for the successful arm. Add a direct positive-control test that constructs equal dynamic types with unequal payloads and requires unequal observations. Amend “injective” to name the precise supported domain and prove injectivity per shape.

### C5. Runtime failure is reported as semantic divergence

`Diff` intentionally treats any failure as a divergence, which is reasonable for fail-closed accounting, but the CLI then counts every returned record as a cross-architecture semantic difference and may print “discrimination proof” or “tag honesty violation.” In this audit, an environment unable to execute 386 binaries produced 300/300 supposed differences and 13 false tag violations.

`Diff` also assumes equal array positions are the same case and checks only result count, not case identity (`conform/conform.go:193-211`). Reports over different same-sized directory sets can therefore be compared as if aligned.

**Recommendation:** use a closed verdict taxonomy: reference failure, SUT failure, timeout, observation mismatch, match, and harness error. Require both runtime reports to be fully runnable before computing discrimination yield or tag honesty. Match cases by stable case ID/hash and reject missing, duplicate, or mismatched identities.

## 4. High-priority findings

### H1. The choice “tape” is a log, not a decoder input

The brief says the generator is a decoder from a choice sequence and that a program can be regenerated from the tape (`BRIEF.md:144-159`; `gen/gen.go:124-126`). The chooser only draws from `math/rand` and appends results; there is no replay source, tape exhaustion policy, or handling for a taped integer that is outside a later choice bound (`gen/choice.go:29-43`). Even the CLI discards the tape.

Seed reproducibility is real and tested. Tape replay, shrinking-by-regeneration, and tape mutation are not yet seams in an executable sense.

**Recommendation:** correct the documentation now: call it a draw trace. Before the first reducer/search work, introduce a choice source with seeded and replay implementations, define consumption/exhaustion semantics, and persist the trace plus generator version/config. Do not build a shrinker before replay is proven byte-identical.

### H2. The conformance statement is ephemeral and incomplete

The charter calls the conformance statement the product (`BRIEF.md:42-43`). The CLI prints it to stdout but persists only `manifest.tsv`, whose rows contain ID, seed, and feature counts. It omits generator revision, full config/swarm/corner resolution, source hash, tape, toolchain identity, GOOS/GOARCH, adapter identity, equivalence policy, per-runtime status/output, timeout, and aggregate report. Existing valuable 1,000/1,500/4,000-case evidence survives only in ignored `.tmp` logs.

**Recommendation:** persist one versioned batch report and one versioned per-case metadata record next to generated cases. This machinery is justified by a named incident from this audit: stored logs and the fresh run gave incompatible cross-arch conclusions that cannot be reconciled from the current manifest alone.

### H3. Coverage cannot yet support “as much of the language as possible”

The brief claims the Go spec surface is enumerated as tags and the uncovered remainder is the roadmap (`BRIEF.md:38-41`). `Optional()` is an emission-gate list, not a spec inventory. Core and informational tags share the same histogram; tags are ad hoc strings; there is no mapping to Go spec sections, no explicit unsupported/deferred status, and no report of zero-count items. Pair reporting is limited to the top 15 and only shown with `-stats`, so it cannot identify missing interactions systematically.

This matters because current generation remains a small subset: no floats/complex, aliases, pointers, channels/select, goroutines, closures with captured state, variadics, multiple assignment semantics beyond selected sites, type switches, generics, embedding/promotion, packages/imports/init, recursion, goto/labels/fallthrough, or broad builtins. Some must be deferred or quotiented, but absence should be explicit.

**Recommendation:** create a lightweight language-surface ledger derived from the Go spec, with `supported`, `partial`, `deferred(reason)`, and `out-of-scope` states and links to emission tags/tests. This is product roadmap data demanded by charter item 3, not a pass/fail gate. Use it to select slices by clone value and prerequisites.

### H4. The project is not independently consumable or legally complete

The repository has no README, LICENSE, or NOTICE, although source comments say code was salvaged from an Apache-2.0 prototype. The only provenance explanation and cited surveys live in ignored `deps/`. A clean clone lacks the evidence needed to understand those claims, and there is no basic build/use/support statement.

**Recommendation:** add the project's actual license and required notice/provenance, a short README with status and one honest supported workflow, and copy the essential prototype lessons/survey conclusions into tracked docs or link to stable published revisions. Do not vendor large reference trees merely to make citations work.

### H5. The CLI is an untested state-mutating boundary

`cmd/gengo` has 0% test coverage. It writes into an existing directory, overwrites live case sources, creates a module only if absent, and removes stale `case_*` directories. It does not validate `n > 0`, timeout positivity, output-path type, or whether an existing directory belongs to grossmith. Negative `n` leads to nonsensical seed-range reporting and then no-case errors only if conformance is requested. Reuse can preserve stale non-case artifacts and a stale incompatible `go.mod`.

**Recommendation:** validate CLI inputs before writes and make batch creation atomic into a new run directory or require an explicit overwrite/resume policy. Add focused tests for zero/negative counts, stale batches, failed generation, comparator failure, and report persistence. These tests are justified by the already-recorded stale-directory incident and the comparator-failure incident found here.

## 5. Medium-priority correctness and design concerns

1. **Outcome classification uses substring heuristics.** `PanicPath` and `Recovered` are inferred with `strings.Contains` (`conform/conform.go:79-84`). This is fragile as the string alphabet or observation protocol grows. Structured observations should carry event types.

2. **The comment and implementation disagree on map range.** `gen/types.go:39-42` says map range is never generated and the fold is future work; it is already implemented. This kind of stale invariant comment is dangerous in generator code.

3. **A conformance timeout still exists despite termination by construction.** A timeout is appropriate as a harness backstop, but the report must classify it as a generator/resource or runtime failure, never a comparable observation. The current `Conformant` definition folds all successful exits together and gives no phase taxonomy.

4. **“Small” is not measured in the batch report.** The one-per-type floor grows with every new type, and default statements were already raised because too much observed state remained initializer echoes. Add descriptive size/output/runtime distributions to the persisted report only when needed for tuning; do not gate them. Otherwise continued type expansion will silently make every program large.

5. **Capability tests emphasize presence more than semantic sensitivity.** Cross-arch is a useful positive control for width, but it says little about maps, interfaces, defer/recover, method dispatch, or optimizer-sensitive deadness. The prototype's mutation/historical-bug lesson remains relevant. After a real clone adapter exists, add a small set of construct-specific planted defects or historical clone bugs and measure detection. This should follow—not precede—the vertical slice.

6. **Reference-version compatibility is implicit.** Generated code uses Go 1.21+ builtins and writes `go 1.26`; that is acceptable if Go 1.26 is the declared target, but the target must be a config/report fact rather than whatever the local module happens to say.

## 6. Assessment against the charter

| Charter goal | Current assessment | Evidence and gap |
|---|---|---|
| Lots of small programs | **Promising, partially demonstrated** | Thousands of cases generate quickly and fresh 300/300 execution succeeded. “Small” lacks reported distributions and the growing type floor works against it. |
| Every outcome deterministic | **Strong construction work, not fully established as cross-implementation conformance** | Map order, evaluation risks, resource growth, and panic order receive careful treatment. `println` and panic prose are implementation-specific; only repeat-on-gc is tested. |
| Broad language and edge-case coverage | **Early but credible foundation** | Many important families are implemented, but only one named corner exists and there is no spec-surface ledger. The claim that the surface is enumerated is false today. |
| Iterative MVP then growth | **Generator succeeded; product MVP not complete** | Grammar growth outran the runtime/oracle/artifact vertical slice. Methods/interfaces landed before a clone can be invoked. |
| Low non-compiling rate | **Met on the reference evidence examined** | Tests pass; fresh run was 300/300; ignored larger logs report 100%. This is the project's clearest success. |

## 7. Recommended forward plan

The order below is deliberately product-first and each piece is tied to a concrete finding above. It does not reintroduce the prototype's process bureaucracy.

### Phase 0 — make the current description honest

- Update the growth ladder to mark defined types, methods, interfaces, and map range fold delivered.
- Rename the current tape to a draw trace in prose/API comments until replay exists.
- State that the current harness supports local gc/GOARCH experiments only, that reference identity is recorded rather than pinned, and that panic comparison is full byte equality.
- Correct stale map-range comments and distinguish present capabilities from planned ones.

**Done when:** a reader can derive the same ground state from `BRIEF.md` as from the code.

### Phase 1 — deliver the actual product MVP

- Split the generated subject from the observation driver or otherwise version the observation boundary.
- Implement explicit reference and clone adapters; reclaim the prototype GoLean adapter if GoLean remains the first customer.
- Define structured outcomes and the two promised panic policies.
- Fail closed when either adapter cannot build/run, without calling that an observation mismatch.
- Persist the complete conformance statement and per-case identity/config/result metadata.

**Done when:** one command runs a checked-in seed range through gc and one real clone, produces a durable report, and a deliberately altered clone result is classified as an observation mismatch while an unavailable clone is classified as infrastructure failure.

### Phase 2 — prove observation sensitivity

- Fix interface payload observation and add direct injectivity positive controls per supported value shape.
- Add a few historical or planted defects across distinct families: width/conversion, control flow, map semantics, defer/recover, and interface dispatch.
- Report cases-to-first-detection, not merely a green compile rate.

**Done when:** every supported observation shape has a targeted unequal-state witness and the end-to-end production path detects the selected defects.

### Phase 3 — make reproduction real

- Persist generator revision, config, case hash, and draw trace.
- Add replay-mode choice consumption with defined exhaustion/out-of-range behavior.
- Only then implement tape shrinking, triggered by the first real divergence as the brief intends.

**Done when:** a report artifact reproduces byte-identical source and observation without relying on an implicit seed-range convention.

### Phase 4 — resume capability scaling from a spec ledger

Create the surface ledger, then prioritize capability slices by clone relevance and composition value. A sound dependency order is:

1. Expand named corners over the existing grammar: signed division/modulo, conversion truncation, dead-rich, shift boundaries, nil-interface/assertion behavior, slice append/reallocation-insensitive cases.
2. Recursion with explicit fuel and broader function signatures/variadics.
3. Pointer parameters, package variables, and closures together with the effect discipline their side effects require.
4. Type switches, embedding/promotion, aliases, and broader method/interface rules.
5. Floats/complex with an explicit equivalence policy before emission.
6. Goroutines/channels/select only with a deliberate deterministic schedule construction or declared observational quotient; never admit them as ordinary byte-equality cases.
7. Generics after the type and instantiation model has a clear validity-by-construction design.

Each slice should keep the existing lightweight rule: emission gate, targeted semantic witness, and observed compile/run rate. Add no new global gate unless a named failure demands it.

## 8. Release recommendation

Do not present the current revision as a usable Go clone-conformance fuzzer. Present it as an advanced deterministic Go program generator with a gc-only batch harness.

The project should call its first product milestone complete only after Phase 1: a real clone can be invoked, observation is portable and versioned, equivalence is explicit, runtime failure is not confused with divergence, and the conformance statement is a durable artifact. That is a focused correction, not a rewrite. The generator core is worth preserving and extending once the product path can consume what it generates.

The strict conclusion is therefore: **healthy generator foundation, incomplete and currently overstated conformance product**. Fixing the boundary now gives grossmith a credible path to scale; continuing grammar expansion first would compound metadata, observation, and adapter debt across every new Go feature.
