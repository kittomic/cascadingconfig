# Implementation plan

This plan orders the work of building `cascadingconfig` from the design in [design.md](design.md), [api.md](api.md) and
[project-layout.md](project-layout.md). It also fixes how the work is done: acceptance tests come first, derived from
those docs and reviewed before any implementation.

**Status: decided.** The decisions are at the end.

## Notation

| Reference      | Means                                                                                         |
| -------------- | --------------------------------------------------------------------------------------------- |
| **DL-n**       | Entry *n* in design.md's [decision log](design.md#decision-log).                              |
| **D-n**        | Row *n* of the [divergence table](config-locations.md#4-divergences) in config-locations.md.   |
| **§ Name**     | A section of design.md or api.md, linked where it first appears.                              |

## Workflow

The agreed approach is:
**you approve *what* must be true, as tests derived from the docs, and the implementation works out *how*.**

Each milestone follows these steps:

1. **Acceptance tests first.** They're written from the docs, never from code. Each test cites the decision or section
   it checks, in its name or a comment, for example `// DL-11: _unset removes only lower layers`. Most are table tests:
   inputs (an in-memory file tree, env vars, options) and expected outputs (merged tree, layer order, error code).
2. **Red first.** The new tests are run against the empty or stubbed API to show they fail. A test that can't fail
   proves nothing.
3. **Tests get their own commit** (`test(scope): …`), separate from the implementation.
4. **Review gate.** You review the milestone's acceptance tests before implementation starts. This is the main point
   where your judgement goes in. The review happens locally, before the branch is pushed: the pre-push hook runs the
   tests, so red tests can't be pushed. The PR, with test and implementation commits kept separate, is the record.
5. **Implementation.** Code is written until the acceptance tests pass. Internal unit tests are added freely along the
   way, without prior agreement.
6. **Approved tests stay fixed.** If an approved test turns out to be wrong, the change goes in a separate commit
   (`test(scope): revise …`) that explains why, and it's raised with you, never folded silently into an implementation
   commit.
7. **Gaps in the spec stop the work.** If implementing reveals an ambiguity or contradiction in the docs, work stops,
   the question is raised, and the doc (and decision log) is updated *before* the code relies on an answer.

**Exceptions** to test-first, where intent and review work better: tooling and CI setup, example code, and exploratory
API ergonomics.

**Security behaviour always gets attack tests first:** traversal, symlinks, archive escapes, URL tricks, permission
checks. Parsers and path validation also get fuzz tests (`go test -fuzz`).

**Every milestone ends when:**

- every acceptance test passes, and they pass with `-race`
- `golangci-lint` and `govulncheck` are clean in every module touched
- the docs are updated wherever implementation clarified a behaviour
- the measured dependency counts still match [project-layout.md](project-layout.md#dependency-analysis) (from M6 on)

## Milestones at a glance

| #   | Milestone                         | Modules touched                          | Depends on |
| --- | --------------------------------- | ---------------------------------------- | ---------- |
| M0  | Repository and tooling            | all (skeletons)                          | —          |
| M1  | Foundations                       | core, `format/json`                      | M0         |
| M2  | Merge engine                      | core                                     | M1         |
| M3  | Options, presets and `New`        | core, `format/json`                      | M1         |
| M4  | Discovery                         | core                                     | M2, M3     |
| M5  | Decoding, results and reports     | core, `internal/compat` (test-only)      | M4         |
| M6  | Format modules                    | `format/toml`, `format/yaml`             | M1         |
| M7  | Includes, policies and transforms | core                                     | M4         |
| M8a | Remote includes                   | core (`httpresolver`)                    | M7         |
| M8b | git resolver                      | core (`gitresolver`)                     | M8a        |
| M8c | OCI resolver                      | `ociresolver`                            | M8a        |
| M9  | Hot reload                        | core, `ociresolver`, `watch/fsnotify`    | M5, M7, M8 |
| M9b | Custom scopes                     | core                                     | M9         |
| M10a | Precedence                       | core                                     | M9b        |
| M10b | Presets                          | `presets`                                | M6, M8b, M8c, M9, M10a |
| M11a | Documentation structure         | docs, tooling                            | M10        |
| M11b | API audit and guard             | all released modules                     | M11a       |
| M11c | Examples                        | `examples`                               | M11b       |
| M11d | README and user guide           | docs                                     | M11c       |
| —   | Release stage                     | all                                      | M11d, M12  |
| M12 | Conformance                       | `format/*`, core (`internal/json`)       | M6         |

M2 and M3 can be done in either order, as can M8b and M8c. M6 can run any time after M1. M12 comes before the release
stage, but needs only M6.

## M0: Repository and tooling

Not test-first: this is tooling.

- **Module skeletons:** `go.mod` for each module in [project-layout.md](project-layout.md#repository-tree), all with
  `go 1.26.0`, and a committed `go.work` listing them.
- **Checks as scripts.** Every check lives in `scripts/`, so lefthook runs it now and CI can run the same scripts later:
  - **`scripts/each-module.sh <command>`:** runs a command in every workspace module.
  - **`scripts/check-go-versions.sh`:** runs the tests on Go 1.26 (through `mise exec go@1.26 --`) and on the latest Go.
  - **`scripts/check-gowork-off.sh`:** builds and tests each module with `GOWORK=off`.
  - **`scripts/check-licenses.sh`:** every module has a `LICENSE` identical to the root one.
  - **`scripts/check-deps.sh`:** the dependency budget, added in M6.
  - **bash 3.2:** the scripts run on the bash that macOS ships, so they avoid `mapfile` and empty arrays under `set -u`.
- **lefthook:**
  - **Loop over the modules:** `go-mod-tidy`, `go-lint`, `go-test`, `golangci-lint` and `govulncheck`, as listed in
    [project-layout.md](project-layout.md#tooling-changes).
  - **Add to pre-push:** the Go-version and `GOWORK=off` checks.
  - **If pre-push gets too slow,** the Go-version check moves to a `mise run check` task, run before opening a PR.
- **No CI yet.** It's deferred to M11 (see [Decisions](#decisions)).
- **Repository files:**
  - **`LICENSE`:** the Apache License 2.0, in the repo root **and in every module's root directory**. Go packages each
    module separately, with only that module's directory, and pkg.go.dev only shows a module's documentation if it finds
    a licence there. A script check keeps the copies identical.
  - **`README.md`:** a stub, filled in at M11.
  - **`SECURITY.md`:** how to report a vulnerability privately, and which versions get fixes.
  - **A CHANGELOG convention:** one `CHANGELOG.md` per module, built from the Conventional Commits that lefthook already
    enforces. A commit's scope names its module, for example `feat(format/toml): …`, and `core` is the scope for the
    core module.
- **Tooling fixes:**
  - **`.golangci.yml`:** goimports `local-prefixes` is set to `github.com/kittomic/agentic-platform`, which belongs to
    another project. It becomes `github.com/kittomic/cascadingconfig`.
  - **`.config/mise.toml`:** every tool is pinned to an exact version instead of `latest`, so lint and `govulncheck`
    results only change when a version is deliberately bumped.
- **The `cctest` package** (core module) is created empty. It's public, so that the other modules' tests, and app
  authors' tests, can use it. Each helper is built in the milestone that defines the interface it fakes, so no interface
  is designed early:

  | Helper                              | Milestone | Fakes                                                          |
  | ----------------------------------- | --------- | -------------------------------------------------------------- |
  | `cctest.FS`                         | M4a       | the `FileSystem` interface: symlinks, permissions, owners      |
  | `cctest.Host`                       | M4a       | the internal host: env vars, home, cwd, `GOOS`, `GOARCH`, UID  |
  | Golden-file helpers                 | M4a       | discovery orders; reused for reports in M5                     |
  | `cctest.Clock`                      | M8a       | the internal clock, first needed for cache TTLs; reused in M9  |
  | `cctest.Notifier`                   | M9        | the `Notifier` interface                                       |

## M1: Foundations

**Scope:**

- **`Tree`** and the canonical value types (§ [Core types](api.md#tree-and-values)).
- **`Path`, `PathSpec` and `ParsePath`.**
- **The error model:** `Code`, `Coder`, the base `BaseError`, every error type with its sentinel, `CodeOf` and `Errors`
  (§ [Errors](api.md#errors)).
- **The `Decoder` and `PositionError` interfaces,** `Document`, `DecodeDocument`, `Warning`, and the JSON decoder in
  `format/json`.

**Acceptance tests:**

- **Canonical values:** JSON integers decode to `int64`; `1.0`, `1e3` and fractions to `float64`; `null` to `nil`. An
  integer out of the `int64` range is a `*ParseError` (DL-49).
- **JSON dialect** (DL-51, DL-52, DL-53):
  - comments and trailing commas are rejected
  - a duplicate key keeps the last value and logs a warning
  - a leading byte-order mark is stripped, and non-UTF-8 input is a `*ParseError`
  - a top-level array or scalar is a `*ParseError`
  - whitespace-only and `null` documents are empty, and `{}` is not
- **Paths:** `ParsePath("server.tls")` and `Path{"plugins", "foo.bar"}` round-trip through `String()`; the empty string
  is the root; empty segments are errors (DL-85).
- **`DecodeDocument`** (DL-82): the byte-order mark is stripped before the decoder sees the input; a decoder error
  becomes a `*ParseError` with the decoder's position; a non-canonical type from a decoder is a 500.
- **Errors:** for every error type, `errors.Is(err, ErrX)`, `errors.As(err, &xErr)` and `CodeOf(err)` give the
  documented code. Joined errors keep working with `Is`, `As` and `Errors`.
- **Parse errors:** a JSON syntax error becomes a `*ParseError` with line and column.
- **New error types** (DL-75): `*IOError` and `*UnsupportedSchemeError` have their codes and sentinels; a cancelled
  context's error satisfies `errors.Is(err, context.Canceled)`.
- **Fuzz:** the JSON decoder never panics and only produces canonical types.

## M2: Merge engine

**Scope:** merging trees layer by layer, with provenance. No discovery yet: tests pass layers in directly, through one
unexported entry point, and check the results only through `Result.Tree` and `OriginOf` (DL-86). This brings forward
`Result` (`Tree` only), `Origin`, `OriginOf`, `MergeStrategy`, `MergeFunc`, `FormatConflict`, and the parts of `Layer`,
`ScopeName`, `Variant` and `TrustLevel` that merging needs. The options that set merging up arrive in M3.

**Acceptance tests:**

- **Deep merge:** objects merge, arrays and scalars are replaced (DL-8, § [Merging](design.md#merging)).
- **Per-key rules** (DL-10, § [Per-key merge rules](design.md#per-key-merge-rules)):
  - every strategy, and `MergeFunc` (DL-87)
  - wildcard matching
  - the most specific rule wins, and the same path again replaces a rule (DL-88)
  - ambiguous rules and a root rule are errors when registered
  - `Replace` and functions cover their subtree
  - type mismatches name the key and both files, and every mismatch is reported (DL-90)
  - without a rule, merging never fails
  - a function's error is a `*MergeError`, and a non-canonical result a 500 (DL-89)
- **`_unset`** (DL-11, § [Unsetting keys](design.md#unsetting-keys)):
  - both path forms, and nested placement
  - removal happens before the layer's own values merge
  - only lower layers are affected
  - a missing key is a no-op
  - the key is stripped from the result
  - it can be renamed or turned off
  - malformed values and `_unset` inside arrays are `*ParseError`s, and a table holding only `_unset` doesn't set its
    key (DL-92)
- **Null** (§ [Null values](design.md#null-values-yaml-and-json)):
  - replaces any type
  - is replaced by any value
  - is never a type mismatch
  - stays in the result
- **Same-slot formats** (DL-3, DL-93, § [File format precedence](design.md#file-format-precedence)): reverse-precedence
  merge order, extension rank within a format, and the `ConflictMerge`, `ConflictFirst` and `ConflictFail` policies,
  with their warnings and errors.
- **Provenance** (DL-91): `Origin` kinds, elements and history for set, merged, appended, prepended, function, null and
  unset values, and history restarting below a replaced table.
- **Ownership:** `Result.Tree()` returns a copy (§ [Concurrency and ownership](api.md#concurrency-and-ownership)).

## M3: Options, presets and `New`

**Scope** (DL-94): § [Building a loader](api.md#building-a-loader): sealed options and the option kinds, `Preset`
(`NewPreset`, `Extend`, `ExtendAs`), `New`, `NewWithPreset` and validation. Only the options whose behaviour can work
now are included ([Settled signatures](api.md#settled-signatures)):

- **Scopes:** selection, and custom scopes with the `Values` kind.
- **Formats and decoders,** and the merge options, which feed M2's engine.
- **`WithEnvs`, `WithConfigFiles`** and env-var bindings.
- **The three layouts,** with the parts these options cover: JSON through `internal/json` (DL-95), the format order, the
  conflict policy, the array default and the bindings. They get their scopes in M4b and M4d (DL-106).

Each later milestone settles its own options at its test review and adds them to the layouts.

**Acceptance tests:**

- **Applied in order:** `WithScopes` then `WithoutScopes` (DL-29). `Extend` never changes its receiver.
  `NewWithPreset(n, p, a)` behaves exactly like `NewWithPreset(n, p.Extend(a))`. `ExtendAs` names an extension and
  records its base (DL-97).
- **Validation:** every problem is reported at once, as `*OptionError`s whose `Op` names the option (DL-104):
  - unknown scopes, duplicates in `WithScopes`, name clashes and bad placement (DL-100)
  - an unsafe app name, and unsafe `WithEnvs` names (DL-99)
  - invalid formats, duplicates, formats with no decoder, and an extension claimed twice (DL-101); `WithDefaultFormats`
    skips formats with no decoder instead (DL-105)
  - invalid merge options and ambiguous rules (DL-102)
  - non-canonical `Values` trees (DL-103)
  - a loader with no scopes, the first [bare-loader](api.md#a-loader-without-a-preset) check. The other two arrive with
    M4a and M7.
- **Custom scopes:** placement with `Before` and `After`, the default trust of `Values`, and `Trust` overriding it.
- **Env-var bindings** (DL-15, DL-98): `{NAME}` is derived as documented (`my-app` → `MY_APP_…`); bindings can be
  renamed and turned off; unknown keys, bad variable names and shared variables are errors; each layout declares its
  documented set.
- **Compile-time checks** (DL-96): misplaced option kinds don't compile, for example a `WatchOption` passed to `New`.
- **Empty calls:** `WithEnvs()` and `WithConfigFiles()` with no arguments, or only empty strings, leave the env vars in
  charge; empty `WithScopes()` and `WithFormats()` are `*OptionError`s (DL-73).
- **Layouts:** built only from exported identifiers, checked by parsing their source (DL-71); each one's formats,
  conflict policy, array default and bindings; `MiseHardenedLayout` extends `MiseLayout`.

## M4: Discovery

The largest milestone, done in four parts (DL-106).

### M4a: Filesystem and loading

**Scope:**

- **Test helpers:** `cctest.FS`, `cctest.Host` and `cctest.Golden` ([api.md](api.md#test-helpers), DL-109).
- **The filesystem hook:** OS-backed, with `os.Root` and symlinks, plus `MountFS`, the `fs.FS` adapter (§
  [Filesystem hook](design.md#filesystem-hook)).
- **Loading:** `Loader.Load` reads, decodes and merges every selected scope (DL-107). Several files in one slot go
  through M2's `orderSlot`.
- **Reading rules** (DL-110): unreadable, non-regular, missing, oversized and empty files; dedup by file identity.
- **Platform dirs** (DL-14, DL-112): `WithPlatformDirs(XDGDirs)`, `{NAME}_CONFIG_DIR` and `{NAME}_SYSTEM_CONFIG_DIR`.
- **Scope kinds:** `FixedDir` with simple patterns (DL-111), `EmbeddedFS`, `ValuesFunc` and `EnvVars` (DL-35, DL-113).
- **Options** (DL-94, DL-115): `WithFileSystem`, `WithMaxFileSize`, `WithLogger` and `WithPlatformDirs`. A file-reading
  scope with an empty format order becomes an `*OptionError`, the bare-loader check moved from M3 (DL-114).

**Acceptance tests:**

- **Loading:** patterns apply lowest precedence first, scopes in their order; a missing directory loads nothing, without
  a warning; `{ext}` expands to the searched formats only, and an extension with no decoder never matches.
- **Same-slot files** (DL-3, DL-93): merged in format order; `ConflictFirst` ignores the rest with warnings;
  `ConflictFail` is a 409.
- **Golden discovery order** for `FixedDir` scopes, with every layer's status (DL-116).
- **Dedup:** a file reached twice, through two patterns, a symlink or a hard link, loads once at its higher position,
  with the trust of its most trusted route (DL-66).
- **Symlinks:** a dotfiles symlink leaving the searched directory loads (DL-62); a broken symlink or a loop is skipped
  with a warning.
- **Special files:** a FIFO or a directory named like a candidate is skipped with a warning, without blocking, on
  `cctest.FS` and on a real filesystem (DL-63).
- **Size:** a local file over the limit is a 413 (DL-64).
- **Case:** `MyApp.TOML` doesn't match `myapp.toml`, on `cctest.FS` and on a real case-insensitive filesystem (DL-66).
- **Unreadable files** are skipped with a warning, as `LayerUnreadable` (D16, DL-110).
- **Errors:** every broken file is reported, joined, and the rest are still read; empty files are `LayerEmpty`.
- **Platform dirs:** `$XDG_CONFIG_HOME/{name}`, then `~/.config/{name}`, and `/etc/{name}`; the bindings override them,
  resolving relative values against the current directory and expanding `~`; no home skips the user dir with a warning;
  Windows uses `%ProgramData%\{name}`, tested through `cctest.Host{GOOS: "windows"}` (DL-70).
- **Kinds:** `ValuesFunc` runs on every load, and its errors are 500s; `EmbeddedFS` reads an `fs.FS`; `EnvVars` maps
  `__` to nesting, excludes discovery bindings, warns on empty segments and clashes, and takes `MapEnv`; value scopes
  drop the unset key with a warning.
- **Options:** invalid `FixedDir`s, file sizes, filesystems and loggers are `*OptionError`s; warnings reach the logger
  with their code and file.
- **Loads:** a cancelled load keeps `errors.Is(err, context.Canceled)` (DL-75); parallel loads under `-race` agree.

### M4b: Patterns, environments and the walk

**Scope:**

- **Patterns:** variants, config roots, globs and `conf.d` fragments.
- **Scope kinds:** `Walk` and `ExplicitFile`, with `Pattern.Key` in place of `Embedded` (DL-25, DL-120).
- **The directory walk** and ceilings (DL-5).
- **Environment selection** and platform envs (DL-30), and **safe environment names** (DL-32).
- **`MiseLayout`'s scopes and patterns.**
- **Explicit config files** (DL-16), and the single-file bindings.
- **Override filenames** and **ignored paths.**
- **Dedup across scopes**, such as a user file that is also a project file of `~` (DL-61).
- **Options** (DL-94): this milestone settles the pattern fields, the walk options and the explicit-file options, and
  adds `MiseLayout`'s scopes.

**Acceptance tests:**

- **Golden discovery order** for one directory, matching
  [config-locations.md 3.2](config-locations.md#32-filenames-within-one-project-directory) (layers A–D) and the
  [worked example](design.md#environments).
- **Walk limits:** a start dir inside `~` stops after `~`, and one outside `~` walks to `/`. Ceilings are exclusive.
- **Dedup:** a file reached as both a project file of `~` and a user file is loaded once, at its higher position, with
  user trust and user policy (DL-61).
- **Platform envs** rank below selected ones, and an explicitly selected platform name ranks as selected.
- **Names:** dotted names match literally and are never combined (DL-31). Traversal attempts are rejected from every
  source.
- **Paths:** relative env-var paths resolve against the current directory; `~` is expanded; ignored paths match files
  and directory prefixes (DL-68, DL-69).
- **Explicit files** replace the project scope only, and a missing one is a 404. They load with any registered decoder,
  even one the format order leaves out, and an unregistered format is a 415 (DL-67).

### M4c: Formats and bootstrap

**Scope:**

- **Format precedence:** env var > bootstrap file > option > preset (DL-4, DL-134), including `{NAME}_CONFIG_FORMATS`
  (DL-136).
- **Bootstrap search,** allowed keys and scope limits (DL-7, DL-26, DL-131 to DL-135).
- **Scope trust levels.**
- **Options** (DL-94): `BootstrapFiles`, `Pattern.SkipHomeAndRoot`, `WithBootstrapKeys`, `WithoutBootstrapKey`,
  `WithBootstrapLimit` and `WithBootstrapTransform`, and the layouts' bootstrap files (DL-138).

**Acceptance tests:**

- **Bootstrap search:** the files are found in their scopes' places, lowest precedence first, a higher file replacing a
  key; `SkipHomeAndRoot` isn't searched in `~` or `/`; an unselected scope's files aren't read; the bootstrap walk
  ignores `ceiling_paths` from bootstrap files.
- **Format order:** the env var beats bootstrap files, which beat the option; each scope takes `formats` from the
  highest file at least as trusted (DL-135); `formats = []` is an error; `{NAME}_CONFIG_FORMATS` errors name the
  variable.
- **Scope limits:** a project bootstrap file's `ignored_config_paths` entry that points at `/etc/{name}/…` is dropped
  with a warning (DL-26). Bootstrap paths resolve against their file (DL-68).
- **Keys:** `WithoutBootstrapKey` and `WithBootstrapLimit` drop keys with a warning; `WithBootstrapTransform` can
  rewrite a file, and its error is a 422 naming the file.
- **Hardened layout:** project bootstrap files may only set `env` and `formats` in `MiseHardenedLayout`; any other key
  is dropped with a warning.
- **Bootstrap keys** (DL-78): an unknown key warns, `env = "dev"` is an `*OptionError`, and `include` or `_unset` in a
  bootstrap file is ignored with a warning.
- **Golden:** `MiseLayout`'s bootstrap files from inside and outside `~`.

### M4d: `CosmiconfigLayout`

**Scope:**

- **First-match resolution** (DL-139).
- **Walk stops and search strategies:** `StopAtStart`, `StopAtMarker` with `WithRootMarker`, `WithStopDir`, and
  `WithSearchStrategy(SearchNone | SearchProject | SearchGlobal)` (DL-140).
- **Choosing a file's format:** `WithFileFormat` with the matchers `Extension`, `FileName`, `Glob`, `Regexp` and
  `MatchFunc` (DL-141).
- **Search places:** rc files and `.config/` in the order of
  [config-locations.md 2.1](config-locations.md#21-search-places-within-one-directory), without JS/TS.
- **The meta config** in `.config/config.{ext}`, through a `{cwd}` scope and bootstrap keys (DL-142). There's no
  `package.json` or `package.yaml` support ([Deviations from cosmiconfig](design.md#deviations-from-cosmiconfig)).
- **The global dir** through `EnvPathsDirs` (DL-143).
- **Explicit files** replacing everything, with `WithConfigFilesOnly` (DL-144).
- **`CosmiconfigLayout`'s scopes** (DL-146).
- **Deferred:** `$import`, which may never be supported (DL-145).

**Acceptance tests:**

- **Search order:** golden tables for each search strategy, including the stop dir and root marker being inclusive. The
  default searches only the start dir and not the user dir (DL-42). One directory with every search place shows the full
  order and the single file loaded (DL-147).
- **First match:** the nearest non-empty file wins across scopes; files below it aren't decoded; empty files are
  skipped; `SearchGlobal` reaches the user dir only when nothing nearer matched.
- **Invalid combinations:** `WithStopDir` without `StopAtHome`, and `StopAtMarker` without a root marker, are
  `*OptionError`s, and so are bad matchers and `{system}` with `EnvPathsDirs`.
- **File formats:** rules beat extensions, the latest rule first; `.{name}rc` decodes as YAML, or JSON without YAML;
  explicit files use the rules.
- **Node.js sources are ignored:** a `package.json` with a `{name}` key loads nothing (DL-44).
- **Explicit files** replace everything in `CosmiconfigLayout`.
- **The meta config's `{name}` key** is checked before the normal search, and its `searchPlaces` and `mergeSearchPlaces`
  change the walk.

## M5: Decoding, results and reports

**Scope:**

- **Decoding:** `Load[T]` and `DecodeAt`, with koanf-compatible decoding: the `koanf` tag, mapstructure v2 and the same
  three hooks, which also parse RFC 3339 dates (§ [Decoding](api.md#decoding), DL-148).
- **Unknown keys:** a warning by default, an error in strict mode, or silence (DL-151).
- **Defaults:** `WithDefaults` and the `defaults` scope (DL-72, DL-153).
- **`RequireConfig`** at every level, globally and per scope ([api.md](api.md#requiring-config), DL-157).
- **Error aggregation:** every error from one stage of a load is returned together (DL-155).
- **Reporting:** `Report`, `Describe()`, `OriginOf` with `EnvVar`, and `KoanfProvider` (DL-152, DL-158, DL-160).
- **First-match fix:** value layers always merge (DL-154).
- **Options** (DL-94): this milestone settles the decoding, defaults, requirement and report options
  ([api.md](api.md#settled-in-m5)). Policies in the report wait for M7, and remote layers for M8 (DL-159).

**Acceptance tests:**

- **koanf parity:** the same structs and trees decode identically with koanf and with cascadingconfig, including day
  durations, `TextUnmarshaler`, weak typing and RFC 3339 dates. These tests live in a test-only module,
  `internal/compat`, so koanf never appears in the core `go.mod`. koanf loads through `KoanfProvider`, which tests it
  too.
- **Null and `_unset`** leave Go-side defaults untouched. With `ZeroFields: true`, a null clears them.
- **Defaults** (DL-72, DL-153):
  - `WithDefaults(v)` is applied on every load, for `Load[T]`, `res.Decode` and `KoanfProvider`
  - only non-zero fields become defaults, and durations, times and `TextMarshaler` types convert to canonical forms
  - a higher `null` or `_unset` clears a default
  - `OriginOf` reports "set by defaults"
  - `RequireConfig(RequireFile)` isn't satisfied by defaults alone
  - the scope survives a later `WithScopes`, and invalid values are `*OptionError`s naming the field
- **Decode errors** (DL-150): one per key, naming the key and the file that set it, never the value.
- **Unknown keys** (DL-151): warned and recorded with their file; one `*DecodeError` each in strict mode; silent with
  `UnknownKeysIgnore`.
- **Custom decoder configs** (DL-149): the app's hooks replace ours, `DecodeHook()` composes, and a `Metadata` is an
  `*OptionError`.
- **No values:** a golden `Report.String()` and JSON contain no config values. Secret values put in fixtures must never
  appear in the output, nor in decode errors or a failed layer's reason.
- **Several broken files** produce one joined error listing all of them.
- **Failed loads** return a non-nil `*Result` whose report shows what broke, as `LayerFailed`, with a nil `Tree()`
  (DL-75, DL-156).
- **Concurrency** (DL-76): parallel `Load`s and decodes under `-race`; modifying `Tree()`'s result doesn't change the
  report, later decodes or later loads. The transform checks wait for M7 (DL-159).
- **`RequireConfig`** (DL-54, DL-157): `RequireAny` is satisfied by flags or the defaults scope, but not by an empty
  value scope; `RequireFile` is not; a whitespace-only file fails `RequireNonEmpty`; with scopes, each listed scope must
  qualify; explicit files count for the scope they replace; several requirements all apply.
- **Dates:** an RFC 3339 string decodes into `time.Time` and into `string`. A date-only string into `time.Time` is a
  `*DecodeError`.
- **First match** (DL-154): a `Values` scope above the chosen file still merges, and defaults sit below it.
- **The report** (DL-158): a golden text and JSON report for a mise project, with bootstrap files, duplicates, probes
  and bindings.

## M6: Format modules

**Scope:** `format/toml` (`pelletier/go-toml/v2` v2.4.3) and `format/yaml` (`goccy/go-yaml` v1.19.2), as decided in
[decision record 0002](decisions/0002-yaml-and-toml-dialects.md) and DL-162 to DL-174.

- **Before the first release** (DL-162): both modules replace the core with the repository's copy, so `go mod tidy` and
  the `GOWORK=off` check work while the repository is private and unreleased.
- **YAML:** goccy parses, and the decoder builds the tree from goccy's syntax tree, typing unquoted values by the YAML
  1.2 core schema (DL-163), with the tag, key, merge-key and alias rules of DL-164 to DL-168.
- **TOML:** go-toml parses, and the decoder turns its date and time types into text (DL-169).
- **Both:** errors never quote the file (DL-170), and a library panic becomes a `*ParseError` (DL-171).
- **Options** (DL-172): `yaml.Decoder` takes `WithLegacyNumbers` and `WithMaxAliasValues`; `toml.Decoder` takes none.
- **Dependency budget** (DL-173): `scripts/check-deps.sh`, in lefthook's pre-push.
- **Not here:** the conformance suites, which wait for [M12](#m12-conformance) (DL-174).

**Acceptance tests:**

- **Canonical types:**
  - every TOML date-time, date and time becomes text, offset ones in RFC 3339 form (DL-169)
  - a YAML date stays a string as written (DL-165)
  - YAML integers outside the `int64` range are a `*ParseError` (DL-49)
- **YAML values** (DL-163): `yes`, `no`, `on` and `off` stay strings; `017`, `0o17`, `0x1F`, `1e3`, `+.inf`, `.nan`,
  `0b11` and `1_000` follow the core schema, and with `WithLegacyNumbers` the 1.1 forms are integers.
- **YAML tags** (DL-164): core tags force their type, `!!int abc` is a `*ParseError`, and `!!binary`, `!!timestamp`,
  `!!set` and `!foo` are `*ParseError`s naming the tag.
- **YAML keys** (DL-166): `0x1F:`, `~:` and `true:` become `"31"`, `"null"` and `"true"`; `1:` with `0o1:`, and `1:`
  with `"1":`, are `*ParseError`s; a table as a key is a `*ParseError`.
- **Merge keys** (DL-167): a single merge, a list where earlier entries win, the table's own keys winning, and a merge
  of a scalar as a `*ParseError`.
- **Aliases** (DL-168): an alias bomb hits the cap, `WithMaxAliasValues` moves it, and a self-alias is a `*ParseError`.
- **Directives** (DL-164, DL-175): `%YAML 1.2` is accepted, 1.1 warns, 2.0 is a `*ParseError`; `%TAG` handles that
  expand to core tags are accepted, and others, a redefined `!!` included, are `*ParseError`s.
- **Dialects** (DL-50 to DL-53):
  - several YAML documents in one file are a `*ParseError`
  - duplicate keys are `*ParseError`s in TOML and YAML
  - a leading byte-order mark is stripped in both, which matters most for YAML, where it would otherwise end up in the
    first key
  - TOML 1.1 syntax that the pinned go-toml accepts parses
- **Errors:** line and column reach `*ParseError`, and no message contains an excerpt of the file (DL-170).
- **YAML extensions:** `.yml` is decoded as YAML, `MYAPP_CONFIG_FORMATS=yml` means `yaml`, and when `.yaml` and `.yml`
  share a slot, `.yaml` wins (DL-47).
- **Across formats:** the same document in TOML, YAML and JSON gives an identical tree.
- **Fuzz:** both decoders never panic and only produce canonical types (DL-171).
- **Dependency budget:** `scripts/check-deps.sh` checks the counts in
  [project-layout.md](project-layout.md#dependency-analysis) for the setups whose modules exist, from lefthook's
  pre-push and from here on (DL-173).

## M7: Includes, policies and transforms

**Scope:** local includes, per-scope policies, file permissions, the trust store and transforms, as decided in DL-176 to
DL-194.

- **The include key** (DL-178): `WithIncludeKey(key, IncludeFlat | IncludeNested)` and `WithoutIncludeKey()`. The mise
  layouts get `"include"`, flat. `CosmiconfigLayout` gets none, so nesting and cycles are tested through a custom
  layout.
- **Include rules:** rank (DL-12), loading once at the highest position (DL-179), values and metadata (DL-184), errors
  (DL-185), the steps for one layer (DL-186) and where includes work (DL-187).
- **The `file` resolver** (DL-23, DL-182, DL-183): built into the loader, with `Confine` (DL-180). Every other scheme is
  a 501 until M8.
- **Allow-lists and matchers** (DL-176): `Prefix`, `MatchRef`, and `?` in `Glob`.
- **Policies** (DL-24, DL-188 to DL-190): `WithDefaultPolicy` and `WithScopePolicy`, with replace-not-merge semantics.
  `RequirePinned` moves to M8 (DL-182).
- **Files named directly** fail instead of being skipped (DL-181).
- **The permission policy** (DL-20, DL-191), with `StrictModes` and owners in `cctest.FS` (DL-108).
- **Trust** (DL-28, DL-192): `RequireTrust` and `TrustPaths`.
- **Layer and result transforms** (DL-36, DL-193).
- **The report** (DL-159, DL-194): each scope's effective policy.
- **Not `$import`:** deferred from M4d, and possibly never supported (DL-145).
- **Options** (DL-94): this milestone settles the include, policy, matcher, permission, trust and transform options, and
  adds the layouts' include keys and `MiseHardenedLayout`'s policy. An include key with no `file` resolver becomes an
  `*OptionError`, the bare-loader check moved from M3 (DL-177).

**Acceptance tests:**

- **Rank:** an included file ranks directly below its includer, and a later entry wins; the includer's `_unset` removes
  keys from it (DL-187).
- **Include behaviour** (DL-79, DL-184, DL-187):
  - the key only works at the top level, or the top of a keyed table, and is removed from the result
  - an empty list includes nothing; an empty string, a non-string element or a number is a `*ParseError`
  - an included layer's scope, variant, env, trust and config root come from its root file, and `IncludedBy` names its
    direct includer
  - an included file's format follows `WithFileFormat` rules and extensions, whatever the format order
  - `EmbeddedFS`, value scopes and bootstrap files keep the key as plain data; an `ExplicitFile` includes
  - under `FirstMatch`, an included file is never the match
  - an included file inherits its includer's trust, and the store isn't asked
- **Loading once** (DL-179): a reference listed twice, a file both found and included, and a diamond each load once, at
  the highest position, with the other routes `LayerDuplicate`; a file that includes itself is a 508.
- **Nesting** (DL-178): `MiseLayout` rejects an include key in an included file as a `*ParseError`; a custom layout with
  `IncludeNested` nests, resolves relative references against each file's own directory, and detects cycles (508).
- **References** (DL-183): `file://` and `~/` references are local; `C:\…` is a path; `git::…`, `oci::…` and `https://…`
  are 501s until M8.
- **Errors** (DL-185): missing 404, unknown scheme 501 and cycle 508 name the including file; a parse error, a 413 or an
  unreadable target names the included file, with `IncludedBy` in the report.
- **Confinement** (DL-180): includes that resolve outside the roots are a 403, symlinks included; roots are resolved
  through symlinks; `/etc/acme` doesn't cover `/etc/acmex`; searched and explicit files aren't confined. This is an
  attack suite.
- **Policies** (DL-24, DL-188 to DL-190): a scope policy replaces the default; `InheritDefault` copies it, and a later
  option replaces only its own setting; a fresh policy allows no resolver; an include is judged by its root file's
  scope, through nested includes too; explicit files by their scope's policy; an undeclared scope or an unregistered
  scheme is an `*OptionError`.
- **Allow-lists** (DL-176): a reference must match the global list and its scheme's; `Prefix`, `Glob` with `?`, and
  `MatchRef` seeing the includer's scope; `MatchRef` in `WithFileFormat` is an `*OptionError`.
- **Turning off local includes** (DL-177): `WithoutIncludeResolver("file")` with an include key is an `*OptionError`; a
  policy without `"file"` makes local references 403s.
- **Permissions** (DL-191): `MiseHardenedLayout` ignores files and directories writable by group or other, or owned by
  someone else, with `WarnFilePermissions` (Unix rules, through `cctest.FS` and on a real filesystem); a sticky
  directory and an unknown owner are allowed; `cctest.Host{GOOS: "windows"}` allows everything; *error* is a 403;
  bootstrap files are checked too.
- **Files named directly** (DL-181): an included or explicit file that would be ignored, or is untrusted, is a 403.
- **Trust store** (DL-192): untrusted files are skipped with `WarnUntrusted` as `LayerUntrusted`, and never decoded;
  untrusted bootstrap files are skipped; the digest is the SHA-256 of the raw bytes; a store error is a 500;
  `TrustPaths` takes globs and whole trees.
- **Transforms** (DL-36, DL-193): they run in order, get the layer's config root, see the tree before the include key is
  read, and cover value scopes and defaults; `nil` merges nothing; a failure or a non-canonical tree is a 422 naming the
  file; the result transform runs before `RequireConfig`.
- **Transforms and concurrency** (DL-76, moved from M5 by DL-159): modifying a transform's input doesn't change the
  report or later loads, and a transform is never called on an ignored, untrusted or duplicate layer.
- **The report** (DL-194): each scope's effective policy, in `Describe()`, its JSON and its text, and a rejected
  reference as a `LayerFailed` layer naming the policy's scope.

## M8: Remote resolvers

M8 is split into three parts, each with its own gap list, test review, PR and merge (DL-195). M8a builds what every
resolver shares, with the http resolver, and M8b and M8c add git and OCI.

### M8a: Remote includes

**Scope:** remote references in the loader, as decided in DL-195 to DL-210.

- **Test helper:** `cctest.Clock` (DL-209).
- **The resolver interface** (DL-196 to DL-199): `IncludeResolver`, `Fetched`, `WithIncludeResolver`, `IncludeRef`'s new
  fields, `WithMaxIncludeSize` and `WithIncludeTimeout`.
- **Reference forms and normalisation** (DL-33, DL-197, DL-200, DL-202): the URL, getter and image forms, checked before
  allow-listing.
- **Pins** (DL-55, DL-201): `RequirePinned`, with integrity fragments verified by the loader.
- **Remote layers** (DL-203, DL-204): loading once, metadata, and relative references inside remote files.
- **Caching** (DL-19, DL-58, DL-206, DL-207).
- **`httpresolver`** (DL-208).
- **Hardened** (DL-205): `WithoutRemoteIncludes`, added to `MiseHardenedLayout`.
- **Include limits** (DL-210): `WithMaxIncludeDepth` and `WithMaxIncludes`.
- **Options** (DL-94): this part settles the resolver, cache, size and timeout options.

**Acceptance tests,** attack tests first, mostly through a fake resolver:

- **Registration** (DL-196): the last registration of a scheme wins; a nil resolver, no schemes, an invalid scheme and
  `file` are `*OptionError`s; `WithoutIncludeResolver` removes one scheme.
- **Normalisation** (DL-197, DL-200):
  - each form's normalised string matches the documented one (DL-56), and the resolver only ever sees normalised
    references
  - userinfo is rejected, except an ssh user name
  - `%2e`, `%2f`, `%5c`, a raw `\`, control characters and non-ASCII hosts are rejected
  - the query is kept, and inner paths are validated
  - the getter form rejects unknown parameters and bad refs, and the image form needs an explicit registry
- **Matchers** (DL-56, DL-202): `Glob` `*` doesn't cross `/` and `**` does; `Regexp` must match the whole string;
  `Extension` and `FileName` see the file path only.
- **Pins** (DL-55, DL-201): an abbreviated git id isn't a pin; `RequirePinned` rejects an unpinned reference before
  fetching; an integrity fragment that doesn't match the content is a `*PolicyError`; the fragment is never passed to
  the resolver or sent to the server.
- **Fetched results** (DL-198, DL-199): the format comes from the path, then the media type, else a 415; the size limit
  gives a 413; resolver errors keep their codes, other errors are 502s and timeouts 504s; `WithIncludeTimeout` is
  honoured; identities are defaulted and checked.
- **Remote layers** (DL-203, DL-204): a reference listed twice is fetched once; the layer's metadata and `Layer.Remote`;
  under `IncludeNested`, relative references resolve against the remote origin, and `~/`, `file://` and climbing out are
  `*ReferenceError`s.
- **http** (DL-208): at most 5 redirects; none from `https` to `http`; every redirect matched against the allow-list
  again; 404 and other statuses; a `Content-Length` over the limit; `WithClient` is honoured and left unchanged.
- **Caching** (DL-58, DL-206, DL-207):
  - pinned references are never fetched again, and unpinned ones are refreshed after the TTL (with `cctest.Clock`)
  - a `*FetchError` uses a stale copy with `WarnStaleInclude`, and other errors don't
  - no cache file name contains any part of a reference
  - cached content is rechecked
  - a corrupt or unwritable cache gives `WarnIncludeCache`
  - `WithoutIncludeCache()` fetches every time
- **Hardened** (DL-205): a remote reference under `MiseHardenedLayout` is a 501, and a policy that allows a remote
  scheme without an allow-list and `RequirePinned()` is an `*OptionError`.
- **The report** (DL-203, DL-205): `Layer.Remote` and `PolicyInfo.RequirePinned`, in `Describe()`, JSON and text.
- **Include limits** (DL-210): an include nested deeper than the limit is a 508, and more references than the limit a
  413, for local and remote includes, with the defaults and with the options.
- **Fuzz tests:** `FuzzNormaliseReference` (normalising is idempotent, and the result never has userinfo, a `..`
  segment, an encoded `.`, `/` or `\`, or an invalid inner path) and `FuzzIncludeRemote` (random references through a
  fake resolver never panic, and never reach it unnormalised).

### M8b: git resolver

**Scope:** `gitresolver`, which runs the `git` program, as decided in DL-59 and DL-211 to DL-214: the fetch procedure,
the isolated environment, redirects followed before git runs, the options, and the errors.

**Acceptance tests,** attack tests first:

- **Fixtures:** test repositories are made with the git binary in temporary directories, and reached through
  `gitresolver.WithProtocols("file")`. An `httptest` server running `git http-backend` through `net/http/cgi` serves
  them over `http`. Tests skip when git is missing or older than 2.29.
- **Fetching** (DL-211): a branch, a tag, a full commit id and no ref; a SHA-256 repository; an internal test that
  forces protocol v0 for the fallback; the identity is the commit id.
- **Modes:** a symlink (mode `120000`), a submodule or a directory is a `*ReferenceError`.
- **No option injection:** a ref that starts with `-` never reaches git.
- **Transports** (DL-212): `ext::`, `file://` and `http` are blocked by default, as a `*PolicyError`, before git runs.
- **No side effects:** no hooks or filters run.
- **Isolated config** (DL-59, DL-212): a `url.insteadOf` rule, a `core.hooksPath` hook and a smudge filter in a test
  global config, and `GIT_CONFIG_COUNT`, have no effect.
- **Redirects** (DL-213): a redirect to an allowed URL is followed, one the allow-list rejects is a 403 that git never
  reaches, `https` to `http` is refused, and more than 5 hops fails.
- **Errors** (DL-214): a missing ref or path is a 404; a file over the limit is a 413; a timeout is a 504; a missing git
  is a 502.
- **Fuzz tests:** `FuzzGitArgs` and `FuzzLsTree`, each with a positive control.

### M8c: OCI resolver

**Scope:** the `ociresolver` module, with `oras-go` v2.6.4, as decided in DL-60 and DL-215 to DL-218: manifests, both
artifact forms, whiteouts, caps, credentials, plain HTTP, redirects and errors. `check-deps.sh` gains a core-oci setup.

**Acceptance tests,** attack tests first:

- **Fixtures:** a hand-written in-memory registry: ping, manifests by tag and digest, blobs, bearer-token auth, and blob
  redirects to a second server.
- **Manifests** (DL-215): a tag and a digest resolve to the manifest digest, which is the identity; an index is a 400; a
  manifest whose digest doesn't match fails.
- **Both artifact forms** (DL-60): per-file artifacts and tar layers, plain and gzip; the last layer wins; whiteouts and
  opaque directories hide earlier files; zstd is a 400.
- **Archive entries:** any absolute, `..` or `\` entry or title fails the whole fetch.
- **Regular files only:** symlinks, hard links and directories are rejected.
- **Digests:** a blob whose digest doesn't match fails.
- **Caps** (DL-216): a blob declared over the layer cap is never downloaded; an oversized decompressed layer and a file
  over the include limit are 413s.
- **Network** (DL-217): credentials only through `WithCredentials`; anonymous tokens; plain HTTP only for listed
  registries; blob redirects to another host followed, but never from `https` to `http` or past 5 hops.
- **Errors** (DL-218): a missing tag, digest or path is a 404; a 401 without credentials is a 502.
- **Fuzz tests:** `FuzzTarEntries`, with a positive control.

## M9: Hot reload

**Scope:** § [Hot reload](design.md#hot-reload), DL-37 to DL-41 and DL-219 to DL-232. M9 covers the built-in scope
kinds; the custom `Scope` interface and `Watchable` are [M9b](#m9b-custom-scopes) (DL-219).

- **Test helpers** (DL-231): `cctest.Notifier`, and timers in `cctest.Clock`.
- **Watching** (DL-220 to DL-223): `Watch[T]`, `Loader.Watch`, `Watcher`, `Change`, `KeyDiff` and the watch options.
- **The watch set** (DL-224): recorded by each watched load, with ancestors of missing dirs, explicit files, local
  includes and symlink targets.
- **Reloading** (DL-222, DL-223, DL-228): debounced full reloads, with the last good snapshot kept on failure.
- **Event sources and polling** (DL-225 to DL-227): `WithNotifier`, stat polling and the watcher's warnings.
- **Conditional fetches** (DL-229) in the cache and all three remote resolvers, and remote polling per source (DL-230).
- **The `watch/fsnotify` module** (DL-232), with a core-fsnotify setup in `check-deps.sh`.
- **Options** (DL-94): this milestone settles `WithNotifier` and the watch options.

**Acceptance tests,** with `cctest.Notifier` and `cctest.Clock`, so no real timers:

- **Debounce** (DL-228): one save gives one reload; bursts respect the quiet period and the maximum delay; a `quiet` of
  0 reloads on every event; invalid values are `*OptionError`s.
- **The watch set** (DL-224): `Watched()` holds the walk and pattern directories, the nearest existing ancestor of a
  missing one, the directories of explicit files, local includes and symlink targets; events that don't pass a
  directory's filter don't reload; a failed reload keeps the old set.
- **New files:** creating a not-yet-existing candidate (`myapp.local.toml`, or a new `conf.d` folder) triggers a reload.
- **Failures** (DL-222): a half-written file keeps the last good config and reports `Change.Err`, with
  `WarnReloadFailed`; the next event retries; the first good reload after a failure calls the handler even with no
  change.
- **Fresh on every reload** (DL-72): deleting a key from a file removes its value on the next reload, rather than
  keeping the old one, and defaults still apply.
- **Callbacks** (DL-40, DL-221, DL-234): a touch or comment-only edit doesn't fire one, unless `WithReloadOnAnyEvent`;
  `Diff` is the leaf-set difference, with origins, and its `String()` holds no values; a changed bootstrap `env`
  re-plans the watch set. `FuzzDiff` checks `Diff` against a reference model, with empty tables, and checks that it
  replays and reverses.
- **Lifecycle** (DL-77, DL-220, DL-223): a failed first load returns a nil watcher; a nil handler works; cancelling
  `ctx` stops the watcher; `Close` twice is fine; `Close` and `Reload` from inside the handler don't deadlock; `Reload`
  after `Close` is a 499; a panicking handler is recovered and watching continues; handler calls never overlap; a value
  moving to another file isn't a change; the same instant written differently is a change (DL-233), and NaN equals NaN.
- **Event sources** (DL-225, DL-227): the factory is called once per watcher; a factory error, a failed `Add`, an error
  on `Errors` and a closed `Events` channel each behave as decided, with `WarnWatchFailed`.
- **Polling** (DL-226): without a notifier, and with `WithPolling()`, edits, creations, removals and same-size edits are
  seen at `WithPollInterval`'s pace.
- **Conditional fetches** (DL-229):
  - **http:** a server that counts conditional requests and answers 304, for an ETag and for `Last-Modified`
  - **git:** a local repo whose branch moves, or doesn't
  - **OCI:** the in-memory registry, with no blob requested when the digest is unchanged
  - **cache:** revalidation after the TTL, `"revalidated"` in the report, the stale fallback on a `*FetchError`, and an
    oversized or non-ASCII validator dropped
- **Remote polling** (DL-230): matched references are polled at their own intervals, the most recent rule winning; 0
  never polls; an unchanged poll doesn't reload, and a changed one does; failures back off up to 8 times; without the
  cache, a poll is a full reload.
- **`watch/fsnotify`** (DL-232), against a real filesystem:
  - an editor-style atomic rename
  - a Kubernetes ConfigMap `..data` symlink swap, built as the kubelet does
  - falling back to polling, with a warning, when a watch fails, using a mode 000 directory; skipped as root and on
    Windows

## M9b: Custom scopes

**Scope:** the `Scope` interface, `Discovery`, `Source` and `Watchable` from
[Extension interfaces](api.md#extension-interfaces), as decided in DL-219 and DL-236 to DL-240: the `Custom` kind,
loading sources, and how a custom scope joins the watch set.

**Acceptance tests,** attack tests first:

- **Registration** (DL-236): a nil scope and `BootstrapFiles` on one are `*OptionError`s; the default trust is
  `TrustProject`, and `Trust` overrides it.
- **`Discovery`** (DL-237): every field comes from the loader and the `cctest` host.
- **Broken scopes** (DL-238): a `Source` with both or neither of `Path` and `Tree`, a relative `Path`, an unregistered
  `Format` or a non-canonical tree is a 500 naming the scope; a coded error from `Discover` keeps its code, and any
  other is a 500.
- **`Path` sources** (DL-238): the order returned, with `Variant` and `Env`; symlinks, ignored paths, dedup, permission
  checks, the trust store, includes, `FirstMatch` and `RequireFile`; a missing file is a 404.
- **`Tree` sources** (DL-238): checked, copied, `_unset` stripped with a warning, no includes, not counted for
  `RequireFile`.
- **Watching** (DL-239): `Path` sources are watched without `Watchable`; `WatchDirs` directories have no name filter;
  missing ones go through their ancestors; a relative directory or an error is warned about; the set is re-planned on
  every reload, and a plain `Load` never calls `WatchDirs`.
- **Report** (DL-240): the kind is `"Scope"`, the policy is shown, and there are no probes.
- **Concurrency:** parallel loads under `-race`.
- **Fuzz test:** `FuzzCustomSources`, with a positive control.

## M10: Presets

M10 is split into two parts (DL-242), each with its own gap list, test review, PR and merge.

### M10a: Precedence

**Scope:** [precedence.md](precedence.md) and DL-243 to DL-247: the precedence spec, the oracle, conformance tests and
`FuzzPrecedence`, and the fix that keeps value scopes in explicit-only loads.

**Acceptance tests:**

- **Drift** (DL-244): the tables in precedence.md parse, and equal the oracle's.
- **Conformance** (DL-245): at least one test per `PR-n` rule, and the worked examples, with hand-written orders.
- **Explicit-only loads** (DL-247): under `WithConfigFilesOnly()`, defaults and `Values` still merge.
- **Fuzz** (DL-246): `FuzzPrecedence` against the oracle, and the mutation check in place of a red run.

### M10b: Presets

**Scope:** `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset` in the `presets` module, as decided in DL-248
to DL-252.

**Acceptance tests:**

- **Doc examples** (DL-250): every usage snippet in design.md and api.md is an `Example` function that runs against
  `cctest.FS`, and a drift test checks each snippet appears in one, unchanged.
- **Composition** (DL-248, DL-251): each preset's name, layout and capabilities match
  [Layouts vs presets](api.md#layouts-vs-presets): each format loads; `mise.Preset`'s `http` resolver fetches, and its
  `git` and `oci` resolvers are registered; `misehardened.Preset` rejects remotes; each default notifier reloads a real
  directory.
- **Order** (DL-251): each preset ranks a rich fixture exactly as its layout does.
- **Dependency budget** (DL-252): a `check-deps.sh` setup per preset package.

## M11: Documentation, API and examples

M11 is split into four stages (DL-253), each with its own gap list, review and PR. Releasing waits for a later stage.

### M11a: Documentation structure

**Scope:** DL-254 and DL-255, and [decision record 0004](decisions/0004-documentation-structure.md). Not test-first.

- **Four homes:** user docs in `docs/`, design records in `docs/design/`, plans in `docs/planning/` and contributor docs
  in `docs/contributing/`, moved with `git mv` and with their names unchanged. Each moved doc says which kind it is, and
  `docs/README.md` indexes them.
- **CONTRIBUTING.md:** setup, the hooks, commit style, tests first, decision records and the doc homes.
- **Links:** pre-push runs `rumdl check .` on every markdown file, so a moved file or renamed heading can't leave a
  broken link anywhere.
- **Paths:** the doc-snippet test (a `test: revise` commit), `check-deps.sh`, `lefthook.yml` and `doc.go`.

### M11b: API audit and guard

**Scope:** decided in its own gap list.

- **Audit, once:** every signature listing in api.md and design.md is checked against the packages' real APIs with
  `go/types` (DL-250). Each difference is raised: a doc error is fixed in the doc, and a real change to the design is
  decided first.
- **Guard, from then on:** a file per released module listing its exported API, checked both ways, so an exported name
  that isn't listed fails, as does one listed that's gone.

### M11c: Examples

**Scope:** decided in its own gap list, starting with the quality bar the examples must meet. They're how the library's
overall quality is judged before the release stage.

- **Programs:** small `main` programs in the `examples` module, never released, each running for real with `go run .`
  from its own directory.
- **Tests:** each program's work is a function its test runs against a `cctest` world, compared with golden output; no
  test reads the real host.

### M11d: README and user guide

**Scope:** decided in its own gap list.

- **README:** install commands, quick starts copied from the M11c examples, the modules and what each adds, and a
  pre-1.0 note.
- **User guide:** what a user needs that only the design records explain today, such as merging.
- **Snippets:** the doc-snippet test covers the README and the guides, and moves out of `presets/mise`.

## Release stage

Not scheduled yet: nothing goes public, and `wip` doesn't go to `main`, until this stage starts. It has its own gap
list.

- **Package documentation:** each package's `doc.go` gives an overview that reads well on pkg.go.dev.
- **Licences:** confirm that the licences of `mapstructure`, `go-toml`, `go-yaml`, `fsnotify` and `oras-go` are
  compatible with Apache-2.0, and check their maintenance status (an open item from
  [api.md](api.md#package-layout-decisions)).
- **CI (GitHub Actions):** a workflow that runs the same `scripts/` on every PR to `wip` and `main`, with merging
  blocked until they pass. It's added now at the latest, or earlier if a second contributor joins.
- **Release:** `wip` goes to `main` by fast-forward or merge commit, never by squash.
- **Vulnerability reporting:** `SECURITY.md` points at GitHub's private vulnerability reporting, which GitHub only
  offers on public repositories. Enable it in the repository settings when the repository is made public.
- **Tags:** the core `v0.1.0` first, then the other modules, then `presets`, in the order set out in
  [project-layout.md](project-layout.md#versioning-and-releases).

## M12: Conformance

**Scope** (DL-174): measure each built-in decoder against its format's test suite. Spec-correctness is a guiding
principle from the start, and every deliberate deviation is already documented; this milestone makes the degree of
conformance a measured number.

- **Suites:** [yaml-test-suite](https://github.com/yaml/yaml-test-suite) for `format/yaml`,
  [toml-test](https://github.com/toml-lang/toml-test) (the TOML 1.1 set) for `format/toml`, and
  [JSONTestSuite](https://github.com/nst/JSONTestSuite) for `format/json` and `internal/json`.
- **Vendored, not imported:** each suite's files at a pinned release, with its licence and a script to refresh them, so
  the dependency counts don't change.
- **Every case is sorted into one of three groups,** and the counts are printed:
  - **pass:** the tree matches the suite's expected result, or a case that must fail does
  - **deliberate deviation:** with the reason and the decision behind it, such as a multi-document YAML stream, a tag
    outside the core schema, or a top level that isn't a table
  - **known library failure:** listed, so that an upgrade that fixes one fails the test until it moves to pass

  Any other outcome fails the test.
- **JSONTestSuite's arrays and scalars:** most of its cases aren't objects, so each runs twice: as a document, for the
  top-level rule, and wrapped as `{"v": …}`, for the value itself.
- **Findings:** a bug found in a decoder approved in an earlier milestone is fixed in its own commit and raised first.

## Decisions

| # | Question                  | Decision                                                                                                     |
| - | ------------------------- | ------------------------------------------------------------------------------------------------------------ |
| 1 | Branching                 | A branch and PR per milestone, **targeting `wip`**, not `main`. `main` takes only releases, from `wip`. The acceptance tests are reviewed locally before the push, since pre-push won't take red tests. The PR is the record. |
| 2 | CI                        | **Deferred** until the release stage, or earlier if a second contributor joins. Until then every check runs from lefthook, as a script in `scripts/` that CI will reuse. |
| 3 | Clock injection           | An internal clock interface, settable only through `cctest`. No public `WithClock` option.                    |
| 4 | Public `cctest` package   | Yes: in-memory filesystem, fake notifier, fake clock and golden-file helpers ([api.md](api.md#test-helpers)). |
| 5 | `internal/compat` module  | Yes, a test-only module for koanf parity tests, so koanf never enters the core `go.mod`.                     |
| 6 | Dependency budget         | Yes, from M6, as `scripts/check-deps.sh` in lefthook's pre-push. It doesn't need CI.                         |
| 7 | Review gate               | Once per milestone, and once per part for M4 and M8.                                                         |
| 8 | Licence                   | Apache-2.0, with a `LICENSE` file in the repo root and in every module root (M0).                            |
| 9 | Repository files          | A README stub, `SECURITY.md` and a per-module CHANGELOG convention, added in M0.                             |
| 10 | Tooling fixes            | Fix the goimports `local-prefixes`, and pin every tool version in `.config/mise.toml` (M0).                  |

**Working with `wip` as the integration branch:**

- **Never squash `wip` into `main`.** Use a fast-forward or a merge commit. A squash gives `main` a commit that `wip`
  lacks, and every later release then conflicts.
- **PRs need `--base wip`,** because GitHub targets the default branch, `main`, otherwise.
- **Closing keywords** ("Closes #12") only close issues when a PR merges into the default branch, so issues closed by
  milestone PRs are closed by hand or on release.
- **Release tags go on `main`.**
- **Hooks are never skipped.** Until CI exists, lefthook is the only enforcement, so no commit or push uses
  `--no-verify` or `LEFTHOOK=0`.
