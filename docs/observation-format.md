# String and panic-message observations

`grossmith-observation-v2` string values preserve Go's byte-string semantics.
They carry `kind: "string"` and their static `goType`, plus one payload:

- `str`: a JSON string for valid UTF-8. The empty string can omit this field.
- `strBytes`: base64-encoded bytes for invalid UTF-8, with `str` absent.

For example, the two halves of `"µ"` are distinct observations:

```json
{"kind":"string","goType":"string","strBytes":"wg=="}
{"kind":"string","goType":"string","strBytes":"tQ=="}
```

The second payload is an additive extension. Existing valid UTF-8 observations
retain their representation; older strict readers reject `strBytes` instead
of silently replacing bytes. New readers reject mixed payloads (including an
explicitly empty `str`), null payloads, numeric arrays in `strBytes`, and
`strBytes` for valid UTF-8, keeping the representation canonical.

`strBytes` and `messageBytes` must be canonical standard base64 (RFC 4648
alphabet with `=` padding): the exact text `base64.StdEncoding` produces for
the bytes. Non-zero padding bits (`"wh=="` for the byte `c2`, whose canonical
form is `"wg=="`), embedded CR/LF, and missing padding are rejected, so every
byte string has exactly one spelling.

No value field may be an explicit JSON `null`: `kind`, `goType`, `bool`,
`int`, `uint`, `str`, `strBytes`, `len`, `elems`, `fields`, `entries`,
`dynType`, and `payload`, nor the `name`/`value` of a struct field or the
`key`/`value` of a map entry. A zero payload is encoded by omitting its
field (`{"kind":"int","goType":"int"}` is the integer 0); the explicit zero
spelling (`"int":0`, `"elems":[]`) is also read, but `"int":null` is not,
because it would otherwise decode to the same zero as a real 0. Numeric
fields must be JSON integers in range (no fraction or exponent).

The same encoding applies to returned values, observation events, and strings
nested in arrays, slices, structs, maps, or interfaces. String map keys are
ordered by their original bytes. Use `observe.StringValue(goType, s)` to build
a string value and `Value.StringData()` to retrieve its original bytes.

JSON evidence must contain valid Unicode: raw invalid UTF-8 and unpaired
surrogate escapes are rejected, along with duplicate fields and trailing
content. Arbitrary Go bytes belong in `strBytes`, not malformed JSON text.
Schema field spelling is exact, including Unicode case variants. Open map
keys remain case-sensitive; external GoLean documents may add unknown fields,
but an alias such as `Status` cannot overwrite the recognized `status` field.

The generator's optional `string_bytes` construct enables string slice bounds
that split a UTF-8 sequence. Profiles can exclude it independently of ordinary
string slicing. GoLean supports these cases through its own byte-array string
channel, witnessed against its real Go and Lean execution paths.

Panic payloads, both top-level and in recovered events, use the same lossless
principle. They carry `kind` and exactly one message payload:

- `message`: a valid UTF-8 JSON string, including an explicitly present `""`.
- `messageBytes`: base64-encoded invalid UTF-8 bytes, with `message` absent.

Missing messages, null payloads, and mixed representations are rejected.
`observe.Panicked` and `observe.NewPanicInfo` construct these payloads;
`PanicInfo.MessageData()` returns the original bytes. A direct struct literal
with a nonempty UTF-8 `Message` remains supported. Empty strings should use the
constructor so that their presence is explicit; invalid UTF-8 in the ordinary
`Message` field is rejected before serialization or comparison.

The `exact` policy compares original bytes; `kind` ignores messages in both
top-level and recovered panics. This is an additive protocol extension: older
readers reject `messageBytes`, and older readers that required nonempty messages
also reject the now-supported explicit empty message.

GoLean's manifest and nested Go oracle cannot express all of these messages
exactly across the supported checkouts. Empty messages, invalid UTF-8, the `-`
sentinel, and messages containing any U+0000–U+001F control character receive a
clone infrastructure verdict, with the reason recorded. Some older GoLean
panic encoders leave JSON control characters unescaped; their comparator parse
failures must not become semantic mismatches. Direct gc comparisons preserve
these cases as semantic observations. Report verification recomputes this
refusal: under the GoLean policy, a recorded reference panic whose message
GoLean cannot represent must carry the clone infrastructure verdict.

## Batch report checks related to these payloads

`gengo -verify` reads `batch.json` with the same strict decoder and also
requires:

- Fixed-length arrays have exactly their length; `seeds` is two integers.
  (encoding/json would otherwise drop extra elements or zero-fill missing ones.)
- `verdicts`, `composition` and `compositionJudged` list exactly the keys that
  occur, with positive counts; a zero-count entry is rejected.
- Under the GoLean policy, the reference side of each verdict matches the
  recorded reference outcome: a reference that did not run, or produced an
  error document, has verdict `ref-infra`, and no other case has `ref-infra` or
  `both-infra`.
- A `gc` or `gc-386` clone records `cloneOracle`, and `cloneIdentity` equals
  the identity derived from its version, path, `GOARCH` and `gcflags`
  (`gc-386` requires `GOARCH` `386`). Reports written before `cloneOracle`
  existed do not verify; other clones must not record `cloneOracle`.
- The GoLean work root contains only `cases/`, the three digested run files,
  and the undigested run diagnostics (`.golean-work`, `diff-coverage.log`,
  `artifacts/`, `goshim/`).
