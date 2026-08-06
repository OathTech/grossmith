# Design draft: observation protocol v2 and runtime adapters (Phase 1)

**Status: DRAFT for discussion — nothing below is implemented.**
Responds to audit findings C1 (no clone adapter), C2 (println is not a
conformance protocol), C3 (pinning/panic policy unimplemented), C4
(interface payload dropped), C5 (runtime failure conflated with
divergence), H2 (ephemeral conformance statement). First clone customer:
GoLean (confirmed 2026-08-06).

## 1. Case layout: subject / driver split

Each generated case directory becomes:

```
case_00042/
  subject.go     import-free: preamble types, helpers, methods, fuzzSubject.
                 NO func main, NO println. Observation events go through the
                 obs* API (§3), which the subject DECLARES nothing for — it
                 just calls them.
  driver_gc.go   generated reference driver (same package): implements the
                 obs* API, calls fuzzSubject, recovers panics, emits ONE
                 observation document (§2) on stdout. May import fmt/os/
                 strconv — the driver is adapter territory, the subject is
                 not.
  case.json      per-case record: schema version, id, seed, generator
                 revision, resolved config (swarm mix, corner), source
                 hash, draw trace, features+counts.
```

`go run ./case_00042` stays a one-command reproduction (both files build
together). A clone either (a) consumes `subject.go` with its own driver
implementing the same obs* API and document schema, or (b) is driven by
its own harness (GoLean today, §5).

## 2. Observation document: `grossmith-observation-v2`

Salvages the prototype's `observe` package (typed JSON, fail-closed
parse) and extends it:

```json
{
  "schema": "grossmith-observation-v2",
  "status": "ok | panic | error",
  "events":  [ {"at": "point|defer|recovered", "value": <Value>}, ... ],
  "values":  [ <Value>, ... ],          // the returned tuple, absent on panic
  "panic":   { "kind": "<PanicKind>", "message": "<raw text>" },
  "error":   { "kind": "compile|timeout|no-observation|...", "detail": "..." }
}
```

`Value` is the prototype's width-preserving shape (`kind`, `goType`,
payload) with three additions:

- `slice`: `{kind: "slice", goType, len, elems: [...]}`
- `map`: `{kind: "map", goType, len, entries: [{key, value}...]}` — entries
  are the case's PROBED alphabet keys in alphabet order (deterministic).
- `interface`: `{kind: "interface", goType, dynType: "T0", value: <Value>}`
  — dynamic type AND payload, fixing C4's injectivity violation.

Events are ordered and typed: observation points and defer prints become
`events` instead of raw println lines; a recovered catch is an event with
the panic kind/message, and execution continues. Temporal information
(what printed before a panic) is preserved structurally.

`PanicKind` is a closed set: `divide`, `index-out-of-range`,
`slice-bounds`, `interface-conversion`, `nil-dereference`, `other`. The
reference driver derives kind from the recovered `runtime.Error` (message
mapping is a DRIVER duty — each adapter's driver owns its
implementation's mapping into the shared taxonomy; the raw message rides
along untouched).

## 3. The obs* API (subject-side, import-free)

The versioned protocol includes a small closed function set the subject
may call and every driver must provide:

```go
func obsBool(v bool)        func obsInt(v int64)     func obsUint(v uint64)
func obsStr(v string)
```

Generated observation points and defers call these (with generated
conversions for sized/named types — evaluation-at-defer-time semantics
preserved). The subject stays import-free; the reference driver
implements them by appending typed events. Injectivity holds per event
because each carries goType from the call site's generated conversion.

## 4. Equivalence policy and verdicts

Two policies, both implemented (C3): `panic-exact` (kind AND message
byte-equal) and `panic-kind` (kind only; message informative). Everything
else stays byte equality over the typed document (canonical JSON
serialization defined by the schema: sorted keys, no floats yet).

Per-case verdict is a closed taxonomy (C5):

```
match | observation-mismatch |
reference-infra-failure | clone-infra-failure | both-infra-failure |
harness-error
```

Discrimination yield, tag honesty, and the conformance statement are
computed ONLY over cases where both adapters ran. Cases are matched by
(id, subject-source hash) — never by array position.

## 5. Runtime adapters

```go
type Adapter interface {
    Name() string
    Identity(ctx context.Context) (string, error) // toolchain path+version / clone commit; persisted
    Run(ctx context.Context, caseDir string) Outcome
}
type Outcome struct {
    Status      OutcomeStatus // ran | build-failed | run-failed | timeout | adapter-error
    Observation observe.Observation
    Detail      string
}
```

Two concrete adapters, no plugin framework (audit's advice):

- **gc reference adapter**: builds subject+driver with the sanitized env,
  toolchain taken from an explicit configured path (PINNED — identity
  persisted, not just recorded), runs with empty env + timeout, parses
  stdout fail-closed.
- **GoLean clone adapter** — two candidate shapes, DECISION NEEDED:
  - **(b) reclaim the prototype adapter** (lowest risk, proven 110/110):
    grossmith writes GoLean's manifest rows (id, go_dir, function,
    expected_status, features) and invokes `scripts/diff-coverage`; the
    comparison happens INSIDE GoLean's harness against `go run`, and
    grossmith parses the closed result vocabulary into verdicts. Fast to
    working, but asymmetric: GoLean never emits an observation document,
    and the expected_status pre-pass means grossmith's own gc adapter runs
    anyway.
  - **(a) symmetric document emission**: GoLean-side work to emit
    observation-v2 documents (its native frontend/stepper evaluating the
    subject and serializing); grossmith compares documents under the
    policy. The end-state shape; requires GoLean changes.
  - Proposed: ship (b) first as the Phase 1 vertical slice, with the
    schema designed so (a) is a drop-in replacement later.

## 6. Persistent artifacts (H2)

- `batch.json` (versioned): generator revision + config, both adapter
  identities, equivalence policy, per-case verdicts, aggregate counts,
  timestamps. The conformance statement IS this file; stdout is a view.
- `case.json` per case (§1): everything needed to regenerate and re-judge
  without implicit seed-range conventions (also Phase 3's replay input).

## 7. Migration and scope

- `conform` refactors around Adapter + verdicts; the current
  gc/GOARCH path becomes the reference adapter used twice (the cross-arch
  experiment survives as a degenerate clone).
- The println driver is replaced by driver_gc.go; witness tests update
  (driver assertions, masking metrics move to events).
- CLI: `-clone golean:<path-to-clone-checkout>`, `-go <path>` (pinning),
  `-panic-policy kind|exact`; input validation + tests (H5) ride along.
- NOT in scope: plugin registry, per-step observation, replay (Phase 3),
  interface payload witness beyond the schema itself (Phase 2 adds the
  positive controls and planted defects).

## 8. Open questions (for discussion before implementation)

1. **GoLean integration shape**: is (b)-then-(a) right, and is GoLean's
   `scripts/diff-coverage` + corpus-format interface still current? Where
   does the GoLean checkout live for the adapter (config path)?
2. **GoLean observable set**: the old brief said its harness reflects over
   bool/sized ints/string/array/named struct/named interface and fails
   closed on slices, maps, pointers. If still true, the adapter must
   restrict generation (a Constructs profile per adapter?) or GoLean's
   harness must grow — which?
3. **Panic-kind taxonomy**: is the six-kind closed set right for GoLean's
   current panic vocabulary?
4. **Canonical JSON**: acceptable as the wire format for GoLean to
   eventually emit (shape (a)), or would a line-oriented format be easier
   on the Lean side?
