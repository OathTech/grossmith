# Backlog

Items outside the governing audit phase plan (that plan lives in
`docs/2026-08-06_project-charter-and-engineering-audit.md`). Ordering
here is not commitment.

## Customer-prioritized: GoLean's generation requests (2026-08-07)

STATUS (Phase 4 arc, 2026-08-07): R1, R2a, R3, R5, R4 DELIVERED as
rungs 1-5 (see docs/spec-ledger.md and the rung commits); the
composition-histogram backlog item was absorbed and closed by rung 5.
The recover WRAPPER observes panics at function level (wrapperCaught
in batch.json); the guarded-STATEMENT recovered-event rung below
remains open — a different mechanism, not absorbed. Remaining from the request set:
R2b (order witnessing — effect-discipline design) and R6 (embedding
matrices — blocked on rungs).

`docs/grossmith-requests-2026-08-07.md` — six requests ordered by
demonstrated yield against their 43-entry bug ledger, filed after their
2,900-case campaign (grossmith 5b4c5b0 vs GoLean 458386d, zero
divergences). These are the Phase 4 "prioritize by clone value" input,
now first-party. Mapping onto this backlog:

- **R1 recover-observation wrapper** (their highest yield): a canonical
  defer/recover-into-named-result idiom — pure Go, no obs* events, so
  it survives the golean profile and DISSOLVES the
  defer/recover-unmeasurable disposition. Absorbs three existing items:
  the recovered-coverage rung, the g06/c54 named-results-defer rung,
  and the Phase 2 defer/recover gap. Their panic-value-to-int encoding
  table keeps it strict-lane. First rung when the ladder resumes.
- **R2a multi-assign with aliased/nested targets**: extends the
  ladder-prioritization item (already top of that list) with their
  seed shapes — `i, a[i] = ...`, chains, comma-ok into mixed targets.
  Spec-defined assignment phases = deterministic, observable via final
  state; no effect discipline needed.
- **R2b order-witnessing generation**: every subexpression bumps a
  counter through a call, the result encodes realized order. Requires
  a deliberate DETERMINISTIC effect mechanism (spec orders function
  calls left-to-right) — i.e. the audit Phase 4 #3 effect-discipline
  design (closures/pointer params), not a quick rung. High yield
  (their evaluation-order bug class), high design cost.
- **R3 kind/definedness matrix corner**: {op site} x {int kinds,
  floats} x {defined vs unnamed} with IN-KIND arithmetic — their
  insight that `int(x)` conversion laundering masks the whole
  kind-defaulting class (the BUG-042/043 family our seed 559 started)
  is a grammar-design constraint, not just a corner: sweeps need
  conversion-free paths. Fits the named-corner machinery.
- **R4 pairwise swarm objective**: n cases per construct PAIR rather
  than per construct — a campaign/CLI orchestration feature over the
  existing Constructs override, mechanizing what their adversarial
  audits do by hand. Their worst cross-cutting bugs were all pair
  interactions.
- **R5 aggregate observation of maps/slices**: order-independent
  encodings (sums, min/max folds, length+membership bits) let
  maps/slices ENTER the observed tier under the golean profile instead
  of being masked — supersedes half the NoObserve disposition at zero
  harness cost. The membership-lane item remains the later, stronger
  form.
- **R6 embedding/promotion/interface matrices**: blocked on the
  embedding + pointer-receiver rungs (Phase 4 dependency order);
  bounded enumeration once those exist.
- Their non-ask list matches ours (concurrency, goto, float
  bit-exactness) — independent convergence worth noting.
- **Bug report accepted (fix immediately, not backlog)**: the witness
  suite is red in stock module mode — our sandbox TMPDIR lives inside
  the repo, so t.TempDir() case dirs inherit the repo go.mod and the
  missing per-dir go.mod is masked locally. Applies to the harness
  test helpers and runReplay's temp dir.

- **GitHub CI** — DELIVERED (2026-08-07, `.github/workflows/`):
  tier 1 per-push (vet, witness suite, race-short, 50-case conformance
  smoke, replay smoke — every step proven locally verbatim) and tier 2
  `golean-nightly` (private checkout + elan/lake cached + n=300
  campaign; fails on harness-error/ref-infra/mismatch, allows and
  archives clone-infra gap counts). PENDING ON MIKE: add a read-only
  deploy key on OathTech/golean and store its private half as the
  `GOLEAN_DEPLOY_KEY` secret on grossmith (setup steps in the workflow
  header) — the nightly fails loudly with instructions until then; first green runs happen on GitHub after push.
- **Tuple-forwarded call arguments rung** (2026-08-08 review §1): the
  shape `sink(pair())` — a multi-result call forwarded as the argument
  list, incl. variadic and mixed fixed/variadic destinations with
  interface parameters — is ungenerated today, and the review found a
  real GoLean interface-boxing bug living exactly there. The six-form
  boundary matrix in the review doc is the rung's witness spec; needs
  impure-free multi-result forwarding only, no effect discipline.
- **Recovered-event coverage rung** (audit deferral, measured 1/120
  cases): force a hot statement form inside guarded statements so
  statement-level catch is exercised end to end; re-measure, then
  consider a `recovered`-rate floor witness. See
  `docs/2026-08-07_phase1-premerge-audit.md`.
- **GoLean natural positive control**: once GoLean fixes the
  defined-type `++`/`--` machine bug (handed over 2026-08-07), the
  broken/fixed commit pair is a free real-world Phase 2 control —
  campaign against the broken rev must yield `observation-mismatch`.
- **Phase 2 defect sourcing from GoLean's bug ledger**: their
  `docs/BUGS.md` holds 15 FIXED differential-pinned fidelity bugs (and
  8 open), each with pinning case IDs — checking out pre-fix revisions
  gives real historical defects with known signatures, exactly the
  "historical clone bugs" the audit's Phase 2 asks for, at zero
  fabrication cost. Measure cases-to-first-detection per defect.
- **Ladder prioritization from GoLean's coverage ledger**
  (`docs/coverage-ledger.md` there): their machine's supported surface
  far EXCEEDS grossmith's grammar — multi-assign/tuple evaluation
  order, labels/goto/fallthrough, type switches, constants/iota,
  closures, pointers, floats/complex, variadics, embedding, generics,
  range-over-int/func are all `active`/`partial` for them and
  ungenerated by us. For those areas grossmith's grammar is the binding
  constraint on finding their bugs; rungs there pay immediately, while
  rungs in their `missing`/deferred areas only yield frontend-export
  noise. Concretely near-term (all deterministic, all in the audit's
  Phase 4 dependency order): multi-assign evaluation order, labeled
  break/continue + goto/fallthrough, type switches, richer constant
  contexts. For the floats rung, adopt their bit-pattern +
  NaN-canonicalization equivalence (their harness already implements
  it). Also: reuse their ledger status vocabulary
  (active/partial/deferred-*/missing/unexpressible) for grossmith's
  Phase 4 spec-surface ledger so the two ledgers can be joined for
  campaign planning.
- **Membership lane for deliberate nondeterminism**: grossmith
  currently quotients all nondeterminism away by construction (map
  range only as commutative fold). GoLean's landed membership lane
  (`docs/2026-08-04_membership-lane-design.md` there) is a
  set-membership oracle for genuinely choice-dependent observables —
  the concrete consumer the "revisit nondeterminism later" plan was
  waiting for. grossmith could emit nondet-tagged variants (raw map
  range order) as `lane=membership` manifest rows with the mandated
  `why`. Their gate rejects singleton-set membership cases, so the
  generator must guarantee genuine multiplicity.
- **Mine GoLean's semantic-edges catalogue as a corner/rung
  specification** (`Corpus/challenges/semantic-edges/manifest.tsv`
  there): 98 curated "weird Go" edge cases with themes and status
  (27 active-covered, 3 candidate, 68 future for them). This is a
  ready-made edge-case inventory for charter #3 ("edge cases are
  hunted, not hoped for") — classify each entry as generated-today /
  named-corner candidate / rung-blocked / quotiented-by-design (with
  the reason) / out-of-scope, and fold the mapping into the Phase 4
  spec ledger. Their future→active promotions are a standing demand
  signal for grossmith rungs. Specific nuggets already visible:
  - **g32 three-index slicing relaxes the cap quotient** — DELIVERED
    (the `slice_triple` rung, 2026-08-07): `s[a:b:c]` pins cap, so
    append reallocation becomes DETERMINISTIC — the shared-backing
    aliasing family (g03, g25, c18, c21) is exercised as an atomic
    derive/append/fold emission; the post-reallocation append tail
    stays quotiented (see the Slices ledger row).
  - Deterministic corners over the EXISTING grammar: shadowing
    depth (g14), wide/negative shift counts (g31), defer-in-loop
    accumulation (g18), string(int) rune conversion (g30).
  - Small deterministic rungs grossmith lacks entirely: string
    indexing/slicing/range (byte-offset semantics — c12/c13/g09) —
    DELIVERED (the strings rung, 2026-08-07; type switches c03 landed
    the same day); named results with defer mutation (g06/c54), method
    values copying their receiver (g16/c53), `copy` builtin (g33),
    uncomparable-dynamic-type interface comparison panics (g11/g17,
    needs non-comparable payloads first).
  - Cross-references that land in EXISTING backlog items: nil-vs-empty
    slice (g13/c22 = our L1 deferral), panic(nil) (c36 = the panic(v)
    deferral; note GoLean pins GODEBUG=panicnil=0), map iteration
    order (g34 = membership lane), unused locals (c43 = negative
    generator), tuple assignment order / labels / type switches /
    constants / embedding / variadics / generics (= the ledger-driven
    rung list).
- **Near-miss negative generator**: GoLean maintains a whole
  compile-negative corpus (`Corpus/coverage/negative/compile/...`) to
  pin frontend REJECTION behavior. grossmith's trap catalogue
  (salvage notes) is precisely a list of one-mutation-from-legal
  compile errors it steers around — inverting each rule (constant
  overflow, unused variable, duplicate case, impossible assertion,
  goto-over-declaration...) generates negative cases by construction,
  a corpus their negative lane can consume directly. New generator
  axis; needs its own expected-error taxonomy.
- **Parameterized subjects**: GoLean's harness natively drives int
  arguments (`arg_ints` per manifest row); grossmith subjects are
  nullary, so every value is baked in. One subject × N argument
  vectors would multiply behavioral coverage per generated program,
  exercise their argument plumbing, and make boundary-value sweeps
  (MinInt, -1, 0, 1, MaxInt over the same body) a manifest concern
  instead of N generated programs.
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
  cases into GoLean's corpus. Adopt GoLean's `check-bugs.sh` pattern:
  pinned entries are machine-cross-checked against the latest run so a
  pin can neither rot in prose nor silently outlive its evidence.
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
