*CLOSED/HISTORICAL (2026-08-09): delivered. Caveat of record: defer/recover was
unmeasurable in this phase (later dissolved by the R1 recover wrapper), and
recovered-event coverage remains open — `docs/roadmap.md` (R7).*

# Phase 2 design: observation sensitivity (2026-08-07)

Audit Phase 2, verbatim done-when: "every supported observation shape
has a targeted unequal-state witness and the end-to-end production path
detects the selected defects." Two slices.

## Slice 1: the positive-control matrix (per-shape doctored clones)

One end-to-end witness per observation shape: a clone whose driver is
doctored to perturb EXACTLY that shape must yield `observation-mismatch`
on at least one case that exercises the shape, and must never leak an
infra or harness verdict. The undoctored clone stays all-match
(specificity — already witnessed in TestVerdictTaxonomy).

Mechanism: the existing doctored-tree pattern — copy the case tree, edit
the clone's `driver.go` by exact string replacement (the replacement
must apply, else the driver drifted and the witness fails loudly),
judge reference-vs-doctored per case.

The matrix (perturbation → shape):

| control | replacement target | shape proven injective end-to-end |
|---|---|---|
| int | `v.Int()` negated | int values (and interface payloads via recursion) |
| uint | `v.Uint()` xor 1 | uint values |
| bool | `v.Bool()` negated | bool values |
| string | `v.String()` + suffix | string values |
| len | `v.Len()` + 1 | slice/array/map length |
| field-name | struct field name + suffix | struct field identity |
| map-order | sort comparators flipped | map entry canonical order |
| dynType | dynType + suffix | interface dynamic type |
| event-order | event append → prepend | event ordering |
| panic-kind | divide → other | panic taxonomy |
| width | goType int8 → int16 | width preservation (goType) |

Interface payload-ONLY (same dynType, different payload) is witnessed at
the Equal level (TestInterfacePayloadIsObservable); end-to-end the
payload flows through the int/uint arms, which the matrix perturbs.

Pool construction is requirement-driven, not seed-lucky: generate
seeds from a fixed base, keep a case iff it satisfies a still-unmet
requirement (shape present in its REFERENCE document — int8 goType,
map with >=2 entries, >=2 events, a divide panic, ...), stop when all
requirements are met (hard cap, loud failure listing what is missing).
Deterministic, minimal, and robust to generator drift: if a future
change stops producing a shape, the witness fails at pool-build with
the shape's name instead of silently thinning the matrix.

## Slice 2: historical-defect campaigns (GoLean bug ledger)

GoLean's docs/BUGS.md carries 15 FIXED differential-pinned fidelity
bugs. Four are squarely inside grossmith's generated grammar:

- BUG-001 — struct-field/array-element WRITE lowered the address base
  as a value (grossmith: fieldAssign/elemAssign).
- BUG-006 — interface slots held RAW values, no conversion wrap
  (grossmith: iface boxing of defined types).
- BUG-012 — a bare call statement discarding results went stuck
  (grossmith: callStmt).
- BUG-021 — append-spill capacity envelope too narrow (grossmith:
  append).

Method — differential of differentials, which makes attribution exact:
run the SAME batch (same seeds) against the pre-fix and post-fix
checkouts of each bug's fix commit; any per-case verdict change between
the two runs is attributable to that fix. Report cases-to-first-
detection (first seed whose verdict differs) and the detected-verdict
class (some of these bugs manifest as STUCK → clone-infra-failure in
our mapping, not observation-mismatch — the measurement records which,
because "grossmith surfaces this defect as a visible red" is the claim,
not "as a mismatch specifically").

An UNDETECTED in-grammar defect is the most valuable outcome: it names
a generator weakness (shape unobserved, construct pattern ungenerated)
and feeds the ladder/corner priority list with evidence.

Deferred within Phase 2: the defined-type ++/-- natural control
(blocks on GoLean's fix landing); planted defects beyond the historical
set (fabricate only if the historical four leave a family unmeasured).
