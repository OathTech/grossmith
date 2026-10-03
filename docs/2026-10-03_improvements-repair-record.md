**Improvements repair record — 2026-10-03**

The [audit of `3bba169`](2026-10-03_unlanded-work-audit.md) found three remaining
gaps in `codex/grossmith-improvements`. All three are repaired, with regression
tests that failed on the prior code and pass on the repaired implementation.
Main's oracle-version and strict-depth fixes are integrated in merge commit
`424eb85`; the README conflict was resolved by retaining both capabilities.

| Finding | Repair | Regression evidence |
| --- | --- | --- |
| F1: false GoLean mismatches for control-character panics | Refuse every U+0000–U+001F panic-message character before translation, recording clone infrastructure failure. The conservative restriction supports older external panic encoders as well as current GoLean. | Every control byte is checked alone and within a message. Real CLI checks of `\x01`, `\a`, and embedded `\x1f` preserve the reference panic and verify their refusal reports. Ordinary and quoted Unicode messages match. |
| F2: `-check` can consume an interrupted previous publish | Refuse any existing `<out>.prev` before staging; leave the previous batch and destination untouched. | The actual `run` path is exercised with both absent and recreated-empty destinations. The original completion record remains byte-identical, the previous batch verifies, and no staging tree is created. |
| F3: malformed string evidence accepted | Decode payload presence and JSON types before accepting a value. Reject mixed `str`/`strBytes`, explicit null, arrays in the base64 channel, and valid UTF-8 in the byte-only channel. | Returned, nested, map-key, interface and event values are covered, plus reports with honestly rebound digests. Omitted/explicit empty UTF-8 strings and legitimate byte strings remain accepted. |

The end-to-end F1 test exposed a related serialization gap: when every case
refused translation, `omitempty` erased the explicit empty clone-work map.
Such reports could not verify. Empty clone-work evidence now survives as `{}`;
nil/absent legacy evidence remains distinguishable. Verification requires a
clone source digest for every semantic verdict and all three work-file digests
when any source was translated. Unrecorded result files are detected even when
the `cases/` directory is absent. These changes make refusal-only batches
verifiable without weakening checks on actual comparisons.

The saved seed-4393 divergence is now a durable fixture at
[`golean/testdata/assignment-before-panic`](../golean/testdata/assignment-before-panic/README.md).
The exact original source is retained. Its reference result observes the first
assignment before the second store panics. The older bundled GoLean checkout
still disagrees, while current local GoLean
`3bb8f4fc9cd7dab16571787140731b6d4c1f9d0e` matches. That result is an external
clone improvement, not a grossmith fix. The current checkout's integration test
and the nightly now require the fixture to match.

The string and CLI integration tests consistently honor `GOLEAN_CHECKOUT`,
allowing the nightly to exercise the intended checkout. The nightly also runs
the byte-string and source-check regressions alongside the strict-depth test.
The push CI race set now includes `internal/strictjson`. README and roadmap
banners reflect the already-closed containment arc and the pending landing of
this improvement branch.

Validation completed with Go 1.26.5 on Linux/amd64:

- `GOLEAN_CHECKOUT=/home/dev/projects/golean go test ./... -count=1` passed,
  including the real current-GoLean tests, strict-depth positive and negative
  controls, the saved assignment regression, and the new source-check cases.
- `go vet ./...` passed.
- Race checks for `harness`, `golean`, `observe`, `cmd/gengo`, and
  `internal/strictjson` passed under `-short`.
- The separate compiler-timeout, subject-output-cap, and identity-probe race
  witnesses passed without `-short`.
- The strict JSON fuzz target ran for 10 seconds with two workers and passed.
- `git diff --check` passed. Local `main` is an ancestor of the branch.

Final campaigns ran from clean implementation commit
`8ad78ef667ab69c6f08efdeed01d8cd98c0d558b`, without `-allow-dirty`:

| Campaign | Result |
| --- | --- |
| 100 cases, seeds 1–100, gc versus gc with `-N -l` | 100 matches; five wrapper catches, all judged; offline verification and case-7 replay passed |
| 100 cases, seeds 4242–4341, current GoLean at the revision above | 100 matches; eight wrapper catches, all judged; zero infrastructure failures; offline verification passed |
| Exact `panic("\x01")` audit reproduction against old and current GoLean | Clone infrastructure refusal on each checkout, original reference message preserved, explicit empty clone-work map, both reports verified |
| Audit report containing both `strBytes` and empty `str`, with rebound digest | Input integrity passed, report decoding refused the mixed payload as required |

Local logs and artifacts are retained in
[`.tmp/landing-2026-10-03`](../.tmp/landing-2026-10-03/). The branch is locally
ready for review and landing; these records do not claim that it was pushed or
merged into `main`.

The earlier audit's limitations remain explicit: this host cannot execute the
386 canary, and no second actual Go release was tested. GitHub PR status and
remote CI are not verified from this sandbox. Current local GoLean includes
commits ahead of its cached remote tracking ref; its exact revision above is
the integration evidence, not a claim about remote deployment. No changes were
made to either GoLean checkout.
