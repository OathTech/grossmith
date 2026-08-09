# Backlog

An issue inventory. Ordering here is not commitment;
`docs/roadmap.md` is the ordering authority (the 2026-08-06 audit
phase plan that used to govern is closed — delivered in full).

## Customer-prioritized: GoLean's generation requests (2026-08-07)

STATUS (Phase 4 arc, 2026-08-07): R1, R2a, R3, R5, R4 DELIVERED as
rungs 1-5 (see docs/spec-ledger.md and the rung commits); the
composition-histogram backlog item was absorbed and closed by rung 5.
The recover WRAPPER observes panics at function level — SITE-encoded
since 2026-08-08 (wrapperCaught + wrapperJudged in batch.json); the guarded-STATEMENT recovered-event rung below
remains open — a different mechanism, not absorbed. Remaining from the request set:
R6 only (embedding matrices — blocked on rungs); R2b was DELIVERED in
the witness arc (its entry below records the details).

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
- **R2b order-witnessing generation**: DELIVERED (witness arc W2,
  `order_witness` — mechanism 1 of the effect-discipline design):
  package accumulator `wOrd` + the designated impure helper `wit`,
  wrapped 2-in-3 at call arguments, min/max arguments, multi-assign
  right sides, index operands, and comparison/equality operands
  (composing with `&&`/`||` into short-circuit witnesses), inside the
  new `order` corner (fourth named corner, 1-in-8; the corner
  force-enables its instrument tag, masks early_return — the arity
  hazard — and biases site-bearing arms). Trailing observed slot after
  the aggregates, snapshotted by the wrapper defer (E3 truncation
  witnessed at exact accumulator values). Campaign at their tip
  3d21582: 282 match / 18 clone-infra (all the short-circuit
  quarantine; 10 of them witnessed subjects — expected, classified),
  28/38 witnessed subjects semantically judged MATCH. Remaining from
  the design: mechanism 2 (pointer-parameter witness), per-type wit
  helpers beyond plain int. Mid-arc review response: exact three-leg
  wrapper gate (wrapperCloneInfra), Validate rejects the vacuous/
  overriding corner configs, witness_shortcircuit stratifier, corner
  collateral documented in the ledger (early_return masked -> dead_code
  -86% in-corner, ~12% corpus-wide).
- **width_dependent precision** (W4): DELIVERED AS SOUND-FLOOR, target
  recorded as missed honestly. Static magnitude bounds (value.bound /
  binding.bound, 0=unknown-conservative, loop-carried writes multiplied
  by the config's worst-case execution count) gate the marks; the
  unconditional keep-set is the *31 folds, unsigned complement,
  signed→unsigned platform conversions, and risky platform division
  (the historically-burned MinInt/-1 family — kept conservative on
  purpose). The ARC-END review then confirmed three under-tag
  mechanisms in the first cut (unsigned negation/subtraction underflow
  — width-sized at any magnitude, invisible to a magnitude algebra;
  conditional writes replacing instead of joining bounds; loop-body
  staleness) with reproduced 32-vs-64 divergences; all fixed
  (unconditional unsigned-minus keep-set, condDepth joins, loop writes
  poison to unknown except construction-derived "+fixed"
  contributions, the soundness screen widened 25→120 samples over the
  breach range). Final saturation 87.5% (n=2000) vs 97.6% before;
  off-tag population 2.4% → 12.5% (5x the cross-arch discrimination
  coverage). The charter's <50%
  aspiration is UNREACHABLE without under-tagging: measured
  decomposition over 1000 seeds — 287/828 tagged programs carry
  window-reaching fold constructs, 537 carry boundary-literal or
  unknown-value arithmetic that genuinely diverges (boundary literals
  are a deliberate near-universal corpus feature), 4 other. Soundness
  witnesses: untagged programs observe only in-window values (runtime
  screen), saturation regression guard both directions. The cross-arch
  CI job remains the oracle; the revert condition stands. Follow-up
  lever if sharper stratification is ever needed: split the tag by
  cause (fold / boundary-arith / unknown-chain) rather than pushing
  the rate down.
- **gengo's local cross-arch printout is vacuously reassuring**
  (arc-end review finding 10, pre-existing): "divergences in-tag 0,
  off-tag 0" prints even when zero cases were judged — the CI job is
  correctly guarded, only the human-facing print conflates
  generated-with-judged. Small print-side fix when picked up.
- **nonConstExpr pureMode fallback lacks a conversions gate** (mid-arc
  review, pre-existing note): the `T(pureBase)` fallback emits a
  conversion without gating or marking `conversions` — visible inside
  any helper body under a conversions-excluded profile. Small honesty
  fix; touches the constant-overflow safety path, so it wants its own
  witness when picked up.
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
- **width_dependent precision rung** (2026-08-08 hunt F10): the tag
  saturates at ~98% of programs, so the cross-arch tag-honesty proof
  is near-vacuous — almost any divergence is in-tag by construction.
  markWidthDep fires on every plain-int touch; precision means firing
  only where a value can actually differ across widths. Until then the
  yield number is coverage, not specificity (noted in the ledger).
- **Guarded-statement observation portability** (hunt F12): guardedStmt
  still uses the p.(error)+Error() pattern the wrapper dropped — fine
  for gc (its only profile today, by exclusion), but the site-encoding
  trick applies there too the day any clone gains obs events; and
  recovered-events is structurally 0 in every golean campaign, which
  the artifact does not say.
- **Element-inclusive range folds** (hunt F14 residual): range-over-
  slice bodies fold only the INDEX (consumeIndex), so a changed
  element value is invisible to that path — slice_triple's shared
  write was judged match through an element-blind observation in 2 of
  5 shared emissions. Folding elements where the elem type allows
  closes it; interacts with the wrapper arity discipline (no mid-body
  aggObserved flips), so it needs the declare-time route.
- **GoLean channel width erasure** (hunt F15, recorded not fixable
  here): their observation encoder collapses every integer kind to one
  tag and drops defined-type identity except for structs/interfaces —
  kind-defaulting bugs that land on the right VALUE are invisible on
  that channel, while our composition reports widths/defined_types as
  coverage. Ledger row notes it; the durable fix is their encoder
  (adapter shape (a) territory) — worth passing to their team.
- **Tuple-forwarded call arguments rung** (2026-08-08 review §1):
  DELIVERED (witness arc W0, `tuple_forward` — sink(src()) pairs across
  the dest×ptype matrix, minimal variadic-parameter sinks, all eight
  cells witnessed plus per-cell runtime pins). The fix-pair
  cross-validation over GoLean's
  BUG-049 fix pair (2e05313 pre → 264520e post, same 300 subjects,
  seeds 84000..84299) flipped 16 verdicts clone-infra→match, every one
  tuple_forward-tagged with an any-slot sink and the pre-fix
  "type assertion from non-interface value" signature; zero flips
  elsewhere; cases-to-first-detection 22. General variadic calls/spread
  remain deferred on the ledger (g24, c55).
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
