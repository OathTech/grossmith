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
- **Smaller deferrals** recorded in the pre-merge audit doc: nil-vs-empty
  slice observation (blocks on design shape (a)), non-error panic
  values (blocks on explicit `panic(v)` in the grammar), go-side
  timeout vs translation-defect stage ambiguity, GOPATH-mode language
  version parity (check before closures / range-over-int rungs).
