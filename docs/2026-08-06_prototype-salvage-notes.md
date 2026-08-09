*REFERENCE (2026-08-09): input record — the trap catalogue and survey
conclusions the code cites; not a plan. Sequencing: `docs/roadmap.md`.*

# Prototype salvage notes: the trap catalogue and survey conclusions

The grossmith 2.0 generator is built on lessons from the frozen
`grossmith-proto` prototype (2026-08-01..05, Apache-2.0, same authors; its
`LESSONS.md` is the master copy) and its four-generator survey (csmith,
YARPGen, gosmith, rustlantis — read for architecture, adversarially
reviewed). `deps/` checkouts are gitignored, so this tracked note records
the conclusions the code cites (audit finding H4).

## The Go trap catalogue (validity by construction)

Each entry was paid for with a prototype debugging session or audit
finding. Violating any of these emits non-compiling programs.

- **Constant expressions are the big one.** Go REJECTS overflowing typed
  constant expressions at compile time (`int8(50)*int8(50)` is an error,
  not a wrap). Track constness through every operator; every
  overflow-capable operator needs a non-constant operand. A naive
  generator loses 30-50% of programs here.
- **Constant CONTEXTS are a family**, not one rule: array indices, array
  lengths, shift counts, case labels, conversion operands — out-of-range
  constants are compile errors in all of them; the same expression with a
  variable is legal (and merely panics or wraps at runtime).
- **Unused anything is fatal**: variables, imports, labels. (Precisely: a
  local must be READ; pure assignment does not count, `x += e` and element
  and field writes do. Params and receivers are exempt.)
- **Termination is yours to guarantee**: literal loop bounds, `+1` steps,
  never assign the index in the body; acyclic call graphs; and (2.0's own
  addition) resource termination — an inner range over a slice re-evaluates
  `len`, so range+append composes into `len·2^len` executed statements.
- **Terminal statements** (`break`/`continue`/`return`) mid-block make the
  rest unreachable — legal, but it silently deadens generated code (34% of
  prototype loops computed nothing until fixed).
- **Switch**: duplicate constant case values are compile errors (draw
  without replacement); labels drawn from a wide range are never *hit*.
- **Zero values are hostile**: a struct's zero value contains nil maps
  (writes panic), nil slices (index panics), nil funcs.
- **Panics are defined behavior, not UB**: division by zero, bounds, nil
  deref panic deterministically. Decide panic/no-panic ON PURPOSE. But
  panic IDENTITY across two hot sites in one statement is
  spec-unspecified — at most one hot site per statement, or quotient.
- **Evaluation order**: function calls are ordered left-to-right; OTHER
  operand order is unspecified. Until an effect discipline exists, side
  effects in statement position only (2.0: pure helpers/methods dissolve
  this for calls).
- **`cap` after `append` is unspecified** — never observe it; whether
  append reallocates is unspecified, so alias + append has unspecified
  write visibility (2.0: every slice owns its backing).
- **Impossible type assertions are COMPILE errors**: asserting a type that
  provably lacks the interface's methods does not build; only legal
  implementers may be named.
- Misc verified edges: `byte`/`uint8` and `rune`/`int32` are the same type
  (structural type identity, never pointer identity); `goto` may not jump
  over declarations; `min`/`max` builtins require go1.21; `print`/`println`
  are implementation-specific (a gc debug channel, not a conformance
  observation — audit C2).

## Survey conclusions that shaped the design

- **One choice primitive: mask, renormalize, draw** — never rejection
  loops (csmith and gosmith both retry-sample and cannot state their
  realized distribution; csmith's docs warn zeroing a probability can hang
  it). Minting is an arm; every type carries a cheap total literal.
- **Determinism cannot be retrofitted**: gosmith admitted nondeterminism
  on day one and its oracle ceiling was crash-finding forever (50+ bugs,
  all crashes); rustlantis constructed it out and 24 of its 32 trophies
  were silent miscompilations.
- **Population diversity over per-program cramming** (swarm testing,
  validated by csmith's drivers); **generate toward named corners**
  (YARPGen, 9.14x measured by optimizer-counter ablation — line coverage
  is the wrong metric for this tool class).
- **Small programs**: divergence value comes from construct composition;
  csmith's multi-KB outputs mandate external reduction machinery.
- **Everything the generator knows survives as data** (csmith emits
  width-dependence as a prose comment; we emit tags with counts).
- **Choice-sequence reduction** (the Hypothesis reducer; Xsmith's Clotho):
  shrink the draw trace and regenerate — every candidate valid by
  construction. Requires a replay source first (audit Phase 3).
- **The process lesson** (prototype §6): no gate, meter, audit layer, or
  process rule without a named incident that demanded it. The prototype
  died of assurance machinery; 2.0's grammar sprint outran its product
  boundary instead — same failure shape, different axis (audit 2026-08-06).
