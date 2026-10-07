# Battery functions and `functions.json`

`gengo -functions N` adds battery functions to every case: parameterised
top-level functions, each with a seeded input battery, and gc's outcome for
every input. They were built for gc-verified (arc charter:
`docs/2026-10-07_gc-verified-arc-charter.md`, rung G1). The subject program
is unchanged by the flag.

Flags:

- `-functions N`: functions per case, 0 to 64 (0 disables; default 0).
- `-battery-inputs K`: inputs per function, 1 to 1024 (default 32).
  `N × K` is capped so each call keeps a usable share of the execution
  budget.
- `-function-panics none|ordinary|biased`: density of sites that can panic
  (default `ordinary`). With `none`, no function can panic.

`-functions` needs a Go toolchain at generation time, because the expected
outcomes come from running the battery under the pinned `go` (`-go`, or
`go` from PATH). It cannot be combined with `-check` or `-clone golean`.

## Case layout

```
case_00001/
  subject.go  driver.go  case.json      (as before)
  functions.json                        (expected outcomes)
  battery/
    functions.go                        (the functions; compiles alone)
    battery_driver.go                   (gc reference driver)
```

The battery sources sit in their own directory because gc compiles every
`.go` file in a directory together. Next to `subject.go`, `functions.go`
would declare the helpers a second time.

`battery/functions.go` is `package main` with no imports, no package-level
variables, no `main` and no `init`. It holds the functions `bf0..bfN-1`
and every declaration they can reach (helpers, struct and defined types,
methods, interfaces). Declarations it does not need are legal Go. Use your
own driver, or `battery_driver.go`.

All five files are listed in `manifest.json` with their digests, so
`gengo -verify` refuses a batch if any of them changes, goes missing, or
gains a neighbour. Verification also checks that `functions.json` is
internally consistent and that it names the same battery sources as the
manifest. `gengo -replay <case>` regenerates the sources (they must match
byte for byte) and reruns the battery (the outcomes must match).

## Functions

- Parameters: the first is plain `int`, followed by up to three more from
  the integer kinds (`int`, `int8` … `uint64`) and `bool`.
- Results: one or two, from the same types.
- Bodies have no observation points, package state, recover or early
  return, so a call's outcome depends only on its arguments. They may call
  helpers.
- When panics are enabled, a statement holds at most one site that can
  panic, so which panic occurs is never left to evaluation order.
- Inputs: the first half of the battery walks edge values (0, ±1, the
  extremes and their neighbours, powers of two ±1 at byte and word
  boundaries). The second half is random: an edge value, a small value,
  or a uniform one.
- Platform `int` and `uint` are **64-bit** for batteries. The battery
  driver refuses to run on a 32-bit target.

## `functions.json` (`grossmith-functions-v1`)

```json
{
 "schema": "grossmith-functions-v1",
 "case": "case_00001",
 "functionsSha256": "<sha256 of battery/functions.go>",
 "batteryDriverSha256": "<sha256 of battery/battery_driver.go>",
 "oracle": {"path": "...", "sha256": "...", "version": "go version go1.26.5 linux/amd64",
            "goos": "linux", "goarch": "amd64", "moduleMode": "module (go 1.26)"},
 "functions": [
  {"function": "bf0", "params": ["int", "uint8"], "results": ["int64"],
   "features": {"division": 1, "panic_risk": 1},
   "inputs": [[0, 255], [-1, 3]],
   "expected": [{"ok": [0]},
                {"panic": {"kind": "divide", "message": "runtime error: integer divide by zero"}}]}
 ]
}
```

- `inputs[i]` and `expected[i]` belong to the same call. Values are bare
  JSON integers (in canonical decimal, exact for every int64 and uint64)
  and `true` / `false`, typed by `params` and `results`.
- Each expectation has exactly one of `ok` (the results, in order) or
  `panic`.
- `panic.kind` comes from the closed observation vocabulary: `divide`,
  `index-out-of-range`, `slice-bounds`, `interface-conversion`,
  `nil-dereference`, `other`.
- `panic.message` is the exact recovered runtime-error message: gc's
  stderr first line without the `panic: ` prefix. Battery bodies contain
  no explicit `panic(v)`, so every panic is a runtime error. A message
  that is not valid UTF-8 would appear as `messageBytes` (base64), as in
  the observation format.
- `features` are the construct and info tags the function's body
  recorded. The case's own `features` in `case.json` still describe the
  subject alone.

Readers in grossmith reject unknown fields, duplicate names, and values
outside their type or spelled non-canonically.
