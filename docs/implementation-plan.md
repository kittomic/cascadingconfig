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
| M3  | Options, presets and `New`        | core                                     | M1         |
| M4  | Discovery                         | core                                     | M2, M3     |
| M5  | Decoding, results and reports     | core, `internal/compat` (test-only)      | M4         |
| M6  | Format modules                    | `format/toml`, `format/yaml`             | M1         |
| M7  | Includes, policies and transforms | core                                     | M4         |
| M8  | Remote resolvers                  | core (`httpresolver`, `gitresolver`), `ociresolver` | M7 |
| M9  | Hot reload                        | core, `watch/fsnotify`                   | M5, M7     |
| M10 | Presets                           | `presets`                                | M6, M8, M9 |
| M11 | Release preparation               | all                                      | M10        |

M2 and M3 can be done in either order. M6 can run any time after M1.

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

**Scope:** merging trees layer by layer, with provenance. No discovery yet: tests pass layers in directly.

**Acceptance tests:**

- **Deep merge:** objects merge, arrays and scalars are replaced (DL-8, § [Merging](design.md#merging)).
- **Per-key rules** (DL-10, § [Per-key merge rules](design.md#per-key-merge-rules)):
  - every strategy
  - wildcard matching
  - the most specific rule wins
  - ambiguous rules are an error when registered
  - `Replace` and `Func` cover their subtree
  - type mismatches name both files
- **`_unset`** (DL-11, § [Unsetting keys](design.md#unsetting-keys)):
  - both path forms, and nested placement
  - removal happens before the layer's own values merge
  - only lower layers are affected
  - a missing key is a no-op
  - the key is stripped from the result
  - it can be renamed or turned off
- **Null** (§ [Null values](design.md#null-values-yaml-and-json)):
  - replaces any type
  - is replaced by any value
  - is never a type mismatch
  - stays in the result
- **Same-slot formats** (DL-3, § [File format precedence](design.md#file-format-precedence)): reverse-precedence merge
  order, and the `merge`, `first` and `error` policies.
- **Provenance:** `Origin` kinds and history for set, appended, null and unset values.

## M3: Options, presets and `New`

**Scope:** § [Building a loader](api.md#building-a-loader): sealed options, the option kinds, `Preset`, `Extend`, `New`,
`NewWithPreset`, validation and env-var binding names.

**Acceptance tests:**

- **Applied in order:** `WithScopes` then `WithoutScopes` (DL-29). `Extend` never changes its receiver.
  `NewWithPreset(n, p, a)` behaves exactly like `NewWithPreset(n, p.Extend(a))`.
- **Validation:** every problem is reported at once, as `*OptionError`s:
  - unknown scopes
  - an unsafe app name
  - invalid formats
  - the three combinations a [bare loader](api.md#a-loader-without-a-preset) rejects
- **Env-var bindings:** `{NAME}` is derived as documented (`my-app` → `MY_APP_…`), and bindings can be renamed and
  turned off (DL-15, § [Environment variables](design.md#environment-variables)).
- **Compile-time checks:** misplaced option kinds don't compile, for example a `WatchOption` passed to `New`.
- **Empty calls:** `WithEnvs()` and `WithConfigFiles()` with no arguments leave the env vars in charge; empty
  `WithScopes()` and `WithFormats()` are `*OptionError`s (DL-73).
- **Layout options:** `MiseLayout`, `MiseHardenedLayout` and `CosmiconfigLayout` are built only from public options
  (DL-71). This milestone settles their exact signatures.

## M4: Discovery

The largest milestone, done in three parts.

### M4a: Filesystem, scopes and the walk

**Scope:**

- **Test helpers:** `cctest.FS`, `cctest.Host` and the golden-file helpers ([M0](#m0-repository-and-tooling)).
- **The filesystem hook:** OS-backed, with `os.Root` and symlinks, plus the `fs.FS` adapter (§
  [Filesystem hook](design.md#filesystem-hook)).
- **Platform dirs** (DL-14).
- **Scope kinds:** `FixedDir`, `Walk`, `ExplicitFile`, `Embedded`, `EmbeddedFS`, `Values` and `EnvVars` (DL-25, DL-35).
- **`MiseLayout`'s scopes and patterns.**
- **The directory walk** and ceilings (DL-5).
- **Dedup** after resolving symlinks.
- **Explicit config files** (DL-16).

**Acceptance tests:**

- **Golden discovery order** for one directory, matching
  [config-locations.md 3.2](config-locations.md#32-filenames-within-one-project-directory) (layers A–D) and the
  [worked example](design.md#environments).
- **Walk limits:** a start dir inside `~` stops after `~`, and one outside `~` walks to `/`. Ceilings are exclusive.
- **Dedup:** a file reached as both a project file of `~` and a user file is loaded once, at its higher position, with
  user trust and user policy (DL-61). Hard links count as one file (DL-66).
- **Symlinks:** a dotfiles symlink leaving the searched directory loads, and `Confine` judges its target (DL-62).
- **Special files:** a FIFO named like a candidate is skipped with a warning, without blocking (DL-63).
- **Size:** a local file over the limit is a 413 (DL-64).
- **Case:** `MyApp.TOML` doesn't match `myapp.toml`, on `cctest.FS` and on a real case-insensitive filesystem (DL-66).
- **Paths:** relative env-var paths resolve against the current directory and bootstrap paths against their file; `~` is
  expanded; ignored paths match files and directory prefixes (DL-68, DL-69).
- **Explicit files** replace the project scope only (everything in `CosmiconfigLayout`), and a missing one is a 404.
  They load with any registered decoder, even one the format order leaves out, and an unregistered format is a 415
  (DL-67).
- **`EnvVars`:** `__` nesting, and discovery bindings are excluded.
- **Windows:** `%ProgramData%\{name}`, tested through `cctest.Host{GOOS: "windows"}` (DL-70).

### M4b: Environments, formats and bootstrap

**Scope:**

- **Environment selection** and platform envs (DL-30).
- **Safe environment names** (DL-32).
- **Format precedence:** env var > bootstrap file > option > preset (DL-4).
- **Bootstrap search,** allowed keys and scope limits (DL-7, DL-26).
- **Scope trust levels.**

**Acceptance tests:**

- **Platform envs** rank below selected ones, and an explicitly selected platform name ranks as selected.
- **Names:** dotted names match literally and are never combined (DL-31). Traversal attempts are rejected from every
  source.
- **Format order:** a project bootstrap file's `formats` affects only the project scope, and `formats = []` is an error.
- **Scope limits:** a project bootstrap file's `ignored_config_paths` entry that points at `/etc/{name}/…` is dropped
  with a warning (DL-26).
- **Hardened layout:** project bootstrap files may only set `env` and `formats` in `MiseHardenedLayout`; any other key
  is dropped with a warning.
- **Bootstrap keys** (DL-78): an unknown key warns, `env = "dev"` is an `*OptionError`, and `include` or `_unset` in a
  bootstrap file is ignored with a warning.

### M4c: `CosmiconfigLayout`

**Scope:**

- **First-match resolution.**
- **Search places:** rc files and `.config/` in the order of
  [config-locations.md 2.1](config-locations.md#21-search-places-within-one-directory), without JS/TS.
- **Search strategies:** `None`, `Project` (with `WithRootMarker`) and `Global` (with `WithStopDir`).
- **The meta config** in `.config/config.{ext}`. There's no `package.json` or `package.yaml` support
  ([Deviations from cosmiconfig](design.md#deviations-from-cosmiconfig)).
- **Extensionless `.{name}rc`** through `WithExtensionlessFormat`.
- **The global dir** through an `env-paths`-compatible resolver.

**Acceptance tests:**

- **Search order:** golden tables for each search strategy, including the stop dir and root marker being inclusive. The
  default searches only the start dir and not the user dir (DL-42).
- **Invalid combinations:** `WithStopDir` without `Global`, and `Project` without a root marker, are `*OptionError`s.
- **Node.js sources are ignored:** a `package.json` with a `{name}` key loads nothing (DL-44).
- **`$import` arrays** concatenate in `CosmiconfigLayout` (DL-43).
- **Empty files** are skipped.
- **The meta config's `{name}` key** is checked before the normal search.

## M5: Decoding, results and reports

**Scope:**

- **Decoding:** `Load[T]` and `DecodeAt`, with koanf-compatible decoding: the `koanf` tag, mapstructure v2 and the same
  hooks (§ [Decoding](api.md#decoding)).
- **Unknown keys:** a warning by default, or an error in strict mode.
- **`RequireConfig`** at every level, globally and per scope ([api.md](api.md#requiring-config)).
- **The date hook:** RFC 3339 strings into `time.Time`.
- **Error aggregation:** every error from one load is returned together.
- **Reporting:** `Report`, `OriginOf` and `KoanfProvider`.

**Acceptance tests:**

- **koanf parity:** the same structs and trees decode identically with koanf and with cascadingconfig, including day
  durations, `TextUnmarshaler` and weak typing. These tests live in a test-only module, `internal/compat`, so koanf
  never appears in the core `go.mod`.
- **Null and `_unset`** leave Go-side defaults untouched. With `ZeroFields: true`, a null clears them.
- **Defaults** (DL-72):
  - `WithDefaults(v)` is applied on every load, for `Load[T]`, `res.Decode` and `KoanfProvider`
  - only non-zero fields become defaults, and durations, times and `TextMarshaler` types convert to canonical forms
  - a higher `null` or `_unset` clears a default
  - `OriginOf` reports "set by defaults"
  - `RequireConfig(RequireFile)` isn't satisfied by defaults alone
- **Decode errors** name the key and the file that set it.
- **No values:** a golden `Report.String()` and JSON contain no config values. Secret values put in fixtures must never
  appear in the output.
- **Several broken files** produce one joined error listing all of them.
- **Failed loads** return a non-nil `*Result` whose report shows what broke, with a nil `Tree()` (DL-75).
- **Concurrency** (DL-76): parallel `Load`s under `-race`; modifying `Tree()`'s result or a transform's input doesn't
  change the report or later loads; a transform is never called on an ignored or untrusted layer.
- **`RequireConfig`** (DL-54): `RequireAny` is satisfied by flags or a defaults scope; `RequireFile` is not; a
  whitespace-only file fails `RequireNonEmpty`; with scopes, each listed scope must qualify; several requirements all
  apply.
- **Dates:** an RFC 3339 string decodes into `time.Time` and into `string`. A date-only string into `time.Time` is a
  `*DecodeError`. The koanf parity tests cover everything except dates, which are a documented difference (DL-48).

## M6: Format modules

**Scope:** `format/toml` (`pelletier/go-toml/v2`) and `format/yaml` (`goccy/go-yaml`).

**Decision needed first:** these are the first modules to import the core, so `scripts/check-gowork-off.sh` can't build
them until the core can be fetched on its own, as a pushed commit or a release. How that check treats them before the
first release is decided at the start of this milestone.

**Acceptance tests:**

- **Canonical types:**
  - every TOML date-time and YAML timestamp becomes an RFC 3339 string (DL-48)
  - YAML `uint64` integers become `int64`, and values out of range are a `*ParseError` (DL-49)
  - YAML non-string keys become strings (DL-50)
- **Dialects** (DL-50 to DL-53):
  - YAML `yes`, `no`, `on` and `off` stay strings
  - several YAML documents in one file, and unknown tags, are `*ParseError`s
  - duplicate keys are `*ParseError`s in TOML and YAML
  - a leading byte-order mark is stripped in both, which matters most for YAML, where it would otherwise end up in the
    first key
  - TOML 1.1 syntax that the pinned go-toml accepts parses
  - an alias bomb hits the expansion cap
- **Positions:** line and column reach `*ParseError`.
- **YAML extensions:** `.yml` is decoded as YAML, `MYAPP_CONFIG_FORMATS=yml` means `yaml`, and when `.yaml` and `.yml`
  share a slot, `.yaml` wins (DL-47).
- **Across formats:** the same document in TOML, YAML and JSON gives an identical tree.
- **Fuzz:** both decoders never panic and only produce canonical types.
- **Dependency budget:** `scripts/check-deps.sh` checks the counts in
  [project-layout.md](project-layout.md#dependency-analysis), from lefthook's pre-push and from here on.

## M7: Includes, policies and transforms

**Scope:**

- **The include key** and nesting rules for each preset.
- **Include rank** (DL-12).
- **The `file` resolver** with `Confine` (DL-23).
- **Allow-lists** and matchers.
- **Policies:** `WithDefaultPolicy` and `WithScopePolicy`, with replace-not-merge semantics (DL-24).
- **Trust:** `RequireTrust` and `TrustPaths` (DL-28).
- **The permission policy** (DL-20).
- **Layer and result transforms** (DL-36).

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

## M8: Remote resolvers

**Scope:**

- **Test helper:** `cctest.Clock` ([M0](#m0-repository-and-tooling)).
- **`httpresolver`.**
- **`gitresolver`**, which runs the `git` program.
- **The `ociresolver` module.**
- **Caching** (DL-19).
- **Traversal protection** (DL-33).

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
