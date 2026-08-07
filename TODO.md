# Backlog

Items outside the governing audit phase plan (that plan lives in
`docs/2026-08-06_project-charter-and-engineering-audit.md`; Phase 2
planted defects is the next phase). Ordering here is not commitment.

- **GitHub CI** (none exists today — no `.github/`). Two tiers:
  - Tier 1, per-push: `go vet ./...` + `go test ./...`. Everything runs
    on a bare runner with only a Go toolchain — the `golean` end-to-end
    test already self-skips when `deps/golean` is absent. ~1 min.
  - Tier 2, nightly/manual: a real GoLean campaign — clone GoLean at a
    pinned rev (private repo: needs a token), elan/lake toolchain
    install + `lake build` (cold: minutes; cacheable), then
    `gengo -n 200 -clone golean` and fail on any `harness-error` /
    unexplained verdict drift. This is the cross-repo conformance
    heartbeat; don't block per-push CI on it.
  - Optional in tier 1: a conformance smoke (`gengo -n 50 -judge`,
    assert ref-ran = 50) to catch driver/toolchain regressions the unit
    witnesses can't.
- **Recovered-event coverage rung** (audit deferral, measured 1/120
  cases): force a hot statement form inside guarded statements so
  statement-level catch is exercised end to end; re-measure, then
  consider a `recovered`-rate floor witness. See
  `docs/2026-08-07_phase1-premerge-audit.md`.
- **GoLean natural positive control**: once GoLean fixes the
  defined-type `++`/`--` machine bug (handed over 2026-08-07), the
  broken/fixed commit pair is a free real-world Phase 2 control —
  campaign against the broken rev must yield `observation-mismatch`.
- **batch.json lacks the composition histogram** (charter, BRIEF "the
  conformance statement is the product": reference version, policy, N,
  rate, *coverage histogram*). Tag counts and per-site choice stats are
  stdout-only today; a batch report read cold cannot state what the
  batch covered. Add aggregate tag counts (and optionally site stats)
  to the report.
- **GoLean campaigns record no clone observation** (pre-merge audit
  F10 residual): `CaseResult.Clone` stays nil; a mismatch is
  re-examined only through the free-text stage detail. Full fix is
  shape (a) below; a cheap interim is persisting their observation
  JSON per failed case.
- **Adapter shape (a): symmetric document emission** (Phase 1 design
  doc §5, the declared end-state): GoLean emits its own observation
  document and the defined v2→golean projection becomes the comparison
  plane, replacing the expected-status pre-pass and giving `-panic-policy`
  meaning for golean campaigns. Requires GoLean-side work; unblocks the
  nil-vs-empty deferral below.
- **Divergence regression corpus** (audit §1 "no fixture corpus"): when
  a campaign finds a real divergence or clone bug, pin the case dir +
  expected verdict as a tracked regression on OUR side (first
  candidate: the defined-type `++`/`--` case). Distinct from promoting
  cases into GoLean's corpus.
- **Release/versioning** (audit §1/§8): no version mechanism or tagged
  release exists; artifacts pin `generatorRev` but a consumer has
  nothing released to pin against. Becomes real the moment GoLean CI
  consumes grossmith.
- **Declared target Go version as a config/report fact** (audit M6):
  generated batches hardcode `go 1.26` in the throwaway go.mod and the
  language target is whatever the toolchain defaults to; make it
  explicit config recorded in batch.json. Folds into the GOPATH-mode
  language-version parity check below.
- **Size watch before resuming the grammar ladder** (audit M4): the
  one-per-type observed floor grows with every type rung; batch.json
  now carries subject-byte min/mean/max — add a trend expectation (or
  budget) so type expansion doesn't silently make every program large.
- **Smaller deferrals** recorded in the pre-merge audit doc: nil-vs-empty
  slice observation (blocks on design shape (a)), non-error panic
  values (blocks on explicit `panic(v)` in the grammar), go-side
  timeout vs translation-defect stage ambiguity, GOPATH-mode language
  version parity (check before closures / range-over-int rungs).
