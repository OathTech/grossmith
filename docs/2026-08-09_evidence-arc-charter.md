*ACTIVE (2026-08-09): governing the evidence arc, branch `evidence-arc`.
The arc is NOT complete — the arc-end review returned seven blocking
findings and two of the exit conditions below are UNMET (E3's and E4's).
Status of record: `docs/2026-08-09_evidence-arc-status.md`. Sequencing:
`docs/roadmap.md`.*

# The evidence arc: charter (2026-08-09)

PURPOSE. grossmith is a Go program generator used for differential
conformance testing: it generates small Go programs, runs them under
the reference Go toolchain and under a Go reimplementation (GoLean),
and reports where the two disagree. The 2026-08-09 comprehensive
technical audit (`docs/2026-08-09_comprehensive-technical-audit.md`)
found that the EVIDENCE BOUNDARY — not the generator — is the weak
layer: a campaign's verdict can be computed against an unrecorded
nested Go oracle, its judged builds are not bound to its recorded
identities, malformed observation documents can judge as matches, the
public config has panic cases, and the resource proof has a gap. Every
finding was independently replicated (or amended with the replication
noted) before this charter was drafted. This arc makes a campaign an
immutable, self-consistent, reproducible experiment. It adds no
language surface.

Scope = the audit's R0-R3 restricted to replicated findings, plus the
small honesty defects (its P2/P3 list) and one correction of ours (the
membership-lane multiplicity proof). R4-R7 are later arcs.

All work accumulates on branch `evidence-arc`; nothing merges to main
until the arc-end sign-off. Decisions are pre-made HERE; the exit
conditions say when to stop anyway.

## Wording

This arc's subject matter — file integrity, toolchain provenance,
process supervision — invites security-incident vocabulary, and the
first draft used it (decoy, injection, tampering, hostile). That
register is wrong for the work and is banned here: nothing in this
project defends against an adversary. It keeps a MEASUREMENT honest
against ordinary causes — a leftover file from an earlier run, a
second Go on the PATH, a hand-edit during debugging, a compiler that
wedges. Write those causes plainly: unlisted file, changed after
generation, PATH precedence, stalled, unbounded output. The same
convention already applies project-wide after the witness arc.

## Ground rules

- The three by-construction invariants and the E1-E4 effect discipline
  are unchanged. deps/golean is READ-ONLY: if a fix requires changing
  their script, the rung stops at a drafted request note instead.
- Every fix lands with its negative witness in the same commit — the
  audit's reproductions become permanent tests (its method IS the
  witness spec).
- Campaign after every harness-touching rung (n=300 vs deps/golean at
  tip; the residual-failure floor is the one short-circuit class, two
  reporting surfaces; anything else is signal and outranks the next
  rung).
- Additive artifact schemas only: new fields, no removals — GoLean
  vendors us.

## Rungs, in order

**E0 — claims and lifecycle reconciliation.** [DELEGABLE] Docs only.
- Create `docs/roadmap.md` as the single living roadmap (this arc, the
  deferred R4-R7, then the capability ladder). No dated plan calls
  itself governing; every closed plan gets a status banner
  (CLOSED/HISTORICAL, SUPERSEDED BY, residual-work pointer) — the
  audit's disposition list is the checklist.
- README/BRIEF/TODO/spec-ledger reconciled to the implementation
  (README still says Phase 4 is next; TODO's preamble calls R2b
  remaining forty lines before recording it delivered).
- The determinism claim rewritten everywhere it appears: "the STRICT
  lane is outcome-deterministic by construction; other lanes carry
  explicit lane-specific oracles" (the lanes doctrine, unchanged — the
  wording just stops overclaiming).
- The membership-lane note's multiplicity proof CORRECTED: distinct
  fold terms do not alone prove distinct outcomes mod 2^w
  (a*31+b ≡ b*31+a whenever a-b ≡ 0 mod 2^31); the note gains the
  bounded-difference condition (emit the two forced entries with
  |termA - termB| in (0, 2^31), which the emitter controls) and a
  BLOCKED-ON banner naming the GoLean reason-code contract.
Exit: the four living documents agree with the code and each other.

**E1 — fail-closed inputs.** Self-contained code fixes, each with the
audit's reproduction as its witness.
- `Config.Validate` checks `NoObserve`: closed shape enum, no
  duplicates; observation promotion made TOTAL for the resolved pool —
  an empty realized pool returns a typed error, never reaches
  `draw(0)`. Witness: the all-256-subsets matrix per seed sample plus
  `Shape(255)` rejection (replication: 16/256 subsets panicked).
- `observe.Document.Validate`: the document as a tagged union — status
  and payload cardinality (panic iff panic payload, error iff error
  payload, ok excludes both), event shape, kind-specific fields,
  nonempty goType, container-length consistency. Called from `Parse`
  AND from `Judge`/`Equal` (adapters hand back exported Documents).
  Invalid documents are infra failures, never matches or mismatches.
  Witness: a rejection table covering every forbidden combination
  (reproduced: panic-without-payload and empty-goType judged match).
- `PanicPolicy`: exhaustive switch; unknown values are errors, not
  silent exact comparison.
- golean API inputs validated: case IDs (shape, uniqueness, path
  containment), TSV fields escaped or rejected.
- CLI honesty smalls: `-go` preflighted before any write; the actual
  `-out` path printed; subject-size mean divided by stat successes;
  the gc-386 local summary prints the judged denominator and calls
  zero-judged an incomplete campaign (arc-end finding 10, now due).

**E2 — one pinned Go oracle.** The P0 trust fix.
- Resolve ONE absolute Go binary at startup (from `-go` or PATH,
  once); record its sha256, version string, GOOS/GOARCH, and module
  mode in `batch.json` as the structured reference identity.
- Thread the same binary through BOTH adapters. GoLean's script
  resolves `go` from PATH and deps/golean is read-only, so the
  mechanism is a PATH SHIM: the adapter builds a private first PATH
  entry containing exactly one `go` symlink to the pinned binary.
  `batch.json` gains the nested-oracle identity (same fields) plus the
  script's sha256.
- Module/language mode declared, not ambient: their oracle forces
  GO111MODULE=off; ours is module-mode with a hard-coded `go 1.26`.
  Record BOTH as explicit fields on the batch (making the language
  target a configurable campaign dimension is R5-territory; this arc
  records what is true).
Witness: a stand-in `go` placed EARLIER in PATH than the real one must
not be the toolchain a campaign uses (it records its own invocations,
so the witness proves both that ordinary lookup would have taken it and
that the pinned resolution does not). Build hosts routinely carry
several toolchains; the property is that the report names the one that
ran.

**E3 — experiment identity and atomicity.** The architecture rung.
- Batches build in a STAGING sibling directory and publish by atomic
  rename; a completion descriptor is written last; an interrupted run
  leaves the previous batch untouched. Symlinked case paths are
  rejected at every output component.
- The manifest becomes the authoritative batch descriptor: exact case
  IDs and per-file digests for the COMPLETE build-input set (subject,
  driver, per-case go.mod, anything else in the case root).
  `RunBatch` validates the descriptor before executing anything:
  extra, missing, duplicate, or digest-mismatched files refuse the
  batch (reproduced: an unlisted `extra.go` whose init panicked was
  compiled into the case and changed its outcome, while the recorded
  subject hash stayed clean).
  Both adapters run against the validated tree; the batch report
  binds to the descriptor digest, not just subject hashes.
- Replay joins strengthened: strict-decode case records, directory
  basename must equal the record ID, regenerated features/counts
  compared (not just source bytes), and non-ran outcomes compared as
  failure CLASS with the wording "same failure class", never
  "reproduced".
- Campaigns of record refuse a dirty generator or clone tree unless
  `-allow-dirty` is passed, and a dirty run records the tree's content
  hash rather than the bare `-dirty` label.
Exit (the audit's, in the arc's wording): extra, missing, or changed
files fail before execution; interruption preserves the previous batch;
offline inspection can prove which bytes both adapters executed.
UNMET at arc-end review: the first two hold (validation refuses before
any adapter runs; atomic publish survived a kill -9 sweep), the third
does not — `batch.json` and `manifest.tsv` are undigested and
`golean-work/`, which holds the source the CLONE compiled, sits outside
the descriptor. A rewritten conformance statement passes `gengo -verify`
with exit 0. Details and E5 scope: the status document.

**E4 — resource guarantees made true.** Measure first, then fix.
- MEASURE the cross-slice range/append amplification: instrument
  executed-statement counts over a large sweep (the pattern occurs
  naturally — replication found it in 4 of the first 41 seeds) and
  record the worst case observed at DefaultConfig.
- Then close it structurally: while ANY slice range is open, appends
  to ALL slices are masked (extending the existing rangedSlices
  mechanism from "the ranged slice" to "every slice"). Slice lengths
  are then bounded by appends executed under literal-bounded loops
  only, and the executed-statement bound is re-derived and documented
  in Validate. Arrays and the slice_triple controlled append are
  unaffected.
- RECONCILE W4: `maxExec` is a soundness dependency of the
  width_dependent bound tracking (the "+fixed" multiplier). The
  re-derived bound replaces the current formula there too, and the
  soundness screen re-runs.
- Phase budgets: the build gets its own deadline (today only the run
  is deadline-wrapped — a wedged compiler hangs a campaign); identity
  probes bounded; subprocesses killed by process group; compiler,
  GoLean, and subject streams capped with truncation recorded and
  overflow classified by phase. Budgets and durations persisted in
  the batch report.
- A direct source-size cap at generation (the size budget the tests
  already watch becomes a generation-time guarantee), and a
  Stmts-vs-source-cost sanity bound in Validate (Stmts=4e6, Depth=0
  currently passes).
Exit: the resource witnesses (cross-slice growth pattern, a compiler
that never returns, unbounded subject output) return typed errors or
stay inside measured, persisted budgets.
UNMET at arc-end review, and refuted by MEASUREMENT: the re-derived
executed-statement bound does not hold (14,372,767 statements measured
against the 4e6 Validate guarantees, at a config Validate accepts),
because the growth mask is lexical while emitted source is re-executed
by enclosing loops; and the mask is slice-only, leaving string
concat-into-string-range entirely ungated. `GcAdapter.Identity` — the
probe gating every batch — has no budget at all, so "identity probes
bounded" is also unmet. Details and E5 scope: the status document.

## Review plan (pre-authorized)

- Mid-arc after E3: one Opus code review scoped to E1-E3 — the trust
  surface. Bars: can a malformed document still reach a semantic
  verdict; can any file influence a build without being in the
  descriptor; can any code path still resolve `go` from ambient PATH;
  atomicity under kill -9 at arbitrary points.
- Arc end: one Opus review of the full branch diff plus a survey pass
  re-running the audit's own probes (malformed-document corpus,
  NoObserve matrix, PATH precedence, extra-file, interruption).
- Findings fixed on-branch; the merge itself waits for the user.

## Exit conditions

- Any fix that requires modifying deps/golean: stop that rung at a
  drafted request note to the GoLean team.
- A campaign failure outside the known quarantine class: stop and
  diagnose before continuing.
- E4's re-derived bound would invalidate existing W4 witnesses in a
  way that needs a design change (not a constant): stop at a design
  note.

## Non-goals

R4 (durable evidence corpus, CI matrices), R5 (machine-readable
ledger, pair accounting), R6 (membership-lane implementation), R7
(sensitivity and capability growth), all grammar expansion, the
shrinker (absent a trigger), and any change to deps/golean.
