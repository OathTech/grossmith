# The witness arc: charter (2026-08-09)

PURPOSE. grossmith is a Go program generator used for differential
conformance testing: it generates small Go programs, runs them under
the reference Go toolchain and under a Go reimplementation (GoLean),
and reports where the two disagree. This charter plans quality
assurance and capability work on grossmith itself.

A long arc, designed for autonomous execution against a standing goal.
Everything a rung needs decided is decided HERE, so no rung stops to
ask; the exit conditions say when to stop anyway. All work accumulates
on branch `witness-arc`; nothing merges to main until the arc-end
sign-off. Mission: implement the agreed effect discipline's first
mechanism (R2b order witnessing), close the observation-integrity debts
the 2026-08-08 review recorded, and bank the two historical-defect
measurements GoLean's fresh tip just made available.

## Ground rules (unchanged, restated for the record)

- The three by-construction invariants are non-negotiable; the effect
  discipline's E1-E4 rules (docs/2026-08-07_effect-discipline-design.md
  §2, signed off 2026-08-08) govern every effectful construct.
- Every emitter gates AND marks exactly what it emits; phantom reads
  discharged; one hot site per statement; constness follows operands;
  ledger row + witness in the same commit as the construct; size
  budget bumped consciously or not at all.
- Campaign after every rung (n=300 vs deps/golean, now at their tip
  3d21582 — the residual-failure floor is ONE class, the short-circuit
  quarantine; any other failure is signal and outranks the next rung).
  gc-386 runtime proofs are CI-only on this host.
- deps/golean is read-only. Scratch in .tmp/. Worktree subagents may
  parallelize rungs marked [DELEGABLE]; the main session reviews every
  delegated branch before folding it in.

## Rungs, in order

**W0 — the tuple-forwarding rung + fix-pair cross-validation.** [DELEGABLE]
Generate `sink(pair())`-shaped calls: multi-result helper calls
forwarded as complete argument lists, across the six-form matrix from
the 2026-08-08 review §1 (fixed/variadic/mixed destinations ×
interface/concrete parameter types — variadic helpers do not exist
yet; add the minimal variadic-PARAMETER form needed, helpers stay
pure). Tag `tuple_forward`. Witnesses: emission shapes + a runtime
witness per matrix row. Then CROSS-VALIDATION across their fix
pair: campaign the same seeds against `e49ebb1` (pre-fix) and
`eb0406f` (post-fix) in a scratch clone — the differential-of-
differentials method used for BUG-012. Verdict changes attributable to
the fix demonstrate the rung reaches the shape their defect lived in.

**W1 — the BUG-042 historical-defect measurement.** The long-promised
Phase-2 debt:
campaign the kinds corner across their 042/043 fix pair (their
2026-08-07 channels-arc-maint commits; locate the pair with git log,
pre = fix^). Expect the stuck family to flip; record
cases-to-first-detection in a short campaign note. No generator
changes; this rung is measurement only.

**W2 — R2b order witnessing (the core).** Per the design note, all
decisions pre-made:
- Mechanism 1: package var `var wOrd int` + impure helper
  `func wit(x int, tag int) int { wOrd = wOrd*31 + tag; return x }`
  (name-collision-check against generated identifiers; only int-typed
  wrapping in v1 — wrapping other types needs per-type helpers, add
  ONLY intTypes-shaped wit for now and note the extension).
- The `order` corner (fourth named corner; swarm arm alongside
  boundary/kinds at the same 1-in-8 class weight): densifies
  witness-wrapping at evaluation-order-rich sites — multi-assign RHS
  and index operands, call arguments, short-circuit operands (these
  land in GoLean's quarantine initially — expected, classified,
  fine). Outside the corner, wrapping is OFF (instruments are
  minorities; the corner IS the minority mechanism).
- Observation: `wOrd` snapshots into a trailing observed int slot at
  the final return (the qP/agg pattern; arity discipline per the
  existing three-slot precedents — the wrapper defer's slot arithmetic
  must account for it, same hazard class as rung 4's, and the
  handcrafted site witness pattern applies).
- E4's BRIEF amendment lands in the same commit (pre-agreed wording:
  helpers are pure EXCEPT designated witness helpers, individually
  tagged, effects confined to the witness accumulator, calls
  spec-ordered — cite the design note).
- Panic interplay per E3: a panic mid-expression truncates the
  accumulator — under the recover wrapper this composes (order-before-
  panic + site + partial state); a witness pins one handcrafted case.
- Witnesses: emission + determinism (the accumulator is identical
  across runs — covered by conformance witnesses but pin one explicit
  repeat-run case) + the E3 composition case + ledger row + witness
  tag in Optional().

**W3 — element-inclusive range folds** (survey finding F14 residual). Range
bodies over int-element slices may fold `base[i]` alongside the index
(declare-time decision, NEVER a mid-body aggObserved flip — the
wrapper/early-return arity hazard is documented in TODO; the safe
route is extending consumeIndex's fold expression, which changes no
tuple arity at all). Re-measure slice_triple's shared-write
observation reach; expect the element-blind judged-match cases to
become genuinely discriminating.

**W4 — width_dependent precision** (survey finding F10). markWidthDep currently
fires on every plain-int touch (~98% saturation). Precision: fire only
where a value can genuinely differ across 32/64-bit — platform-width
arithmetic that can overflow int32, conversions from platform width,
the aggregate/witness folds — not on bounded literals or sub-width
kinds. Measure the new saturation (target: meaningfully below 50%);
the cross-arch CI job's in-tag/off-tag then means specificity. Risk:
UNDER-tagging breaks the discrimination proof loudly in CI — if the
first CI run after this rung shows off-tag divergences, revert the
rung rather than patch forward, and record the failure analysis.

**W5 (stretch) — membership-lane emission, design only.** Write the
design note for emitting `lane=membership` rows (raw map-range
observables, genuine multiplicity per their gate, the `why` column,
verdict mapping for their membership stages — currently unmapped-by-
design in golean.judge). Implementation is the NEXT arc; this rung
ends at a reviewable note. If W0-W4 consumed the arc's budget, skip
W5 entirely — it is a stretch, not a debt.

## Code-review plan (pre-authorized by the goal that adopts this charter)

- Mid-arc: after W2, a single Opus code review scoped to W0-W2 (the
  effect-discipline implementation is the trust-surface change; scope
  it to determinism of the witness under E1-E4, the fourth
  trailing-slot arity interplay, and the corner's containment —
  witness text must not appear outside the corner).
- Arc end: a single Opus code review of the full branch diff, plus a
  survey pass reusing the three defect classes the 2026-08-08 review
  identified (generated-vs-judged coverage, environment sensitivity,
  metric honesty) over anything the arc added.
- Findings fixed on-branch; review-response commits as usual. The
  merge itself still waits for the user.

## Exit conditions (in addition to the goal's own)

- A campaign failure outside the short-circuit quarantine: STOP the
  rung queue and diagnose (if it needs minimizing, that is the
  shrinker's trigger condition — building the shrinker then outranks
  everything, per the replay design's standing rule).
- W4's revert condition fires twice: stop, record, ask.
- The witness mechanism turns out to need a type the discipline can't
  order (a genuine E1-E4 gap): stop at a design note, do not improvise
  semantics.

## Non-goals

Pointer-parameter witness (mechanism 2), closures, R6 embedding,
floats, the shrinker (absent a trigger), concurrency, guarded-
statement re-encoding (survey finding F12 stays recorded), string-variable
slicing. Membership-lane IMPLEMENTATION.
