*DRAFT (2026-10-08): design note for gc-verified arc rung G3, pointers
stage 0. This is mechanism 2 of `docs/2026-08-07_effect-discipline-design.md`.
Decisions marked ⚑ need Mike's sign-off before implementation.*

# Pointers, stage 0: design note

PURPOSE. grossmith is a Go program generator for differential conformance
testing. This note decides how pointers enter it while keeping its three
by-construction invariants: programs compile, halt, and (in the strict
lane) have deterministic outcomes. The consumer is gc-verified's first
memory stage, M-mem-0: pointers to locals that do not escape, and pointer
parameters into caller-owned objects.

## 1. What is new about pointers

Today the generator's state is a set of named local variables, and every
write names its target. Pointers introduce ALIASING: a write through `p`
changes `x` without naming it. Three consequences matter.

1. **Evaluation order.** Go fixes the order of calls within an
   expression but not the order of a variable read relative to a call.
   So `x + f(&x)`, where `f` writes through its parameter, has an
   unspecified outcome. The effect discipline (E1, E2) excludes exactly
   this shape. Pointers make it easy to emit by accident.
2. **Observation.** Addresses are never deterministic across runs or
   implementations, and GoLean refuses programs that observe them. A
   pointer must never be printed, returned from the subject, or folded
   into an observed value. Pointees and pointer equality are
   deterministic, with one exception: equality between distinct
   zero-size variables is unspecified. grossmith never declares
   zero-size variables (arrays have length 2 or more, structs 2 or more
   fields), and this note keeps it that way.
3. **Panics.** Dereferencing a nil pointer panics
   ("invalid memory address or nil pointer dereference", kind
   `nil-dereference` in the existing vocabulary).

## 2. Stage 0 constructs (all OPT-IN)

⚑ **Every pointer tag is opt-in** (`gen.OptIn()`, the mechanism
`variable_shift` introduced). Pointer tags are never swarm-drawn, so no
seed is re-mapped and GoLean campaigns are unaffected unless a profile
requests them.

| Tag | Emits | Determinism argument |
|---|---|---|
| `addr_local` | `p := &x`, `&s.f`, `&a[i]` for a local scalar, struct, or array element (constant or safe index; a raw index is a hot site under the existing risk rules) | Taking an address has no effect. Index operands follow the existing index policy. |
| `deref_read` | `*p` as an operand | A read, like reading a variable. The hazard is a concurrent write in the same expression, and stage 0 has no expression-position writes (below). |
| `deref_write` | `*p = e`, `*p op= e`, `(*p)++` in STATEMENT position only | Statement-position writes are ordered. In `*p = e` the right side is evaluated first, then the store, as for any assignment. Multi-assign targets like `*p, x = ...` with `p == &x` follow the spec's left-to-right phase-2 order, which grossmith already relies on. |
| `ptr_param` | helper and battery-function parameters of type `*T`, READ-ONLY in the callee; callers pass `&local` | A callee that only reads through its parameter has no effect, so the call can sit anywhere a pure call can (E1 holds trivially). |
| `nil_ptr` | `var p *T` (nil), and `p == nil` / `p != nil` comparisons | Comparison with nil is deterministic. Dereferencing a nil pointer is a hot site, gated by the risk budget like division by zero. |

`T` ranges over the existing scalar and struct types. ⚑ Pointer-to-pointer
and pointers to slices, maps, strings and interfaces are deferred: none of
them is needed for M-mem-0.

**Observation.** Pointer variables are never observed. Their pointees
already are, because every pointee is a local variable with its own
observation. A write through `p` therefore shows up in the final value of
`x`. A bool from `p == q` or `p == nil` can feed observed values like any
other bool.

**Halting.** No pointer construct adds a loop or a call that the
execution budget does not already price. A read-only pointer parameter
changes a helper's signature, not its cost.

## 3. Deferred to stage 1 (mechanism 2 proper): writes through calls

A helper that WRITES through a pointer parameter is an effectful call.
Under E2, the state it mutates may be read only at designated points,
never bare in the same expression. ⚑ Stage 1 rule: an effectful call
appears only as a statement, either bare (`f(&x)`) or as the whole right
side of an assignment whose target does not alias the pointee
(`y = f(&x)`, or `x = f(&x)`, where the spec orders the call before the
store). This is the "pointer-parameter witness" the effect-discipline
design anticipated. It also gives battery functions observable
post-states: the driver records the pointee after the call.

## 4. Deferred to later stages

- `ptr_escape`: `new(T)`, `&T{...}` returned or stored, and pointers
  returned from helpers (M-mem-1). Pointees stay observable; the
  pointers do not.
- Nil dereference beyond the risk-budget hot site, and pointer
  receivers on methods.
- Closures capturing locals (mechanism 3).

## 5. Battery functions with pointer parameters

gc-verified's R3 asks for "a description of the object graph to build"
for pointer parameters. In stage 0, with read-only scalar or struct
pointees, the description is just the pointee's value. ⚑ Proposal: a
parameter type `*T` in `functions.json` takes, as its input, the
pointee's value in T's existing encoding. The battery driver declares
`v := T(...)` and passes `&v`. This is additive within
`grossmith-functions-v1`: readers that check types refuse `*T` until
they support it, which fails closed. Struct pointees need a value
encoding first, so stage 0 battery pointer parameters are scalar-only.
In stage 1, a write-through parameter adds the pointee's post-call value
to each expectation.

## 6. gc-specific caveat for the consumer

"Does not escape" is a property of gc's escape analysis, not of the
source. grossmith can guarantee only the SOURCE-level shape: pointers
held in locals and passed to callees that neither store nor return them.
gc may still heap-allocate (for example, when a callee is not inlined and
its parameter is treated as leaking). gc-verified should classify by what
gc actually did; grossmith tags source shapes.

## 7. Witnesses (same commit as each construct, as always)

- No address reaches an observation: an AST scan finds no pointer-typed
  expression in any observed position, and no pointer printed or
  returned from the subject.
- Determinism: replay byte-identity, plus repeated runs and gc `-O` vs
  `-N -l` agreement over a seed sweep with every pointer tag requested.
- E1/E2 for stage 0: no expression contains both a write through a
  pointer and a read of its pointee (writes are statement-only by
  construction; a white-box check pins it).
- Opt-in: byte-identical output to the previous generator when no
  pointer tag is requested.
- Classifier cross-check: gc-verified's `srcclass` places pointer cases
  in M-mem-0 as defined there. Its current rules bar `pointer`, so they
  extend it first; the cross-check runs once they have.
