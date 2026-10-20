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
| M8  | Remote resolvers                  | core (`httpresolver`, `gitresolver`), `ociresolver` | M7 |
| M9  | Hot reload                        | core, `watch/fsnotify`                   | M5, M7     |
| M10 | Presets                           | `presets`                                | M6, M8, M9 |
| M11 | Release preparation               | all                                      | M10        |
| M12 | Conformance                       | `format/*`, core (`internal/json`)       | M6         |

M2 and M3 can be done in either order. M6 can run any time after M1. M12 comes last, but needs only M6.

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
  | `cctest.Clock`                      | M8        | the internal clock, first needed for cache TTLs; reused in M9  |
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

**Scope:**

- **The include key** and nesting rules for each preset.
- **Include rank** (DL-12).
- **The `file` resolver** with `Confine` (DL-23).
- **Allow-lists** and matchers.
- **Policies:** `WithDefaultPolicy` and `WithScopePolicy`, with replace-not-merge semantics (DL-24).
- **Trust:** `RequireTrust` and `TrustPaths` (DL-28).
- **The permission policy** (DL-20), with owners in `cctest.FS` (DL-108).
- **Layer and result transforms** (DL-36).
- **Not `$import`:** deferred from M4d, and possibly never supported (DL-145).
- **Options** (DL-94): this milestone settles the include, policy, matcher and transform options, and adds the layouts'
  include keys and `MiseHardenedLayout`'s policies. An include key with no `file` resolver becomes an `*OptionError`,
  the bare-loader check moved from M3.

**Acceptance tests:**

- **Rank:** an included file ranks directly below its includer, and a later entry wins.
- **Include behaviour** (DL-79): the key only works at the top level and is removed from the result; a diamond loads the
  shared file once; an `EmbeddedFS` source's include key is plain data; an included file inherits its includer's trust.
- **Nesting:** `MiseLayout` rejects nesting, and `CosmiconfigLayout` detects cycles (508).
- **Confinement:** includes that resolve outside the confinement roots are a 403, symlinks included. This is an attack
  suite.
- **Policies:** a scope policy replaces the default, `InheritDefault` copies it, and an include is judged by its
  includer's scope, through nested includes too.
- **Permissions:** files writable by group or other are ignored with a warning in `MiseHardenedLayout` (Unix).
- **Trust store:** untrusted files are skipped with a warning.
- **Transforms:** they run in order, get the layer's config root, and a failure is a 422 naming the file.
- **Transforms and concurrency** (DL-76, moved from M5 by DL-159): modifying a transform's input doesn't change the
  report or later loads, and a transform is never called on an ignored or untrusted layer.
- **The report** gains each scope's effective policy (DL-159).

## M8: Remote resolvers

**Scope:**

- **Test helper:** `cctest.Clock` ([M0](#m0-repository-and-tooling)).
- **`httpresolver`.**
- **`gitresolver`**, which runs the `git` program.
- **The `ociresolver` module.**
- **Caching** (DL-19).
- **Traversal protection** (DL-33).
- **Options** (DL-94): this milestone settles the resolver and cache options, and adds `WithoutRemoteIncludes` to
  `MiseHardenedLayout`.

**Acceptance tests,** all attack tests first:

- **References:**
  - normalised before allow-listing
  - userinfo rejected
  - `%2e`, `%2f` and `%5c` rejected
  - inner paths validated
  - the normalised string for each scheme matches the documented form (DL-56)
- **Matchers:** `Glob` `*` doesn't cross `/` and `**` does; `Regexp` must match the whole string.
- **Pins** (DL-55): an abbreviated git id isn't a pin; an https integrity fragment that doesn't match the downloaded
  bytes is a `*PolicyError`; the fragment is never sent to the server.
- **http:**
  - at most 5 redirects
  - none from `https` to `http`
  - every redirect matched against the allow-list again
  - the size limit gives a 413
  - `WithClient` is honoured
- **git:**
  - **Fixtures:** test repositories are created in temporary directories.
  - **Modes:** a symlink (mode `120000`) or a submodule is rejected.
  - **No option injection:** a ref that starts with `-` is rejected.
  - **Transports:** `ext::` and `file://` are blocked.
  - **No side effects:** no hooks or filters run.
  - **Isolated config:** a `url.insteadOf` rule in a test `~/.gitconfig` has no effect (DL-59).
  - **Fixtures** are reached through `gitresolver.WithProtocols("file")`, which the tests set explicitly.
- **oci:**
  - **Fixtures:** an in-memory registry.
  - **Archive entries:** any absolute or `..` entry fails the whole fetch.
  - **Regular files only:** symlinks and hard links are rejected.
  - **Digests:** a digest mismatch fails.
  - **Both artifact forms** (DL-60): per-file artifacts and tar layers; the last layer wins; an oversized decompressed
    layer fails.
- **Caching:** pinned references are never re-fetched, unpinned ones are refreshed after the TTL, and a failed fetch
  uses a stale copy with a warning. No cache file name contains any part of a URL (DL-58). Cached content is rechecked
  against policy. `WithoutIncludeCache()` fetches every time.
- **Hardened:** a remote reference under `MiseHardenedLayout` is a load error.

## M9: Hot reload

**Scope:** § [Hot reload](design.md#hot-reload) and DL-37 to DL-41:

- **Test helper:** `cctest.Notifier` ([M0](#m0-repository-and-tooling)).
- **Watching:** `Watch[T]`, `w.Current()`, `w.Reload()` and the watch options.
- **The watch set:** planning it, including the ancestors of missing dirs.
- **Reloading:** debounced full reloads, with the last good snapshot kept on failure.
- **Callbacks:** only on changes, and one at a time.
- **Stat polling,** and opt-in remote polling.
- **The `watch/fsnotify` module.**
- **Options** (DL-94): this milestone settles `WithNotifier` and the watch options.

**Acceptance tests,** with `cctest.Notifier` and `cctest.Clock`, so no real timers:

- **Debounce:** one save gives one reload, and bursts respect the quiet period and the maximum delay.
- **New files:** creating a not-yet-existing candidate (`myapp.local.toml`, or a new `conf.d` folder) triggers a reload.
- **Failures:** a half-written file keeps the last good config and reports `Change.Err`, and the next event retries.
- **Fresh on every reload** (DL-72): deleting a key from a file removes its value on the next reload, rather than
  keeping the old one, and defaults still apply.
- **Callbacks:** a touch or comment-only edit doesn't fire one, and a changed bootstrap `env` re-plans the watch set.
- **Lifecycle** (DL-77): cancelling `ctx` stops the watcher; `Close` twice is fine; `Close` from inside the handler
  doesn't deadlock; a panicking handler is recovered and watching continues; a value moving to another file isn't a
  change; `time.Time` values are compared as instants.
- **`watch/fsnotify`,** against a real filesystem:
  - an editor-style atomic rename
  - a Kubernetes ConfigMap `..data` symlink swap
  - falling back to polling, with a warning, when a watch fails

## M10: Presets

**Scope:** `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset` in the `presets` module.

**Acceptance tests:**

- **End to end:** every Go example in design.md and api.md runs as a test against `cctest.FS`.
- **Composition:** each preset's layout and registered capabilities match
  [Layouts vs presets](api.md#layouts-vs-presets).
- **Dependency budget:** each preset matches the measured counts (5 for `misehardened`, 9 for `mise`).

## M11: Release preparation

Not test-first.

- **Documentation:** a README with quick starts for each preset, and package documentation.
- **Examples:** a `examples/` module, not released.
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
| 2 | CI                        | **Deferred** until M11, or earlier if a second contributor joins. Until then every check runs from lefthook, as a script in `scripts/` that CI will reuse. |
| 3 | Clock injection           | An internal clock interface, settable only through `cctest`. No public `WithClock` option.                    |
| 4 | Public `cctest` package   | Yes: in-memory filesystem, fake notifier, fake clock and golden-file helpers ([api.md](api.md#test-helpers)). |
| 5 | `internal/compat` module  | Yes, a test-only module for koanf parity tests, so koanf never enters the core `go.mod`.                     |
| 6 | Dependency budget         | Yes, from M6, as `scripts/check-deps.sh` in lefthook's pre-push. It doesn't need CI.                         |
| 7 | Review gate               | Once per milestone, and once per part for M4.                                                                |
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
