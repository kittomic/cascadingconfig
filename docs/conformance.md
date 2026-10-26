# Conformance

Each format's specification is the standard `cascadingconfig`'s decoders follow: RFC 8259 for JSON, TOML 1.1 and YAML
1.2. Where a decoder departs from it, the departure is deliberate and documented. This page measures how closely each
decoder follows its spec, against the format's own test suite.

## How it's measured

The suites are vendored in the repository at pinned versions, and every case runs on each push:

- **[toml-test](https://github.com/toml-lang/toml-test)**, the TOML 1.1 cases: a valid file must decode to the values
  the suite expects, by type, and an invalid one must be rejected.
- **[yaml-test-suite](https://github.com/yaml/yaml-test-suite):** an error case must be rejected; any other must decode,
  to the values its JSON form holds where it has one.
- **[JSONTestSuite](https://github.com/nst/JSONTestSuite):** each case runs twice, as a whole document and wrapped as
  `{"v": …}`, since a config file's top level must be an object but the suite's values mostly aren't. Valid cases must
  be accepted and invalid ones rejected. The suite leaves some cases to the implementation; their outcomes are listed.

A case that doesn't pass is one of two kinds, each listed below with its reason:

- **A deviation** is deliberate: a config file holds one document whose top level is a table, for example. Each cites
  its entry in the [decision log](design/design.md#decision-log) as `DL-n`.
- **A library failure** is a bug or gap in the parsing library the decoder uses, such as goccy/go-yaml. If an upgrade
  fixes one, the test fails until the case moves to the passing count, so the numbers here stay true.

## Results

<!-- generated: go test -run TestConformanceDoc -update in internal/conformance -->

| Suite | Version | Cases | Pass | Deviations | Library failures | Implementation-defined |
| ----- | ------- | ----: | ---: | ---------: | ---------------: | ---------------------: |
| toml-test (TOML 1.1) | `v2.2.0` | 681 | 681 | 0 | 0 | — |
| yaml-test-suite | `data-2022-01-17` | 402 | 212 | 178 | 12 | — |
| JSONTestSuite | `1ef36fa` | 636 | 481 | 85 | 0 | 70 (15 accepted, 55 rejected) |

### yaml-test-suite

**Deviations:**

- 127: the top level must be a table (DL-53)
- 16: a config file holds one document (DL-50)
- 13: a tag outside the YAML 1.2 core schema is an error (DL-164)
- 11: a sequence or table as a key is an error: a config key is a name (DL-166); goccy rejects it too
- 8: the top level must be a table (DL-53); goccy can't parse it either
- 2: a config file holds one document (DL-50); goccy can't parse it either
- 1: its two empty keys are duplicates, an error (DL-51); goccy can't parse empty keys either

**Library failures:**

- `FRK4`, `NHX8`, `S3PD`, `SM9W/01`: goccy rejects an empty (null) key, though it accepts the same key written ~
- `SU5Z`: goccy accepts a comment with no space after a quoted scalar
- `9C9N`: goccy accepts a wrongly indented flow sequence
- `QB6E`: goccy accepts a wrongly indented multi-line quoted scalar
- `L24T/01`: goccy drops a literal block's trailing line of spaces
- `6XDY`: goccy parses two empty documents as one, so the stream is accepted instead of rejected (DL-50)
- `VJP3/01`: goccy rejects a flow collection spread over many lines
- `4MUZ/02`: goccy rejects a flow mapping whose colon is on the line after its key
- `DK95/04`: goccy rejects a tab on an otherwise empty line

### JSONTestSuite

**Deviations:**

- 82: the top level must be an object (DL-53)
- 2: a document of whitespace only is an empty config (DL-53)
- 1: a leading byte-order mark is stripped, leaving an empty document, an empty config (DL-52, DL-53)

**Accepted, where the suite leaves it to the implementation:**

- 11: a lone or invalid surrogate escape becomes U+FFFD, as encoding/json does
- 2: a number that underflows decodes to 0
- 1: a leading byte-order mark is stripped (DL-52)
- 1: deep nesting is accepted

**Rejected, where the suite leaves it to the implementation:**

- 26: input that isn't valid UTF-8 is an error (DL-52)
- 20: the top level must be an object (DL-53)
- 8: a number outside the int64 or float64 range is an error (DL-49)
- 1: a byte-order mark inside the document isn't valid JSON

<!-- end generated -->
