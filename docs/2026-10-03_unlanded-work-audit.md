**Recovery and audit of unlanded work — 2026-10-03**

Follow-up: the [repair record](2026-10-03_improvements-repair-record.md) records
the fixes and validation completed after this historical audit.

Recommendation: fix the three reproduced findings below before landing
`codex/grossmith-improvements`. The ordinary generated-program paths passed
validation, but the new source-check path can produce a false semantic mismatch
and can consume a recoverable previous batch. The string wire reader also
accepts representations its new contract forbids. No implementation fixes or
merges were made during this audit.

**Where work stopped.** The only local branch not merged into `main` is
`codex/grossmith-improvements`, at `3bba169ecf4fadf6383487f2be4700ddb19624d7`.
It contains eight commits from September 17, based on `e68867d`:

| Commit | Intended improvement |
| --- | --- |
| `a1c2b00` | Preserve aggregate observations on early returns and recovered panics |
| `579bdcc` | Compare gc against another Go executable or different compiler flags |
| `14b56a7` | Validate both adapter outcomes before classifying any verdict |
| `5fcfeea` | Require complete, internally consistent evidence during report verification |
| `f92c5ef` | Reject duplicate fields, trailing JSON, and lossy Unicode input |
| `e75105a` | Preserve arbitrary string bytes and generate slices that split UTF-8 |
| `73e6f11` | Add `-check` for saved case directories and edited source files |
| `3bba169` | Address four findings from the September 17 pre-landing audit |

The branch changes 42 files, with 3,164 insertions and 310 deletions relative to
its merge base. The earlier audit and response are preserved on that branch:
[audit](2026-09-17_grossmith-improvements-prelanding-audit.md),
[response](2026-09-17_grossmith-improvements-review-response.md).
The last recorded step was remediation and validation; the branch was not merged
into local `main`.

Local `main` and the cached `origin/main` point to `41d284e`. Two September 22
fixes landed independently: `18232a2` selects GoLean's pinned Go oracle in CI,
and `41d284e` supplies the strict campaign choice-depth budget. Preserve both
when landing the improvement branch. A `git merge-tree --write-tree` check found
one conflict, in README.md; the GoLean code and tests merged automatically.
The resulting combined revision has not been executed in this audit.

There were no working-tree changes or stashes at the start. Both old Claude
worktrees were clean, and their commits, as well as all other local branch tips,
are ancestors of `main`. In particular, `golean-feedback` being ahead of its
own remote tracking branch does not mean its work is missing from `main`.

Remote state could not be refreshed: `git fetch` failed to resolve github.com,
and `gh` could not read its configuration through the nono OS sandbox.
`nono why` confirmed `path_not_granted` for
`/home/dev/.config/gh/config.yml`. Thus this is an audit of local refs and local
artifacts, not a claim about current PRs or GitHub CI status.

**F1 — P1: control-character panics become false GoLean mismatches.**

Location: reviewed [golean/golean.go](../.tmp/audit-2026-10-03/golean/golean.go),
lines 426–429. The new panic-message gate excludes NUL, tabs, newlines, invalid
UTF-8, and sentinel values, but allows other JSON control characters.

Minimal source:

```go
package main
func fuzzSubject() int { panic("\x01") }
```

Using the reviewed executable:

```sh
gengo -check control.go -clone golean:/path/to/golean -out fresh-output
gengo -verify fresh-output
```

Against installed GoLean `3d2158224d06b6d7118c4f2af7a4c125c88768be`, this records
`observation-mismatch`, and offline verification succeeds. Lean's message is
correctly JSON-escaped as `\u0001`; the nested Go oracle's message contains a
raw control byte. GoLean's `scripts/diff-coverage` `json_string_literal` helper
escapes only backslash, quote, newline, carriage return, and tab. Its comparator
fails with `offset 13: unexpected character in string`, and grossmith treats
that differential-stage failure as semantic disagreement. Both runtimes
actually panic with the same message. `panic("\a")` reproduces the same failure;
a quoted ordinary message matches.

Attribution: the external JSON encoder defect is inherited. The new `-check`
input domain exposes it, and the new representability gate does not contain it.
This is a remaining gap in the earlier panic-message remediation, not evidence
that the new lossless grossmith encoder itself loses these bytes.

Required change: classify these inputs as clone infrastructure failures until
the supported GoLean harness can encode them, or fix and pin that external
encoding contract. A comparator parse error must not become a semantic
mismatch. Test the full U+0000–U+001F range, with ordinary printable and quoted
messages as positive controls.

Evidence: [input](../.tmp/audit-2026-10-03/.tmp/probes/panic-control.go),
[report](../.tmp/audit-2026-10-03/.tmp/probes/panic-control/batch.json),
[external results](../.tmp/audit-2026-10-03/.tmp/probes/panic-control/golean-work/results.tsv).

**F2 — P2: source checking can delete a previous batch during recovery.**

Location: reviewed [cmd/gengo/check.go](../.tmp/audit-2026-10-03/cmd/gengo/check.go),
lines 30–38, interacting with `stageBatchDir` in `main.go`, lines 122–148.

The new mode promises a fresh destination and preservation of previous runs.
It checks only whether `out` is empty or absent. After a publish interruption,
the previous batch can reside at `out.prev` while `out` is absent. Validation
passes; staging restores the old batch; publication then replaces and deletes
that restored batch.

Reproduction used only newly created audit data:

1. Check a source returning 101 into `result`.
2. Rename `result` to `result.prev`, recreating the interrupted-publish state.
3. Check a source returning 202 into `result`.

Step 3 exited successfully, printed that it recovered the previous publish,
then removed `result.prev` and replaced the old batch with the new one. The
previous batch was no longer available at either output path.

Attribution: replacement during generation is existing behavior. Its reuse
without a recovery-aware freshness check is new to `-check` and violates that
mode's preservation contract. Refuse a recoverable previous run before starting
the new check, or restore it and then require a different destination. Add an
interrupted-publish witness that asserts the old batch remains intact.

Evidence: [rerun log](../.tmp/audit-2026-10-03/.tmp/recovery-probe/rerun.log),
[saved old report](../.tmp/audit-2026-10-03/.tmp/recovery-probe/old-batch.json),
[replacement report](../.tmp/audit-2026-10-03/.tmp/recovery-probe/result/batch.json).

**F3 — P2: the new string payload reader does not enforce its wire contract.**

Location: reviewed [observe/observe.go](../.tmp/audit-2026-10-03/observe/observe.go),
lines 448–454, and `Value`'s default JSON decoding.

The documentation requires one payload: a UTF-8 `str`, or a base64 `strBytes`
with `str` absent. Validation compares decoded zero values and does not track
presence. The following fragments are all accepted as string values:

| Input fields | Decoded bytes |
| --- | --- |
| `"strBytes":"wg==","str":""` | `c2` |
| `"strBytes":"wg==","str":null` | `c2` |
| `"strBytes":[194,null]` | `c2 00` |
| `"strBytes":null` | empty string |

The array form is accepted by `encoding/json` for `[]byte`; its null element
silently becomes zero. Exact field spelling and duplicate-name checks do not
catch any of these cases. Adding `str:""` to the reference side of a valid
byte-string comparison, and honestly recomputing its report digest, still
passes `-verify` as a match. The contradictory empty-string payload disappears
during canonicalization.

This is new to the `strBytes` extension. Normal producer output is lossless;
the defect is acceptance of malformed or contradictory evidence. Decode string
payloads with presence and JSON-type checks, as the branch already does for
panic-message payloads. Cover direct observations, nested values, events, and
saved-report decoding. Retain support for the documented omitted `str` on an
empty UTF-8 string.

Evidence: [standalone parser probe](../.tmp/audit-2026-10-03/.tmp/json-probe/main.go),
[mixed-payload report](../.tmp/audit-2026-10-03/.tmp/wire-probe/result/batch.json),
[successful verification](../.tmp/audit-2026-10-03/.tmp/wire-probe/verify.log).

**Validation and limits.** Tests ran against the exact branch head in the clean,
detached worktree [`.tmp/audit-2026-10-03`](../.tmp/audit-2026-10-03/), using
Go 1.26.5 on Linux/amd64 and the local GoLean revision named above.

| Check | Result |
| --- | --- |
| `go test ./... -count=1` | Passed; optional GoLean integration tests initially skipped because the isolated checkout had no dependency checkout |
| `go vet ./...` | Passed |
| `go test -race -short -count=1 ./harness ./golean ./observe ./cmd/gengo ./internal/strictjson` | Passed |
| Real `TestGoLeanEndToEnd` and `TestGoLeanByteStrings` after linking the dependency | Passed, without skipping |
| Copied-current-driver CLI check with canonical GoLean checkout path | One match; report verified |
| gc optimization comparison, seeds 1–100, `-clone-gcflags='-N -l'` | 100 matches; five wrapper catches, all judged; verification and replay of case 7 passed |
| GoLean campaign, seeds 4242–4341 | 90 matches, 10 frontend infrastructure refusals, zero mismatches; report verified |
| `git diff --check main...HEAD` | Passed |

The GoLean campaign's ten refusals all concern calls/allocations in short-circuit
operands. Eight wrappers caught panics: seven reached semantic judgments and
one was refused by the frontend.

The isolated `TestCheckGoLeanCurrentDriver` invocation failed because that test
uses a symlinked dependency path, while the existing identity check requires the
literal Git repository root. The equivalent CLI directory check with the
canonical checkout path matched and verified. This environment setup failure
is not counted as a branch regression.

A fresh 386 canary still fails execution with `signal: trace/breakpoint trap`.
No cross-architecture semantic validation was obtained. No second actual Go
release was available for this audit. The current remote GoLean revision and
the newer strict-depth guard were not validated against this older checkout.

The earlier audit's saved divergence, originally generated at baseline seed
4393, remains reproducible: GoLean returns `"gros"` where gc returns `""`.
Rechecking the saved source produced an observation mismatch again. Preserve
this [case and report](../.tmp/audit-2026-10-03/.tmp/known-divergence/batch.json)
as a regression candidate. It is inherited, not a regression introduced by this
branch; a clean campaign with newly generated seeds does not establish its fix.

The prior fixes for copied-driver refusal, field-name aliases, and source-origin
validation held in the reviewed tests. Empty and invalid-UTF-8 panic messages
are covered by the branch's passing regressions; F1 concerns other control
characters. Aggregate fingerprints still have their documented collisions;
GoLean reports still contain external attestations rather than independently
re-judgeable clone observations; whole-case resource limits remain deferred.

**Resume point.** Resolve F1–F3 with focused regression witnesses, integrate
the two landed CI changes and resolve the README conflict, then validate the
combined revision against the intended GoLean checkout. Reconcile the stale
README/roadmap “containment in progress” banners with the already-merged
containment closing record. Preserve the saved seed-4393 divergence before
resuming the larger roadmap: durable regression evidence and CI matrices,
machine-readable coverage accounting, membership-lane work once the GoLean
contract is available, then further sensitivity and language coverage.

The audit report is the only new non-ignored file in the original working tree.
Reproduction artifacts remain under the ignored audit worktree. Neither the
improvement branch nor `main` was modified or merged.
