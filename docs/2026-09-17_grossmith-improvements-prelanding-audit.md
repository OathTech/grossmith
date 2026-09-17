**Pre-landing audit: `codex/grossmith-improvements` — 2026-09-17**

**Recommendation: request changes before landing the complete branch.** The
generated-program path passed the full suite and a 100-case optimization
comparison. However, the new `-check` workflow admits inputs for which a recorded
match is misleading or panic evidence is silently changed. The JSON hardening
also leaves a reproducible ambiguity, and verification does not validate the new
source-provenance record.

These are four findings, with attribution made explicit below. I did **not**
find a demonstrated compile, termination, or ordinary generated-program
comparison regression in the tested population. An inherited defect is not
counted as a newly introduced regression simply because this review found it.

| Finding | Priority | Attribution | Practical consequence |
| --- | --- | --- | --- |
| F1. Copied drivers are ignored by the GoLean comparison | P1 | New `-check` path exposes an existing adapter assumption | `match` can accompany reference values different from the values GoLean compared |
| F2. Imported panic messages exceed the observation protocol's assumptions | P1 | Existing encoder/validator limitations become accepted CLI inputs through `-check` | Distinct panic bytes collapse; an empty panic becomes infrastructure failure |
| F3. Differently cased JSON fields still overwrite the same destination | P1 | Incomplete hardening; also reproducible on `main` | Contradictory observation/report fields pass strict parsing and verification |
| F4. Source-check provenance is not validated | P2 | New metadata lacks corresponding reader validation | Contradictory origin and generation-history claims verify successfully |

P1 means a material verdict, evidence, or reliability defect to resolve before
relying on the affected feature. P2 means a narrower correctness or validation
gap. No P0 finding was established.

**Scope and method.** Reviewed all seven commits from
`e68867d6a61061ead0922703ce58d1c7a461b3b8` (`main`, also the local `origin/main`)
through `73e6f112543f295af0e2cda9057ac7bda1edf0df`
(`codex/grossmith-improvements`): 37 changed files, 2,055 insertions and 289
deletions. The comparison uses the local refs; no remote refresh was performed.
The working tree was clean when review and validation began.

The review covered aggregate observation at returning exits, execution-budget
accounting, driver generation, arbitrary string bytes, adapter outcome
classification, strict JSON readers, report consistency, clone toolchain options,
source-check input handling, documentation, and the associated tests. I followed
the changed paths into the existing GoLean translation and publication code.
Validation included real subprocesses, real GoLean runs, deliberately
contradictory but correctly digested artifacts, and a build of the merge-base
source for attribution. No implementation fixes are included in this report.

Environment: Linux/amd64, `go1.26.5`, GoLean checkout
`3d2158224d06b6d7118c4f2af7a4c125c88768be`. Source line references below refer to
the reviewed head. Local reproduction material is retained in
[`.tmp/prelanding-review-73e6f11`](../.tmp/prelanding-review-73e6f11/), which is
ignored by Git and is not part of the proposed change.

**F1 — P1: do not report a GoLean match against an unchecked copied driver.**

Relevant code: [cmd/gengo/check.go](../cmd/gengo/check.go), lines 79–88;
[cmd/gengo/main.go](../cmd/gengo/main.go), lines 939–974;
[golean/golean.go](../golean/golean.go), lines 408–461.

Directory-mode `-check` deliberately copies `subject.go` and `driver.go` exactly.
The gc reference executes both files. GoLean translation copies only
`subject.go`; its external harness creates a different driver and computes its
own Go reference values. Grossmith then copies the external verdict into the
report without checking that this nested reference agrees with the recorded
reference values.

That assumption was reasonable for a controlled generated driver. The new
directory input accepts edited drivers as part of the supplied experiment, so
the two reference executions can now measure different things.

**Reproduction, executed against the real GoLean checkout:**

1. Use `package main; func fuzzSubject() int { return 1 }` as the subject.
2. Generate its current driver with `gen.DriverForSource`.
3. Change the driver's `r0 := fuzzSubject()` to `r0 := fuzzSubject() + 1`.
4. Run `gengo -check <case-directory> -clone golean -out <fresh-directory>`.
5. Run `gengo -verify <fresh-directory>`.

Observed: the saved reference value is `2`; the translated subject and nested
Go driver return `1`; GoLean publishes `PASS`; grossmith records `match`; offline
verification succeeds. The local result is
[`checked-edited-driver/batch.json`](../.tmp/prelanding-review-73e6f11/checked-edited-driver/batch.json).
Its translated wire program explicitly returns `1`, and its external results
file contains `PASS` for the case.

This does not require concurrent mutation or an incorrect clone. A saved driver
with filtering, extra calls, initialization, or different serialization can
similarly change the experiment. The report's existing warning that GoLean
verdicts are external attestations does not explain that the attestation may
use a different reference observation from the one displayed beside it.

**Required change:** make the comparison contract explicit and enforce it.
Either refuse copied drivers whose semantics the GoLean path cannot establish,
or persist and compare the nested reference observation with the supplied-driver
reference under the supported equivalence before accepting a semantic verdict.
If the product intentionally offers a subject-only GoLean experiment, record
that experiment and its actual reference separately. Silently regenerating a
driver would also violate directory mode's promise to preserve the supplied
experiment.

**Regression test:** use the edited-driver case above and require a clear
refusal or an evidence disagreement, never `match` beside reference value `2`
when the external comparison uses `1`. Keep an unchanged-driver positive case.

**F2 — P1: panic observations cannot faithfully represent all source accepted by `-check`.**

Relevant code: the new [gen/driver.go](../gen/driver.go) `DriverForSource`, lines
12–51; the same file's panic serialization, lines 115–117 and 249–252;
[observe/observe.go](../observe/observe.go), lines 160–164, 312–326 and 509–532.

The new entry point accepts a parameterless `fuzzSubject` with results, including
valid Go programs that explicitly panic with arbitrary strings. The existing
observation protocol assumes a nonempty panic message and stores it as an
ordinary JSON string. The new lossless `strBytes` encoding applies to string
values and point/defer value events, but not to panic messages.

Two failures were reproduced:

| Subject body | Actual message | Recorded result |
| --- | --- | --- |
| `panic("\xc2")` | One byte, `c2` | `status: "panic"`, message U+FFFD |
| `panic("\xb5")` | One byte, `b5` | The same `status: "panic"` and U+FFFD message |
| `panic("")` | Empty string | Observation parse failure; gc versus itself produces `both-infra-failure` |

Each subject used `func fuzzSubject() int` and was passed through single-file
`-check`, so these results use the branch's current generated driver, not a
stale copied driver. See
[`checked-panic-c2`](../.tmp/prelanding-review-73e6f11/checked-panic-c2/batch.json),
[`checked-panic-b5`](../.tmp/prelanding-review-73e6f11/checked-panic-b5/batch.json),
and [`checked-empty-panic`](../.tmp/prelanding-review-73e6f11/checked-empty-panic/batch.json).

There is also a direct comparator witness: two `StatusRan` outcomes constructed
with `observe.Panicked(nil, observe.PanicOther, "\xc2")` and the corresponding
`"\xb5"` message produce `Judge(..., observe.PanicExact) == match`. Validation
accepts the messages, and canonical JSON changes both to the same replacement
character. Parse-time Unicode checks cannot recover bytes already lost during
serialization.

The byte-collapse behavior also reproduces on `main`; this is an inherited
observation weakness newly exposed by the accepted source-check input domain.
The existing generator does not emit explicit string-valued panics. The
empty-message validator likewise reflects the old generated-runtime-panic
assumption, which no longer covers the new entry point.

**Required change:** preserve arbitrary panic-message bytes, including recovered
panic events, and allow an explicitly present empty message. Missing required
evidence should remain distinguishable from an empty Go string. Apply the same
contract to direct in-memory documents and serialized documents. Until lossless
encoding exists, malformed in-memory message bytes must at least fail closed
instead of producing a false exact match.

**Regression tests:** different invalid byte sequences must mismatch under
`exact`; identical byte sequences must round-trip; an explicit empty panic must
remain a semantic panic when comparing gc with itself; `kind` policy should
retain its documented behavior. Cover both top-level panic and recovered-event
payloads. The current byte-string tests exercise value payloads only.

**F3 — P1: strict JSON validation misses destination-field aliases.**

Relevant code: [internal/strictjson/strictjson.go](../internal/strictjson/strictjson.go),
lines 16–22 and 95–105; [golean/golean.go](../golean/golean.go), lines 730–739.

The structural walk compares decoded JSON names exactly, while the subsequent
`encoding/json` struct decoder accepts field names without regard to case.
Consequently, two distinct names can address the same Go field and overwrite
contradictory evidence. `DisallowUnknownFields` does not reject these aliases.

Both of these observations were accepted with no error:

```json
{"schema":"grossmith-observation-v2","status":"error","Status":"ok","values":[{"kind":"int","goType":"int","int":1}]}
```

```json
{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int","int":999,"Int":1}]}
```

The first becomes an ordinary `ok` document; the second becomes integer `1`.
This affects observations and the struct-backed campaign artifacts. The
external GoLean observation decoder also uses the same permissive struct
decoding after `strictjson.Validate`.

The end-to-end artifact witness replaced `"total": 100` in a valid report with
`"total": 999999, "Total": 100`, then honestly recomputed the report digest in
`complete.json`. `-verify` still succeeded and announced report consistency.
The raw report's canonical `total` key says 999999, while the Go reader uses 100.
See [`mutated-json-alias`](../.tmp/prelanding-review-73e6f11/mutated-json-alias/batch.json).

This is **not a regression relative to main**: the baseline reader accepts the
same alias examples. It is a remaining hole in the new unambiguous-evidence
reader. The patch correctly closes exact duplicate names, escaped exact aliases,
trailing data, invalid UTF-8, and unpaired surrogates, but that does not establish
unambiguous assignment into a Go struct.

**Required change:** enforce exact schema field spelling when decoding structs,
or detect aliases that map to the same destination field before decoding.
Preserve legitimate case-sensitive keys in open maps. For the GoLean protocol,
unknown-field compatibility can remain, but differently spelled unknown names
must not overwrite recognized fields such as `status` and `schema`.

**Regression tests:** include case aliases at the document, nested value,
outcome, report, manifest, and completion levels, plus Unicode case-fold aliases
that the decoder recognizes. Retain a case-sensitive map-key positive control.
Repeat the honestly rebound report test above.

**F4 — P2: verification ignores the new source-check provenance contract.**

Relevant code: [harness/harness.go](../harness/harness.go), lines 174–184;
[harness/report.go](../harness/report.go), lines 60–81;
[cmd/gengo/main.go](../cmd/gengo/main.go), lines 796–801.

`CaseOrigin` introduces a new record variant: `kind: "source-check"`, a source
path, a driver choice of `copied` or `current`, zero seed, and no generated
configuration or draw tape. `readCaseFeatures` decodes the origin but checks none
of these invariants. Replay treats any non-nil origin as a source check,
regardless of its kind or contents.

Starting with a valid source-check batch, I changed its case record to include:

```json
"origin": {"kind":"generated","path":"","driver":"unrecognized-driver"},
"config": {"claim":"generated"},
"drawTrace": [1,2,3]
```

After recomputing the case-record digest in the manifest and the manifest digest
in `complete.json`, `-verify` returned success. See
[`mutated-origin/case_00000/case.json`](../.tmp/prelanding-review-73e6f11/mutated-origin/case_00000/case.json).
This demonstrates missing semantic validation, not a failure of hash checking.

**Required change:** validate the origin as a record discriminator: recognized
kind and driver, nonempty source path, and consistent absence of generated
config/tape with the source-check placeholder seed. Check the original source
path's recorded structure, not its continued existence; offline verification
must still work after the source is moved. Share this validation between replay
and report readers. Do not infer an origin merely from missing legacy metadata.

**Regression test:** mutate each origin field and each mutually exclusive
generation field separately, rebind the digests, and require a refusal naming the
contradiction.

**Validation results.** All ordinary test commands completed successfully.

| Check | Result and scope |
| --- | --- |
| `go vet ./...` | Pass |
| `go test -short ./...` | Pass |
| `go test -count=1 ./...` | Pass for all six packages; `gen` completed in about 75 seconds |
| `go test -race -short -count=1 ./harness/ ./golean/ ./observe/ ./cmd/gengo/ ./internal/strictjson/` | Pass |
| Race run of `TestStalledCompilerIsBounded`, `TestUnboundedSubjectOutputHitsTheCap`, and `TestIdentityProbesBounded` | Pass |
| Verbose `TestGoLeanEndToEnd` and `TestGoLeanByteStrings` | Both ran and passed against the installed checkout; neither skipped |
| `FuzzValidate`, 15-second target, two workers | Pass; 709,099 executions, no panic |
| `git diff --check main...HEAD` | Pass |

The fuzz result establishes that no crash was found in that run; it is not an
ambiguity or protocol-completeness proof.

| Campaign | Result |
| --- | --- |
| Head: 100 cases, seeds 1–100, `-clone gc -clone-gcflags='-N -l'` | 100 reference runs, 100 matches; 5 wrapper catches, all judged; offline verification and replay of case 7 passed |
| Head: 300 cases, seeds 4242–4541, `-clone golean` | 300 reference runs, 281 matches, 19 clone infrastructure failures, no mismatches or harness errors; offline verification passed |
| Head: 100 cases, seeds 9000–9099, `-clone gc-386` | 100 reference runs; all 100 clone executions failed with `signal: trace/breakpoint trap`; report verification passed, but no cross-architecture semantic validation was obtained |
| Merge-base generator: 300 cases, seeds 4242–4541, same GoLean checkout | 289 matches, 10 clone infrastructure failures, 1 observation mismatch |

All 19 head GoLean infrastructure failures were frontend refusals concerning
calls/allocations in short-circuit operands. The campaign included 21
`aggregate_observed` cases: 19 matched and 2 were refused by the clone frontend.
Five aggregate cases caught a panic in the wrapper. The single `string_bytes`
case matched. Controlled tests, rather than this random sample, provide the
early-return aggregate witness: the 21 aggregate cases did not include an
`early_return`-tagged case.

The merge-base comparison is an attribution check, not an identical-program
comparison: adding `string_bytes` changes swarm draw consumption and therefore
the programs generated from the same seeds. Its one mismatch was
`case_00151`, seed 4393, where the reported string result was `"gros"` on Lean
and empty on Go. Checking that saved baseline case with the head CLI reproduced
the mismatch. It is an existing conformance finding, not a branch regression,
and its absence from the new seed population is not evidence of a fix. The case
is retained in
[`checked-baseline-divergence`](../.tmp/prelanding-review-73e6f11/checked-baseline-divergence/batch.json).

The baseline executable was built from `git archive main` and run from this
checkout. The existing cwd-based `generatorRev()` therefore stamps those
baseline artifacts with the surrounding checkout revision. Baseline source
attribution here comes from the archived source and separately built executable,
not that metadata field. This pre-existing identity limitation was not counted
as a new branch finding.

**Changes that held up under review.**

- Aggregate snapshots now use the same folds on normal return, early return,
  and recovered panic. Early-return blocks isolate generated aggregate names;
  wrapper slot ordering preserves the panic-site and order-witness positions.
  The new controlled mutation tests actually distinguish pre-exit container
  mutations. Existing append accounting already charged three fold visits per
  appended element; the budget diff corrects its commentary. The measured
  execution tests and the full suite found no accounting regression.
- Direct gc clone flags are passed as a single build argument and appear in
  both textual and structured identities. Clone preflight precedes staging.
  The optimization campaign demonstrates that the clone configuration reaches
  execution while reference flags remain unchanged.
- `Outcome.Validate` and the reordered `Judge` correctly prevent a failure on
  one side from hiding a malformed outcome on the other. Invalid policies,
  unknown statuses, and failure outcomes carrying documents are covered by the
  new classification tests.
- Report validation now requires per-case records, complete direct-comparison
  outcomes and verdicts, and the correct histograms. It recomputes wrapper legs
  from individual cases. The new tests rebind digests after mutations, so they
  exercise meaning checks rather than merely observing checksum failures.
- The new string value encoding preserves invalid bytes in returned values,
  containers, map keys, and value events. Bytewise map-key ordering uses the
  recovered bytes. The installed GoLean frontend and evaluator passed the new
  byte-string witness. F2 concerns the separate panic-message channel.
- Source checking uses a fresh output, validates explicit options, rejects
  input/output overlap through existing symlink aliases, and distinguishes
  generated records from source checks when producing them. Result arity is
  obtained from the AST, including grouped named results. The missing pieces
  are the adapter contract in F1, the widened panic domain in F2, and reader
  validation in F4.

**Remaining limits and follow-up priorities.** The 386 campaign could not test
semantic discrimination on this host. The ordinary clone-toolchain test uses a
distinct path to the same Go executable; this audit did not run a second actual
Go release. Those matrix gaps should be covered on a runner that can execute
32-bit binaries and with two explicitly pinned supported toolchains.

The documented aggregate fingerprint collisions, GoLean's lack of persisted
structured clone observations for offline re-judging, and resource costs outside
the subject execution counter remain. This branch does not establish fixes for
those deferred areas. Also, structured oracle entries remain optional in report
verification, and the existing validator does not implement all the structural
budget/size checks suggested by its broad introductory comment. Treat the
verification claim as the specific checks implemented, not an assertion that
every report field has been independently established.

Before landing the entire branch, close F1 and F2 for the new source-check
workflow and close F3 if unambiguous evidence decoding is part of the release
claim. F4 is a smaller, directly related validation addition and should accompany
the new record format. Add the focused negative witnesses described above,
then rerun the existing suite and affected campaign checks. The tests passing
today provide useful positive evidence, but they do not cover the reproduced
counterexamples in this report.
