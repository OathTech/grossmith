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
of silently replacing bytes. New readers reject mixed payloads and reject
`strBytes` for valid UTF-8, keeping the representation canonical.

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

GoLean's `expected_reason` manifest column cannot express all of these messages
exactly. Empty messages, invalid UTF-8, the `-` sentinel, and messages containing
NUL, tab, or line breaks receive a clone infrastructure verdict, with the reason
recorded. Direct gc comparisons preserve these cases as semantic observations.
