*CLOSED/HISTORICAL (2026-08-09): Phase 1 merged; findings fixed or recorded.
Residual work (recovered-event coverage, adapter shape (a)) lives in
`docs/roadmap.md` (R7) and `TODO.md`.*

# Phase 1 pre-merge audit — findings and dispositions (2026-08-07)

Two adversarial Opus-class reviewers over `git diff main...product-phase01`
at the branch's final state, decorrelated by dimension: (A) the
observation/comparison trust surface (observe, driver, harness, gen
profile lever); (B) the GoLean adapter and CLI. Both grounded findings in
real runs; every finding cited file:line and was tagged VERIFIED or
SUSPECTED. Top findings were independently re-verified before fixing.

## Fixed on the branch (commit per theme)

| # | Finding (tag) | Fix |
|---|---|---|
| B-F1 | CRITICAL, demonstrated: diff-coverage exits 1 both for FAIL rows and for a lake-build failure that publishes nothing; with index-based case IDs a previous campaign's results.tsv passed every id check and was republished — fabricated mismatch included — as this run's batch.json while GoLean never ran | results/meta removed before every run; published meta's `manifest_sha256` must equal the manifest just written; missing meta = explicit error |
| A-H1 | Driver-internal panics (unobservable kind, unsortable map key) unwound into main's recover and were emitted as legitimate `status:"panic"` documents at exit 0 | `_gEmit` recovers its own panics → exit 3, no document (adapter reports infra); map-key comparator fails closed; witness builds a driver over a float64 subject |
| A-H2 | `Document.Failed()` was dead code; two error documents judged `match`, one against a real observation judged `observation-mismatch` | Judge gained the document-status infra axis with the same never-conflate rule; four-cell witness |
| A-M3 | Parse validated only 3 of 6 closed vocabularies (`Event.At`, panic kinds, error kinds parsed open) | all vocabularies closed at parse time; under `-panic-policy kind` the kind IS the verdict |
| A-M4 | Trailing bytes after a document parsed silently | `dec.More()` → error |
| A-M5 | case.json couldn't regenerate its case (no resolved config); generatorRev was "unknown" under `go run` | record embeds the generator config; git-at-CWD fallback with explicit `cwd-git:` provenance prefix |
| A-M6 | Exclude doc comment overclaimed "draw trace unchanged" | corrected: mix/corner draws unperturbed, downstream draws diverge |
| A-L3 | Toolchain re-resolved per run despite "pinned" claim | resolution cached on first use; relative GoBin absolutized |
| A-L5 | RunBatch had no cancellation path — aborts became all-infra reports with nil error | cancellation drains the pool and returns an error; no report |
| B-F2 | diff-coverage output discarded (F1 was loud on their side, muted on ours) | always persisted as `golean-work/diff-coverage.log` |
| B-F3 | Interrupted generation wedged the out dir permanently (ownership token written last) | header-only manifest written first |
| B-F4 | Stale batch.json survived regeneration next to new cases | removed before cases are written |
| B-F5 | `-panic-policy` recorded for golean campaigns but never applied | explicit flag + golean refused; report records the equivalence actually applied |
| B-F6 | A stuck machine (`"status":"stuck"`) judged observation-mismatch | the interpreter analogue of a frontend gap → clone-infra-failure |
| B-F9 | Missing/typo'd checkout failed only after minutes of judged cases | checkout stat'ed in validate(); `-out ""` refused |
| B-F10 | Infra-failure cases invisible in the printed report | every non-match verdict gets a detail line |
| B-F7/F8 | judge docstring overclaimed "translation broken" for their go-side timeouts; membership stages unmapped without a note | docstrings corrected |

## Recorded, deliberately not fixed now

- **A-L1** (nil vs empty slice/map unobservable): unreachable — no
  zero-value slice/map declaration path exists. Becomes live under design
  shape (a); revisit there.
- **A-L2** (guarded recover discards non-error panic values, unlike the
  driver's main recover): unreachable — every generated panic is a
  `runtime.Error`. Documented at the emission site; reconcile before
  explicit `panic(v)` enters the grammar.
- **A-L4** (recovered-event runtime coverage): measured post-audit at
  1 fired / 120 cases (42 carrying guards, seed 7000) — live but ~2% per
  guard-bearing case, so the statement-level-catch semantics is barely
  exercised end to end. The bias machinery works; a reliable fix is
  structural (force a hot statement form inside guards) and is a
  generator rung with its own measurement, not an audit patch.
- **B-F7 residual**: their go-harness/go-run timeout is indistinguishable
  from a translation defect in the stage vocabulary; detail text is the
  only discriminator. Acceptable while both land on fail-closed red.
- **B "could not verify": language-version parity** — grossmith's
  reference builds in a `go 1.26` module; GoLean's oracle builds in
  GOPATH mode where the language version is the toolchain default.
  Unobservable today (no closures, no range-over-int in the grammar);
  becomes a live divergence source if either enters. Check before adding
  those rungs.

## Sound (checked hard, both auditors)

Judge's 25-cell outcome cross-product; reflection fidelity including
signed/unsigned map-key sorting and width preservation; events surviving
the panic path; driver/parser panic-kind agreement; recursive
unknown-field rejection; canonical-form determinism; sealed run
environment; GoLean translation byte-fidelity through their
reparse/reformat pipeline (structural, not luck: import-free, main-free,
already-gofmt'd subjects); the observed-tuple shape contract holding
transitively (struct fields scalar-only, array elems drawn pre-slice/map,
interfaces always named) over an 800-seed profile sweep; manifest
escaping vs their quoted-glob matching; parseResults' fail-closed checks.
