# String observations

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

The generator's optional `string_bytes` construct enables string slice bounds
that split a UTF-8 sequence. Profiles can exclude it independently of ordinary
string slicing. GoLean supports these cases through its own byte-array string
channel, witnessed against its real Go and Lean execution paths.
