*ACTIVE (2026-08-09): the evidence arc's honest status record. The arc is
NOT complete and `evidence-arc` is NOT merge-ready. Sequencing:
`docs/roadmap.md`; charter: `docs/2026-08-09_evidence-arc-charter.md`.*

# Evidence arc: status at arc-end review (2026-08-09)

The arc-end review (branch `evidence-arc`, `a6ad853..47999ad`) returned
**seven blocking findings and a do-not-merge recommendation**. Two of the
charter's own exit conditions are unmet, and one is refuted by
MEASUREMENT rather than by argument.

This document exists because the findings would otherwise live only in a
review transcript while the repository kept asserting the claims they
refute. Nothing here is fixed. Everything here is logged.

## Refuted: E4's executed-statement bound does not hold

E4's commit claims "the executed-statement bound closes". It does not.

The growth mask (`appendableSlices`) closes appends **lexically inside**
an open range body. Generation is a single pass over emitted source
order; EXECUTION re-runs that source under every enclosing loop. So an
append emitted AFTER a range, inside a body that repeats, grows the
container the range will walk on the next iteration — while the range's
emission-time gate already passed against the pre-growth bound:

```go
for i0 := 0; i0 < 239; i0++ {
    for i1 := range v12 { ... }          // emitted first: gate sees len 4, passes
    for i2 := 0; i2 < 167; i2++ {
        v12 = append(v12, ...)           // emitted second: no range open, allowed
    }
}
```

Measured at `Stmts=1, Depth=2, LoopCap=250` — a config `Config.Validate`
ACCEPTS — seed 174813 executes **14,372,767 statements against the
4,000,000 the validator guarantees** (3.59x), with the stale range
reaching **39,750 trips against a claimed per-level factor of 2,000**
(19.9x). Arithmetically self-checked: `4 + 238*167 = 39750`.

Not chance, and not a rare interleaving: across 60,000 seeds producing 43
matching programs, **43/43 had the append emitted after the range, zero
before** — exactly what single-pass emission ordering predicts.
`DefaultConfig` is comfortably safe (max trip 7 against 48); the nested
stale shape appears at `Depth=3` at roughly 2 in 300,000 seeds.

This is the SAME error class the mid-arc review caught in W4's bound
tracking (a bound read at emission time describing state that execution
changes later). It was fixed there, in `writeBound`, and then
reintroduced days later in E4's new range gate. Worth recording as a
pattern, not just an incident: any gate that consults a static bound at
emission must account for the emitted statement being re-executed.

## Incomplete: the growth mask is slice-only

String growth has no mask at all. `s += ...` (`compoundAssign`) is
ungated even while a slice range is open, and `stringRangeFold` picks any
string variable with no length gate — no analogue of `rangeableVars`'
8*LoopCap check. The same growth-feeds-a-range family E4 closed for
slices is entirely open for strings, and it occurs naturally: **17 of
3,000 `DefaultConfig` seeds** emit a string range over a concat-grown
string.

Escape routes the reviewer checked and found genuinely closed, worth
keeping recorded so the next rung does not re-litigate them: only two
`append` sites are ever emitted; `assign`/`compoundAssign` exclude
slices; no `copy` is emitted; helper and method parameters are scalars
only, so a grown slice cannot be laundered through a helper into an
ungated range; the `slice_triple` temp cannot grow any binding. The
source-size cap also survived attack by `Depth` (worst subject past
`Validate`: 0.35 MB).

## Unmet: E3's "prove which bytes both adapters executed"

- **`batch.json` is undigested.** The reviewer rewrote `total: 99999`,
  fabricated verdicts, and a false `referenceIdentity` into a published
  batch; `gengo -verify` returned **exit 0** with "every input digest
  intact". `manifest.tsv` is unbound the same way. This contradicts the
  E3 bullet "the batch report binds to the descriptor digest".
- **`golean-work/` is outside the descriptor.** It is allowlisted at the
  root and never descended into — but it contains the `main.go` the
  CLONE actually compiled. The reference side is digested; the clone side
  is not, so the exit condition's "both adapters" is false.
- **The corruption detector crashes on corruption.** `complete.json`
  containing `{}` panics `gengo -verify` (unguarded `[:12]` slice)
  instead of refusing. The shipped witness used a 16-character stand-in
  digest — exactly long enough to step over the bug.
- **The completion binding is fail-open.** The check sits inside
  `if ... err == nil`, so an unreadable `complete.json` skips the gate
  and still prints "completion bound". `schema` is decoded but never
  compared; no `DisallowUnknownFields`.

## Unmet: identity probes are not bounded

`GcAdapter.Identity` — the probe `RunBatch` calls to gate EVERY batch —
has no budget at all (measured still running at 45s). `Oracle` got the
30s budget but neither got `killGroup`/`WaitDelay`, so a child holding
the pipe defeats even that (measured: blocked at 50s, child alive). This
is the audit-P1 failure mode E4 claims to close, surviving on the path
that gates production. Of twelve `exec.Cmd` sites, three are hardened;
four use bare `exec.Command` with no context.

## Narrowed, not fixed: mid-arc findings 2, 4, 6, 8

My own claim that mid-arc findings "1-8 fixed on-branch" is corrected
here: **1, 3, 5, 7 and 13a verify clean; 2, 4, 6 and 8 were narrowed**
with residuals that were recorded nowhere until now:

- Recovery ownership is a filename test, not a content test — the marker
  is written but never read back, bound to no out-dir, batch or run. A
  leftover `<out>.prev` containing a file merely NAMED `manifest.json` is
  renamed over `out`. Root-level symlinks under allowlisted names are
  accepted, including `manifest.json` itself.
- `dirtyContentHash` still has content-blind paths: files inside an
  untracked DIRECTORY collapse to one `?? newdir/` line (the same class
  the fix closed, one level up), and git-quoted paths `continue`
  silently. `-uall -z` fixes both.

## Also recorded

- **W4 saturation drifted and its recorded figure is stale.** E4's wider
  `maxExec` is directionally safe (over-approximates, so no
  under-tagging, and the soundness screen passes: 120 untagged programs,
  1,457 values, all in-window). But saturation moved 86.65% -> 88.90%
  (n=2000), and on the test's own window 91.50% -> 93.75% against a hard
  threshold of 0.94 — **passing by 0.25 percentage points**, with the
  docstring still citing 87.5%. The off-tag population, which is the
  denominator the cross-arch discrimination job runs against, shrank ~15%
  relative and is recorded nowhere.
- **Two witnesses assert less than they read.** The flood witness checks
  `Detail` contains "cap", which a slice-capacity panic satisfies without
  flooding a byte; the group-kill assertion sits inside `if err == nil`
  on reading the pid file, so the one assertion distinguishing E4 from
  plain `CommandContext` skips silently if the stub never writes it.
- **The build-timeout message names a duration that was not the cause.**
  An already-expired parent also reports `DeadlineExceeded`, so a 3s
  parent cancellation prints "build timeout after 2m0s". `BuildTimeout`
  is a const exercised by nothing in the suite.
- **Budget persistence is incomplete**: golean's 16MB script cap is an
  inline literal absent from `BatchBudgets`, `LakeBuildTimeout` (20m, the
  largest budget in the system) is unrecorded, and `RunBatch` cannot
  state `runTimeout` itself.
- **`setsid()` escapees leak** and cost a silent 5s `WaitDelay` each, so
  the real worst case is `budget + 5s`. Off unix, `killGroup` is a total
  no-op that drops `WaitDelay` along with `Setpgid`.
- **E2's witness is environment-fragile**: `goroot()` hard-codes
  `/usr/local/go` and `t.Skip`s if absent, so the arc's P0 trust witness
  can silently vanish on a differently-laid-out host. A silent skip is
  the wrong failure mode for a P0 witness.
- **E1 changed what the sensitivity matrix proves** (`len` and
  `map-order` now resolve to clone-infra rather than semantic mismatch).
  Defensible under the lanes doctrine and documented in the test, but the
  closed Phase 2 doc still reads "delivered" against a done-when of
  "every supported observation shape has a targeted unequal-state
  witness".
- **Membership proof completeness nit**: the correction covers the two
  forced entries in isolation; with other surviving entries the
  difference is `(31^i - 31^j)*d`, not `30*d` — still nonzero at
  realistic map sizes, but not what the note says.
- The E4 witnesses live in `harness/manifest_test.go` (the E3 file); CI
  never exercises `gengo -verify`, and its race leg is `-short`, which
  skips both E4 witnesses.

## What survived review

Recorded so the next rung does not redo it: E1's tagged-union gate closes
on all three paths (all 90 kind x field combinations refuse, on `Parse`,
`Equal` AND `Judge`, landing as `harness-error` — the zero-valued
foreign-field hole was probed specifically and is not one, since
`omitempty` makes a zero field byte-identical to an absent one); E3's
atomic publish survived a `kill -9` sweep with the previous batch intact
every time; E4's process-group cancellation is load-bearing (removing it,
the harness blocks past 20s and leaks); the W4 soundness screen holds;
symlinked case dirs, roots, files and dangling links all refuse before
any adapter runs (spy adapter: 0 invocations); budget values read the
same constants the code uses, with no duplication; the E0 docs fold is
solid and TODO honesty passed.

## E5 progress (2026-08-09, the rung now running)

The user agreed to keep the arc whole; E5 executes under the charter's
new rung. Recorded as items land:

- **A1 mechanism CLOSED.** The loop-nest freeze (`loopFrozenSlices`)
  bans appends to a nest-ranged slice until the nest closes. Witnesses:
  `TestNoAppendAfterRangeInLoopNest` (structural, over DefaultConfig
  and the review's config; catches seed 174813 exactly when the freeze
  is disabled), `TestLoopNestFreezeMechanism` (white-box), and
  `TestExecutedStatementsMeasured` (instrumented replay: the
  counterexample neighborhood peaks at 123,825 executed statements
  against the review's 14,372,767).
- **Incidence corrected.** Our own re-measurement of the pre-freeze
  generator at the review's config finds the append-after-range shape
  in exactly ONE program in seeds 150,000-210,000 — seed 174813
  itself, reproduced byte-for-byte. The review's "43 matching programs,
  43/43 append-after" was evidently a broader filter; the direction of
  its ordering claim stands (the converse order is blocked by the gate,
  since an in-loop append makes the slice unrangeable), but the
  frequency this document previously implied was ours to check and is
  now measured.
- **A2 CLOSED.** Strings carry a byte-length bound (`value.strLen`,
  `binding.maxLenBound`) tracked at every write site with a fail-safe
  unknown default at the `writeBound` chokepoint; string ranges take
  variable operands only at the subject's top level under that bound,
  and literals inside loops and pure bodies (helper string parameters
  have caller-decided lengths). Witness:
  `TestStringRangeOperandsGated`.
- **Found in passing, fixed with the above:** the map-fold bound
  conflated its own trip count with site executions; `projectInner`
  read declaration-time bounds for an inner variable the block may
  have written (under-tagging ints, and unsound for string lengths);
  `maxExec` at Depth=0 under-multiplied helper-internal loop writes
  (helper bodies nest one loop level regardless of Depth — floored).
- **A1's universal-bound half STOPPED at a design note**
  (`docs/2026-08-09_execution-bound-design-note.md`), per the
  charter's exit condition. An honest closed form must price block
  branching, fold trips, and calls (helper loops in 15.9% of
  DefaultConfig programs, helper-calls-helper in 15.2%, ~7.5 nested
  call sites per program), and cannot fit the 4e6 ceiling without
  grammar restrictions nowhere pre-authorized. The note recommends an
  emission-time cost budget; `Validate`'s formula is marked a
  plausibility screen until the user decides. E4's exit condition
  stays UNMET on this half.
- **B1-B4 CLOSED.** `complete.json` digests `batch.json` and
  `manifest.tsv` (the reviewer's rewritten-report reproduction is a
  refusal, library-level and end-to-end through the CLI, and CI now
  runs `-verify` plus one edit it must catch); the clone's per-case
  compiled source and work files are recorded in `batch.json`
  (`CloneSourceSHA256`, `CloneWorkFiles`) and re-checked offline by
  `golean.VerifyWork`; the completion check fails closed (unreadable =
  refusal, schema compared, strict decode); every digest read from
  disk must be digest-shaped, which also removes the `[:12]` crash —
  witnessed with the `{}` descriptor the old 16-character stand-in
  stepped over. Legacy batches verify with their reduced scope stated
  plainly.
- **C1 CLOSED.** All fourteen exec sites carry a budget and group
  cancellation: `Identity`/`Oracle` (witnessed with stalled and
  pipe-holding stubs against both), golean's and gengo's git probes,
  and a hard outer ceiling on the diff-coverage invocation.
  `IdentityTimeout`, the clone log cap (previously an inline 16MB
  literal), the lake budget, and the run ceiling join the persisted
  `BatchBudgets`. Off unix, `killGroup` keeps the portable `WaitDelay`
  backstop.
- **Measurements and witnesses re-trued.** W4 saturation re-measured
  after the per-site `maxExec` re-derivation: 87.30% (n=2000), 91% on
  the test window — margin back to ~3pp, off-tag population ~12.7%,
  soundness screen green (120 untagged programs, 1,526 values,
  all in-window); docstring now carries the measurement history. The
  flood witness asserts the full flood phrase against a 32MB-by-
  construction writer; the group-kill witness fails when the pid file
  is missing; the build-timeout message names the deadline that
  actually expired (`BuildBudget` makes the budget path exercisable,
  and both paths are asserted); `goroot()`'s hard-coded path is gone —
  the E2 witness resolves the real toolchain before reordering PATH
  and is fatal, never a skip.
- **Narrowed mid-arc residuals closed.** Recovery ownership is a
  content test: `<out>.prev` must parse as a gengo manifest, and the
  staging marker binds its out dir (witnessed: a file merely named
  manifest.json, and a marker for a different out dir, both refuse
  untouched). `dirtyContentHash` uses `-uall -z`, so files inside
  untracked directories and git-quoted paths are hashed (witnessed).
  Root-level symlinks under allowlisted names refuse. The membership
  note's multiplicity proof is re-corrected for general positions
  (odd term difference; 2-adic valuation of 31^k - 1 at most 7 at
  alphabet sizes); the Phase 2 banner states the E1 narrowing.
- **Explicitly deferred, with reasons:** `setsid()` escapees still
  leak until process exit and cost a silent 5s `WaitDelay` each (the
  real worst case is budget + 5s) — closing that needs PID-namespace
  or cgroup supervision, out of the arc's scope; recorded here rather
  than half-built. The `.staging`-holds-a-finished-batch crash window
  (TODO item 11) and the validate-vs-build edit window remain recorded
  residuals for R4's content-addressed snapshots.

## The E5 scope (agreed 2026-08-09; progress above)

As drafted before agreement: every finding has a local fix; none
suggests a redesign. (A1's bound half has since proven the exception —
see the design note referenced above.)

1. **A1** — defer the range gate to a post-pass over the emitted body, or
   widen `maxLenBound` for any slice appended-to anywhere in an enclosing
   loop. Witness must be the measured counterexample, not a static shape
   check.
2. **A2** — bring strings under both rules slices got: a growth mask and
   a length gate on `stringRangeFold`.
3. **B1-B4** — digest `batch.json`/`manifest.tsv`, bring `golean-work/`
   under the descriptor (or narrow the exit condition to the reference
   side and say so), guard the `[:12]` slice, close the fail-open.
4. **C1** — budget and group-kill `Identity`, and sweep the twelve
   `exec.Cmd` sites.
5. Re-measure and re-state the W4 saturation figures; strengthen the two
   weak witnesses; then re-review.
