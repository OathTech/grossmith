**Landing review and repairs — 2026-10-04/05**

Follows the [repair record](2026-10-03_improvements-repair-record.md). A fresh
pre-landing review of `codex/grossmith-improvements` at `615498d` found no P0
or P1 findings. It covered three areas in parallel: the evidence and report
layer, the `gengo` CLI with GoLean, and the generator. [USER] Mike, 2026-10-04:
"Yes, fix anything you found." Every finding below is repaired on the branch,
each with a regression test that fails on the prior code. Decisions taken
without a user ruling are marked [AGENT].

| Finding | Origin | Repair |
| --- | --- | --- |
| `-check` publication replaced `-out` even if another writer had filled it since startup; concurrent checks removed each other's staging (P2) | branch | Check mode claims staging exclusively and publishes only onto an absent or still-empty `-out`; on refusal the result stays in staging, named in the error |
| A trailing slash on `-out` placed staging inside the batch, which then failed verification (P2) | main | `-out` is cleaned before sibling paths derive; `.`, `..` and roots are refused |
| GoLean-policy verdicts were not checked against the recorded reference outcome (P2) | main | `-verify` recomputes the reference side and the panic-message refusal under the external policy |
| Seed-to-program mapping change undocumented (P2) | branch | README and roadmap say that seeds generate different programs than on `main`, and that saved cases replay only with their recorded revision |
| Plain `go test ./...` failed against the stale default `deps/golean` (P2, local) | branch | GoLean integration tests require ancestor `3bb8f4f`: an implicit checkout that is too old skips and names it; an explicit `GOLEAN_CHECKOUT` that is too old fails |
| Bad `-clone-gcflags` or an old `-clone-go` produced an all-infrastructure batch, exit 0, no warning (P3) | branch | Preflight builds and runs a trivial program with each toolchain and its flags. [AGENT] A zero-judged clone campaign exits non-zero; the nightly already failed in that case |
| gc clone identity not cross-checked against its toolchain record (P3) | branch | `cloneOracle` is required for gc clones and `cloneIdentity` must equal the string rebuilt from it |
| Fixed-size arrays truncated or zero-filled; non-canonical base64; `null` on scalars read as zero; zero-count histogram entries accepted (P3) | branch | All refused, documented in `observation-format.md` |
| Value decoding quadratic in nesting depth (P3) | branch | One strict validation pass plus one token walk; depth 3000 went from 26.3 s to 0.06 s |
| Unrecorded files at the `golean-work/` root were not noticed (P3) | branch | The root's entry set is closed; leftovers, wrong types and symlinks are refused |
| `-check -clone gc-386` printed every divergence as UNTAGGED; symlinked `-out`; extra `.go` files in a case directory ignored (P3) | branch | Stratification reported as not applicable; both inputs refused |
| Nightly `-run` filters would pass if a test were renamed (P3) | branch | Each named witness must report PASS; the GoLean checkout is fetched with full history for the ancestry check |
| `string_bytes` rarely realised: 5 of 52 eligible cases, seeds 1–1000 (P3) | branch | [AGENT] A `utf8-split` slice arm, active only when the construct is enabled, gives 30 of 52. Programs with the construct off are byte-identical |

Found during the repairs:

- **Generator revision taken from another checkout.** Go stamps `vcs.revision`
  from the nearest enclosing `.git` directory. A build in a git worktree nested
  in a checkout, or in an exported tree unpacked inside one, recorded the
  enclosing HEAD as clean (reproduced: a worktree at `8ebb102` stamped
  `1aed69d`, `vcs.modified=false`). The stamp is now kept only when the
  stamping repository tracks the compiled source file. Otherwise the cwd-git
  probe is used if the working directory's repository tracks that file, and
  `unknown` is recorded if not. Checked end to end: plain build, worktree build
  run inside and outside its worktree, and an exported older tree.
- **Judged campaigns accepted an unstated generator revision** (`main`):
  `unknown`, and `-dirty-unknown`, which also escaped the `-dirty` suffix test
  and so the content-hash requirement. Both are now refused.
- [AGENT] **Batch schema bumped to `grossmith-batch-v2`.** v2 reports require
  fields that `main`'s reports lack. A v1 report is now refused by schema name
  instead of by whichever field the strict decode meets first. Nothing outside
  grossmith reads this schema; the GoLean checkout was searched read-only.
  The observation protocol stays `grossmith-observation-v2` (additive).

Other [AGENT] decisions: the reference toolchain is preflighted as well, so
`-clone gc-386` now refuses up front on this host, which cannot run 386
binaries. The symlink refusal applies to check mode only. A leftover
`<out>.staging` from an interrupted check blocks later checks to that `-out`
until it is removed. The panic-message predicate is duplicated in `harness`,
because `golean` imports `harness`; a drift test keeps the copies equal.
Document- and event-level `null` fields still read as absent.

Validation at `dea6599b5bd47c1b66d4eaf04fb355c483828d72`, from a clean tree, with
Go 1.26.5 linux/amd64 and GoLean `0b072f7dc9980ef9c77129299ae976bda8e0032b`
(contains `3bb8f4f`; the checkout advanced during the session and was not
modified by this work):

- `go test ./... -count=1` passed with `GOLEAN_CHECKOUT` set, and also without it.
- `go vet`, the race set (`harness`, `golean`, `observe`, `cmd/gengo`,
  `internal/strictjson`), the three race witnesses, `git diff --check
  main...HEAD` and `gofmt` all passed. Every commit since `615498d` passes
  `go vet` on its own.
- gc against gc with `-N -l`, seeds 1–100: 100 matches; five wrapper catches,
  all judged; `string_bytes` in 3 cases; `-verify` passed.
- GoLean, seeds 4242–4341: 100 matches, zero infrastructure failures; eight
  wrapper catches, all judged; `string_bytes` in 2 cases; `-verify` passed.
- A reviewer's campaign of gc-vs-gc seeds 9001–9200 (200 matches) and a probe
  of 129 aggregate early-return/recovery programs (deterministic across
  builds) ran on the pre-repair head.

Logs: `.tmp/landing-2026-10-05/` (ignored). Unchanged limits: no 386
execution on this host, no second Go release, remote CI and PR state not
verifiable from this sandbox. Nothing was merged into `main` or pushed.
