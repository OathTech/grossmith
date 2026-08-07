# The effect discipline and the observability horizon (2026-08-07)

Design note for R2b (order-witnessing generation) and the effect
discipline it requires — the gate the audit's Phase 4 placed in front
of pointers, closures, and package variables. Status: DRAFT for
discussion; decisions marked ⚑ need sign-off before implementation.

## 1. Framing: diversity versus observability

The project's foundational trade, stated plainly rather than embedded
in mechanism: every observability instrument risks narrowing the
generated language, and an instrument that quietly homogenized the
corpus would defeat the charter's coverage goal while appearing to
serve its determinism goal. Three principles govern every design in
this note (and, prospectively, future instruments):

1. **Invariant-forced exclusions are not costs.** A program whose
   OUTCOME depends on spec-unspecified behavior was never generable —
   outcome-determinism is a charter invariant, not a preference. The
   effect discipline excludes exactly the expressions where an
   effectful call co-occurs with a bare read of the state it mutates
   (Go's one unspecified-order effect hazard — Go already confines
   expression effects to calls/receives, so the discipline is nearly a
   description of Go, not a restriction of it).
2. **Conservative over-exclusion is a debt, tracked and ratcheted.**
   By-construction rules are sufficient conditions, not exact
   characterizations: deterministic programs exist that the rule
   cannot prove deterministic. Each such class is a LEDGER entry with
   a reason, and relaxations are deliberate rungs (precedent: g32
   carved the cap quotient). The first ratchet here is free —
   *interference separation*: an effect that touches only state
   nothing else reads (the witness accumulator) is order-observable
   and interference-immune simultaneously.
3. **Instruments are minorities.** Witness-wrapping changes programs
   (more calls; for a future optimizing clone, suppressed
   optimizations — the printf-debugging problem). The population
   structure absorbs this: witnessing is a named corner, the
   unwitnessed majority stays natural, and swarm diversity is the
   mechanism that lets instruments exist without homogenizing the
   corpus.

## 2. The discipline

- **E1**: every side effect in expression position lives inside a
  function call. (Statement-position effects keep their existing
  rules.) The Go spec orders all function calls, method calls, and
  receives lexically left-to-right during operand evaluation, so
  call-confined effects execute in a specified order everywhere they
  can appear — single expressions, multi-assign phase 1, index
  operands.
- **E2**: an effectful call's mutated state is read only at designated
  observation points (statement position), never bare inside an
  expression. This closes the one unspecified-order hazard. First
  instantiation: the witness accumulator, mutated only by the witness
  helper, read only at the final-observation snapshot.
- **E3**: panic interplay is a feature. A panic mid-expression skips
  the lexically-later witness calls; under the R1 recover wrapper the
  accumulator then reports HOW FAR evaluation proceeded — composing
  order-before-panic with R1's store-before-panic. Specified as
  intended behavior with a witness test, not left as an accident.
- **E4**: purity doctrine amendment (⚑ BRIEF edit): helpers are pure
  EXCEPT designated witness/effect helpers, individually tagged, whose
  effects satisfy E2. The blanket "pure helpers dissolve evaluation
  order" claim becomes "E1+E2 pin evaluation order"; the pure majority
  is unchanged.

## 3. Mechanism: three rungs, in machine-risk order

1. **Package-var witness (first, R2b's whole yield).**
   `var w int` + `func wit(x, tag int) int { w = w*31 + tag; return x }`;
   the generator wraps drawn subexpressions as `wit(<expr>, k)` with
   distinct tags. Non-commutative accumulation encodes REALIZED order;
   a clone that evaluates right-to-left, hoists, or reorders produces
   a different value. Observation: `w` snapshots into a trailing
   observed slot (the qP/agg pattern — arity discipline proven in
   rung 4's audit). Needs no pointers, no closures; GoLean's globals
   support is active. Targets their BUG-023/026/032 class.
2. **Pointer-parameter witness (second).** `wit(p *int, x, tag int)` —
   introduces pointer types through the already-proven discipline,
   as the deliberate first pointer rung rather than a side effect of
   one.
3. **Closures (third).** Captured-state witnesses; gated on their
   machine's closure support maturing and on our projection-rule
   interaction analysis.

⚑ Wrapping scope: a named corner `order` (dense wrapping concentrated
in evaluation-order-rich shapes — multi-assign, index chains,
short-circuit operands) rather than an always-on minority tag.
Rationale: principle 3, plus witness value concentrates where phase
semantics live. Short-circuit right-operands additionally encode WHICH
branch ran — noting that wrapped short-circuit forms initially land in
GoLean's frontend-quarantined class (visible clone-infra, fine).

## 4. The observability horizon (medium term)

User direction 2026-08-07: the COMPLETE commitment to outcome
observability is to be revisited in the medium term. The sanctioned
shape is a LANES model, not a loosened invariant:

- **Strict lane (default, unchanged):** outcome-deterministic by
  construction, byte-compared documents. Everything above lives here.
- **Membership lane (first relaxation):** programs with genuinely
  choice-dependent observables — raw map iteration order, append
  capacity envelopes — emitted deliberately, tagged, and judged by a
  set-membership oracle (every reference-observed behavior lies inside
  the clone's admitted set). GoLean's membership lane is live and is
  the concrete consumer; their gate rejects singleton sets, so
  emission must guarantee genuine multiplicity. This turns two of our
  standing quotients (map order, the BUG-021 capacity class) from
  unobservable into differently-observable.
- **Relational/permutation oracles (later):** order-insensitive
  equality, permutation matching — the forms concurrency would need.
  Concurrency itself stays out until both roadmaps say otherwise.

The strict lane remains the default and the bulk of the corpus; lanes
are per-case declarations in the manifest/record, never a global mode.
Determinism-by-construction remains the strict lane's invariant — the
revisit adds lanes beside it rather than weakening it.

## 5. Decisions

- ⚑ E4's BRIEF amendment (purity doctrine wording).
- ⚑ Mechanism order 1→2→3 as above.
- ⚑ `order` as a corner, not a tag.
- ⚑ The lanes model as the medium-term observability direction, with
  membership-lane emission as its first rung (sequenced after the
  witness rungs unless a campaign finding reorders it).
