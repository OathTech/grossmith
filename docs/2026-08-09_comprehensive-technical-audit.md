*REFERENCE (2026-08-09): external project-level audit; its replicated
findings are being addressed by the evidence arc
(`docs/2026-08-09_evidence-arc-charter.md`) with R4-R7 deferred on
`docs/roadmap.md`, which owns sequencing.*

# Grossmith comprehensive technical audit (2026-08-09)

## Executive assessment

Grossmith has a serious and unusually well-tested generator core. Its strongest
ideas are good ones: masked weighted choice, a replay tape, explicit observation
documents, a closed verdict vocabulary, capability profiles, and deliberate
measurement against historical GoLean defects. The stock witness suite, vet,
short race suite, and a fresh 500-case reference campaign all pass.

The project nevertheless does **not yet justify several of its strongest product
claims**. The most important gaps are not missing Go syntax. They are trust-
boundary and guarantee gaps:

1. the GoLean verdict can be computed against a different, unrecorded `go`
   executable from the one named as the reference;
2. the bytes hashed in `batch.json` need not be the complete program that was
   built, and reused output directories can retain unrecorded `.go` files;
3. malformed observation documents can parse and be judged as semantic matches;
4. the public generator configuration has panic cases, contrary to its API
   contract;
5. the resource-bound argument misses cross-slice range/append amplification;
6. build time and process output are unbounded by the advertised per-case
   timeout/resource story; and
7. the live plan and public status text substantially lag the implementation.

These are fixable without replacing the generator. The constructive priority is
to make a campaign an immutable, self-consistent, reproducible experiment before
adding another language rung. Until the P0/P1 items below are closed, Grossmith
should describe itself as a promising differential-testing system with a strong
generator and an incomplete evidence boundary, not as a conformance statement
whose artifacts alone are authoritative.

## Scope and method

This review covered `gen`, `observe`, `harness`, `golean`, `cmd/gengo`, the
checked-in GoLean dependency, tests and workflows, README/BRIEF/TODO, the spec
ledger, and all dated design/audit documents. Independent first-pass reviews
examined the generator, adapter/protocol boundary, and tests/plans. A second wave
then targeted by-construction guarantees, malformed protocol inputs, nested
Go-toolchain provenance, membership-lane prerequisites, and artifact integrity.

Verification performed from this tree:

- `go test ./...` and `go vet ./...`: pass with a writable temporary `GOCACHE`;
- `go test -short -race ./...`: pass (the full generator package is slow under
  race, but completed during this review);
- 500 freshly generated and reference-judged cases, seeds 120000..120499: all
  500 ran; 99 unrecovered panic paths, 3 recovered events, 27 wrapper catches;
  source size min/mean/max 946/2091/4019 bytes;
- a 10,000-configuration generator matrix: no unexpected panics outside the
  `NoObserve` family described below;
- all 256 `NoObserve` subsets for a fixed seed: 13 panic-producing subsets;
- direct malformed-observation reproductions and a PATH-shadowed Go probe for
  the nested GoLean oracle;
- source-level resource, evaluation-order, map-order, string-growth, replay,
  and output-directory analyses.

No production code was changed as part of the audit.

## Findings

Severity means: **P0** can invalidate a conformance verdict or a foundational
guarantee; **P1** materially weakens reproducibility, fail-closed behavior, or
campaign completion; **P2** is coverage/measurement/product debt; **P3** is a
localized correctness or usability issue.

### P0. GoLean is compared to an unrecorded second Go reference

`-go` pins the `GcAdapter` used for the initial reference pass
(`cmd/gengo/main.go:385-391`), but `runGoLean` does not pass that binary into
`golean.Config` (`cmd/gengo/main.go:615-617`). The GoLean adapter preserves
ambient PATH (`golean/golean.go:301-306`), and its script invokes plain `go run`,
including for its Go oracle (`deps/golean/scripts/diff-coverage:603-617`). That
oracle also forces `GO111MODULE=off`.

The initial pinned document supplies expected status/panic text during
translation (`golean/golean.go:223-245`); normal returned values are compared by
GoLean against the PATH-resolved Go executable. Therefore a report can name Go A
as `referenceIdentity` while the actual value comparison was Go B versus GoLean.
This can both fabricate and hide mismatches, especially across language/runtime
versions.

**Fix.** Resolve one absolute Go binary before any write. Pass it through the
adapter and GoLean script, use one declared language/module mode, and persist a
structured nested-oracle identity: binary digest/path/version, GOOS/GOARCH,
language version, module mode, and sanitized environment policy. Refuse the
campaign if the nested oracle cannot prove it used that identity.

### P0. The judged build is not bound to the recorded case identity

Generation reuses case directories and overwrites only `subject.go`,
`driver.go`, and `case.json` (`cmd/gengo/main.go:267-285`). A stale or injected
`extra.go` remains. `GcAdapter` builds the whole directory (`go build ... .`,
`harness/harness.go:314-329`), while `CaseResult` records only the subject hash
(`harness/harness.go:135-142`). Replay creates a fresh directory containing only
the regenerated subject, driver, and module (`cmd/gengo/main.go:524-540`).

A concrete example is an extra file with an `init` panic: judging executes it,
but subject/driver replay omits it and still verifies the recorded hashes. The
same issue applies to an altered `driver.go` and module/build inputs. In
particular, root `go.mod` is written only when absent (`cmd/gengo/main.go:182-187`),
so reuse preserves arbitrary module/toolchain/replace directives while replay
synthesizes a canonical module.

**Fix.** Make the complete build-input set the case identity. Generate each case
in a fresh staging directory, permit only a versioned file manifest, hash every
input (or a Merkle root), and atomically publish it. Both adapters must execute
immutable snapshots of the same verified tree. Batch results should bind to the
case-record digest, driver digest, module/language settings, and complete input
root, not just `subject.go`.

### P0. Batch membership and file integrity are not verified

`RunBatch` discovers `root/*/subject.go` by glob
(`harness/harness.go:361-373`). It does not use the manifest as an authoritative
case list, read `case.json`, verify schemas/IDs/hashes, reject extra cases, or
notice a declared case whose subject disappeared. A tampered subject is simply
rehashed and judged as though it were the generated case
(`harness/harness.go:401-415`).

There is also a time-of-check/time-of-use race: a worker reads/hashes the subject
and later builds a mutable directory. Reference and clone run sequentially, so a
concurrent mutation can make the recorded digest name neither execution, or make
the two adapters execute different programs.

**Fix.** Introduce an authoritative, versioned batch descriptor with exact IDs,
case roots, and expected digests. Validate the whole batch before execution,
reject extra/missing/duplicate/inconsistent inputs, snapshot once, and give both
adapters immutable copies or a content-addressed tree.

### P0. Observation parsing is vocabulary-strict, not structurally fail-closed

`observe.Parse` checks enums and recursively checks that value kinds are known,
but does not validate the document as a tagged union
(`observe/observe.go:199-278`). It accepts, among other impossible states:

- `status:"panic"` without `panic` information;
- `status:"ok"` with an `error` payload;
- `status:"error"` without an error payload;
- recovered events without panic data and point events without values;
- empty `goType`, contradictory kind fields, and inconsistent container data.

Confirmed reproductions round-tripped the first two through `Parse`, then
`harness.Judge` returned `match` for identical malformed documents. `Failed`
only tests `StatusError` (`observe/observe.go:172-173`), and Judge then calls
equality (`harness/harness.go:86-102`). This directly contradicts “fail-closed
parsing” and “infrastructure failure is never conflated with semantic
divergence.”

**Fix.** Add `Document.Validate` with status/payload cardinality, event-shape,
kind-specific field, nonempty type, interface, container-length, and map
canonicality invariants. Call it from Parse and from Judge/Equal because Adapter
returns an exported `Document` directly. Invalid documents are adapter/harness
failures, never mismatches or matches. Add an adversarial table covering every
forbidden combination.

### P0. Public `Config.NoObserve` values can panic the generator

`Config.Validate` does not validate `NoObserve` (`gen/gen.go:73-148`). During
declaration, the generator may exclude every resolved candidate for promotion
and call `pick` on an empty slice (`gen/gen.go:1192-1215`), reaching `draw(0)`
(`gen/choice.go:115-124`). This violates the package contract that bad configs
return errors rather than panic.

The problem is seed-dependent, not limited to masking every enum value. For one
seed, 13 of 256 shape subsets panicked; some nominally left arrays/slices allowed
but the resolved pool contained neither. Invalid persisted values such as
`Shape(8)` and `Shape(255)` are silently accepted.

**Fix.** Validate the closed enum and duplicates; make observation promotion
total for the *resolved* pool. The simplest policy is to retain an observable
scalar floor, but the robust design has promotion return a typed config/generator
error if the realized pool is empty. Add a panic-free matrix/property test over
profiles, seeds, and all shape masks.

### P0/P1. The runtime resource proof misses slice-range amplification

The validation bound assumes each nesting level executes at most
`2*LoopCap` (`gen/gen.go:101-113,422-425`). Slice ranges are instead bounded by
the slice length (`gen/stmt.go:1141-1177`). The generator prevents appending to
the slice currently being ranged (`gen/gen.go:1477-1491`), but permits:

```go
for range a { b = append(b, x) }
for range b { a = append(a, x) }
```

Repeated statements give alternating/Fibonacci growth. Thus slice range trip
counts need not be LoopCap-bounded, and “append is linear in executed
statements” is circular because the growing slices determine how many statements
execute. This does not prove nontermination—each generated program is still
finite—but it falsifies the stated time/memory bound for the public config and
can produce impractical cases.

Separately, `Stmts=4,000,000, Depth=0` passes the execution-count check. It may
respect the runtime count formula while imposing enormous generator, formatter,
source, and compiler costs, contrary to “small.”

**Fix.** Either range only over arrays, forbid all slice appends inside any slice
range, or track symbolic lengths with an acyclic range-to-append dependency
graph. Derive a real maximum from those dependencies. Add a direct syntactic
size/source-byte/helper cap and separate generation, compilation, runtime, and
output budgets. Pin the cross-slice adversary as a structural test.

### P1. The advertised timeout excludes compilation and may leak descendants

`GcAdapter.Run` builds using only the caller context and creates the configured
timeout after compilation (`harness/harness.go:325-348`). The CLI supplies a
background context. A wedged compiler can hang indefinitely, so `-timeout` is a
run timeout, not a per-case timeout. Identity probes are likewise unbounded.
`exec.CommandContext` kills the immediate process, not necessarily compiler or
shell descendant process groups; this matters even more for `bash
diff-coverage`.

**Fix.** Define and persist separate identity/build/run/adapter budgets, or a
single end-to-end case deadline with phase attribution. Kill process groups (and
use Windows job objects where supported). Test a compiler and adapter that spawn
children and ignore termination.

### P1. Process output and observation input are memory-unbounded

Compiler and GoLean output use `CombinedOutput`; subject stdout/stderr use
unbounded `bytes.Buffer` (`harness/harness.go:328-350`,
`golean/golean.go:307`). A faulty subject, compiler, or adapter can exhaust the
harness before timeout classification.

**Fix.** Cap each stream and observation document, record truncation, retain
size-capped diagnostic artifacts, and classify overflow by phase. Resource
budgets belong in the batch report.

### P1. In-place regeneration is not transactional

On reuse, `manifest.tsv` is first truncated to a header and `batch.json` is
deleted (`cmd/gengo/main.go:188-201`); case dirs are overwritten one at a time;
the full manifest and stale-case cleanup happen later. Interruption leaves a
hybrid directory that still passes the weak ownership check because a manifest
exists. Direct writes can also leave truncated JSON/TSV files.

**Fix.** Treat batches as immutable run directories. Build a complete sibling
staging tree, write records via temp-file/fsync/rename, write a signed-off
completion descriptor last, and atomically publish. Preserve the previous valid
batch until publication succeeds. Reject symlinks at every output component (or
use directory-relative no-follow operations): today an existing
`case_00000 -> /some/writable/directory` is accepted by `MkdirAll`, and subsequent
`WriteFile` calls follow it (`cmd/gengo/main.go:267-285`), allowing a nominal
batch reuse to overwrite files outside the output tree.

### P1. Replay verifies source bytes better than experiment semantics

Replay checks subject and driver hashes, but does not compare regenerated feature
metadata, enforce directory basename equals record ID, validate batch schema and
ID uniqueness, or join the batch subject hash/revision to the case record
(`cmd/gengo/main.go:486-563`). Unknown case-record fields pass through ordinary
`json.Unmarshal`. Feature metadata matters: it drives attribution, profiles,
width claims, and GoLean manifests.

If a fresh replay has a non-ran outcome, only its status is compared
(`cmd/gengo/main.go:568-575`): any build failure “reproduces” any other build
failure, and timeout settings are not stored.

**Fix.** Strictly decode and validate all artifact joins; compare regenerated
features/counts and canonical config; store normalized phase/failure signature
and budgets. Distinguish “same failure class” from “same failure.” Do not call
status-only equality reproduction.

### P1. Dirty identities are labels, not reproducible provenance

Generator and GoLean identities collapse arbitrary changes to `<HEAD>-dirty`
(`cmd/gengo/main.go:713-750`, `golean/golean.go:93-125`). Different source trees
therefore share the same recorded identity.

**Fix.** For campaigns of record, require clean trees or record relevant source
tree/diff/untracked-content and binary hashes. Persist the GoLean script hash as
well as repository commit.

### P1/P2. Pair mode does not deliver an authoritative pair-coverage product

The brief says the batch report tracks pairwise tag co-occurrence, but
`batch.json` stores only `Pairs`, `Composition`, and `CompositionJudged`
(`harness/harness.go:186-200`). Requested/realized pair counts are computed for
stdout and left derivable from manifest/cases. Existing evidence reports only
393/1128 pairs realized at least once in one campaign.

`-pairs` also accepts `-swarm=false`. With nil explicit constructs, that enables
all optional constructs, so forced pairs become no-ops rather than pairs inside
an ordinary swarm (`cmd/gengo/main.go:257-263`, `gen/gen.go:426-463`).

**Fix.** Define whether the feature promises requested pairs or realized
co-emission. Persist requested, compatible, generated, and semantically judged
pair matrices plus zero pairs; classify incompatible pairs. Reject
`-pairs -swarm=false` or define a separate baseline mix. Set a meaningful
per-compatible-pair realization exit criterion.

### P1/P2. Target Go language version is implicit and inconsistent

Generated/replay modules hard-code `go 1.26`
(`cmd/gengo/main.go:179-185,529-536`), while GoLean's oracle uses GOPATH mode and
the ambient toolchain default. The language target is absent from Config,
case.json, and structured batch provenance. This limits older implementations
and makes “Go spec surface” ambiguous.

**Fix.** Make target Go version a validated, persisted campaign dimension. Pin
toolchain and language target independently, reject unsupported combinations,
and test minimum/current targets.

### P2. Evidence for the strongest guarantees is too narrow and not durable

The checked-in suite is thoughtful, but repeated generation covers 50 seeds,
the structural termination test focuses classic bounded loops, and the ordinary
reference smoke is 50 cases. There is no checked-in fuzz/property suite for
config/replay/protocol mutation, high-N repeated-runtime nondeterminism campaign,
multi-Go-version matrix, or OS matrix.

Historical campaigns are largely `.tmp`/gitignored records. The repository lacks
a compact, checked-in regression corpus pinning generator/clone revisions,
subject hashes, and expected broken-to-fixed verdict transitions for BUG-012,
BUG-042/043, and BUG-049.

**Fix.** Preserve compact manifests and representative cases, regenerate them in
CI, add protocol/config fuzzers and repeated-runtime campaigns, and publish
last-green external clone identities. Keep bulky campaign output as artifacts,
not source.

### P2. The ledger gate proves mention, not support

The ledger defines supported as generated, tagged, and witnessed, but
`TestLedgerNamesEveryTag` only searches prose for backticked tag names
(`gen/ledger_test.go:9-23`). It cannot catch stale tags, wrong status, duplicated
ownership, missing witnesses, or profile limitations.

**Fix.** Make the surface table machine-readable (generate Markdown if desired):
status, tags, witness IDs, profiles, quotients, language version, and last
verified revision. Check bidirectional tag equality and execute named witnesses.

### P2. Public status and planning sources contradict the implementation

Examples:

- README says the Phase 4 ledger/ladder are next (`README.md:10-19`), although
  the ledger, Phase 4, and witness arc are complete.
- TODO says the 2026-08-06 audit plan is governing (`TODO.md:3-5`) and initially
  lists R2b as remaining, then describes it as delivered later.
- BRIEF still calls the ledger planned/absent and retains delivered interface and
  type-switch gaps (`BRIEF.md:57-62,91-100,276-285`).
- Phase 2's completion language exceeds its own evidence: defer/recover was
  unmeasurable, and recovered-event coverage remains open.
- The witness closing document still has merge/push action language even though
  the repository history shows later folding/closing work.

**Fix.** One living roadmap owns ordering. Dated plans get explicit
`CLOSED/HISTORICAL`, `SUPERSEDED BY`, and residual-work banners. TODO becomes an
issue inventory linked to roadmap IDs. README and BRIEF must be regenerated or
reviewed whenever ledger status changes.

### P2. Membership-lane implementation is not ready and changes the charter

The active design proposes the first intentionally nondeterministic cases, while
README/BRIEF state universal outcome determinism. It also assumes GoLean
membership failures can map to semantic versus infrastructure verdicts, but the
dependency currently emits the same `membership` stage for enumerator failure,
singleton, pin drift, coupling errors, and a sample outside the set; only free
text differs (`deps/golean/scripts/diff-coverage:867-952`). Current translation
hard-codes the strict lane and has no lane/why/params model.

The proposed proof that swapping two fold terms gives two different fixed-width
integer outcomes is incomplete under modular overflow: distinct `a` and `b` do
not alone prove `30*(a-b) != 0 mod 2^w`.

**Fix before emission.** Establish stable machine-readable GoLean reason codes;
add explicit per-case lane/why/params and `verdictsByLane`; prove/bound
non-singleton outcomes on 32/64 bits; validate choice width mechanically; and add
a forced membership canary covering all verdict classes. Revise the charter to
“strict lane is outcome-deterministic; alternate lanes use explicit lane-specific
oracles.” Never silently mix membership cases into a byte-equality headline.

### P2/P3. Smaller honesty and API defects

- `-go` is not preflighted, so the CLI writes a batch before discovering a bad
  executable, contradicting “validates every input before writing anything”
  (`cmd/gengo/main.go:91-176`). Preflight adapter identity before mutation.
- Unknown exported `PanicPolicy` values silently behave like exact comparison
  (`observe/observe.go:296-309`). Use an exhaustive switch and error otherwise.
- GoLean package IDs/features are not safe as general API inputs: raw IDs can
  traverse directories or inject TSV fields (`golean/golean.go:248-270`). Validate
  IDs, uniqueness, path containment, and every TSV field.
- Subject-size mean divides by all cases even if some stats failed
  (`harness/harness.go:453-467`). Prefer failing integrity validation; otherwise
  divide by successful stat count.
- CLI prints the literal `out/batch.json` even for another `-out`
  (`cmd/gengo/main.go:637-639`). Print the actual path.
- The local gc-386 summary can print reassuring zero/zero counts when no cases
  reached semantic judgment. Report judged denominator and call zero judged an
  incomplete campaign.
- `boundary` is present in almost every ordinary campaign (499/500 in this
  review), so it is not a useful corner indicator. Separate boundary-literal
  occurrence counts from `corner_boundary` case presence.
- Direct artifact ownership is authorized by the mere presence of
  `manifest.tsv`, after which unmatched `case_*` paths may be recursively
  deleted (`cmd/gengo/main.go:149-160,320-330`). Prefer immutable batch dirs;
  otherwise require a structured ownership marker and delete only IDs from the
  prior valid descriptor.

## What appears sound

Critical review should preserve what is working:

- replay draw exhaustion, out-of-range, and surplus handling is fail-closed;
- seeded and replay choosers copy mutable inputs;
- the ordinary generator configuration survived a broad 10,000-config probe;
- string concatenation has a one-variable rule and literal-only compound growth;
- map-range folds are commutative fixed-width addition and map observations sort
  keys; no additional map-order nondeterminism was found;
- witness effects appear confined to specified-order sites; defers and event
  calls use specified statement ordering;
- current generated programs compiled and ran in the fresh 500-case campaign;
- infrastructure versus semantic vocabulary is conceptually correct—the missing
  step is validating all inputs before applying it;
- the spec ledger is candid about many partial and quotiented areas, including
  slices, strings, constants, and the narrower GoLean observation channel.

## Proposed revision to the plan

The present capability-first backlog should be replaced by the following order.
Each stage has an objective exit rather than a prose declaration.

### R0 — reconcile claims and plan lifecycle

- Make this audit (or a derived `docs/roadmap.md`) the single living roadmap.
- Update README/BRIEF/TODO/spec-ledger status and universal-determinism wording.
- Add status/supersession/residual-work banners to dated plans.

**Exit:** the four living documents agree on current capabilities and next work;
no closed dated plan calls itself governing.

### R1 — restore experiment identity and atomicity

- Immutable staged batches and cases; authoritative descriptors; complete input
  roots; strict membership/integrity checks; same immutable snapshot for both
  adapters; atomic records and completion marker.
- Strict replay joins and full semantic metadata comparison.
- Clean-tree requirement or content hashes for dirty generator/clone trees.

**Exit:** injected/extra/missing/changed files fail before execution; interruption
preserves the previous batch; offline inspection can prove exactly which bytes
both adapters executed.

### R2 — make oracle/protocol boundaries fail closed

- One pinned Go oracle through gc and GoLean; explicit language/module target.
- Full `Document.Validate`, policy validation, safe GoLean IDs/TSV.
- Structured reference/clone/nested-oracle identities and environment policies.

**Exit:** PATH shadowing cannot change a verdict; every malformed-document corpus
entry is an infrastructure/harness failure; artifacts expose all implementation
identities and semantic modes.

### R3 — make resource guarantees true and measurable

- Close slice range/append amplification and `NoObserve` panic.
- Separate generation/build/run/output limits; process-tree cancellation; capped
  logs; persisted budgets and durations.
- Add a real small-source limit rather than relying only on execution count.

**Exit:** adversarial profile/resource witnesses return typed errors or stay
within measured bounds; no phase can run or allocate without a declared limit.

### R4 — durable evidence and operational CI

- Checked-in compact BUG-012/042/043/049 transitions and fake-clone contract
  tests; deterministic 386 positive and width-independent negative canaries.
- Configure the GoLean nightly, publish last-green identities/date, and retain
  red case trees plus manifests/results/meta/environment.
- Add minimum/current Go and at least one second-OS lane where practical; pin
  installer/action inputs in the evidence workflow.

**Exit:** a clean clone reproduces representative historical detections; the
external integration has a visible heartbeat rather than a self-skipping claim.

### R5 — machine-readable surface and coverage accounting

- Structured ledger, witness linkage, profile support, language-version scope.
- Pair compatibility denominator and generated/judged realization matrices.
- Per-profile/corner size and coverage trends; generated versus judged counters
  for recovered events and every major feature.

**Exit:** CI rejects phantom/misclassified tags, absent witnesses, impossible
pairs in the denominator, and unexplained coverage regressions.

### R6 — membership lane vertical slice

- First agree on stable GoLean reason codes and durable lane schema.
- Then implement a bounded, modularly proven raw-map witness, explicit width,
  replay support, separate lane verdicts, and a forced non-vacuous canary.

**Exit:** PASS, outside-set mismatch, enumerator infra, singleton/generator error,
and unknown-code failure are independently witnessed; strict-lane headlines are
unchanged and membership is reported separately.

### R7 — observation sensitivity and capability growth

- Close guarded recovered-event positive control and persist clone-side
  structured observations/reasons.
- Then pursue deterministic corners, a typed negative-generator lane, and
  parameterized subjects.
- After the effect discipline's mechanism 2: pointers, pointer receivers, then
  embedding/promotion R6. Add floats/complex only after an explicit bit/NaN
  equivalence. Generics and concurrency remain later dependency-driven work.

**Exit:** each rung has a machine-readable ledger entry, targeted witness,
generated/judged denominator, and at least one suitable positive control.

## Planning-document disposition

Keep live:

- `docs/spec-ledger.md`, after conversion to/generated from structured data;
- one revised living roadmap;
- `docs/2026-08-07_effect-discipline-design.md` as partially implemented design
  authority for mechanisms 2/3;
- `docs/2026-08-09_membership-lane-emission-design.md`, marked active but blocked
  on R2/R6 contract work.

Mark closed/historical: the project audit, Phase 1 audit, Phase 2 design and
campaign, Phase 3 replay, Phase 4 scope, witness charter/closing, and W1
measurement. Mark observation-protocol design “implemented with superseded
details” and list the symmetric-adapter residual. Mark salvage notes, GoLean
requests, semantic-divergence review, and handover notes as reference/input, not
live plans.

## Release recommendation

Do not block ordinary generator experimentation: the default path is productive
and the test corpus is valuable. Do block “campaign of record” claims on R1-R3.
In particular, do not treat current GoLean `match` results as fully pinned
conformance evidence until the nested Go oracle is identified and shared, and
do not treat an existing batch directory as immutable/replay-complete until its
entire build input set is bound to the artifact.

Once those boundaries are fixed, Grossmith will have something rarer than a
large grammar: a differential-testing product whose verdict can be independently
reconstructed and challenged. That is the right foundation on which to resume
language-surface growth.
