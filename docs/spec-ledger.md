# The spec-surface ledger

The Go language surface as grossmith's roadmap (audit H3/Phase 4): what
is generated, what is deliberately quotiented, what is deferred and WHY,
and what is out of scope. Status vocabulary is shared with GoLean's
coverage ledger so the two can be joined for campaign planning:
`supported` (generated, tagged, witnessed) | `partial` (important
subareas remain) | `deferred(reason)` | `out-of-scope`.

Maintenance rules: every `supported` row names its emission tags —
`TestLedgerNamesEveryTag` fails if an optional construct tag is missing
from this file. Every deferral names its reason; "deferred" without a
reason is a lint error by convention. Update the row in the same commit
that moves the surface. `sx:` references are GoLean's semantic-edges
catalogue (`Corpus/challenges/semantic-edges/manifest.tsv`, 98 entries);
their full disposition is the appendix.

## Types and values

| Area | Status | Tags / witnesses | Notes |
|---|---|---|---|
| Integer kinds (int, int8..64, uint, uint8..64) | supported | `ints`, `widths`, `width_dependent`; 386 typecheck witness | Width-preserving observation (`goType`); cross-arch quotient declared via tag. |
| Booleans | supported | `bools` | |
| Strings: literals, concat, len, compare | partial | `strings`, `concat`, `len`, `equality` | Linear-growth concat rule (space half of HALTS). Missing: index/slice/range over strings — byte-offset semantics, deterministic, a rung (sx: c12, c13, c14, g09). |
| Arrays | supported | `arrays`, `index`, `range` | Value semantics exercised via copy-on-assign contexts (sx: g08, g25, c19 covered by construction). |
| Slices | partial | `slices`, `append`, `index`, `len`, `range` | Owned backing, literal-born, never nil. QUOTIENTED: cap never observed, no whole-slice assignment, no aliasing (append realloc unspecified). Planned relaxation: three-index `s[a:b:c]` pins cap and makes the aliasing family deterministic (sx: g32, g03, c18, c21; TODO g32 item). `copy` builtin missing (sx: g33). Nil-vs-empty unobservable pending adapter shape (a) (sx: g13, c22). |
| Maps | partial | `maps`, `delete`, `comma_ok`, `range` | Literal-born (never nil — sx: g04 by construction), restricted key alphabets, full-map sorted observation. QUOTIENTED: iteration order — range only as the commutative fold (`map_range_fold`); membership-lane emission is the stronger future form (sx: g34). Missing: NaN keys (needs floats), map-element addressability negatives (sx: c16 → negative generator). |
| Named structs | supported | `structs`, `field` | Scalar/string fields only (profile-transitivity guarantee). Comparability panics via interface fields deferred(needs uncomparable payloads) (sx: g17). |
| Defined types | supported | `defined_types` | Integer-underlying only today. The R3 kind/definedness matrix corner (rung 3) sweeps these against every op site. |
| Interfaces | supported | `interfaces`, `assertion` | Named, satisfaction-by-construction, dynType+payload observed (C4 fix), empty interface included. Nil interfaces never generated (sx: g01, c03 deferred(nil-interface rung)). Comparison operators on interfaces deferred(panic-on-uncomparable needs those payloads) (sx: g11, c02). |
| Constants | partial | `literals`, `boundary` | Typed literals with constant-context discipline (the trap catalogue's core). Missing: untyped-constant arithmetic/adaptation, iota, grouped decls (sx: c07-c11, g12, c08) — enters with the R3 corner's in-kind sweep or its own rung. |
| Floats / complex | deferred(equivalence policy) | — | GoLean supports both with bit-pattern + NaN-canonicalized observation; adopting that equivalence is the entry condition (their float design note). |
| Pointers, closures, package vars | deferred(effect discipline) | — | The R2b order-witnessing design (audit Phase 4 #3) is the gate; nothing enters until the discipline is designed. (sx: g29, c45, c52, g16 partially.) |
| Generics, embedding, aliases, type params | deferred(rung order) | — | R6's matrices blocked on embedding + pointer receivers (sx: c04, c05, c56-c58, g17). |

## Statements and control flow

| Area | Status | Tags / witnesses | Notes |
|---|---|---|---|
| Declarations, short decls, block scoping | supported | `short_decl`, `block_decl` | Projection rule folds inner decls outward. Shadowing NOT generated — fresh names only; a corner candidate (sx: g14, c33). |
| Assignment: single, compound, inc/dec, multi-target | supported | `assignment`, `multi_assign` | Rung 2 (R2a): swaps (`a, b = b, a`), aliased targets (`u, a[u%N] = ...` — phase-1 index evaluation witnessed via final state; raw-index hot minority), mixed plain/element/blank targets, comma-ok and multi-result calls into element targets (sx: g19, c33 covered; `multi-assign/chain-field-over-index` chains through pointers remain deferred(effect discipline)). |
| `if` / bounded `for` / `range` | supported | `if`, `loops`, `range`, `control_flow` | Literal loop bounds (HALTS). Range-over-int deferred(language-version parity check first) (sx: c30). |
| `switch` | supported | `switch`, `cases`, `default`, `unreachable_case` | Constant labels, draw-without-replacement. Type switches deferred(rung; sx: c03), fallthrough deferred(low yield — GoLean non-ask adjacent; sx: c28). |
| `break` / `continue` (unlabeled) | supported | `break`, `continue` | Labeled forms + goto deferred(low yield — GoLean non-ask; sx: g23, c34). |
| Multiple return sites | supported | `return`, `early_return` | Named results enter with rung 1 (R1 wrapper; sx: c54, g06). |
| `defer` / `recover` | partial | `defer`, `recover`, `recover_wrapper` | Three forms: obs-event print defer + statement-level guarded catch (gc profile only — no obs* model in GoLean), and the R1 recover-observation WRAPPER (rung 1): named results + deferred recover encoding panic kind to an int result and snapshotting observed locals — pure Go, profile-safe, so GoLean campaigns now exercise defer/recover. Remaining: defer-in-loop corner (sx: g18), defer-during-panic (c37), `panic(v)` explicit deferred(panic-identity encoding; L2). |
| Helpers and calls | supported | `helpers`, `bare_call`, `functions` | Pure, acyclic, top-level; results assigned or discarded bare (BUG-012's shape). Recursion deferred(fuel design), variadics deferred(rung; sx: g24, c55), method values/expressions deferred(rung; sx: g16, c53). |
| Methods and dispatch | supported | `methods` | Pure value receivers on defined types; interface dispatch included. Pointer receivers deferred(effect discipline). |
| Panics (deliberate) | supported | `panic_risk`, `division`, `modulo`, `linearize` | One HOT site per statement (panic-identity quotient); linearized multi-trap pins order by sequencing. Kinds: divide, index, assert. |

## Operators and builtins

| Area | Status | Tags / witnesses | Notes |
|---|---|---|---|
| Arithmetic, bitwise, shifts, comparisons | supported | `division`, `modulo`, `bitwise`, `shifts`, `comparisons`, `equality`, `boundary` | Constant-overflow discipline throughout; boundary corner biases literals/divisors/shift counts. Wide/negative shift corner candidate (sx: g31). |
| Conversions | supported | `conversions` | R3's constraint honored going forward: sweeps need conversion-FREE in-kind paths (`int(x)` launders the kind-defaulting class). `string(int)` deferred(rung; sx: g30). |
| `min` / `max` | supported | `min`, `max` | |
| `len` | supported | `len` | Strings, slices, maps. `cap` quotiented (see slices). |
| Other builtins (copy, clear, new, make-with-cap) | deferred(per-builtin rungs) | — | (sx: g33, c60.) |

## Observation and population (not spec surface, but ledger-adjacent)

| Area | Status | Notes |
|---|---|---|
| Liveness tiers (observed/feeder/dead) | supported | `dead_code`, `dead_value`, `feeder_value`; aggregate observation of maps/slices under capability profiles arrives at rung 4 (R5). |
| Observation points | supported | `observe_point` — mid-function obs* events (gc profile; excluded under GoLean's until adapter shape (a)); event order and position are compared fields with sensitivity controls. |
| Corners | partial | `boundary` (`corner_boundary`: literals/divisors/shifts at type edges) and `kinds` (`corner_kinds`, rung 3/R3: conversion-FREE in-kind sweeps, dense inc/dec+compound sites, defined-type targets weighted up — the BUG-042/043 family's habitat); dead-rich and conversion-truncation remain planned. |
| Swarm | supported | Per-seed mixes; pairwise coverage objective arrives at rung 5 (R4). |

## Out of scope (deliberate, both roadmaps)

Goroutines, channels, select, sync (concurrency — GoLean's enumerator
covers it ahead of generation); unsafe; cgo; reflection; fmt/stdlib
behavior (sx: g13's JSON half, g15, g21, c39-c51 stdlib entries);
`print`/`println` as observation (spec-reserved, audit C2); imports and
multi-package programs (harness is single-file; revisit when GoLean's
multi-package frontend need matures).

## Appendix: semantic-edges disposition (all 98)

Grouped disposition of GoLean's catalogue; the named entries are the
representatives, membership per group verified against the manifest.

- **Covered by today's construction** (12): g03*, g04, g08, g10, g22,
  g25, g32*, g33*, c12-c14*, c18*, c19, c20*, c21*, c22*, g35 — the
  starred ones covered in their SAFE form only (the quotiented aliasing/
  cap/copy/string-index halves are the g32/copy/string rungs above).
- **Corner candidates over the existing grammar** (6): g14/c33
  (shadowing), g31 (wide/negative shifts), g18 (defer-in-loop — after
  rung 1), g30 (string(int)), c24/c25 (complement/bit-clear — present,
  corner-bias candidates), c35 (for-only-loop — trivially held).
- **Rung-blocked, scheduled this arc** (9): g19 (rung 2), g05/g06/c54
  (rung 1), c07-c11/g12/c08 (constants, with rung 3), g34 (rung 4's
  aggregate form; membership lane later).
- **Rung-blocked, later arcs** (31): strings family (g09, c12-c14 full
  forms), variadics (g24, c55), method values (g16, c53), nil-interface
  family (g01, c02, c03, g11, g17), pointers/closures (g29, c45, c52,
  g02), embedding/generics (c04-c06, c56-c58), type switches (c28),
  labels/goto (g23, c34), range-over-int/func (c30, c31), floats (g20,
  c23, c27), panic-nil (c36), builtins (c60), recursion-adjacent (g21's
  recursion half), naked returns (c54), defer-during-panic (c37, g28,
  c38's sequential half).
- **Negative-generator material** (4): c43 (unused locals), c16
  (map-element addressability), g37's compile-error half, c26 (no
  implicit conversion) — the trap-catalogue inversion item.
- **Out of scope** (36): every concurrency entry (g02's goroutine half,
  g07, g26, g27, g36, c38-c42, g34's scheduler half), stdlib/fmt/JSON/
  time (g13, g15, g21, c39-c42, c46-c51), unsafe/sizeof (c06's unsafe
  half), visibility/packages (c59, c44).
