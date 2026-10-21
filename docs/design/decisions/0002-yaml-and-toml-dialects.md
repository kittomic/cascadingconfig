# 0002: YAML and TOML dialects

**Date:** 2026-10-20. **Status:** decided; see the outcome below and DL-162 to DL-174 in
[design.md](../design.md#decision-log).

This weighs how `format/yaml` and `format/toml` should turn a file into a tree. The question is whether to keep each
library's own behaviour or follow each format's spec where they differ, and what that means for DL-48 and DL-50.

**Sources:** probes run on 2026-10-20 against `goccy/go-yaml` v1.19.2, `go.yaml.in/yaml/v3` v3.0.5 (the YAML library
koanf uses) and `pelletier/go-toml/v2` v2.4.3, plus a reading of goccy's decoder and token sources.

---

## What the libraries do

### YAML

Each row is a value decoded into `any`. The 1.2 core column is what the
[YAML 1.2.2 core schema](https://yaml.org/spec/1.2.2/#1032-tag-resolution) defines.

| Input                       | YAML 1.2 core           | goccy v1.19.2              | yaml.v3 v3.0.5             |
| --------------------------- | ----------------------- | -------------------------- | -------------------------- |
| `yes`, `no`, `on`, `off`    | strings                 | strings                    | strings                    |
| `True`, `TRUE`              | `true`                  | `true`                     | `true`                     |
| `017`                       | 17                      | 15 (1.1 octal)             | 15 (1.1 octal)             |
| `0o17`                      | 15                      | 15                         | 15                         |
| `0b11`                      | the string `0b11`       | 3 (1.1)                    | 3 (1.1)                    |
| `1_000`                     | the string `1_000`      | 1000 (1.1)                 | 1000 (1.1)                 |
| `1e3`                       | the float 1000          | **the string `1e3`**       | the float 1000             |
| `+.inf`                     | +Inf                    | **the string `+.inf`**     | +Inf                       |
| `18446744073709551615`      | out of `int64` range    | a `uint64`                 | a `uint64`                 |
| `-9223372036854775809`      | out of `int64` range    | **the string**             | **a float**                |
| `2001-12-14` (plain)        | a string                | a string                   | a `time.Time`              |
| Two documents               | a stream of two         | **first kept, silently**   | **first kept, silently**   |
| `!foo bar`                  | unresolved: not loadable | `"bar"`, tag ignored      | `"bar"`, tag ignored       |
| `!!binary aGVsbG8=`         | not a core tag          | a `[]byte`                 | the string `hello`         |
| `!!timestamp 2001-12-14`    | not a core tag          | a `time.Time`              | a `time.Time`              |
| `!!int abc`                 | an error                | **0, silently**            | an error                   |
| `!!set {a, b}`              | not a core tag          | a map of nulls             | a map of nulls             |
| Key `0x1F:`                 | the integer 31          | the key `"31"`             | the integer key 31         |
| Key `~:`                    | null                    | the key `"null"`           | a nil key                  |
| Keys `1:` and `"1":`        | different nodes         | duplicate, an error        | duplicate, an error        |
| `<<: *a` merge key          | not in 1.2 (a 1.1 type) | merged                     | merged                     |
| `<<: [*a, *b]`, both set `x` | earlier wins (1.1 merge spec) | **later wins**       | earlier wins               |
| `<<: *a` where `a` is 5     | —                       | an error                   | an error                   |
| `&a [*a]`                   | an error                | **`[null]`, silently**     | an error                   |
| 7-level alias bomb, ~350 B  | —                       | **5,380,839 values, no limit** | "excessive aliasing" error |
| Error messages              | —                       | **quote an excerpt of the file** | line numbers only    |

Bold marks behaviour that silently loses data, accepts what the spec rejects, or is unsafe.

**Findings:**

- **There is no single standard behaviour to follow.** The two common Go libraries disagree with each other on `1e3`,
  `+.inf`, timestamps, keys and merge order, and both keep YAML 1.1 number forms. Taking "the library's behaviour" means
  taking one library's choices, which change if the library is ever swapped.
- **goccy means to be YAML 1.2.** Its token package says it "is supposed to be compliant only with YAML 1.2", and it
  already treats `yes`/`no`/`on`/`off` as strings, as 1.2 does. Its departures are leftovers, not a chosen dialect.
- **goccy's parser is the strong part.** By its README it passes about 60 more yaml-test-suite cases than yaml.v3. Its
  own suite run skips 47 of the 351 cases it ships. The weaknesses above are all in the step after parsing: turning
  plain text into typed values, and building the result.
- **Some fixes are needed whatever we choose:**
  - **Non-canonical types:** `uint64`, `[]byte` and `time.Time` aren't canonical, so `DecodeDocument` would reject them.
  - **Unsafe input:** the alias bomb and the self-alias.
  - **Silent data loss:** a second document dropped, and `!!int abc` read as 0.
  - **Leaked contents:** error messages that quote the file (R9).

  Detecting a second document or an alias bomb already means walking goccy's syntax tree.

### TOML

go-toml v2.4.3 follows the TOML spec, and nothing here changes how it parses. The only issue is the types it returns for
dates and times: `time.Time` for offset date-times, and its own `LocalDateTime`, `LocalDate` and `LocalTime`. None of
these is canonical (DL-48), so the decoder has to turn them into something.

---

## R2: How the YAML decoder resolves values

| Option | What it is | Departs from goccy | Departs from YAML 1.2 |
| ------ | ---------- | ------------------ | --------------------- |
| **A. goccy's values, made safe** | `yaml.Unmarshal`, plus a walk of the syntax tree for the fixes every option needs, and a pass that converts or rejects non-canonical types | Only where it is unsafe or silently loses data | Yes: `017`, `0b11`, `1_000`, `1e3`, `+.inf`. DL-50 would change. |
| **B. goccy's parser, the 1.2 core schema** | goccy parses. We resolve plain scalars with the 1.2 core schema's rules and build the tree | As A, plus the five number forms above | No. DL-50 stands. |
| **C. B, with an option for 1.1 numbers** | B by default; `yaml.WithLegacyNumbers()` accepts `017` as octal, `0b11`, and `_` in numbers, as both libraries do | As B, or as A for numbers, by the app's choice | Only if the app opts in |
| **D. yaml.v3 instead** | koanf's library | — | Yes, more than goccy: timestamps and typed keys too |
| **E. Several YAML modules** | `format/yaml` and, say, `format/yamlv3` | — | Depends on the module |

**What B actually writes.** It isn't a YAML parser. goccy still does all the hard parts: indentation, flow and block
styles, quoting and escapes, block scalars, comments, anchors and the syntax errors. B adds:

1. **Resolution:** a small table that gives each *plain* (unquoted, untagged) scalar its type. The YAML 1.2.2 spec
   defines it exactly, as regular expressions (§10.3.2), and quoted scalars are always strings.
2. **Building the tree:** tables, sequences, aliases with the cap, merge keys and key conversion. Options A and B both
   need this anyway, because of the alias cap and the self-alias.
3. **Conformance tests:** goccy's module ships the yaml-test-suite data (MIT licence), and each case has the expected
   JSON. Running B against those cases tests the resolution step against the suite's own answers. That's a stronger
   check than A gets.

**Risks of B:**

- **Users expecting 1.1 numbers.** The real case is file modes: `mode: 0644` is 420 in goccy and yaml.v3, but 644 under
  1.2. Option C covers it, and the docs can list the five differences as a migration note.
- **goccy's syntax-tree API is less used than `Unmarshal`.** It is still exported, semver-covered API. The pinned
  version and the conformance tests would catch any drift on an upgrade.
- **Our code can have bugs.** That's limited by the spec's exact rules, the suite and fuzzing (M6 already plans fuzz
  tests).

**Benefits of B:**

- **One documented dialect,** "YAML 1.2 core", which doesn't change if the library changes.
- **Agrees with JSON.** The 1.2 core schema is built to agree with JSON. So the across-formats test, the same document
  in TOML, YAML and JSON giving the same tree, holds for `1e3` and `+.inf`, which under A it wouldn't.
- **Every silent failure becomes an error,** rather than only those we notice and patch one by one.

**Against D:** yaml.v3 departs from 1.2 more than goccy does (timestamps become `time.Time`, keys keep their types), and
it parses fewer suite cases. Its one advantage, an alias cap, B has too.

**Against E:** every YAML module adds a dependency and its own dialect to support. And the `Decoder` interface is
already public: an app that wants yaml.v3's behaviour can wrap it in about 40 lines and register it with `WithDecoder`.
`DecodeDocument` still applies the shared rules (byte-order mark, UTF-8, canonical types), so a custom decoder can't
break the tree. The docs can include that recipe.

**Recommendation: C.** It keeps DL-50, gives one dialect that's tested against the suite, and its one opt-in covers the
only 1.1 behaviour likely to matter in config.

---

## R3: Tags

goccy understands the core tags plus `!!binary`, `!!timestamp` and the ones it maps to a table (`!!set`, `!!omap`). It
ignores any other tag, such as `!foo`. Its handling has silent failures: `!!int abc` becomes 0, a bad `!!timestamp`
becomes the zero time, and `!!bool yes` becomes `true`, a YAML 1.1 reading.

The 1.2 spec says a document with a tag the application doesn't recognise can't be fully loaded (§3.3.2). So rejecting
an unknown tag *is* the spec's behaviour, and ignoring it is goccy's own departure. `!!binary` and `!!timestamp` would
give non-canonical values that must be rejected or converted anyway.

**Recommendation:**

- **Allowed:** the core schema's tags (`!!str`, `!!int`, `!!float`, `!!bool`, `!!null`, `!!map`, `!!seq`) and the
  non-specific `!`. A tag forces its type, and `!!int abc` is a `*ParseError`.
- **Rejected:** every other tag is a `*ParseError` naming it.
- **Later:** if apps need more, an option can register a tag with a function that returns a canonical value, such as
  `!!timestamp` turned into RFC 3339 text. Nothing in M6 needs one.

## R4: Plain timestamps

**Not a departure from goccy.** goccy already keeps `2001-12-14` and `2001-12-14t21:59:43.10-05:00` as strings, exactly
as written, as the 1.2 core schema does. yaml.v3 is the library that turns them into `time.Time`.

Only the plan's wording changes. The plan says "every YAML timestamp becomes an RFC 3339 string"; it should say that
YAML has no timestamp type, so dates stay as written. One written in RFC 3339 form still decodes into a `time.Time`
field (DL-148). The explicit `!!timestamp` tag falls under R3.

## R5: Non-string keys

goccy resolves a key's value, then prints it with `fmt.Sprint`: `0x1F:` becomes `"31"`, `~:` becomes `"null"`, and
`true:` becomes `"true"`. It already treats `1:` and `"1":` as duplicates.

**Revised recommendation:** follow goccy's rule, resolving with the 1.2 core schema (R2), then printing in canonical
form: integers in decimal, floats as Go prints them, `true`/`false`, and `null`. That keeps DL-50's "as goccy does".

- **Where it differs from goccy:** only where resolution differs. `017:` becomes `"17"`, not `"15"`, and `1_000:` stays
  `"1_000"`.
- **Duplicates** are compared after printing, as goccy compares them, so `1:` and `01:` clash.
- **Invalid keys:** a table or sequence used as a key is a `*ParseError`, as in both libraries.

My earlier proposal, keeping the key as written, would have departed from goccy for no gain.

## R6: Merge keys

**goccy supports them,** both `<<: *a` and `<<: [*a, *b]`, and rejects a merge whose value isn't a table. It has one
bug: with a list, the *later* table wins. The merge-key spec (yaml.org/type/merge) says keys in earlier tables win, and
yaml.v3 follows it.

**Recommendation:**

- **Keep merge keys**, with the spec's order: the table's own keys win, then earlier list entries.
- **Cap:** merged values count towards the alias cap.
- **Docs:** they're a 1.1 type that 1.2 dropped, kept because compose-style configs use them. This is the one deliberate
  departure from 1.2, and design.md says so.

## R7: Aliases

**goccy has no limit.** A ~350-byte, seven-level alias bomb expanded to 5,380,839 values. Nine levels from a file well
under the 10 MiB size limit would exhaust memory. A self-alias, `&a [*a]`, silently becomes `[null]`. yaml.v3 stops the
bomb with an "excessive aliasing" heuristic, and rejects the self-alias.

**Recommendation:** keep R7. At most 10,000 values may come from aliases and merge keys per document, and going over is
a `*ParseError` at the alias that crossed the line. A self-alias is a `*ParseError`. The cap is the
`WithMaxAliasValues(n)` option's default (R11).

## R8: TOML dates and times

**Not a departure from go-toml's parsing.** The values are parsed exactly as go-toml parses them. The question is only
how the tree stores them, and DL-48 already decided that every date-time is a string in the tree.

| Option | Pros | Cons |
| ------ | ---- | ---- |
| **Keep go-toml's types** (widen the canonical types) | Exactly what go-toml returns; matches koanf with TOML | The same date from YAML or JSON is a string, so values from different formats no longer compare, merge or diff alike; four new canonical types for every decoder to handle; reports and reload diffs need special cases |
| **RFC 3339 strings** (DL-48, recommended) | One representation for every format; a `time.Time` field still decodes, through the `TextUnmarshaler` hook; a `string` field gets readable text rather than Go's `time.Time` print form | An app reading `Tree()` directly sees a string, not a `time.Time`; the offset form is normalised, so `+00:00` becomes `Z` |
| **The exact source text** | Nothing normalised at all | go-toml's `Unmarshal` doesn't expose the source text; only its unstable syntax-tree API does |

**Recommendation:** keep DL-48, as R8 proposed:

- **Offset date-times:** they become `RFC3339Nano` text, which keeps the offset and the instant.
- **Local values:** local date-times, dates and times become their TOML text.
- **Docs:** the koanf difference is already in api.md's Dates row. The normalisation (`+00:00` becomes `Z`) goes in
  design.md's dialect table.

## R11: Options on the decoders

**Recommendation:**

- **`yaml.Decoder(opts ...yaml.Option)`** with two options:
  - **`WithLegacyNumbers()`:** YAML 1.1 integers: `017` as octal, `0b11`, and `_` separators (R2, option C).
  - **`WithMaxAliasValues(n)`:** the alias cap, 10,000 by default (R7). A value below 1 makes every `Decode` fail with
    an error saying so, because the constructor has no error to return. The first load reports it.
- **`toml.Decoder()`** stays without options, because there's nothing to configure yet. Adding variadic options later
  doesn't break any call.
- **Full replacement** stays available through the public `Decoder` interface and `WithDecoder`, with the recipe in the
  docs (R2, against E).

---

## What changes in the design

- **DL-50 stands:** YAML 1.2 core, now with the evidence above. Two parts become explicit: merge keys are a deliberate
  1.1 extension, and keys are resolved and then printed.
- **New entries** in the decision log for tags (R3), merge keys (R6), the alias cap (R7), the decoder options (R11), and
  the "no timestamp type" wording (R4).
- **design.md's dialect table** gains rows for numbers, tags, merge keys, aliases and the TOML date text.
- **api.md** gains the `format/yaml` options and the custom-decoder recipe.
- **M6's acceptance tests** gain a yaml-test-suite conformance test, cases for the five number forms with and without
  `WithLegacyNumbers`, the merge order, and the self-alias.

---

## Outcome

- **The YAML 1.2 spec is the source of truth** for `format/yaml`, as each format's spec is for its decoder. Deviations
  are deliberate and documented.
- **R2: option C,** with `WithLegacyNumbers` accepted tentatively. It may be dropped if it complicates the decoder or
  costs performance, since an app can always register its own decoder (DL-163).
- **R3, R4, R6, R7, R8 and R11:** accepted as recommended (DL-164, DL-165, DL-167 to DL-169, DL-172).
- **R5:** accepted as revised. YAML 1.2 lets any node be a key, and two keys clash when they have the same tag and the
  same canonical value (§3.2.1.3), so `1:` and `0o1:` are duplicates. It doesn't define how keys become strings, which
  is the tree's choice: `1:` and `"1":` are different YAML keys, but they clash once printed, so they're a `*ParseError`
  too (DL-166).
- **Conformance suites** (yaml-test-suite, toml-test and JSONTestSuite): the plan to measure against them is accepted,
  but deferred to a new last milestone, M12 (DL-174). Until then the decoders behave as described here, with the
  deviations documented.
