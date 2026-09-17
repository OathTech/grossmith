# Pre-landing review response — 2026-09-17

Addresses F1–F4 in the
[review of `73e6f11`](2026-09-17_grossmith-improvements-prelanding-audit.md).

| Finding | Change | Regression evidence |
| --- | --- | --- |
| F1: GoLean ignores copied drivers | Directory checks against GoLean require a byte-identical current observation driver before staging. Other supplied drivers are refused; file mode explicitly selects a current driver. | The edited `fuzzSubject() + 1` driver is refused before output writes. An unchanged driver reaches a real GoLean match and its report verifies. |
| F2: panic-message bytes and empty messages | Top-level and recovered panic payloads preserve invalid UTF-8 as base64 `messageBytes`. Explicit empty strings are valid; missing/null messages remain invalid. Constructors and serializers enforce the same contract for in-memory documents. | Distinct invalid bytes mismatch under `exact`, match under `kind`, and round-trip; actual generated drivers preserve both top-level and recovered messages. Empty and byte-valued panics match gc against itself through `-check` and verify offline. |
| F3: destination-field aliases | Struct decoding enforces exact field spelling. GoLean retains unknown-field compatibility while refusing aliases of recognized fields. Map keys remain case-sensitive. | ASCII and Unicode aliases are refused in observations, outcomes, reports, case records, manifests, completion records, and external GoLean status documents. A report with honestly rebound digests still refuses a contradictory `total`/`Total` pair. |
| F4: unchecked provenance | Report and replay readers share `CaseRecord.ValidateOrigin`. Source checks require a recognized kind/driver, a nonempty path without NUL, zero seed, and no generated config, tape, or features. | Each contradiction is tested separately with rebound file digests. Both valid driver modes verify with a nonexistent original source path; legacy records without an origin retain their existing interpretation. |

The GoLean driver restriction is intentionally conservative: it does not claim
to establish equivalence of arbitrary driver implementations. Likewise, panic
messages that GoLean's manifest cannot represent exactly receive a clone
infrastructure verdict. These limitations are recorded in the CLI and protocol
documentation.

Validation completed on Go 1.26.5/linux-amd64 and GoLean
`3d2158224d06b6d7118c4f2af7a4c125c88768be`:

- Full `go test ./... -count=1` and `go vet ./...` passed.
- Race checks passed for `harness`, `golean`, `observe`, `cmd/gengo`, and
  `internal/strictjson` under `-short`.
- The expanded JSON fuzz target exercised structural, strict-schema, and
  extensible decoding for 10 seconds: 146,514 executions, no crash found.
- A 100-case gc optimization comparison produced 100 matches and verified
  offline. A 30-case GoLean campaign produced 26 matches and four frontend
  refusals for calls/allocations in short-circuit operands, with no mismatches;
  its report also verified offline.

The original review is preserved unchanged. Its separate matrix limitations
(386 execution on this host and a second actual Go release) remain outside
these four fixes.
