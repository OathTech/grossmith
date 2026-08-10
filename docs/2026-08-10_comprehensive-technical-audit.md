# Grossmith technical audit (2026-08-10)

## Executive assessment

Grossmith has a thoughtful generator core and a much stronger test culture than
most early differential-testing tools. Masked weighted choice, explicit feature
tags, typed observations, replay traces, capability profiles, deliberate panic
generation, historical-defect campaigns, and the new emission-time execution
budget are all sound architectural choices. The stock suite and vet pass.

The project nevertheless still overstates what its artifacts prove. The
evidence arc repaired many findings from the 2026-08-09 audit, but its closure
did not bottom out the evidence boundary. This review found new high-severity
failures in path containment, output ownership, report validation, adapter
classification, and observation sensitivity. In particular:

1. a manifest can name a case outside the batch root; validation will accept it
   and judging will execute it;
2. an unrelated output directory containing a file merely named
   `manifest.json` or `manifest.tsv` can be accepted and deleted on publish;
3. `batch.json` can be byte-authentic yet internally false, and `-verify` will
   call it verified;
4. GoLean results are stored as conclusions without enough structured clone
   evidence to re-judge them, and free-text suffix matching decides whether a
   failure is semantic or infrastructural;
5. the aggregate observation used for GoLean is lossy and disappears on early
   return and recovered-panic paths; and
6. the execution budget excludes potentially dominant observation-tree and JSON
   costs.

The right next step is not another syntax rung. It is a short containment and
evidence-correctness arc, followed by measured observation-sensitivity work.
Until the P0/P1 findings below are closed, `batch.json` should be described as a
campaign report with integrity bindings, not an independently verifiable
conformance statement.

## Scope and method

The review covered every first-party Go package, the CLI, tests, README,
`BRIEF.md`, `TODO.md`, the living roadmap and spec ledger, all dated design and
audit documents, and the GoLean adapter boundary. Three independent first-wave
reviews examined the generator, harness/protocol/CLI, and plans/GoLean. A second
wave targeted the highest-risk findings with adversarial API and artifact probes.

Verification from commit `01fab3f` on `main`:

- `go test ./...`: pass;
- `go vet ./...`: pass;
- focused package tests for `gen`, `harness`, `observe`, `cmd/gengo`, and
  `golean`: pass;
- a direct `golean.Run` duplicate-ID probe returned `err=nil` and `match`;
- a direct `golean.Run` probe with an impossible exported observation document
  also returned `err=nil` and `match`;
- source-level proofs were completed for manifest path escape and destructive
  output replacement;
- `gofmt -l` still reports nine first-party files.

Passing tests therefore do not imply the findings below are hypothetical; the
two GoLean boundary failures were reproduced, while the filesystem failures
follow directly through the validation and publication paths.

Severity: **P0** can invalidate a verdict or delete unrelated data in normal API
use; **P1** materially compromises evidence, sensitivity, or reliability;
**P2** is coverage, portability, planning, or maintainability debt.

## Findings

### P0. Manifest-controlled paths are not contained within the batch

`caseDirRe` is declared but unused (`harness/manifest.go:35-36`).
`ReadManifest` validates digest strings but not root-file names, case IDs, or
case-file names (`harness/manifest.go:115-144`). `ValidateBatch` joins those
untrusted components directly (`harness/manifest.go:167-180,197-202`).
`RunBatch` later joins the same case ID and hands the directory to adapters.

Consequently, a case ID such as `../outside` can validate a sibling directory:
the root closure does not require a corresponding child because its `seen` key
is literally `../outside`. Judging then reads and compiles that outside case.
`RootFiles` can similarly name `../outside-file`. This breaks the defining E3
claim that the descriptor names a closed batch tree.

**Fix.** Before any filesystem access, require the case-ID regex, require every
name to be a single local basename (`filepath.IsLocal`, `Base(name)==name`, no
cleaning change), require `rootFiles` to be exactly `{go.mod}`, and define the
closed case-file schema. Prove every resolved relative path remains beneath the
opened root. Prefer directory-relative no-follow opens or immutable snapshots
over repeated path resolution. Add traversal cases for IDs, root files, and case
files to both validation and end-to-end judge/verify tests.

### P0. A magic filename can authorize deletion of an unrelated output tree

CLI validation says ownership is load-bearing, but accepts any nonempty `-out`
when either `manifest.json` or `manifest.tsv` merely exists
(`cmd/gengo/main.go:272-287`). Publication renames that directory to `.prev`,
publishes the staging tree, then removes `.prev` (`cmd/gengo/main.go:199-211`).
No schema, completion record, directory binding, or digest is checked at the
ownership decision.

A directory containing valuable files plus an unrelated empty `manifest.tsv`
therefore passes and is deleted after successful generation.

**Fix.** Only replace a directory that passes strict owned-batch validation and
whose completion record binds to that exact output path/run. Do not retain the
filename-only legacy escape hatch. If legacy batches must be upgraded, require
an explicit migration command that never deletes the source. Add regression
tests for both magic filenames and malformed/foreign manifests.

### P0/P1. Integrity verification does not validate the conformance statement

`VerifyBatch` checks descriptor and completion digests, but not the meaning of
the report. The CLI decodes `batch.json` with ordinary `json.Unmarshal` and then
checks only clone work digests (`cmd/gengo/main.go:318-348`). It does not validate
the batch schema, unknown or duplicate fields, exact manifest/report membership,
duplicate IDs, subject hashes, outcome enums and tagged unions, recorded verdict
versus `Judge`, totals, verdict histograms, wrapper identities, composition, seed
range, budgets, or identities.

Thus a producer defect can write a self-contradictory report, bind its bytes in
`complete.json`, and receive a successful “verified” message. Cryptographic
integrity is not semantic self-consistency.

**Fix.** Add strict `ReadBatchReport` and `ValidateBatchReport`, cross-join the
manifest and every `case.json`, validate each ran document before classification,
recompute every derivable verdict and aggregate, and reject absent/extra cases.
Keep “input integrity verified” and “report self-consistency verified” as
separate explicit claims. CI needs a faithfully digested but inconsistent-report
negative test.

### P1. GoLean reports cannot be independently re-judged

`golean.Result` contains only verdict, stage, and detail
(`golean/golean.go:99-105`). `runGoLean` copies the conclusion into the case
result but does not populate a clone `Outcome`. Work/source/result-file digests
show which adapter artifacts survived, not the actual structured semantic
observation from which the verdict was derived. The actual nested `go run`
copies under GoLean's artifacts are also covered only transitively by checkout
and script identity (`golean/workdigest.go:11-21`).

This means the durable report cannot recompute a GoLean verdict offline and
cannot show the actual value or panic-message difference behind a mismatch.

**Fix.** Version a structured GoLean result document containing expected and
actual status/value/panic, typed stage/reason, lane, and producer schema. Persist
it per case and make grossmith's comparator pure and replayable from the report.
Have the external script publish a closed digest manifest for the exact nested
sources and harness files it executed. Until then, call GoLean reports
attestations by a pinned external adapter, not self-contained evidence.

### P1. Free-text parsing controls the semantic/infra boundary

A GoLean `lean-observation` failure starts as `observation-mismatch` and is
downgraded to clone infrastructure only when its detail text literally ends in
one of two JSON suffixes (`golean/golean.go:520-539`). Whitespace, field order,
an added field, or encoder evolution can therefore turn the same structured
“unsupported” or “stuck” state into a semantic divergence. This contradicts the
project's typed-observation rule and the promise that infrastructure is never
conflated with semantics.

**Fix.** Require a versioned structured result field, decode it strictly, and
switch on a closed typed status. Unknown schemas/statuses are `harness-error`,
never mismatch. Pin protocol-shape mutation tests.

### P1. GoLean's exported boundary is not fail-closed

Two second-wave reproductions demonstrate this:

- duplicate input IDs first create a harness-error, but the translated row's
  later result overwrites it (`golean/golean.go:201-227,263-275`), returning
  `err=nil` and `match`;
- `translate` trusts an exported ran `Outcome.Document` by status without
  calling `Document.Validate` (`golean/golean.go:281-305`), so an empty-schema
  `status:"ok"` document carrying a panic payload also returns match.

The default CLI does not naturally construct these inputs, but `golean.Run` is
an exported product boundary and must keep its contract for programmatic users.

**Fix.** Prevalidate the complete case slice—safe unique IDs, valid ran
documents, features, source availability/capability—before any write. A duplicate
should fail the run, not become an overwritable per-case entry. Add the two
reproductions as API tests.

### P1. `Judge` validates too late and can misclassify invalid documents

For two ran outcomes, `Judge` checks `Document.Failed()` before calling
`observe.Equal`, where full validation occurs (`harness/harness.go:68-97`). An
invalid exported document with `status:"error"` can therefore enter the infra
branches without ever being structurally validated. Depending on the other
side, an adapter contract violation can be labeled reference-, clone-, or
both-infrastructure failure instead of `harness-error`.

**Fix.** Validate both ran documents first, with the side named in diagnostics.
Any invalid document is a harness error; only structurally valid error documents
participate in infrastructure classification. Add the full invalid/valid/error
side matrix.

### P1/P2. GoLean work publication and profile composition are unsafe APIs

`golean.Run` creates/reuses `workDir/cases` in place and overwrites case files
without first closing or clearing the tree (`golean/golean.go:192-234`). Stale
case directories are detected only later by optional digest verification, and
existing path entries can affect writes. In addition, `golean.Profile` replaces
the caller's `NoObserve` and `Exclude` slices instead of merging them
(`golean/golean.go:74-87`), silently discarding caller policy.

**Fix.** Prevalidate, write to a fresh exclusive staging directory with
no-follow checks, and atomically publish or return the staged evidence. Make the
profile a deterministic union/deduplication operation, preserving caller masks
and exclusions. Test nonempty/symlink work roots and preconfigured profiles.

### P1. Aggregate observation is lossy despite key-sensitive claims

Profile-masked maps and slices are folded to `int` at function exit
(`gen/gen.go:1477-1547`). For string map keys the key term is only `len(k)`; for
string values and string slice elements the value term is only `len(e)`
(`gen/gen.go:1497-1541`). The literal alphabet deliberately contains distinct
equal-length strings—`go`, `ab`, and `µ`; `fuzz`, `gros`, and `mith`
(`gen/expr.go:111-114`). Deleting/substituting such keys or values can therefore
produce identical observations. Integer polynomial folds can also collide after
wraparound.

The comment that the map fold is key-sensitive is true only of the reduced
length projection, not of Go values. This permits semantic false negatives in
the very GoLean channel meant to make aggregates observable.

**Fix.** Prefer explicit bounded entries/elements in a structured clone channel.
If scalar folding is unavoidable, encode finite-alphabet symbols injectively,
use enough independent lanes with a stated collision argument, and never call a
hash injective. Exhaustively enumerate the bounded alphabets and pin collision
tests before claiming sensitivity.

### P1. Aggregate-observed state vanishes on sharp control-flow paths

Aggregate folds run only at the normal function tail. Early return substitutes
zero for every aggregate result (`gen/stmt.go:139-150`), and the recover wrapper
cannot reconstruct map/slice state during panic before returning its aggregate
slots. A mutation before an early return or caught panic is therefore invisible
even though the case retains `aggregate_observed` and may be discussed as
partial-state/panic coverage.

**Fix.** Either compute path-valid aggregates at every return and recovery path,
or explicitly tag/demote these slots as `normal_exit_only` and ensure each sharp
mutation also feeds a directly observable scalar. Add path-sensitive mutation
witnesses for normal exit, early return, guarded recovery, wrapper recovery, and
unrecovered panic.

### P1. The HALTS budget excludes observation cost

`ExecBudget` bounds subject statement executions and derives a retained-memory
estimate from defer/append records (`gen/budget.go:3-39`). The driver then
recursively materializes reflection values as nested `map[string]any` trees and
marshals the complete tree to JSON (`gen/driver.go:87-202`). A large observed
slice can approach the execution budget in element count; the reflection tree,
JSON allocation, output buffer, parser, and retained document can dominate both
the subject's memory and runtime. Existing execution measurements use small
cases and do not establish a whole-case resource bound.

**Fix.** Introduce separate limits for observed elements, encoded bytes, parser
bytes, and end-to-end case RSS/time. Stream observations where practical, or
reserve observation cost during generation. Construct maximum-growth tapes and
measure driver peak RSS, output size, parse time, and both adapters before
retaining “time and memory by construction” without qualification.

### P1. Shared diagnostic buffers have data races

The same unsynchronized `*cappedBuffer` is assigned to both stdout and stderr
for gc builds (`harness/harness.go:443-445`) and GoLean invocation
(`golean/golean.go:414-416`). `os/exec` may copy the two pipes concurrently,
mutating the buffer, cap, and truncation flag from separate goroutines.

**Fix.** Make the buffer concurrency-safe (including snapshot reads) or use
separate capped streams and combine deterministically. Add a race test whose
child floods both descriptors. The normal suite passing does not exercise this
concurrency reliably.

### P1/P2. Accepted extreme configuration values undermine safe arithmetic

`Depth` and `LoopCap` have lower bounds but no upper bounds
(`gen/gen.go:78-106`). `tripCap = 8 * int64(LoopCap)` can overflow
(`gen/gen.go:486-499`), while budget arithmetic assumes nonnegative operands.
Extreme depth can also move generator recursion/formatting cost outside the
execution budget. “Extreme configs degrade to cheap arms” is therefore not true
for every accepted integer input.

**Fix.** Set documented conservative caps, compute all derived quantities with
checked/saturating conversions, reject values that do not fit, and assert every
cost/multiplier is nonnegative. Fuzz `Config.Validate` plus `Generate` across
integer boundaries with a no-panic/no-hang property.

### P2. Replay claims and behavior need a precise contract

README says a case replays “from its record alone,” while code correctly states
that replay is same-revision and requires the compatible generator decoder
(`cmd/gengo/main.go:735-748`). A value-only tape records neither choice-site nor
bound (`gen/choice.go`), so revisions that retain compatible draw counts can
decode differently; the final subject hash catches the difference, but the tape
does not explain it. On non-ran outcomes, two unrelated failures with the same
broad `OutcomeStatus` return success (`cmd/gengo/main.go:822-829`). Successful
replay also reads adjacent `batch.json` without first verifying its bindings.

**Fix.** Say “record plus the compatible generator revision.” Record
site/bound/value tuples and a decoder schema. Treat the stored source/driver
digests as artifact identity and tape decoding as regeneration. For non-ran
cases, persist and compare a structured phase/failure fingerprint or return
nonzero with “source reproduced; observation unavailable.” Verify the batch and
report before comparing recorded outcomes.

### P2. Default panic policy does not match semantic-model conformance

The docs acknowledge that gc panic prose is implementation detail and expose a
kind-only policy, but the CLI defaults to exact message equality. GoLean hard
pins exact messages externally. A semantically conforming model with different
prose can therefore produce a headline mismatch.

**Fix.** Default cross-implementation semantic campaigns to kind equality;
reserve exact for runtime-fidelity or same-toolchain campaigns. Record the
campaign goal and rationale. Until GoLean exports a policy-aware structured
panic result, label its panic conclusions as exact-message fidelity.

### P2. Complex-feature incidence is not demonstrated at campaign-useful rates

Independent half-probability swarm gates multiply across prerequisites.
Constructs such as three-index slice aliasing, string folds, and type switches
need several tags and suitable generated state. Many tests establish only that
a feature appears at least once over a sweep. The ledger's “supported” status
therefore does not imply useful incidence or mutation-detection probability.

**Fix.** Add dependency-aware bundles/named populations and persist generated,
reached, observed, and semantically judged rates per feature and feature pair.
Set minimum sensitivity targets from historical-fix campaigns rather than mere
tag presence.

### P2. The order witness is a collision-prone platform-width hash

`wOrd = wOrd*31 + tag` stores an event sequence in one `int`. It wraps, tags are
not constrained as base-31 digits, and millions of calls are theoretically
allowed. Distinct evaluation sequences can therefore collide; marking the case
`width_dependent` stratifies the weakness but does not restore sensitivity.

**Fix.** Use explicit bounded event sequences on capable channels. For narrow
channels, cap witness length and use multiple specified-width lanes with a
quantified collision bound. Property-test reachable distinct sequences.

### P2. Compilation and portability claims exceed their witnesses

The principal generated-program compile test runs `go/types` over 300 seeds,
which does not exercise cmd/compile lowering, code generation, linking, or both
architectures. Full build/run coverage is narrower. The harness also executes
binaries from `TMPDIR`, which may be mounted `noexec`, and non-Unix process
cancellation kills only the direct child (`harness/proc_other.go`). Atomic
publish uses rename without fsync, so it is process-interruption-safe but not
power-loss durable.

**Fix.** Add randomized real build/run matrices for minimum/current Go,
amd64/386, and a second OS; configure/preflight executable scratch; use Windows
job objects if Windows is supported; fsync files and parent directories or scope
durability claims explicitly. Reword the `go/types` test as a frontend/type
checker sample.

### P2. Protocol strictness has remaining edges

JSON strict decoders still accept duplicate object keys. Non-scalar map key
validation rejects only adjacent equal canonical encodings and does not enforce
a universal order (`observe/observe.go:426-456`). Current generated map keys are
narrower, but the exported observation protocol presents a broader `Value` type.
The GoLean adapter also detects forbidden `obs*` calls with a regex, so comments
or string literals can false-positive, and validates feature tags only after
writing a case file.

**Fix.** Reject duplicate JSON keys, define or restrict canonical map-key kinds,
use Go AST call inspection, and complete all metadata validation before writes.

## Planning-document audit

The planning lifecycle is currently broken at exactly the point the roadmap
says it is enforced. `main` is at the evidence-arc completion commit, but
README still says the evidence arc is in progress (`README.md:22-25`), and the
“one living roadmap” still calls it in progress and awaiting merge
(`docs/roadmap.md:37-47`). The evidence charter/status retain ACTIVE banners.
This contradicts the roadmap's same-commit rule (`docs/roadmap.md:3-6`).

`TODO.md` also duplicates delivered work as open: its opening says R1-R5 are
delivered and only R6 remains, then later lists R3, R4, and R5 again. It says the
evidence arc still needs merge/push even though it is on `main`. Some historical
audit text is valuable, but it is mixed into the live inventory without stable
IDs or supersession markers. The nine-file gofmt item remains accurate.

### Proposed revisions to the living plan

1. **Immediate R0 — stop-the-line containment and lifecycle correction.**
   Close/historical-banner the evidence documents; update README and roadmap to
   the actual merged state; remove stale duplicate TODO entries. Fix manifest
   containment and output ownership before running `gengo` on a reusable output
   directory. Add the capped-buffer race fix.

2. **R1 — report and adapter evidence correctness.** Add strict report
   validation/cross-joins; validate ran documents before any classification;
   prevalidate GoLean inputs; replace free-text result classification with a
   versioned structured protocol; persist enough clone evidence to re-judge;
   directly bind nested executed files. Exit only when a deliberately
   inconsistent but correctly digested campaign is refused.

3. **R2 — observation sensitivity and whole-case budgets.** Replace lossy
   aggregate folds or quantify them honestly; make sharp exits observable;
   account for driver/JSON/parser resources; collision-test order witnesses.
   Re-run historical defect pairs and planted mutations stratified by exit path
   and observation channel.

4. **R3 — durable evidence corpus and operational matrix** (the old roadmap
   R4). Check in compact fix-pair transitions and fake-adapter contracts; add
   minimum/current Go, amd64/386, second OS, noexec/temp and power-loss scope;
   publish last-green identities and retain red cases.

5. **R4 — machine-readable ledger and measured pair accounting** (old R5).
   Give issues stable IDs/statuses, generate README/TODO summaries from one
   structured ledger, and track generated/reached/observed/judged sensitivity.

6. **R5 — language/version contract.** Before more version-sensitive grammar,
   persist target language/module mode and make GoLean's nested GOPATH/default
   language behavior agree with the reference campaign or be an explicit
   quotient.

7. **Then resume membership and language growth.** Keep membership blocked on
   GoLean's stable reason-code contract. After it lands, resume recovered-event,
   pointer/effect, embedding, and float rungs in measured clone-value order.

The current roadmap's R4-R7 content is mostly good; it should be renumbered
behind these newly demonstrated correctness prerequisites, not discarded.

## Recommended third-wave investigations

These should become executable campaign assets rather than another prose-only
audit:

- generate traversal/ownership artifact fixtures and run `-verify`, `-judge`,
  interrupted publish, and recovery paths against them;
- build a report mutator that preserves completion digests while changing one
  semantic invariant at a time;
- enumerate all bounded aggregate states over the current literal alphabets and
  measure collision classes, then plant mutations in each class;
- audit every mutation emitter against normal, early-return, guarded-recovery,
  wrapper-recovery, and panic exits;
- synthesize maximum-growth replay tapes and measure subject statements, peak
  RSS, observation bytes, parse allocations, build time, and clone time;
- race-test every subprocess with concurrent stdout/stderr floods and stubborn
  descendants on every supported OS;
- replay archived records across generator revisions and minimum/current Go,
  distinguishing artifact identity from decoder compatibility;
- execute historical GoLean fix pairs under both panic policies and a pinned
  language/module mode, measuring cases-to-first-detection and false-negative
  populations.

## Bottom line

Grossmith is not failing because it lacks ambitious ideas. It is failing at a
more actionable boundary: several strong claims are one validation layer ahead
of their implementation. The generator can already produce valuable tests, but
the system must first guarantee that it executes the declared tree, preserves
unrelated data, validates the report it authenticates, records enough clone
evidence to re-judge, and observes the state it claims to observe. Closing those
items will make subsequent language coverage trustworthy rather than merely
larger.
