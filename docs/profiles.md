# Construct profiles

`gengo -profile <file>` generates every case under a named construct
profile: an allow-list of grossmith's optional construct tags. Every
optional tag the file does not list is excluded. Profiles were built for
gc-verified's fragments (arc charter `docs/2026-10-07_gc-verified-arc-charter.md`,
rung G2), and work for any consumer that scopes by construct.

## File format (`grossmith-profile-v1`)

```json
{
 "schema": "grossmith-profile-v1",
 "name": "f1",
 "description": "free text",
 "constructs": ["if", "switch", "bitwise", "loops", "shifts"]
}
```

- `name`: 1-64 characters from `[A-Za-z0-9._-]`.
- `constructs`: optional tags from `gen.Optional()`. Unknown tags, core
  tags (always enabled: ints, bools, widths, assignment, literals,
  short declarations, the subject function, return, control flow, switch
  cases and default), and duplicates are refused. `[]` means core
  constructs only.
- Unknown fields, duplicate keys and trailing content are refused.

An allow-list fails safe: a tag grossmith adds later is excluded from every
existing profile until its owner lists it.

## What grossmith guarantees

- **By construction:** a disabled construct is never emitted; the
  generator panics instead. Swarm sampling still varies the mix within
  the allow-list, and corners whose instrument is excluded are not drawn.
- **Checked at generation:** a case whose subject or battery functions
  recorded an optional tag outside the profile fails the batch.
- **Recorded:** the profile file is copied verbatim into the batch root
  as `profile.json` and digested in `manifest.json`'s `rootFiles`. Every
  `case.json` names it (`"profile": {"name", "sha256"}`). The case's
  config records the resulting exclusions.
- **Re-derived by `gengo -verify`:** every case names the batch's
  profile, excludes every tag the profile does not allow, forces none,
  and recorded no optional construct outside it (subject features from
  `case.json`, battery features from `functions.json`).
- Combining: `-pairs` restricts its pair universe to the allow-list;
  `-clone golean` adds GoLean's own exclusions on top; `-check` refuses
  `-profile`.

## What a tag admits

A profile constrains grossmith's TAGS. Whether a tag's source forms fall
inside a consumer's fragment is the consumer's rule, so the consumer checks
the mapping with its own classifier. Examples of the mapping:
- `defined_types` admits named integer types;
- `methods` admits static calls to value-receiver methods;
- `range` ranges over literal arrays and strings;
- `strings` admits string literals in any expression, including constant
  ones such as `"go"[1]`;
- folds that observe slices and maps contain loops even when `loops` is
  excluded. They are emitted only when `slices` or `maps` is allowed.

## Cross-check against gc-verified's classifier (2026-10-08)

The fixtures in `gen/testdata/profiles/` (`f0`-`f3`) approximate
gc-verified's F0-F3. They are test fixtures; gc-verified owns its real
fragment files. For each profile we generated 150 cases with
`-functions 4 -function-panics biased` and classified every function with
gc-verified's `tools/differential/srcclass`.

| Profile | Battery functions | Helpers | Subjects | Outside target |
|---|---|---|---|---|
| f0 | 600 F0 | — | 150 F0 | 0 |
| f1 | 264 F0, 336 F1 | — | 29 F0, 121 F1 | 0 |
| f2 | 87 F0, 95 F1, 418 F2 | 22/32/156 (F0/F1/F2) | 6/17/127 | 0 |
| f3 | 99 F0, 81 F1, 420 F2 | 22/32/156 | 2/6/16/126 (F0-F3) | 0 |

Battery functions never reach F3 under f3: their bodies cannot declare
local arrays or structs yet (recorded G1 follow-up). Subjects do reach F3.
