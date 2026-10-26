# cascadingconfig design

**Kind: design record.** It explains why the library is built as it is, and it grows as decisions are made. Its code
listings record the design when it was decided; the current API is in the package documentation.

This document records the design decisions for `cascadingconfig`'s config discovery and merging: presets, scopes,
formats, includes, trust and the loader model. The research it builds on is in
[config-locations.md](config-locations.md). That file describes how cosmiconfig and mise search for and combine config
files, and lists where the two diverge.

Notation (`{name}`, `{env}`, `{ext}`, precedence written **lowest → highest**) is defined in
[config-locations.md](config-locations.md#notation).

## Overview

**The order of layers** (which files are candidates, and where each ranks) is specified in
[precedence.md](../precedence.md), which is normative (DL-243). The sections below explain the layouts, and defer to it
on order.

- **The default preset is `mise.Preset`.** Where the two tools diverge, it follows mise, except for the deliberate
  changes listed in [Deviations from mise](#deviations-from-mise).
- **We ship three presets.** `mise.Preset` and `cosmiconfig.Preset` each mirror their tool as closely as Go allows. The
  main exception is that JS/TS config formats aren't supported. `misehardened.Preset` is `mise.Preset` with hardened
  settings (see [`misehardened.Preset`](#misehardenedpreset)).
- **Users can define their own presets.** A preset sets the scopes, the search paths and patterns within each scope, the
  variants, and the precedence. The built-in presets use the same API as user presets, with no special-casing.
- **Presets can extend other presets.** `MiseHardenedLayout` is defined as an extension of `MiseLayout`, and user
  presets can be built the same way (see [api.md](api.md#layouts-vs-presets)).
- **Writing config files is out of scope for now** (see
  [Non-goals](#non-goals)).

## Scope terminology

We use these names for the tiers, mapped to each tool's own terms:

| Scope      | mise term | cosmiconfig term        | Kind                | Default location             |
| ---------- | --------- | ----------------------- | ------------------- | ---------------------------- |
| `system`   | system    | none                    | fixed dir           | `/etc/{name}`                |
| `user`     | global    | global config dir       | fixed dir           | `$XDG_CONFIG_HOME/{name}`    |
| `project`  | project   | directory walk          | walk (start → stop) | every dir from cwd upwards   |
| `override` | override  | none                    | fixed dir           | `{user}/config.override.*`   |

**"Local" is not a scope.** As in mise, `.local` marks a *variant*: a personal, uncommitted file, such as
`{name}.local.toml` in a project dir or `config.local.toml` in the user dir. It can appear in any scope. Variants are
independent of scopes:

| Variant     | Example                   |
| ----------- | ------------------------- |
| `shared`    | `{name}.toml`             |
| `env`       | `{name}.{env}.toml`       |
| `local`     | `{name}.local.toml`       |
| `env-local` | `{name}.{env}.local.toml` |

The three presets, in these terms:

| Preset              | Scopes (lowest → highest)                | Variants          | Resolution                                            |
| ------------------- | ---------------------------------------- | ----------------- | ----------------------------------------------------- |
| `mise.Preset`        | system, user, project, override          | all four          | merge all                                             |
| `misehardened.Preset`  | as `mise.Preset`                          | as `mise.Preset`   | as `mise.Preset`; remote includes disabled             |
| `cosmiconfig.Preset` | project (the start dir only), plus user with the `Global` search strategy | `shared` only | first match; the walk is set by the [search strategy](#search-strategies) (default: none) |

## Environments

Environments select the `env` and `env-local` [variants](#scope-terminology): files like `{name}.{env}.toml` and
`{name}.{env}.local.toml`.

**Selection**, highest precedence first, as in mise (flag > `MISE_ENV` > `.miserc.toml`):

1. `WithEnvs(...)`, the app's option, normally fed from a `--env` / `-E` flag
2. `{NAME}_ENV`, a comma list
3. `env = [...]` in a bootstrap file

The first source that is set wins. A name selected twice keeps only its later position (DL-121). An unsafe name from
`{NAME}_ENV` is an `*OptionError` at `Load` whose `Op` is the variable's name, and so is a built filename longer than
255 bytes or one that leaves its directory.

**Platform environments** are always active (see [Deviations from mise](#deviations-from-mise)):

- **Names:** `unix` (where it applies), then `{os}`, then `{os}-{arch}`, such as `linux-x64` or `macos-arm64`. The names
  come from Go's `GOOS` and `GOARCH`:

  | Name part | Rule                                                                                                |
  | --------- | --------------------------------------------------------------------------------------------------- |
  | `unix`    | Active on every `GOOS` except `windows`, `plan9`, `js` and `wasip1`. So FreeBSD, Android and iOS count as `unix`. |
  | `{os}`    | `darwin` becomes `macos`, as mise names it. Every other `GOOS` keeps its Go name: `linux`, `windows`, `freebsd`, `android`, … |
  | `{arch}`  | `amd64` becomes `x64`, as mise names it. Every other `GOARCH` keeps its Go name: `arm64`, `386`, `riscv64`, `ppc64le`, … |

- **Rank:** they rank **below** every selected environment.
- **Selecting one explicitly** (for example `MYAPP_ENV=linux`) takes it out of the platform list, so it ranks as a
  selected environment, as in mise.
- **Windows:** the OS family is the OS itself, so the list is `windows`, `windows-{arch}`, as in mise.
- **Not part of the selection:** they don't appear in `{NAME}_ENV` or in the selected list that `Describe()` reports as
  chosen. `Describe()` lists them separately.

**Order within one directory**, lowest → highest:

1. **shared** files
2. **env** files, one group per environment: platform envs first, then selected envs in order, so a later env wins
3. **local** files
4. **env-local** files, in the same environment order as layer 2

Every local file beats every shared file, whatever its environment. This is mise's `layer_config_filenames` order
([3.2](config-locations.md#32-filenames-within-one-project-directory)).

**Worked example.** On Linux x64 with `MYAPP_ENV=dev`, the `{name}.*` files in one directory apply in this order:

```text
myapp.toml                    shared
myapp.unix.toml               env        unix < linux < linux-x64 (platform)
myapp.linux.toml              env
myapp.linux-x64.toml          env
myapp.dev.toml                env        selected envs rank above platform envs
myapp.local.toml              local      beats every shared and env file
myapp.unix.local.toml         env-local
myapp.linux.local.toml        env-local
myapp.linux-x64.local.toml    env-local
myapp.dev.local.toml          env-local  highest
```

So `myapp.linux.local.toml` beats `myapp.local.toml`, and `myapp.local.toml` beats `myapp.dev.toml`. The other name
patterns (`.{name}.*`, `{name}/config.*`, `.config/{name}/config.*`, …) slot into the same four layers at their own
positions.

**Environment names follow mise.** Names are matched literally and are never combined:

- **Any name is allowed,** dots included. The value is only split on commas, and empty entries are dropped.
- **Literal matching:** a selected name is put into the filename patterns exactly as written. `MYAPP_ENV=dev.linux`
  selects `myapp.dev.linux.toml` and `myapp.dev.linux.local.toml`.
- **No combinations:** `myapp.dev.linux.local.toml` does **not** load for `MYAPP_ENV=dev` on Linux, or for
  `MYAPP_ENV=dev,linux`. To configure `dev` on Linux, use `myapp.dev.toml` and `myapp.linux.toml`. Both load, and `dev`
  wins where they overlap.
- **Unmatched files are silently not loaded,** as in mise. There's no warning, because with dotted names allowed, any
  such file might belong to an environment that simply isn't selected.
- **Ambiguous filenames are resolved by the selection.** `myapp.dev.local.toml` is the local file of `dev` when `dev` is
  selected, and the env file of `dev.local` when `dev.local` is selected. A name such as `local` can make one file match
  two patterns. For example, `myapp.local.toml` is both the local file and the env file of env `local`. The file is
  loaded **once**, at its higher-precedence position, by the same dedup rule as [3.1](config-locations.md#31-tiers).
- **Names must be safe in a path.** This is the one deviation from mise here, and it is a safety check, not a naming
  rule (see [Safe environment names](#safe-environment-names)).

### Safe environment names

A selected name is inserted into filename patterns, and a project's bootstrap file can select names. So every name must
give a **valid filename** and must not **escape the directory** being searched. mise does neither check; it only splits
the value on commas.

- **Rejected characters:** `/` and `\` (on every OS, so a config repo behaves the same everywhere), NUL and other
  control characters (`U+0000`–`U+001F`, `U+007F`), and the characters Windows forbids in filenames: `<`, `>`, `:`, `"`,
  `|`, `?` and `*`.
- **Valid result:** the filename built from the name must also be valid, for example `myapp.{env}.local.toml` with at
  most 255 bytes.
- **Contained path:** after each filename is joined to its directory, the resolved path must stay inside that directory.
  Go's `filepath.IsLocal` checks this, and the filesystem hook opens files through `os.Root`. A rule that only matched
  characters can miss cases, so this second check runs as well.
- **Literal insertion:** names are inserted literally and are never read as glob syntax. `[` and `]` are escaped, as
  mise does with `glob::Pattern::escape`.
- **Errors:** an invalid name is an `*OptionError` from any source. From `WithEnvs` it fails at `New`; from `{NAME}_ENV`
  or a bootstrap file it fails at `Load`, when those are read. The error names the source and the offending character.
- **Dotted names stay valid,** so mise-style setups such as `MYAPP_ENV=dev.linux` port unchanged.
- **The app name gets the same check** (`{name}`), when the preset is built, as `env-paths` does for cosmiconfig. It
  must also not be empty, `.` or `..`, and is at most 255 bytes (DL-99). `WithEnvs` names are checked at `New` in the
  same way; the contained-path check on each built filename runs when the filenames are built.

## Choosing scopes

Each preset declares its default set of scopes. Two modifiers change it, and they can be used in any `Extend`:

- **`WithScopes(...)`** *replaces* the current set with exactly the scopes listed.
- **`WithoutScopes(...)`** *removes* the listed scopes from the current set.

Defining a [custom scope](#custom-scopes) with `WithScope` also *adds* it to the current set (DL-261).

<!-- example -->
```go
// a system daemon: only /etc/{name} (or %ProgramData%\{name})
misehardened.Preset.Extend(
	cascadingconfig.WithScopes(cascadingconfig.ScopeSystem),
)

// everything misehardened.Preset has, minus system config
misehardened.Preset.Extend(
	cascadingconfig.WithoutScopes(cascadingconfig.ScopeSystem),
)
```

Modifiers apply **in order**, left to right, and across chained `Extend` calls. So
`P.Extend(WithScopes(A, B, C)).Extend(WithoutScopes(B))` selects `A` and `C`.

| Preset              | Default scopes                     |
| ------------------- | ---------------------------------- |
| `mise.Preset`        | `system`, `user`, `project`, `override` |
| `misehardened.Preset`  | `system`, `user`, `project`, `override` |
| `cosmiconfig.Preset` | `project`, plus `user` with `WithSearchStrategy(Global)`, as in cosmiconfig |

- **Selecting, not reordering.** Both modifiers choose which of the preset's scopes are on. Their precedence stays as
  the preset defines it, whatever order they're listed in. A different order is a preset definition, not a scope choice.
- **Unknown scopes are an error.** Naming a scope the preset doesn't define fails when the option is applied, for either
  modifier. So does listing a scope twice in `WithScopes` (DL-100).
- **Removing an unselected scope is a no-op.** `WithoutScopes` on a scope that is defined but already not selected does
  nothing, so chained extensions stay safe to compose.
- **Each scope is independent.** `override` lives in the user dir, but it is enabled only if it is listed. So
  `WithScopes(ScopeUser, ScopeProject)` turns override files off, and `WithoutScopes(ScopeUser)` leaves them on.
- **Fully excluded:** a scope that isn't selected is left out of both the main search and the bootstrap search. If
  `user` isn't selected, its bootstrap files aren't read either.
- **Explicit config files still load when `project` isn't selected.** Naming a file explicitly is a deliberate choice.
  An app that wants no project-level config at all also leaves out `WithConfigFiles` and turns off the
  `{NAME}_CONFIG_FILE` binding.
- **Who chooses:** only the app, in code. Bootstrap files and env vars can't. A project's bootstrap file must not be
  able to switch off a system scope that may hold enforced policy.
- **`Describe()`** lists the selected scopes, and also the preset's scopes that aren't selected, so their absence is
  visible.

**Why `misehardened.Preset` keeps all four scopes:**

- **Its protections apply in every scope.** They limit what a file can do and decide whether it is trusted: no remotes,
  permission checks, include confinement. Narrowing the scopes adds little on top.
- **Which scopes fit depends on the app.** A CLI wants `user` and `project`; a system daemon wants `system` alone.
- **It stays a drop-in replacement.** Switching from `mise.Preset` makes loading stricter without silently ignoring a
  user's own config.

## Custom scopes

Apps can define new scopes alongside `system`, `user`, `project` and `override`. Per-scope policies let each custom
scope carry its own trust level.

<!-- example -->
```go
mise.Preset.Extend(
	// Built-in defaults compiled into the binary, below everything else.
	cascadingconfig.WithScope("builtin",
		cascadingconfig.EmbeddedFS(defaultsFS, cascadingconfig.Pattern{Template: "{name}.toml"}),
		cascadingconfig.Before(cascadingconfig.ScopeSystem),
		cascadingconfig.Trust(cascadingconfig.TrustSystem),
	),
	// Kubernetes-mounted config, above user config.
	cascadingconfig.WithScope("mounted",
		cascadingconfig.FixedDir("/run/secrets/{name}", cascadingconfig.Pattern{Template: "config.{ext}"}),
		cascadingconfig.After(cascadingconfig.ScopeUser),
	),
	cascadingconfig.WithScopes(
		"builtin", cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser, "mounted",
		cascadingconfig.ScopeProject, cascadingconfig.ScopeOverride,
	),
)
```

- **`FixedDir(dir, patterns...)`** (DL-111) searches one directory. `dir` is absolute, or starts with `~`, `{user}` or
  `{system}`, and may use `{name}`. Each `Pattern`'s `Template` is a relative file path that may use `{name}` and
  `{ext}`, and may include subdirectories. Patterns are listed lowest precedence first. A relative directory, a template
  that leaves its directory, or an unknown placeholder is an `*OptionError`. Its default trust is `TrustProject`.
- **Kinds:** a custom scope is built from one of the existing kinds: `FixedDir`, `Walk`, `ExplicitFile`, or `EmbeddedFS`
  / any `fs.FS`. A key inside a manifest file, such as `package.json#{name}`, is a pattern with a `Key` (see
  [Patterns](#patterns)); no preset uses one. Fully custom discovery implements the `Scope` interface, just as resolvers
  implement `IncludeResolver` (see below).
- **`Walk(patterns...)`** (DL-122) searches the start directory and each parent, as described in
  [Directory walk](#directory-walk), applying them root-most first. Its default trust is `TrustProject`.
- **`ExplicitFile(paths...)`** (DL-126) loads the files as given, like [explicit config files](#explicit-config-files).
  Its default trust is `TrustProject`.
- **Scope options for bindings** (DL-125): `ReplacedByFile(b)` makes the scope load exactly the file that binding `b`
  names, when it is set, as an explicit file. `DisabledBy(b)` skips the scope when `b` is set. `ReplacedByConfigFiles()`
  marks the scope that explicit config files replace. `ConfigRoot(dir)` sets the config root of the scope's
  `RootSearchDir` patterns, for example `~`.
- **Defaults:** `WithDefaults(v)` is a ready-made defaults scope, built from a Go value and placed below every other
  scope ([api.md](api.md#defaults)).
- **Placement** (DL-100): set with `Before(scope)` or `After(scope)`. Precedence follows the preset's order with the
  custom scopes slotted in.
  - **The anchor must already be defined** when the option runs, because options apply in order.
  - **Insertion:** `After(x)` inserts the scope directly above `x`, and `Before(x)` directly below it. So a scope placed
    `After(x)` later sits between `x` and any scope placed there earlier.
  - **Neither:** the scope goes above every scope defined so far.
  - **Both** is an `*OptionError`.
- **Names** must be unique. A clash with any scope, built-in or custom, is an error.
- **Selection** (DL-261): defining a scope selects it, so `Extend(WithScope("env", EnvVars("MYAPP_")))` is all an app
  needs. Options apply in order, so a later `WithScopes(...)` that leaves the scope out deselects it, and a later
  `WithoutScopes` removes it. A preset that offers an optional scope, off by default, defines it and then removes it
  with `WithoutScopes`. The defaults scope is always selected (DL-153).
- **Fully custom discovery** (DL-236 to DL-240): `Custom(s Scope)` turns an app's `Scope` into a kind for `WithScope`.
  The built-in kinds don't implement `Scope`: they stay sealed, because they need the loader's internals.
  - **The kind:** a nil `Scope` is an `*OptionError`. Its default trust is `TrustProject`, the lowest, since it can
    return files from anywhere, and it needs a registered decoder, as `FixedDir` does. `BootstrapFiles` on it is an
    `*OptionError`, as on every kind but `FixedDir` and `Walk`.
  - **`Discover(ctx, d)`** gets a `Discovery`: the app name, the scope's name, the start dir, the home dir, the selected
    and platform environments, the scope's searched formats, the loader's filesystem, the host's env vars and the
    logger. Everything comes through the loader's host, so `cctest` fakes reach custom scopes too.
  - **Sources,** lowest precedence first: each has exactly one of `Path`, which must be absolute, and `Tree`. A
    `Format`, when given, must be registered. A broken `Source` is a 500 naming the scope. An error from `Discover` that
    carries a cascadingconfig code keeps it, and any other is a 500 naming the scope.
  - **`Path` sources** join the pipeline exactly like searched files: symlinks, ignored paths, dedup, permission checks,
    the trust store, includes, `FirstMatch` and `RequireConfig(RequireFile)`. Their config root defaults to the file's
    own directory. One that doesn't exist is a `*NotFoundError` naming the scope, as for `ExplicitFile`. A `Path` with
    dot segments is cleaned first, as explicit files are, and a file a scope lists twice is a duplicate like any other
    (DL-241).
  - **`Tree` sources** are like a `Values` scope: checked for canonical values and copied, `_unset` stripped with a
    warning, no includes, and they don't count for `RequireFile`. Each is its own layer.
  - **Concurrency:** `Discover` and `WatchDirs` can be called from several goroutines at once.
  - **Watching** is described under [Hot reload](#hot-reload).
- **Env-var bindings:** none are created automatically. An app adds them explicitly, for example
  `WithEnvBinding("MOUNTED_DIR", "MYAPP_MOUNTED_CONFIG_DIR")`.
- **Trust:** each scope kind has a default trust level, and `Trust(...)` overrides it. File-based kinds default to
  project trust, the lowest level. `EnvVars` and `Values` default to user trust (see [Scope trust](#scope-trust)).

## Scope trust

**Precedence** says which value wins. **Trust** says what a scope's files may do. The two are different orderings:
`override` wins over `project` and is also more trusted, but `user` is more trusted than `project` while losing to it.

| Trust level     | Built-in scopes                        |
| --------------- | -------------------------------------- |
| `TrustSystem`   | `system`                               |
| `TrustUser`     | `user`, `override`                     |
| `TrustProject`  | `project`, plus explicit config files  |

Trust is used by the bootstrap scope limits ([Bootstrap file](#bootstrap-file)) and by `Describe()`. Per-scope policies
are set by scope, not by trust level.

**A file reached by two routes** is loaded once. When the current directory is under `~`, the user config
`~/.config/{name}/config.toml` is also a project file of `~`:

- **Precedence** comes from the higher-precedence route, the project position, as mise does
  ([3.1](config-locations.md#31-tiers)).
- **Trust, policy and `Layer.Scope`** come from the **more trusted** route, the user scope. Otherwise the user's own
  global config would be treated as a project file, needing project approval and limited by project bootstrap rules.
- **`Describe()`** records both routes: the other route is listed as `LayerDuplicate`, naming the route that was kept
  (DL-158).

## Warnings and logging

The loader reports every non-fatal problem as a warning. Each warning is recorded on the layer it concerns, so
`Describe()` and tests can see it without capturing logs, and it is logged. Logging goes through a `*slog.Logger`:
`slog.Default()`, unless the app sets its own with `WithLogger(l)`.

**Levels** (DL-263). A warning's code decides its level:

- **Error:** a state that needs attention and that the app may not otherwise see: a watch handler that panicked.
- **Warn:** something the user wrote or set up was ignored, or may be wrong: `unreadable`, `not-a-file`,
  `broken-symlink`, `file-permissions`, `duplicate-key`, `reserved-key`, `unknown-key`, `env-var-skipped`,
  `env-var-clash`, `bootstrap-unknown-key`, `bootstrap-key-dropped`, `stale-include`, `include-cache`, `watch-failed`
  and `reload-failed`, and a format module's own codes, such as `format/yaml`'s `yaml-version`.
- **Info:** expected behaviour worth knowing: `untrusted` (the trust store doing its job), `format-conflict` (a
  first-match policy) and `no-home` (common in containers). `slog.Default()` prints Info, so these show by default.
- **Debug:** tracing, for troubleshooting:
  - each load's summary, with the layers loaded and skipped and how long it took
  - each file loaded or skipped, with the reason
  - each remote include's cache hit, revalidation or fetch
  - a non-directory on a search path, which is skipped like a missing directory (DL-262)
  - for a watcher: each event, each reload started, each reload applied with its number of changed keys, the size of the
    watch set when it's planned, and polling used in place of a notifier

Records carry `path` and `scope` where they apply, and a warning's `code`. Messages aren't API; a Debug record's `op`
attribute says what it traces, and its other attributes are stable:

- **`load`:** `loaded` and `skipped`, the numbers of layers, and `duration`.
- **`layer`:** `status`, with `scope`, and `path` for a file.
- **`remote`:** `ref`, and `cache`: `fetched`, `cached`, `revalidated` or `stale`.
- **`not-a-dir`:** `path`, the directory that was searched.
- **`event`:** `path`, from the notifier. **`reload`:** a reload starting. **`reloaded`:** `changes`, the number of
  changed keys. **`watch-set`:** `dirs`, the number of directories watched or polled. **`polling`:** polling in place of
  a notifier.

**Quieter logging:**

- **`WithLogLevel(level)`** drops records below `level` before they reach the logger. `WithLogLevel(slog.LevelWarn)`
  drops Info and Debug, and a `*slog.LevelVar` changes the level at run time. Without it, the logger's handler decides.
- **`WithLogger(slog.New(slog.DiscardHandler))`** silences the loader entirely.
- **Custom scopes** get the same filtered logger, through `Discovery.Logger`.

## Patterns

A scope kind lists `Pattern`s, lowest precedence first (DL-117):

```go
type Pattern struct {
	Template string   // {name}, {env}, {ext}, and * inside a path segment
	Variant  Variant  // VariantShared (the default), VariantEnv, VariantLocal, VariantEnvLocal
	Root     RootMode // RootSearchDir (the default) or RootFileDir
	Key      string   // optional: a dotted key inside the file whose table is the config
}
```

- **Variants:** a template has `{env}` exactly when its variant is `VariantEnv` or `VariantEnvLocal`; anything else is
  an `*OptionError`.
- **Order within one directory:** patterns are grouped by variant: shared, then env once per environment (platform
  environments first, then the selected ones in order), then local, then env-local in the same environment order. Within
  a group they keep their listed order. A layout reproduces mise's layers A–D by listing them in mise's order.
- **Globs** (DL-118): `*` is the only glob character, and it matches within one path segment. In the file name it
  matches a non-empty name with no dots, so `conf.d/*.{ext}` never takes `10-node.dev.toml` as a shared file and
  `conf.d/*.{env}.{ext}` reads its environment from the dot. In a directory segment it matches any directory whose name
  doesn't start with a dot. Matches apply in alphabetical order, and each stem is its own slot. Other glob characters
  are literal, and `**` is an `*OptionError`.
- **Config roots** (DL-119): `RootSearchDir` is the directory being searched, as mise uses even for
  `.config/{name}/config.toml`. `RootFileDir` is the file's own directory, as for `.config/{name}/{name}.toml` and each
  `conf.d/*/` folder. The scope option `ConfigRoot(dir)` replaces the search directory as the root, as `MiseLayout`'s
  user scope does with `~`. Every layer records its `ConfigRoot`, `Pattern`, `Variant` and `Env`.
- **Keys inside a file** (DL-120): `Key` is a dotted path that may use `{name}`, such as `{name}` for `package.json` or
  `tool.{name}` for `pyproject.toml`. The table at that key is the file's config. A missing key counts as the file not
  being found; a value that isn't a table is a `*ParseError` naming the file and the key.

## Directory walk

`mise.Preset` walks the `project` scope upwards from the start dir:

- **Start dir inside `~`:** the walk searches `~` itself and then stops. Directories above `~` are never searched.
- **Start dir outside `~`:** for example `/srv/app`, or a container with no home above the cwd. The walk goes all the
  way to `/`.
- **Extra ceilings:** `{NAME}_CEILING_PATHS`, or `ceiling_paths` in the bootstrap file, adds exclusive ceilings on top,
  as in mise.
- **Order:** directories are applied root-most first, so the nearest directory wins.

mise already uses this rule for its `.miserc.toml` walk ([3.7](config-locations.md#37-bootstrap-file-miserctoml)). Using
it for the main search is a deliberate deviation, because real mise walks the main search to `/`. The option
`WithWalkStop(Root)` restores mise's exact behaviour.

The `user` scope (`~/.config/{name}`) is a separate scope and is not affected by the walk limit.

**Options** (DL-122): `WithStartDir(dir)` sets where the walk starts; by default it is the current directory at each
load, and a relative value resolves against it. `WithWalkStop(StopAtHome)` is the rule above and the default;
`WithWalkStop(StopAtRoot)` always walks to `/`. `WithCeilingPaths(paths...)` adds exclusive ceilings, together with
`{NAME}_CEILING_PATHS`: a ceiling is never searched, and nor is anything above it. The constants aren't `Home` and
`Root`, because `Root` is already the filesystem's root type.

**A moved user directory** (DL-128): when `{NAME}_CONFIG_DIR` moves the user directory, files under the default
`~/.config/{name}` that the walk finds are dropped, as mise does, so the old directory doesn't keep loading as project
config of `~`. They aren't listed among the layers at all.

## `MiseLayout`'s scopes

`MiseLayout` defines four scopes, lowest precedence first, and selects them all (DL-124). Every pattern uses `{ext}`
where mise has `.toml`.

| Scope      | Kind                  | Patterns                                                                                     | Options                                   |
| ---------- | --------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------- |
| `system`   | `FixedDir("{system}")` | `MiseGlobalPatterns()`: mise's global layout ([3.4](config-locations.md#34-global-directory)) | `Trust(TrustSystem)`, `ReplacedByFile(BindSystemConfigFile)` |
| `user`     | `FixedDir("{user}")`   | the same                                                                                     | `Trust(TrustUser)`, `ConfigRoot("~")`, `ReplacedByFile(BindGlobalConfigFile)` |
| `project`  | `Walk`                 | layers A–D ([3.2](config-locations.md#32-filenames-within-one-project-directory)), without `.tool-versions` | `ReplacedByConfigFiles()`                 |
| `override` | `FixedDir("{user}")`   | `config.override.{ext}`, then `config.{env}.override.{ext}`                                  | `Trust(TrustUser)`, `ConfigRoot("~")`, `DisabledBy(BindGlobalConfigFile)` |

- **Dotted `conf.d` fragments:** each `conf.d/*.{env}.{ext}` env pattern, and its env-local counterpart, comes just
  before the folder fragments of the same `conf.d`, mirroring the shared order of files before folders.
- **Config roots:** `{name}.{ext}` directly in `.config/{name}/`, and every `conf.d/*/` folder fragment, use
  `RootFileDir`, as in [3.10](config-locations.md#310-config-root). Everything else uses the search directory, or `~`
  for the user and override scopes.

## Bootstrap file

**Each preset names its own bootstrap files**, including where they are searched and which keys they may set. The
obvious name in one preset is a config file in another: `.myapprc.json` is a normal config file in `cosmiconfig.Preset`.
A user-defined preset can name any bootstrap files, or have none.

| Preset              | Bootstrap files, highest first                                                                                                                                                                 | Allowed keys |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `mise.Preset`        | Walk from the start dir, with the same stop rule as above: `.{name}rc.local.{ext}`, `.{name}rc.{ext}`, `.config/{name}rc.{ext}` (not read in `~` or `/`). Then `{user}/{name}rc.local.{ext}` and `{user}/{name}rc.{ext}`. Then `{system}/{name}rc.{ext}`. | `env`, `ceiling_paths`, `ignored_config_paths` and `override_config_filenames` (`.miserc.toml`'s keys, without mise's migration flags), plus `formats` |
| `misehardened.Preset`  | As `mise.Preset`.                                                                                                                                                                              | As `mise.Preset`, except that **project**-scope bootstrap files may only set `env` and `formats`. |
| `cosmiconfig.Preset` | The cwd only: the `cosmiconfig` key of `.config/config.{ext}`. cosmiconfig also reads `package.json` and `package.yaml`; we don't (see [Deviations from cosmiconfig](#deviations-from-cosmiconfig)). | cosmiconfig's keys (`searchPlaces`, `mergeSearchPlaces`), plus `formats` |

- **Declaring them** (DL-131): the scope option `BootstrapFiles(patterns...)` makes a scope's own kind also search for
  bootstrap files, in the same places as its config files: the directory of a `FixedDir`, every directory of a `Walk`.
  On any other kind it is an `*OptionError`. A pattern with `SkipHomeAndRoot` isn't searched in `~` or `/`; only `Walk`
  honours it. A bootstrap search runs before the main search, and an unselected scope's bootstrap files aren't read. A
  scope whose config is replaced, by explicit files or `ReplacedByFile`, still has its bootstrap files read.
- **Keys** (DL-132) are of type `BootstrapKey`: `BootstrapEnv`, `BootstrapFormats`, `BootstrapCeilingPaths`,
  `BootstrapIgnoredConfigPaths` and `BootstrapOverrideConfigFilenames`. `WithBootstrapKeys(keys...)` enables keys,
  `WithoutBootstrapKey(key)` removes one, and `WithBootstrapLimit(scope, keys...)` restricts one scope's bootstrap files
  to the keys listed.
- **Override files:** bootstrap files are ordered like config files, and each key set in a higher bootstrap file
  replaces the same key from lower files, as in mise. The exceptions are `ceiling_paths` and `ignored_config_paths`,
  which only add, so every file's entries are combined: otherwise a project file could cancel a system file's ignored
  paths.
- **Values** (DL-133): `env`, `formats` and `override_config_filenames` are lists of strings; `ceiling_paths` and
  `ignored_config_paths` are lists of paths.
- **Relative paths** in a bootstrap file (`ceiling_paths`, `ignored_config_paths`) are resolved against that file's
  directory, as mise does, and a leading `~` is expanded.
- **App config inside the meta file:** in `cosmiconfig.Preset`, as in cosmiconfig, the meta file's `{name}` key is also
  checked as app config (see [2.5](config-locations.md#25-meta-config)).
- **Formats:** bootstrap files use the format order from the environment variable, the app's setting or the preset,
  never from a bootstrap file (see [File format precedence](#file-format-precedence)).
- **Unknown keys** are a `WarnBootstrapUnknownKey` warning, so a newer bootstrap file doesn't break an older app.
- **Keys outside the limits** (the scope limits below, `WithBootstrapLimit`, or `misehardened.Preset`'s project limits)
  are dropped, with a `WarnBootstrapKeyDropped` warning naming the file and the key.
- **Wrong types**, such as `env = "dev"` instead of a list, are an `*OptionError` at `Load`.
- **No includes and no `_unset`.** Bootstrap files are read before the main search, so they stay simple. Either key is
  an unknown key, with a warning.
- **Ceilings for the bootstrap walk:** it honours the ceilings from `WithCeilingPaths` and `{NAME}_CEILING_PATHS` only,
  not `ceiling_paths` from a bootstrap file, which can only be read once the walk has found it. mise has the same rule.
- **Not merged:** bootstrap files never become part of the config (DL-137).

**Precedence against options and env vars** (DL-134):

- **Single settings,** `formats` and `override_config_filenames`: the env var wins, then the bootstrap file, then the
  app's option, then the preset. For `env`, `WithEnvs` comes first, then `{NAME}_ENV`, then the bootstrap file.
- **Lists that only add,** `ceiling_paths` and `ignored_config_paths`: the option, the env var and every bootstrap file
  are combined.
- **Locking a setting:** an app that wants its own value to stand turns the key off with `WithoutBootstrapKey`, or
  limits it to some scopes with `WithBootstrapLimit`.
- **Per-value control:** `WithBootstrapTransform(func(ctx context.Context, l Layer, t Tree) (Tree, error))` runs on each
  bootstrap file's tree, a copy, before its keys are read, in registration order. It can rewrite or drop keys, for
  example to allow only some environments. An error it returns is a `*TransformError` naming the file. It is the
  bootstrap counterpart of `WithLayerTransform`, which never sees bootstrap files.

**Scope limits** (DL-135). A bootstrap file belongs to the scope it was found in, and has that scope's trust. Any key
that can **hide or skip** files only takes effect in scopes **at most as trusted** as that file's scope (see
[Scope trust](#scope-trust)). Those keys are `ignored_config_paths`, `formats`, `ceiling_paths` and
`override_config_filenames`. So a project's bootstrap file can't make the loader skip `/etc/{name}/config.toml`:

- **`ignored_config_paths`:** an entry never applies to a more trusted scope. If it would have matched a file there, it
  is dropped for that file, with a `WarnBootstrapKeyDropped` warning naming the bootstrap file and the entry.
- **`formats`:** each scope uses the `formats` of the highest-precedence bootstrap file that is at least as trusted as
  the scope, or else the env var, the app's setting or the preset. So a project bootstrap file's `formats` orders and
  filters formats in the `project` scope only, and a user bootstrap file's in the `user`, `override` and `project`
  scopes. The env var, when set, still wins in every scope.

| Bootstrap file sets `formats` in | `system` scope uses | `user` and `override` use | `project` uses |
| -------------------------------- | ------------------- | ------------------------- | -------------- |
| `project` only                   | app or preset       | app or preset             | project file   |
| `user` only                      | app or preset       | user file                 | user file      |
| `user` and `project`             | app or preset       | user file                 | project file   |
| `system` only                    | system file         | system file               | system file    |

- **`ceiling_paths` and `override_config_filenames`:** these only ever act on the `project` walk, so they are already
  within the limit for every bootstrap file.
- **`env` is the exception.** It applies in every scope, as in mise. It doesn't hide files; it chooses among variants
  such as `config.{env}.toml` that each scope's own author wrote.

`misehardened.Preset` goes further, and project-scope bootstrap files may only set `env` and `formats` (see the table
above), through `WithBootstrapLimit(ScopeProject, BootstrapEnv, BootstrapFormats)` (DL-138).

**`MiseLayout`'s bootstrap files** (DL-138), lowest precedence first, with all five keys enabled:

- **system:** `{name}rc.{ext}`.
- **user:** `{name}rc.{ext}`, then `{name}rc.local.{ext}`.
- **project, on the walk:** `.config/{name}rc.{ext}` with `SkipHomeAndRoot`, then `.{name}rc.{ext}`, then
  `.{name}rc.local.{ext}`. These are mise's locations exactly; no `.config/{name}/` location is added.

## Merging

- **Rule:** objects deep-merge. Any other value, **including arrays**, is replaced by the higher layer.
- **Applies everywhere:** the same rule is used across scopes, variants, same-slot formats and includes.
- **Changing the array default:** `WithArrayMerge(Append)` makes arrays concatenate wherever no per-key rule applies.
  `WithArrayMerge(Replace)` is the default. `cosmiconfig.Preset` sets `Append`, because cosmiconfig concatenates arrays
  in `$import` merges (`mergeImportArrays: true`), and those are the only merges it does.
- **mise's section rules:** some mise sections replace a whole entry rather than deep-merging it. For example, a
  `[tools]` entry replaces the parent's entry including its options. Rules like this depend on the app's schema, so they
  are expressed with [per-key merge rules](#per-key-merge-rules), not in the default rule.

### Per-key merge rules

The app can attach a merge strategy to a key path. The rule is used whenever two layers both set that path.

**Strategies:**

| Strategy    | Result for `lower` ⊕ `higher`                                                  |
| ----------- | ------------------------------------------------------------------------------ |
| `DeepMerge` | Merge objects key by key. This is the default for objects.                       |
| `Replace`   | `higher`, whole, with nothing below it inherited. This is the default for every other value. |
| `Append`    | `lower` + `higher`, for arrays only.                                           |
| `Prepend`   | `higher` + `lower`, for arrays only.                                           |

The four strategies are constants of type `MergeStrategy`, set with `WithMergeRule(path, strategy)`. A custom merge is a
`MergeFunc`, `func(path Path, lower, higher any) (any, error)`, with its own option, `WithMergeFunc(path, f)`, so the
strategies can stay constants (DL-87).

**Paths:**

- **Syntax:** a path is a list of key segments. The string form is dot-separated, and `*` matches exactly one segment:
  `tools.*`, `env.*`, `server.tls`.
- **Keys containing dots:** use the list form, for example `Path("plugins", "foo.bar")`.
- **Matching** is case-sensitive, because TOML, YAML and JSON keys all are.

**Matching rules** (DL-88):

- **Most specific rule wins.** At a given key, an exact path beats a path with wildcards, and fewer wildcards beat more.
  Every rule that matches a key has the key's length, because `*` matches exactly one segment. A shorter rule only
  reaches deeper keys by covering its subtree (below).
- **Ambiguous rules are an error.** Two rules are ambiguous when they have the same length and the same number of
  wildcards, and some key matches both, such as `a.*` and `*.b`, which both match `a.b`. This is an `*OptionError` when
  the rules are set, not while loading.
- **The same path again replaces the earlier rule,** so an app can override a rule that came in a shared option group.
  `"tools.*"` and `Path{"tools", "*"}` are the same path.
- **No rule on the root.** A rule on the empty path is an `*OptionError`.
- **`Replace` and functions cover the whole subtree.** Rules on paths below the matched one are not consulted.
- **Type mismatches:** a `DeepMerge` rule needs two tables, and `Append` and `Prepend` need two arrays. A rule that
  meets other types is a `*MergeError`, which names the key and both layers' files (DL-90). The merge carries on, and
  every mismatch is reported, joined. Null is never a mismatch (see [Null values](#null-values-yaml-and-json)).
- **Without a rule, merging never fails.** A value of another type simply replaces the lower one.
- **Functions** get copies of both values. An error a function returns is a `*MergeError` that wraps it: rejecting a
  combination of values is a problem with the config. A returned value outside the
  [canonical types](api.md#tree-and-values) is a bug in the app, reported as a `*BaseError` with `CodeInternal` (DL-89).

**Where rules come from:**

- **Set by the app in code**, for example `WithMergeRule("tools.*", Replace)`. Merge rules are part of the app's schema
  contract.
- **Not set by presets, bootstrap files or env vars.** Presets describe *discovery*, not schema, so `mise.Preset` ships
  with no merge rules. An app that wants mise's section semantics declares them itself. The one preset setting in this
  area is `cosmiconfig.Preset`'s array default (`WithArrayMerge(Append)`), which is a default strategy, not a per-key
  rule.
- **Used for every merge:** across scopes, variants, same-slot formats and includes. One rule set means one result,
  whichever route a file was found by.

**Provenance:** each merged value records the layer it came from. For `Append` and `Prepend`, each element records its
own layer. For a function, the result is attributed to the higher layer, with the lower layer in its history. A table
that several layers contributed to is `OriginMerged` (see [`Origin`](api.md#result-report-and-origin)). This is what
lets `Describe()` answer "where did this value come from?".

#### Example: mise's section semantics as rules

<!-- example -->
```go
cascadingconfig.WithMergeRule("tools.*", cascadingconfig.Replace)   // nearest tool entry replaces it whole
cascadingconfig.WithMergeRule("tasks.*", cascadingconfig.Replace)   // nearest task replaces it whole
cascadingconfig.WithMergeRule("daemons.*", cascadingconfig.Replace)
// [env], [vars], [settings]: per-key, which DeepMerge already does
```

### Unsetting keys

A layer can remove keys that lower layers set, using the reserved key `_unset`:

```toml
# ~/work/app/myapp.local.toml
_unset = ["proxy", "server.tls"]

[server]
port = 8080
```

```yaml
# same thing in YAML
_unset: [proxy, server.tls]
server:
  port: 8080
```

The default name is `_unset` so that TOML, our first-ranked format, can write it as a bare key, with no quotes needed.

**Value and placement:**

- **Value:** a list of paths, relative to the table that contains `_unset`. An entry is either a dot-separated string
  (`"server.tls"`) or a list of segments for keys that contain dots (`["plugins", "foo.bar"]`).
- **No wildcards:** to clear a whole table, unset the table itself.
- **Malformed values** are a `*ParseError` naming the file: a value that isn't a list, an entry that isn't a string or a
  list of strings, or an empty path (DL-92).
- **Not inside arrays:** `_unset` in a table inside an array is a `*ParseError`. Arrays are never merged, so it could
  never remove anything.
- **Placement:** `_unset` may appear in any table, not only at the top level. `[server]` with `_unset = ["tls"]` is the
  same as a top-level `_unset = ["server.tls"]`.

**Merge order:**

1. Remove each listed path from the result built from **lower** layers.
2. Merge this layer's own values on top, as usual. This includes any `null` values (see
   [Null values](#null-values-yaml-and-json)).

So one file can unset a key and redefine it, which gives a one-off `Replace` for that key. In the example, the result
has `server = { port = 8080, … }` with any lower `server.tls` gone. Other keys of `server` are still inherited.

**Other rules:**

- **Only lower layers are affected.** A higher layer can set the key again.
- **Missing keys:** unsetting a key that no lower layer set does nothing, and leaves no trace in the key's history. It
  isn't an error, because lower layers vary from machine to machine.
- **A table holding only `_unset`** doesn't count as setting its key. `[server]` with nothing but `_unset = ["tls"]`
  neither creates an empty `server` table nor replaces a lower `server = 5`. This applies recursively: a table left
  empty only by such tables doesn't count either.
- **Arrays:** unsetting removes the whole array. There is no way to remove single items; use a `Replace` rule or
  redefine the array.
- **Merge rules:** removal happens before [per-key merge rules](#per-key-merge-rules) run. Rules, including functions,
  only run when both sides still have the path after removal.
- **Never in the result:** `_unset` is stripped from the merged config. `Describe()` records each removal ("`proxy`
  unset by `~/work/app/myapp.local.toml`").
- **Applies everywhere:** across scopes, variants, same-slot formats and includes.
- **Presets:** it is on in all presets, including `cosmiconfig.Preset`, where it applies to `$import` merges. This is a
  deviation from cosmiconfig, which would pass `_unset` through as data.
- **Configuration:** the app can rename the key (`WithUnsetKey("$unset")`) or turn the feature off (`WithoutUnset()`).
  Use either one if the schema already uses `_unset`.

### Null values (YAML and JSON)

An explicit `null` (YAML `~` or `null`, JSON `null`) is a real value. It takes part in merging like any other value. It
is **not** the same as `_unset`:

|                                         | `port: null` (YAML/JSON)              | `_unset = ["port"]`         |
| --------------------------------------- | ------------------------------------- | --------------------------- |
| Key in the merged result                | Present, with value `null`            | Absent                      |
| Lower layers' value visible             | No                                    | No                          |
| A higher layer can set it again         | Yes                                   | Yes                         |
| Can be written in TOML                  | No (TOML has no null)                 | Yes                         |
| `Describe()` shows                      | "set to null by `<file>`"             | "unset by `<file>`"         |

**Merging:**

- **Decoders keep null distinct from a missing key.** `port: ~` decodes to a key `port` with a nil value. A key that
  isn't written decodes to no key at all. A decoder that drops nil entries is a bug.
- **A higher null replaces the lower value whatever its type:** a scalar, an array or a whole object. `server: null`
  replaces the entire inherited `server` table, with no deep merge into null.
- **A higher non-null value replaces a lower null.** An object over a null is taken as it is; there is nothing to
  deep-merge into.
- **Null is never a type mismatch.** When either side is null, the merge is a `Replace` and the higher value wins. This
  holds whatever per-key rule matches: `DeepMerge`, `Append` and `Prepend` all give way. A `MergeFunc` is the exception:
  it receives `nil` and decides for itself.
- **Same-slot formats:** null follows the normal order. If `myapp.yaml` has `port: ~` and `myapp.toml` has `port = 80`,
  TOML is applied last, so the result is `80`. If only the YAML file sets `port`, the result is `null`.
- **Decoding into structs:** a null leaves the field untouched, as koanf does. In a fresh struct that means `nil` or the
  zero value. In a struct pre-filled with Go defaults, the default stays (see [api.md](api.md#decoding)). Pointer fields
  can't tell "set to null" from "not set". An app that needs to know reads the merged tree or uses `OriginOf` (see
  [api.md](api.md#decoding)).

## Deviations from mise

`mise.Preset` is mise's discovery generalised to any app name, with these deliberate changes:

| Change                                                  | Why                                                                     |
| ------------------------------------------------------- | ----------------------------------------------------------------------- |
| The walk stops at `~` (see [Directory walk](#directory-walk)). | Walking above home is rarely wanted.                             |
| YAML and JSON are accepted alongside TOML, and same-slot formats are merged. | Go apps aren't TOML-only; TOML still wins.        |
| `formats` is a bootstrap key, and `{NAME}_CONFIG_FORMATS` is an env var. | Lets users and operators change the format order.          |
| `.tool-versions` is not included.                       | It is a mise/asdf-specific format, not a generic config file.            |
| Every file is trusted by default (D18); approval is the opt-in `RequireTrust` policy (see [Trust store](#trust-store)). | We don't execute config, so approval is opt-in.                         |
| Platform environments (`unix`, `{os}`, `{os}-{arch}`) are always active below the selected envs, and dotted `conf.d` fragments (`conf.d/*.{env}.toml`) always select an env. There are no `auto_env` / `env_conf_d` settings. | These are mise's planned end-state defaults (2027.6.0 and 2027.8.10). With no existing users, we don't need a date-based switch-over. |
| Environment names (and the app name) must give a valid filename and can't escape the searched directory (see [Safe environment names](#safe-environment-names)). | mise doesn't check names, so a project's `.miserc.toml` could select `env = ["../../x"]`. Dotted names stay valid, as in mise. |
| Objects are deep-merged generically (see [Merging](#merging)). | mise's per-section rules depend on its schema.                   |
| `_unset` removes inherited keys (see [Unsetting keys](#unsetting-keys)). | mise has no way to remove an inherited key.            |
| The system dir is `%ProgramData%\{name}` on Windows (see [Platform directories](#platform-directories)). | mise uses the literal `/etc/mise` everywhere. |
| Remote includes also accept `http(s)://`, and go through replaceable resolvers with allow-lists (see [Includes](#includes)). | mise supports only `git::` and `oci::`, with no allow-list. |
| `{NAME}_CONFIG_FILE` names explicit project files (see [Explicit config files](#explicit-config-files)). | In mise, `MISE_CONFIG_FILE` is a legacy alias for the global config file. |
| Legacy aliases (`MISE_CONFIG_FILE`, `MISE_SYSTEM_DIR`) and write-only settings (`MISE_DEFAULT_CONFIG_FILENAME`) aren't carried over. | They are mise history, or about writing, which is a [non-goal](#non-goals). |

## Deviations from cosmiconfig

`cosmiconfig.Preset` mirrors cosmiconfig's load paths, not its Node.js integration. The aim is a quasi-standard set of
load paths, so concepts specific to Node.js are left out. Users can add them back if they want them.

| Change                                                                 | Why                                                                                  |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| No JS or TS config files (`.{name}rc.js`, `{name}.config.ts`, …).       | They need a JavaScript runtime. `{name}.config.*` drops out entirely, because cosmiconfig only lists it with JS/TS extensions. |
| No `package.json` or `package.yaml`: no `{name}` property lookup (`packageProp`), and no `cosmiconfig` key in them for the meta config. | Node.js manifests. Users can add a manifest source with a pattern `Key`, for example `Pattern{Template: "package.json", Key: "{name}"}`. |
| The `Project` search strategy has no default root marker.              | cosmiconfig stops at the first directory containing `package.json`/`package.yaml`. Here the app names its own marker with `WithRootMarker`, for example `.git` or `go.mod`. |
| `_unset` removes inherited keys ([Unsetting keys](#unsetting-keys)).    | cosmiconfig would pass `_unset` through as data.                                     |
| `loaders` and `searchStrategy` in the meta config are unknown keys, with a warning. | cosmiconfig rejects them with an error; here every unknown bootstrap key warns, so a newer meta file doesn't break an older app (DL-142). |
| `$import` isn't supported yet, and may never be (DL-145).               | It resolves paths relative to the importing file with no allow-list or confinement, which has to be reconciled with the include security and trust model ([Includes](#includes)). |

**`CosmiconfigLayout`** (DL-146), lowest precedence first:

- **`user`,** selected only by `WithSearchStrategy(SearchGlobal)`: `FixedDir("{user}")` with `config.{ext}`, then
  `config`, which a `WithFileFormat(FileName("config"), "yaml", "json")` rule decodes.
- **`project`:** a `Walk` with `.config/{name}rc.{ext}`, `.config/{name}rc`, `.{name}rc.{ext}` and `.{name}rc`, so
  `.{name}rc` ranks highest, as in cosmiconfig. Each `{ext}` slot puts JSON above YAML, and `.yaml` above `.yml`.
- **`meta`:** `FixedDir("{cwd}", Pattern{Template: ".config/config.{ext}", Key: "{name}"})`, the meta file's `{name}`
  key, highest, so under first-match resolution it wins outright when it's non-empty (DL-142). The same file's
  `cosmiconfig` key is the scope's bootstrap file, with the keys `formats`, `searchPlaces` and `mergeSearchPlaces`.
- **Settings:** `WithResolution(FirstMatch)`, `WithSearchStrategy(SearchNone)`, `WithPlatformDirs(EnvPathsDirs)`,
  `WithConfigFilesOnly()`, and `WithFileFormat(…, "yaml", "json")` rules for `FileName(".{name}rc")`, for
  `FileName("{name}rc")`, which `.config/{name}rc` needs, and for the user directory's `FileName("config")`.

### Search strategies

These options mirror cosmiconfig's `searchStrategy` and `stopDir`. They are shorthand for general walk stops (DL-140):

- **Walk stops:** `StopAtStart` searches the start directory only. `StopAtMarker` walks up to and including the first
  directory that contains an entry named by `WithRootMarker(names...)`, or to `/`. `StopAtHome` walks up to and
  including the stop dir, `~` unless `WithStopDir(dir)` changes it, or to `/` if the stop dir isn't an ancestor; for
  mise nothing changes. `StopAtRoot` always walks to `/`.
- **`WithSearchStrategy`:** `SearchNone` sets `StopAtStart` and `SearchProject` sets `StopAtMarker`, and both deselect
  the `user` scope; `SearchGlobal` sets `StopAtHome` and selects `user`. The selection changes when the option runs,
  like any other option. The constants are prefixed, because `None` and `Global` would be too generic in the package.

| Strategy                           | Directories searched                                                                                  | Scopes            |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------------- |
| `WithSearchStrategy(None)` (default) | The start dir only.                                                                                 | `project`         |
| `WithSearchStrategy(Project)`      | The start dir, then each parent, up to and **including** the first directory containing a root marker set with `WithRootMarker(names...)`, or up to `/`. | `project` |
| `WithSearchStrategy(Global)`       | The start dir, then each parent, up to and **including** the stop dir (`WithStopDir(dir)`, default `~`), or up to `/` if the stop dir isn't an ancestor. Then the user dir. | `project`, `user` |

- **Invalid combinations are `*OptionError`s,** as in cosmiconfig: `WithStopDir` with a walk stop other than
  `StopAtHome`, and `StopAtMarker` with no root marker.
- **First-match resolution** (DL-139): `WithResolution(FirstMatch)` loads only the highest-precedence non-empty file,
  across every scope, so the nearest file wins and the user directory is reached only if nothing nearer matched. The
  other files are listed as `LayerIgnored`, without a warning. Files below the chosen one aren't decoded, so a broken
  file there doesn't fail the load, as in cosmiconfig. Empty files are skipped. Only files are chosen between: value
  layers, such as `Values`, `EnvVars` and the [defaults](api.md#defaults), always merge at their own position, so flags
  still override the chosen file and defaults still sit below it (DL-154). `WithResolution(MergeAll)` is the default.
- **Explicit files replace everything** (DL-144): with `WithConfigFilesOnly()`, explicit config files replace every
  scope that searches for files, as with cosmiconfig's `load(path)`. Value scopes, such as flags and the defaults, still
  take their places (DL-247). Bootstrap files are still read.
- **The current directory:** `FixedDir` accepts a `{cwd}` placeholder, for cosmiconfig's meta file, which is read from
  the current directory only, never from the start directory or its parents (DL-142).
- **Meta config keys:** `BootstrapSearchPlaces` (`searchPlaces`), file names that may use `{name}`, which are added to
  every walk's patterns at the highest precedence; and `BootstrapMergeSearchPlaces` (`mergeSearchPlaces`), a boolean,
  true by default, which when false makes them replace the walk's patterns instead. A bootstrap pattern's `Key` picks
  the table its keys are read from.
- **`EnvPathsDirs`** (DL-143) is cosmiconfig's `env-paths` user directory: `$XDG_CONFIG_HOME/{name}`, else
  `~/.config/{name}`, on Linux; `~/Library/Preferences/{name}` on macOS; `%APPDATA%\{name}\Config` on Windows, falling
  back to `~\AppData\Roaming` when `APPDATA` isn't set. It has no system directory, so `{system}` with it is an
  `*OptionError`.
- **`WithRootMarker` is generic.** Any walk can use it, in any preset, for example to stop at a repository root.

## File format precedence

A preset's patterns use `{ext}` for the file extension, so one pattern can match several files in one directory:

```text
~/work/app/myapp.toml
~/work/app/myapp.yaml
~/work/app/myapp.json
```

The **slot** is one pattern in one directory, with everything except `{ext}` filled in. In the example, the slot is
`~/work/app/myapp.*`. When a slot matches more than one file, **all of them are loaded and merged**:

- **Merge order:** the files are applied in **reverse** format precedence: lowest-ranked first, highest-ranked last. On
  any key that more than one of them sets, the highest-ranked format wins. With the default order, the example applies
  `myapp.json` < `myapp.yaml` < `myapp.toml`.
- **Position:** the files are merged as consecutive layers at the slot's position. Format never moves a file past
  another pattern's files.
- **One layer per file:** each file keeps its own metadata (path and format), so diagnostics can show which file set
  which key.
- **The `FormatConflict` policy**, set with `WithFormatConflict` (DL-93), can be:
  - `ConflictMerge` (`merge`): the default, as described here.
  - `ConflictFirst` (`first`): load only the highest-ranked file. The others get the status Ignored and a
    `WarnFormatConflict` warning.
  - `ConflictFail` (`error`): fail with a `*ConflictError` when a slot matches more than one file. Two files of one
    format, such as `.yaml` and `.yml`, count too. The error lists every file in the slot.
- **Format ranking only orders files within a slot.** Across different patterns, pattern order decides as usual. For
  example, `myapp.toml` and `.myapp.yaml` are different slots. `.myapp.yaml` ranks higher because its pattern does, even
  though TOML outranks YAML.
- **Globs work the same way.** In `conf.d/*.{ext}`, the slot is the file stem. `conf.d/10-node.yaml` and
  `conf.d/10-node.toml` merge as one pair, with TOML applied last. Each stem's group is applied in alphabetical order,
  whatever its extensions.

**Default order, highest first:**

| Preset              | Format precedence                         | Why                                                                  |
| ------------------- | ----------------------------------------- | -------------------------------------------------------------------- |
| `mise.Preset` (default) | `toml`, `yaml`, `json`                  | TOML wins. mise is TOML-only, so the other formats are additions.    |
| `cosmiconfig.Preset` | *(none)*, `json`, `yaml`                  | cosmiconfig's own search order. *(none)* is the extensionless `.{name}rc`, decoded by a `WithFileFormat` rule: YAML if a YAML decoder is registered, otherwise JSON. TOML is not in this preset. The preset uses `FormatConflict: first`, because cosmiconfig stops at the first match. |

- **The order lists formats, not extensions.** One format can have several extensions: YAML is both `.yaml` and `.yml`.
- **Within a format, extensions have a fixed rank.** For YAML, `.yaml` ranks above `.yml`, because `yaml` sorts first
  alphabetically. A slot containing both loads both under the default `merge` policy, with `.yaml` applied last so it
  wins where they overlap. Under `first`, only `.yaml` loads. Under `error`, the pair is a `*ConflictError`.
- **Files without an extension,** or with one that should mean another format, get their format from a rule (see
  [Choosing a file's format](#choosing-a-files-format)). A template without `{ext}` that no rule or extension covers
  isn't searched.
- An order set by the app, an env var or a bootstrap file may only name formats with a registered decoder. A layout's
  *default* order may name others, which are simply not searched ([api.md](api.md#layouts-vs-presets)).
- Extensions with no decoder never match, so `.js` and `.ts` are simply never found.

**Overriding the order.** The format precedence is part of the loader's initial configuration. It can be set in four
places. Highest precedence first:

| # | Source                                   | Example                      |
| - | ---------------------------------------- | ---------------------------- |
| 1 | Environment variable                     | `MYAPP_CONFIG_FORMATS=yaml,toml` |
| 2 | Bootstrap file (see [D14](config-locations.md#4-divergences)) | `formats = ["yaml", "toml"]` |
| 3 | The app's programmatic option            | `WithFormats("toml", "json")` |
| 4 | Preset default                           | `WithDefaultFormats("toml", "yaml", "json")` |

This mirrors mise. There, `MISE_ENV` beats `env = [...]` in `.miserc.toml`, and `.miserc.toml` beats mise's built-in
defaults.

**In the bootstrap file:**

- **What it controls:** `formats` sets the order for the **main search only**. It can't change how the bootstrap files
  themselves are found, because they have to be found before they can be read. Bootstrap discovery uses sources 1, 3 and
  4. This is the same rule mise applies to `MISE_CEILING_PATHS` during `.miserc.toml` discovery.
- **Several bootstrap files:** when more than one sets `formats`, the highest-precedence file's list **replaces** the
  others. Lists aren't merged, just as mise's `env` key replaces the inherited list. Which file applies to which scope
  follows the [scope limits](#bootstrap-file).
- **Empty list:** `formats = []` is an error, because it would disable every format.
- **Same rules as the variable:** values are validated just like the environment variable (see below). Listed formats
  are the only ones enabled, and an unknown format is an error.
- **The app's option** (DL-101): `WithFormats` takes the variable's syntax too: an extension stands for its format, and
  leading dots and surrounding spaces are allowed. A format listed twice is an `*OptionError`, and every listed format
  needs a registered decoder, checked at `New` after every option has run, so their order doesn't matter.
- **The preset default** (DL-105) is set with `WithDefaultFormats`, which takes the same syntax. Unlike `WithFormats`,
  it may name formats with no registered decoder, which are simply not searched, so a layout can list TOML and YAML
  before any decoder for them is added. A `WithFormats` from any option replaces it, whatever their order.
- **Neither set:** every registered decoder's format is searched, in registration order.
- **Decoders:** `WithDecoder` for a format that already has one replaces it, so an app can swap a layout's decoder. Two
  formats claiming the same extension are an `*OptionError`.
- **Locking the order:** an app author who wants to fix the order can turn off both overrides, with
  `WithoutEnvBinding("CONFIG_FORMATS")` and `WithoutBootstrapKey("formats")`.

**The environment variable:**

```text
{NAME}_CONFIG_FORMATS=yaml,toml
```

- **Name:** `{NAME}` is the app name in upper case, with any character outside `[A-Z0-9]` replaced by `_`. For example,
  `my-app` gives `MY_APP_CONFIG_FORMATS`.
- **Value:** a comma-separated list of formats, highest precedence first: `toml`, `yaml`, `json`, or any other
  registered format. An extension is accepted as an alias for its format, so `yml` means `yaml`. Leading dots and
  surrounding spaces are allowed: `.yaml, toml`.
- **Formats not in the list are disabled.** `MYAPP_CONFIG_FORMATS=toml` means only TOML files are found. This matches
  `MISE_OVERRIDE_CONFIG_FILENAMES`, which also *replaces* its list rather than adding to it.
- **An empty value counts as unset**, as in mise.
- **Errors:** an unknown format, or one with no registered decoder, is an `*OptionError` at `Load`, when the env var is
  read, not a silent skip.
- **Opting out:** the app author can turn the binding off (`WithoutEnvBinding("CONFIG_FORMATS")`), as with every other
  env binding (see D17).
- **Effect on the bootstrap file:** this is the only source outside the app's code that also changes how the bootstrap
  files themselves are found. The app's own option affects it too.

### Choosing a file's format

Every file the loader decodes gets its format from rules (DL-141), so names without an extension, such as cosmiconfig's
`.{name}rc`, can be read:

<!-- example -->
```go
cascadingconfig.WithFileFormat(cascadingconfig.FileName(".{name}rc"), "yaml", "json") // YAML if registered, else JSON
cascadingconfig.WithFileFormat(cascadingconfig.Extension(".conf", ".cfg"), "toml")
cascadingconfig.WithFileFormat(cascadingconfig.Glob("/etc/**/*.settings"), "yaml")
cascadingconfig.WithFileFormat(cascadingconfig.Regexp(`.*/\.?myapprc$`), "yaml")
```

- **`WithFileFormat(m, formats...)`:** a file the matcher accepts is decoded with the first listed format that has a
  registered decoder. With none registered, the rule doesn't apply.
- **Matchers:** `Extension(exts...)` matches one or more extensions, with or without the dot, so
  `Extension(".yaml", ".yml")` keeps simple cases simple. `FileName(glob)` matches the base name and may use `{name}`.
  `Glob(pattern)` matches the whole slash path, with `*` within one segment and `**` across segments. `Regexp(expr)`
  must match the whole path. `MatchFunc(f)` takes a function. These are the same `Matcher` values that allow-lists use
  ([Allow-lists](#allow-lists)). A pattern that doesn't compile is an `*OptionError` named after its constructor.
- **One model with extensions:** each decoder's extensions are built-in rules, `Extension(...)` for its format. Rules
  from `WithFileFormat` are checked first, the most recently registered first, so an app can override a layout's rule;
  the extension rules come last.
- **Everywhere:** the rules apply to searched candidates, explicit files (so `--config ~/.myapprc` works) and includes.
  A searched file is still only loaded when its format is in the scope's searched order.
- **Templates:** a template with `{ext}` still expands to the registered decoders' extensions. A template without
  `{ext}`, such as `.{name}rc`, matches that exact name, and its format comes from the rules; a name no rule matches
  isn't a candidate.

## Format dialects

What each built-in decoder accepts, and the rules every decoder follows. These were checked against the pinned libraries
(go-toml v2.4.3, goccy/go-yaml v1.19.2, `encoding/json`), and the reasoning is in
[decision record 0002](decisions/0002-yaml-and-toml-dialects.md).

**Each format's spec is the source of truth:** RFC 8259, TOML and YAML 1.2. Where a decoder departs from it, the
departure is deliberate and listed here. Measuring conformance against each format's test suite is planned for the last
milestone, [M12](../planning/implementation-plan.md#m12-conformance) (DL-174).

| Rule                  | JSON                                              | TOML                                     | YAML                                      |
| --------------------- | ------------------------------------------------- | ---------------------------------------- | ----------------------------------------- |
| Dialect               | Strict RFC 8259: no comments, no trailing commas, as cosmiconfig's `JSON.parse` | Whatever the pinned go-toml accepts. v2.4.3 already accepts TOML 1.1 syntax (multi-line inline tables, times without seconds, `\e`). | YAML 1.2 core schema, parsed by goccy, with the core schema's rules giving unquoted values their types (DL-163): `yes`, `no`, `on`, `off` and `y` stay **strings**. |
| Numbers               | RFC 8259                                          | The TOML spec; `inf` and `nan` are floats | The core schema: `017` is 17, `0o17` is 15, `1e3` is a float and `+.inf` is infinity; `0b11` and `1_000` stay strings. `WithLegacyNumbers()` accepts the YAML 1.1 forms `017` (octal), `0b11` and `1_000`, as goccy and yaml.v3 do (DL-163). |
| Duplicate keys        | The **last** one wins, as in `JSON.parse` and `encoding/json`, and a **warning** is logged | `*ParseError`, as the TOML spec requires | `*ParseError`, as the YAML spec requires   |
| Non-string map keys   | n/a                                               | n/a                                      | Resolved by the core schema, then printed: `0x1F:` becomes `"31"`, `~:` `"null"`, `true:` `"true"`, as goccy does. Keys equal after resolution (`1:` and `0o1:`) are duplicates, as the spec says; keys that only clash once printed (`1:` and `"1":`) are a `*ParseError` too, because the tree can't hold both. A table or sequence as a key is a `*ParseError` (DL-166). |
| Several documents     | n/a                                               | n/a                                      | `*ParseError`. goccy would silently keep only the first. |
| Aliases               | n/a                                               | n/a                                      | Allowed. At most 10,000 values may come from aliases and merge keys in one document, and `WithMaxAliasValues(n)` changes it; going over, or an alias inside its own anchor, is a `*ParseError` (DL-168). goccy has no limit. |
| Directives            | n/a                                               | n/a                                      | A document of directives belongs to the document after it. `%YAML 1.2` is accepted; `%YAML 1.1` and later 1.x versions are parsed as 1.2 with a `yaml.WarnVersion` warning, as §6.8.1 asks; `%YAML 2.0` and later majors are a `*ParseError`. `%TAG` handles are expanded (DL-175). goccy v1.19.2 can't parse two directives before one document, such as `%YAML` then `%TAG`: a known library failure, for M12. |
| Merge keys            | n/a                                               | n/a                                      | `<<: *a` and `<<: [*a, *b]` are supported, though they are a YAML 1.1 type: the table's own keys win, then earlier list entries, as the merge-key spec says (goccy lets later ones win). A merge of anything but a table is a `*ParseError` (DL-167). |
| Tags                  | n/a                                               | n/a                                      | Only the core schema's: `!!str`, `!!int`, `!!float`, `!!bool`, `!!null`, `!!map`, `!!seq` and the non-specific `!`. A tag forces its type, so `!!int abc` is a `*ParseError`. Any other tag, such as `!!binary`, `!!timestamp`, `!!set` or `!foo`, is a `*ParseError`, as the spec treats an unresolved tag. Tags are judged after expanding `%TAG` handles, so `!e!str` under `%TAG !e! tag:yaml.org,2002:` is `!!str`, and `!!str` under a `!!` redefined elsewhere is rejected (DL-164). |
| Dates and times       | n/a                                               | Offset date-times as RFC 3339 text (`+00:00` becomes `Z`, and trailing fractional zeros go); local date-times, dates and times as their TOML text (DL-169) | No timestamp type in the core schema: a date stays a string exactly as written (DL-165) |
| Parse errors          | Position and a short message                      | Position and go-toml's short message     | Position and goccy's short message, never its excerpt of the file (DL-170) |

**Rules for every format:**

- **Encoding:** input must be UTF-8. A leading UTF-8 byte-order mark is stripped first. Without that, JSON and TOML
  reject the file, and YAML **silently** makes the mark part of the first key (`a` becomes `"\ufeffa"`). Other
  encodings, such as UTF-16, are a `*ParseError`.
- **The top level must be a map** (a table or object). A top-level array or scalar is a `*ParseError`, because only maps
  can be merged.
- **Empty files:** a file is *empty* if it holds only whitespace or comments, or decodes to null (YAML `~`, JSON
  `null`). An empty file contributes nothing and is reported with status `Empty`. In a first-match layout, it's skipped
  and the search continues, as in cosmiconfig. `{}` is **not** empty: it's an explicit empty config, and in a
  first-match layout it stops the search.
- **Integers** are `int64` only when the literal has no fraction or exponent and fits in range. JSON `1.0` and `1e3` are
  floats. An integer outside the `int64` range is a `*ParseError`.
- **Dates and times are strings** in the tree. TOML's are written in RFC 3339 form (a TOML `1979-05-27 07:32:00Z`
  becomes `"1979-05-27T07:32:00Z"`); a YAML date is already a string. The choice of type is deferred to decoding, so a
  `string` field gets the text and a `time.Time` field gets a parsed time when the text is RFC 3339
  ([api.md](api.md#decoding)).
- **A library panic** while parsing is recovered and returned as a `*ParseError`, so a hostile file can't crash the app.
  Only the library's parse call is guarded: a panic in the decoder's own code isn't recovered, so it surfaces as a bug
  (DL-171).
- **A custom decoder** can replace a built-in one through the public `Decoder` interface and `WithDecoder`, for example
  to wrap yaml.v3 for its YAML 1.1 behaviour. `DecodeDocument` still applies the rules above that every format shares
  ([api.md](api.md#settled-in-m6)).

## Includes

A config file can pull in other files through an include key. Every reference, local or remote, goes through a resolver
(see [Resolver hooks](#resolver-hooks)). Each preset names its key and chooses which resolvers it registers. Every
preset registers the `file` resolver (see [Local files](#local-files)).

| Preset              | Include key | Remote resolvers registered | Nesting                                   |
| ------------------- | ----------- | --------------------------- | ----------------------------------------- |
| `mise.Preset`        | `include`   | `http`/`https`, `git`, `oci` | No: an included file can't include (mise) |
| `misehardened.Preset`  | `include`   | none, so local files only   | No                                        |
| `cosmiconfig.Preset` | `$import`, deferred and possibly never supported (DL-145) | none, so local files only (as cosmiconfig) | Yes, with cycle detection (as cosmiconfig) |

The include key is a layout setting (DL-178): `WithIncludeKey(key, nesting)`, where `nesting` is `IncludeFlat` (an
included file can't include) or `IncludeNested` (with cycle detection), and `WithoutIncludeKey()` turns includes off. No
built-in layout nests includes while `$import` is deferred, so `IncludeNested` is for custom layouts.

**Rules for every include:**

- **Placement:** the include key only works at the **top level** of a file, as in mise and cosmiconfig. For a pattern
  with a `Key`, that's the top of the keyed table, where `_unset` is read too. The key is removed from the result, as
  `_unset` is. Elsewhere in a file, it's ordinary data.
- **Value:** a string or a list of references. An empty list includes nothing. An empty string, a non-string element or
  any other type is a `*ParseError` (DL-184).
- **Rank:** an included file ranks directly below the file that includes it, and a later entry beats an earlier one.
  This is mise's rule, and it is also how cosmiconfig's `$import` resolves (own keys > last import > first import).
- **Local paths** are relative to the including file's directory, for nested includes too. Absolute paths are allowed,
  and a leading `~/` is the home directory (see [Local files](#local-files)).
- **Formats:** an included file's format comes from the same rules as a searched file's: `WithFileFormat` rules, then
  extensions ([Choosing a file's format](#choosing-a-files-format)). The format order doesn't apply, so any registered
  decoder can load it. A file with no registered decoder is an `*UnsupportedFormatError` (415).
- **Merge behaviour:** `_unset`, `null` and per-key merge rules all work as in any other layer. An included file ranks
  below its includer, so the includer's `_unset` removes keys from it, which is how a file drops a key from its own base
  (DL-187):

  ```toml
  # myapp.toml
  include = ["base.toml"]
  _unset = ["proxy"] # removes base.toml's proxy, and any lower layer's
  ```

- **Each file loads once** (DL-179), as any file reached twice does ([Filesystem hook](#filesystem-hook)): at its
  highest position, with its other routes listed as `LayerDuplicate`, a reference listed twice included. Reaching a file
  that is already an ancestor in its own chain, such as a file that includes itself, is a cycle: a `*CycleError` (508).
  A remote file is identified by its normalised reference, before it's fetched, so a reference listed twice is fetched
  once, and different references to the same content are separate layers (DL-203).
- **Nesting:** under `IncludeFlat`, an include key in an included file is a `*ParseError` naming the file and the key,
  as mise rejects it. Under `IncludeNested`, included files can include, with cycle detection.
- **Limits** (DL-210): every reference is new to cycle detection when a remote server answers each fetch with another
  relative include, so two limits bound every load, local and remote alike. An include nested more than
  `WithMaxIncludeDepth(n)` levels below its root file (16 by default) is a `*CycleError` (508). More than
  `WithMaxIncludes(n)` include references in one load (1000 by default), a reference listed twice counting twice, is a
  `*TooLargeError` (413). Both name the including file, and a limit that isn't positive is an `*OptionError`.
- **Where includes work** (DL-187): in files from `FixedDir`, `Walk` and `ExplicitFile` scopes, and in a custom scope's
  `Source`s with a `Path`. In `EmbeddedFS` sources, value scopes (`Values`, `ValuesFunc`, `EnvVars`) and bootstrap
  files, the key is ordinary data: there's no file location to resolve a relative reference against, and bootstrap files
  have their own keys. Under `FirstMatch`, an included file never counts as the match.
- **Metadata** (DL-184): an included layer takes `Scope`, `Variant`, `Env`, `Trust` and `ConfigRoot` from the root file
  of its chain. Its `Path` is the absolute path as joined, before symlinks, like a searched file's; its `Format` comes
  from its own name; its `Pattern` is empty; and `IncludedBy` is its direct includer's `Path`. A remote layer's `Path`
  is the reference as written (the joined reference, for a relative one inside a remote file), and `Layer.Remote` holds
  its scheme, normalised reference, fetched identity, whether it's pinned, and whether it came from the cache (DL-203).
- **Trust:** an included file inherits its includer's trust decision, as in mise
  ([3.9](config-locations.md#39-includes)), and the trust store isn't asked about it. It's judged by its root file's
  scope policy ([Per-scope policies](#per-scope-policies)).
- **Steps for each layer** (DL-186): decode, take the keyed table, run the layer transforms, read and remove the include
  key, then apply `_unset`. Includes are read after the transforms, so an app can expand variables in references.
- **Errors:** a reference that can't be loaded is a load error. Includes are never silently skipped, because silently
  dropping config is worse than failing, so an included file that a permission policy would ignore is a `*PolicyError`
  (403) (DL-181). Where the error points (DL-185):
  - **Errors about the reference** (missing 404, policy or confinement 403, unknown scheme 501, cycle 508, and an
    invalid reference 400): `Path` is the including file, `Op` is `"include"`, and the message quotes the reference as
    written. A broken symlink points at nothing, so it counts as missing.
  - **Errors inside the included file** (parse 400, too large 413, unreadable or not a regular file `*IOError`): `Path`
    is the included file, and its `LayerFailed` entry shows `IncludedBy`.
  - **An empty included file** is `LayerEmpty`, as any empty file is.

### Local files

Local includes are handled by the built-in `file` resolver (DL-182). It's part of the loader, not an ordinary
`IncludeResolver`, because a local read needs the filesystem hook, dedup by file identity, symlink handling and the
permission policy, which `Resolve` can't express. It's registered by default, even without a preset, and can't be
replaced.

- **References** (DL-183): plain paths (`base.toml`, `../shared/base.toml`, `/etc/acme/base.toml`) and `file://` URLs
  with an empty host or `localhost` both go to this resolver. A relative path resolves against the including file's
  directory, and a leading `~/` against the home directory. A reference is remote only if it starts with `name::` (as
  `git::` and `oci::` do) or with `scheme://`, where the scheme has two or more letters. Anything else is a path, so
  `C:\cfg\base.toml` is a Windows path, not the scheme `c`. A `file://` URL naming any other host is a `*ReferenceError`
  (400).
- **Allow-lists:** it accepts the same matchers as the remote resolvers. They are matched against the absolute slash
  path after symlinks are resolved.
- **`Confine(roots...)`** (DL-180): the resolved path must be one of the roots or lie below one, compared by whole path
  segments after symlinks, so `/etc/acme` doesn't cover `/etc/acmex`. Roots use `FixedDir`'s syntax: absolute paths,
  `~`, `{user}`, `{system}` and `{cwd}`, plus `{config_root}`, the including file's config root (the root file's, for a
  nested include). Roots are resolved through symlinks too, so `/etc` on macOS matches `/private/etc`. A root that
  doesn't exist matches nothing, and `{user}` with no home is dropped. `misehardened.Preset` sets
  `Confine("{config_root}", "{user}", "{system}")`. Confinement applies to includes only, never to searched or explicit
  files.
- **Reads** go through the [filesystem hook](#filesystem-hook), so they get its permission policy.
- **Turning it off** (DL-177): `WithoutIncludeResolver("file")` removes it, and with an include key still set that's an
  `*OptionError` at `New` ([api.md](api.md#a-loader-without-a-preset)). To forbid local includes in some scopes, give
  them a policy whose `AllowResolvers` leaves out `"file"`; a local reference there is a `*PolicyError` (403).

### Remote references

| Scheme          | Form                                   | Example                                                               |
| --------------- | -------------------------------------- | --------------------------------------------------------------------- |
| `http`, `https` | A URL, optionally with `#sha256=<hex>`  | `https://config.example.com/myapp/base.toml#sha256=9f86d0…`           |
| `git`           | `git::<repo-url>//<path>[?ref=<ref>]`  | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<image-ref>[//<path>]`            | `oci::ghcr.io/acme/myapp-config:1.4.0`                                |

**Three forms** (DL-197). The loader reads a reference's form from its text, not from the resolver, so a custom resolver
gets the same parsing and checks as a built-in one written the same way:

- **URL form,** `scheme://…`: `http`, `https`, and any custom scheme written as a URL.
- **Image form,** `oci::<registry>/<repository>[:<tag>][@<digest>][//<path>]`:
  - **The registry must be explicit:** it contains a `.` or a `:`, or is `localhost`. There's no implicit `docker.io`,
    so an allow-list sees where the fetch goes.
  - **Grammar:** repository, tag and digest follow the OCI distribution spec, and only `sha256` digests are accepted.
  - **Defaults:** with neither tag nor digest, the tag is `latest`; with both, the digest is fetched. The path defaults
    to `{name}.toml` at the artifact root, `{name}` being the app's name.
- **Getter form,** any other `name::…`: `name::<url>//<path>[?ref=<ref>]`, as `git::` uses.
  - **The URL** is any `scheme://` URL, and the resolver decides which transports it allows. So `ext::` and local paths
    never reach a resolver.
  - **The path** is required.
  - **`ref`** is optional, and without it the default branch is fetched. Any other query parameter, such as go-getter's
    `sshkey` or `depth`, is a `*ReferenceError`.
  - **A ref** follows git's ref-name rules: it isn't empty, doesn't start with `-`, and contains no `..`, `@{`,
    whitespace, control characters or any of `~^:?*[\`. It also can't contain `&`, `#`, `%` or `+`, which the
    reference's own syntax uses.
  - **A trailing `/`** on the repo URL is dropped, since it names the same repository and would run into the `//` before
    the path.
- **`http::…`,** the getter form with a URL scheme, is a `*ReferenceError`.

**Format** (DL-198): a remote file's format is chosen as a local file's is: `WithFileFormat` rules, matched as
allow-lists match the reference (`Extension` and `FileName` see its file path, the other matchers its normalised form),
then the extension of the reference's file path (the URL path, or the inner path). Only when neither gives one does it
come from the media type the resolver reports, such as http's `Content-Type`, matched against the decoders' media types.
Finding none is a 415. For the getter and image forms, an inner path with no known format is a 415 before anything is
fetched.

**Pinned references** name content that can't change. A policy with `RequirePinned()` accepts only these:

| Scheme          | Pinned when                                                                                              |
| --------------- | -------------------------------------------------------------------------------------------------------- |
| `git`           | `ref=` is a **full** commit id: 40 hex characters (SHA-1) or 64 (SHA-256). An abbreviated id isn't a pin. |
| `oci`           | The image reference has a digest: `@sha256:<64 hex>`.                                                    |
| `http`, `https` | The URL has an integrity fragment, `#sha256=<64 hex>`. The downloaded bytes are hashed and must match, or the fetch is a `*PolicyError`. The fragment is never sent to the server. |

- **The loader decides** (DL-201). Pins follow from the form, so resolvers need no interface for them: a URL-form
  integrity fragment, a getter-form ref of 40 or 64 hex digits, or an image-form digest. Any custom URL-form scheme can
  be pinned with an integrity fragment.
- **Integrity fragments are the loader's job:** it strips the fragment before calling the resolver, so the fragment is
  never sent, and checks the SHA-256 of the content itself, so no resolver can skip the check. A mismatch is a
  `*PolicyError`.
- **`RequirePinned()`** is checked before anything is fetched. An unpinned reference is a `*PolicyError`.

### Resolver hooks

Remote fetching is done by resolvers. The built-in remote resolvers, `httpresolver`, `gitresolver` and `ociresolver`,
are ordinary implementations of the same interface:

```go
type IncludeResolver interface {
	// Schemes this resolver handles, e.g. "http", "https".
	Schemes() []string
	// Resolve fetches ref. It must honour ctx for cancellation and timeouts.
	Resolve(ctx context.Context, ref IncludeRef) (*Fetched, error)
}

type IncludeRef struct {
	Raw      string // as written in the config file
	Scheme   string // "https", "git", "oci", …
	Ref      string // the normalised reference string that matchers see; for "file", the resolved path
	Location string // the URL without its fragment, the repo URL, or registry/repository; normalised
	Path     string // path inside a repo or artifact, if any
	Version  string // git ref; oci tag, digest or tag@digest; sha256:<hex> from an integrity fragment
	From     Layer  // the including file: path, scope, variant
	MaxSize  int64  // the size limit the loader enforces
	Validator string // the validator from the last fetch, when the loader holds that content; "" otherwise

	// Check normalises another reference, such as a redirect target, and runs the scheme and allow-list checks this
	// reference passed, under the same policy.
	Check func(ref string) error
}

type Fetched struct {
	Content   []byte
	MediaType string // the media type the transport reports, if any, e.g. Content-Type
	Identity  string // canonical identity, e.g. commit id or digest; shown in reports
	Validator string // opaque; passed back in IncludeRef.Validator next time
}

// ErrNotModified is returned by a resolver when the source still matches IncludeRef.Validator.
var ErrNotModified = errors.New("cascadingconfig: not modified")
```

- **Registering** (DL-196): `WithIncludeResolver(r)` maps each of `r.Schemes()` to `r`, and the last registration of a
  scheme wins. Schemes are lowercased. These are `*OptionError`s: a nil resolver, an empty `Schemes()`, an invalid
  scheme (a letter, then letters, digits, `+`, `-` or `.`, two characters or more), and `file`, which can't be replaced.
  `WithoutIncludeResolver(scheme)` removes one scheme, even from a resolver that handles others.
- **Size** (DL-198): `MaxSize` is `WithMaxIncludeSize(n)`, 1 MiB by default. A resolver should stop reading past it and
  return a `*TooLargeError`. The loader checks the content's length anyway, so it's a 413 either way.
- **`Check`** (DL-198) returns a `*ReferenceError` or a `*PolicyError`. The http resolver calls it for every redirect.
- **Identity** (DL-198): an empty `Identity` becomes `sha256:<hex>` of the content. One longer than 256 bytes, or with
  control characters, is a `*FetchError`, because it's shown in reports.
- **Errors** (DL-199): an error carrying a cascadingconfig code keeps its type, so a resolver can return
  `*NotFoundError`, `*TooLargeError`, `*PolicyError`, `*ReferenceError` or `*FetchError`. The load's own cancellation
  stays a 499 or 504 `*BaseError`. Any other error becomes a `*FetchError`: 504 for a timeout, 502 otherwise. Errors
  point at the including file, with `Op` `"include"`, as for local includes (DL-185).
- **Timeouts** (DL-199): each fetch gets the load's context with a deadline, 30 seconds by default, set with
  `WithIncludeTimeout(d)`. Running out is a 504 `*FetchError`.
- **One at a time** (DL-199): references are fetched one after another, in walk order, which keeps loads deterministic.
- **Conditional fetches** (DL-229): a resolver may return a `Validator` with its content. The loader keeps it only if
  it's printable ASCII and at most 1 KiB, so a hostile server can't inject headers through it, and passes it back in
  `IncludeRef.Validator` only when it holds the content to fall back on. A resolver that finds the source unchanged
  returns `ErrNotModified`, wrapped or not, and the loader uses its copy. A resolver may always ignore the validator and
  fetch.

The app can add a resolver for a new scheme or replace a built-in one with `WithIncludeResolver(r)`, and can remove one
with `WithoutIncludeResolver("git")`.

### Allow-lists

Each resolver can have an allow-list. There is also an allow-list that applies to every scheme:

<!-- example -->
```go
cascadingconfig.WithDefaultPolicy(
	cascadingconfig.Allow( // every scheme
		cascadingconfig.Prefix("https://config.example.com/"),
	),
	cascadingconfig.AllowFor("git",
		cascadingconfig.Glob("git::https://github.com/acme/*//**"),
		cascadingconfig.MatchRef(func(ref cascadingconfig.IncludeRef) bool {
			return ref.From.Scope != cascadingconfig.ScopeProject // only user/system files may fetch
		}),
	),
)
```

- **Matchers** (DL-176): `Prefix`, `Glob`, `Regexp` and `MatchFunc` match the normalised reference string, as they match
  a path in `WithFileFormat`. `Extension` and `FileName` look at the last segment of the reference's file path, the URL
  path or the inner path, ignoring the query and the ref (DL-202). `MatchRef` gets the whole `IncludeRef`, including the
  file that asked for it; it's for allow-lists only, and an `*OptionError` in `WithFileFormat`.
  - **`Glob`:** `*` matches within one `/`-separated segment, and `**` matches across segments. So
    `git::https://github.com/acme/*//**` matches every file in `…/acme/config.git` but not in `…/acme/team/config.git`,
    and `git::https://github.com/acme/**` matches both. A git reference's normalised string goes on with `//<path>`, so
    `git::https://github.com/acme/*` alone matches none. `?` matches one character, and there are no other
    metacharacters.
  - **`Regexp`** is anchored at both ends: it must match the **whole** normalised string.
  - **`Prefix`** is a plain string prefix, so end it with `/` to match a directory: `Prefix("/etc/acme")` also matches
    `/etc/acmex/base.toml`. It works in `WithFileFormat` too.

**The normalised reference string** that matchers see:

| Scheme          | Form                                                    | Example                                                                 |
| --------------- | ------------------------------------------------------- | ----------------------------------------------------------------------- |
| `http`, `https` | `scheme://host[:port]/path[?query]`, normalised as in [Traversal protection](#traversal-protection), with the integrity fragment dropped. | `https://config.acme.internal/myapp/base.toml` |
| `git`           | `git::<repo-url>//<path>[?ref=<ref>]`, with the repo URL normalised like an http URL, the inner path cleaned, and no `?ref=` when no ref is given. | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<registry>/<repository>[:<tag>][@<digest>]//<path>`. The registry is lowercased, the tag is `latest` when there's neither tag nor digest, and the inner path is always present (`{name}.toml` by default). | `oci::ghcr.io/acme/myapp-config:1.4.0//myapp.toml` |
| `file`          | The absolute path, after resolving symlinks.            | `/etc/acme/base.toml`                                                   |

- **Matching:** a reference must match **at least one** entry on each allow-list that applies to it, the global one and
  its scheme's.
- **No allow-list configured:** every reference is allowed in `mise.Preset`, as in mise. `misehardened.Preset` has no
  remotes at all, and requires an allow-list if an app turns them back on.
- **No match is a load error** naming the including file, never a silent skip.
- **Redirects:** the `http(s)` resolver checks each redirect target with `IncludeRef.Check`, against the same
  allow-lists.
- **No address filtering** (DL-202): a reference to an internal address, such as `http://169.254.169.254/…`, is allowed
  unless an allow-list leaves it out. The allow-list is the control.
- **References are shown unredacted** (DL-202), in reports, errors and logs. Credentials belong in resolver options
  (`httpresolver.WithClient`, and the git and OCI resolvers' credential options), never in a reference.
- **`Describe()`** shows each remote layer's raw reference and its fetched identity.

### Traversal protection

Remote references carry paths and URLs written in config files, so every resolver must stop them from reaching outside
the repo, artifact or origin they name. Some checks are enforced by the **loader**, around every resolver. The rest are
per-scheme rules for the **built-in** resolvers.

#### Enforced by the loader, for every resolver

These checks also cover resolvers that apps write themselves:

1. **Before matching:** the reference is normalised and validated **before** it is matched against allow-lists, so a
   reference can't pass an allow-list in one form and be fetched in another (DL-200):
   - **URLs:** parsed, with the scheme and host lowercased, a trailing dot and the default port (80 for `http`, 443 for
     `https`, 22 for `ssh`) removed, dot segments resolved (RFC 3986 §5.2.4), an empty path made `/`, escapes of
     unreserved characters decoded, and other escapes uppercased. A bad port is a `*ReferenceError`.
   - **Hosts must be ASCII:** a host with any other character is a `*ReferenceError`, so write it in its `xn--` form.
     This keeps IDNA's dependency out of the core, and rules out lookalike hosts. A host must be a name of non-empty
     labels (letters, digits, `-` and `_`) separated by single dots, an IPv4 address, or a bracketed IPv6 address.
   - **Userinfo is rejected,** because `https://config.acme.internal@evil.example/` would otherwise match a `Prefix` on
     the internal host. The one exception is a user name without a password in an `ssh` URL, which
     `git::ssh://git@github.com/…` needs; it stays in the normalised string, and may only hold letters, digits, `.`, `_`
     and `-`. The scp form (`git@github.com:acme/config.git`) isn't accepted.
   - **Also rejected:** a percent-encoded `.`, `/` or `\` in the path (`%2e`, `%2f` or `%5c`, in either case), a raw
     `\`, NUL and control characters, and any fragment other than an integrity fragment.
   - **The query** is kept in the normalised string, so an allow-list can see it.
   - **Inner paths** (the `//<path>` of `git::` and `oci::`) are cleaned with `path.Clean` and must then satisfy
     `fs.ValidPath`, which rules out a leading `/` and `..` segments. They must not contain `\`, `#`, NUL, control
     characters or a percent-encoded `.`, `/` or `\` (a resolver that builds a URL from the path would have the server
     decode it), and must have a known format.
2. **After fetching:** the `Fetched` result is checked. `Content` must be within `MaxSize`, an integrity fragment must
   match its SHA-256, and its format is chosen as described in [Remote references](#remote-references) (DL-198).
3. **Relative references inside a remote file** (DL-204) resolve against that file's **remote origin**, relative to its
   directory: against its URL (RFC 3986 §5.2), or in the same repo and ref, or in the same artifact.
   - **Never local:** climbing above the repo or artifact root, `~/` and `file://` are `*ReferenceError`s, so a remote
     file can never reach the local filesystem. In a repo or artifact, a path starting with `/` is a `*ReferenceError`
     too; in a URL-form file, it resolves against the URL's origin, as RFC 3986 says.
   - **Errors** about a relative reference name the remote file that wrote it, by its `Path`.
   - **Absolute remote references** are allowed, and judged by the root file's policy.
   - **Rechecked:** the joined reference is checked again from step 1.
   - **Nesting only:** this only matters for layouts with `IncludeNested`, because the mise layouts don't nest.
4. **Cache paths** are built from hashes, never from any part of the URL, repo or path: content is stored under its own
   SHA-256, and its index entry under the SHA-256 of the normalised reference (DL-206). Cache files are written through
   `os.Root` on the cache dir, by atomic rename.

#### `git`

- **No worktree.** The file is read from the object database, not from a checked-out tree, so there are no files on disk
  for a symlink to redirect (DL-211). Each fetch uses a fresh bare repo in a 0700 temp dir, removed afterwards:
  1. `git fetch --depth=1 --no-tags` of the ref, the commit id, or `HEAD` when there's no ref, with protocol v2 forced
  2. `git rev-parse --verify --end-of-options FETCH_HEAD^{commit}`; for a commit id, the result must equal it
  3. `git ls-tree <commit> -- <path>` for the entry's mode and type
  4. `git cat-file -s` for its size, checked against `MaxSize` before anything is read
  5. `git cat-file blob` for the content
- **Regular files only:** the tree entry's mode must be `100644` or `100755`. A symlink (`120000`) or submodule
  (`160000`) is rejected, even if it points inside the repo. This is stricter than mise, which canonicalises the path in
  a checkout and rejects it if it escapes or isn't a regular file (`validate_remote_git_path`).
- **No option injection:** the ref is resolved to a full commit SHA with `git rev-parse --verify --end-of-options`, and
  refs starting with `-` are rejected.
- **Transports:** only `https` and `ssh` are allowed by default. The repo URL's scheme is checked before git starts, and
  one that isn't allowed is a `*PolicyError` (DL-212); git also gets `GIT_ALLOW_PROTOCOL` and `protocol.allow=never`
  with each allowed protocol set to `always`. That blocks `ext::`, `fd::`, `file://` and local-path remotes.
- **No side effects:** fetches are shallow, of that commit only. They use no submodules, no LFS or other filters, and
  `core.hooksPath` is set to an empty directory. `transfer.fsckObjects` checks every object received.
- **Isolated environment** (DL-212): git runs with every `GIT_*` variable removed, because `GIT_CONFIG_COUNT` and
  `GIT_CONFIG_KEY_<n>` set config even with `GIT_CONFIG_NOSYSTEM=1`. It keeps `PATH`, `HOME`, `SSH_AUTH_SOCK`, the proxy
  variables and, on Windows, the system variables, and gets `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` pointing at an
  empty file, `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=never`, `LC_ALL=C` (so its messages can be recognised) and an
  empty stdin. Automatic `gc` and maintenance are off. So the user's and the system's git config never apply: otherwise
  a `url.<base>.insteadOf` rule could rewrite a URL *after* the allow-list check, and `includeIf` or credential helpers
  would run. Credential helpers are opt-in, with `gitresolver.WithCredentialHelper(helper)`. ssh runs with
  `BatchMode=yes`, so nothing prompts.
- **Redirects** (DL-213): git never follows one itself (`http.followRedirects=false`), because with its default it
  silently follows a redirect to another host. For an `http` or `https` repo URL, the resolver first requests
  `<repo>/info/refs?service=git-upload-pack` with Go's HTTP client, following redirects under `httpresolver`'s rules: at
  most 5 hops, none from `https` to `http`, and each one through `IncludeRef.Check`. git then fetches from the final
  URL. The layer's `Remote.Ref` and the cache key stay the original reference. An answer that isn't a redirect, such as
  a 401 from a private repo, leaves the original URL, and so does a network failure, which git then reports.
  `gitresolver.WithClient(c)` sets that client.
- **Version:** git 2.24 or newer is required, for `--end-of-options`, and 2.29 for SHA-256 repositories. An older or
  missing git is a `*FetchError` that says so, checked once per resolver. `gitresolver.WithGit(path)` names the program;
  by default it's found on `PATH`.
- **Fetching by commit:** protocol v2 servers serve any reachable commit by its id, but some v0 and v1 servers refuse.
  Then the resolver fetches every branch and tag with `--filter=blob:none` (a server without filter support sends
  everything), checks the commit is among them, and reads the file (DL-211).
- **SHA-256 repositories:** a 64-hex commit id fetches into a SHA-256 repo. A named ref in a SHA-256 repository is
  fetched again into one when git reports the object-format mismatch.
- **Errors** (DL-214): a ref the remote doesn't have, or a path the commit doesn't have, is a `*NotFoundError`. A
  symlink, submodule or directory is a `*ReferenceError`, so no stale copy stands in for it. A file over `MaxSize` is a
  `*TooLargeError`. Anything else is a `*FetchError` with the first line of git's error output, at most 200 characters.
  A timeout kills git and its ssh child.
- **Identity:** the full commit id. There's no media type.
- **Conditional fetches** (DL-229): the validator is the commit id. For a branch or tag with a validator,
  `git ls-remote` runs first, with the same isolation and redirect rules as the fetch, and the same commit is
  `ErrNotModified`. A commit id is pinned, so it's never rechecked. The ref is matched as `git fetch` matches it: as
  given, then under `refs/`, `refs/tags/` and `refs/heads/`, with an annotated tag's commit taken from its peeled line.
  A ref `ls-remote` doesn't list isn't `ErrNotModified`, so the fetch reports it.
- **Protocols:** `gitresolver.WithProtocols(...)` changes the allowed transports. The default is `https` and `ssh`.
  Tests use it to allow `file` for repositories in temporary directories, and `http` for a test server.

#### `oci`

- **Manifests** (DL-215): the tag or digest is resolved to a manifest digest before anything else is read, and that
  digest is the layer's identity. A manifest is capped at 4 MiB, and a larger one is a `*TooLargeError`; it's verified
  against its digest. OCI image manifests and Docker v2 schema 2 manifests are accepted; an image index or any other
  type is a `*ReferenceError` that asks for a manifest.
- **Two artifact forms.** An artifact pushed with `oras push` stores each file as its own blob, named by the
  `org.opencontainers.image.title` annotation. An image-style artifact stores files in tar layers. Both are supported
  (DL-215):
  - **A titled layer** without `io.deis.oras.content.unpack: "true"` is one file, named by its title, whatever its media
    type: `oras push` gives a file the media type `application/vnd.oci.image.layer.v1.tar` by default, though its
    content is the file itself.
  - **A titled layer with that annotation,** as `oras push` stores a directory, is read as tar entries. oras puts the
    directory's name at the start of every entry, so `conf/a.json` matches the inner path `conf/a.json`.
  - **A layer without a title** is read as tar entries when its media type is a tar type (OCI or Docker, plain or gzip),
    and is ignored otherwise.
  - **zstd** layers are a `*ReferenceError` for now, because zstd needs a new dependency.
- **No extraction to disk.** Layers are read as streams, and the requested file is found among them. A titled file that
  matches is downloaded only if no later layer replaces or whites it out. Only the titled blobs that match are
  downloaded; every tar layer is read in full, for the malformed-entry rule and for whiteouts.
- **Duplicates:** layers are taken in manifest order, and if the same path appears in several, the **last** wins, as in
  an image. Whiteouts work as in an image too: `.wh.<name>` in a later layer removes `<name>`, and anything below it,
  from earlier layers, and `.wh..wh..opq` hides what earlier layers put in its directory.
- **Caps** (DL-216): `ociresolver.WithMaxLayerSize(n)`, 64 MiB by default, caps each blob's declared size (checked from
  the manifest before downloading), each download, and each decompressed layer, so a small compressed layer can't expand
  without bound. The matched file is capped by the include size limit, from its header before it's read. Either is a
  `*TooLargeError`.
- **Options** (DL-217): `ociresolver.WithCredentials(func(ctx, registry) (Credential, error))` supplies registry
  credentials, as the package's own `Credential` type. By default none are sent, and `~/.docker/config.json` isn't read.
  `ociresolver.WithPlainHTTP(hosts...)` allows plain-HTTP registries, by `host[:port]`, for tests and local registries
  only; every other registry uses https. `ociresolver.WithClient(c)` sets the HTTP client, which is copied.
- **Auth:** the standard bearer-token flow, with the credentials for the registry, and anonymous tokens otherwise. The
  token service may be on another host, such as Docker Hub's `auth.docker.io`, as the distribution spec allows. Tokens
  are cached per resolver.
- **Redirects** (DL-217): registries redirect blob downloads to CDNs, so the resolver follows a registry's redirects
  without matching them against the allow-lists: at most 5 hops, never from `https` to `http`. Unlike `http` and `git`,
  that gives a redirect no power: blobs and digest-pinned manifests are verified, and a tag's manifest is the registry's
  to choose anyway.
- **Entry matching:** each entry name is cleaned, and a leading `./` is fine, then compared to the validated inner path.
  An entry or title with an absolute name, a `..` segment or a `\` fails the whole fetch, even if it isn't the requested
  file, because it means the artifact is malformed or hostile.
- **Regular files only:** the matched entry must be a regular file (`TypeReg`). Symlinks, hard links, devices and
  directories are rejected.
- **Digests:** every blob's digest, and the manifest's, is verified as it streams.
- **Errors** (DL-218): a missing repository, tag, digest or path is a `*NotFoundError`. An index, an unsupported layer
  type, a malformed entry, a layer that isn't a valid tar or gzip stream (once its digest checks out) or a non-regular
  file is a `*ReferenceError`, so no stale copy stands in for it. A cap is a `*TooLargeError`. Anything else, such as a
  network failure, a 401 or 403, a 5xx, a digest mismatch or an error from the credentials function, is a `*FetchError`.
- **Conditional fetches** (DL-229): the validator is the manifest digest. The tag is resolved first anyway, so the same
  digest is `ErrNotModified` before any blob is downloaded.

#### `http` / `https`

- **Options** (DL-208): `httpresolver.New(opts...)` handles `http` and `https`. Its one option, `WithClient(c)`,
  supplies the `*http.Client`. The resolver copies it and installs its own redirect check, which runs before the
  client's own, so the app's client isn't changed. Auth headers go in the client's transport.
- **Redirects:** at most 5 hops, and more is a 502 `*FetchError`. A redirect from `https` to `http` is a `*PolicyError`.
  Each hop goes through `IncludeRef.Check`, which normalises it and matches it against the allow-lists again. The final
  URL is what's fetched.
- **Responses** (DL-208): only a 200 is content. A 404 or 410 is a `*NotFoundError`, and any other status, 204 included,
  a 502 `*FetchError`. A 304 is `ErrNotModified` in answer to a conditional request, and a 502 otherwise. A
  `Content-Length` over `MaxSize` is a 413 before the body is read. `MediaType` is the `Content-Type` header, and
  `Identity` is `sha256:<hex>` of the body.
- **Conditional fetches** (DL-229): the validator is the `ETag` header, weak or strong, or else `Last-Modified`,
  recorded as `etag:<value>` or `last-modified:<value>`. The resolver sends it back as `If-None-Match` or
  `If-Modified-Since`.
- **No local schemes:** `file://` URLs never reach this resolver. They go to the `file` resolver, with its own
  confinement.
- **Exact bytes:** the response body is the content, and no decompression beyond `Content-Encoding` is applied. A `.tar`
  or `.zip` body is not unpacked.

#### Errors

A traversal or validation failure is a **load error**, never a skip. The error names the including file, the reference
as written and the rule it broke, for example "`..` segment in OCI layer entry".

### Per-scope policies

All the resolver rules can differ by scope, so each scope can have its own trust level. A **policy** bundles:

- the resolvers that are usable, by scheme (`AllowResolvers(...)`)
- the allow-lists, both global (`Allow(...)`) and per scheme (`AllowFor(...)`)
- whether remotes must be pinned (`RequirePinned()`)
- the `file` resolver's confinement roots (`Confine(...)`)
- whether files must be approved before they load (`RequireTrust(store)`, see
  [Trust store](#trust-store))
- the file permission policy (`FilePermissions(...)`, see
  [Filesystem hook](#filesystem-hook))

<!-- example -->
```go
misehardened.Preset.Extend(
	cascadingconfig.WithIncludeResolver(httpresolver.New()),
	// Only system config may fetch, and only from the internal config host.
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeSystem,
		cascadingconfig.InheritDefault(),
		cascadingconfig.AllowResolvers("file", "https"),
		cascadingconfig.AllowFor("https", cascadingconfig.Prefix("https://config.acme.internal/")),
		cascadingconfig.RequirePinned(), // required in misehardened: refs need #sha256=…
	),
	// Project config may only include files inside its own project.
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.Confine("{config_root}"),
	),
)
```

**How policies combine:**

- **The default policy** is set with `WithDefaultPolicy(...)`, which takes the same options as `WithScopePolicy`. It
  applies to every scope that has no policy of its own. `WithDefaultPolicy` and `WithScopePolicy(scope)` are single
  settings, so the last call wins.
- **A scope policy replaces the default for that scope** as a whole, so a scope can be *more or less* trusted than the
  default. If policies were merged, a scope could only ever narrow the default. `InheritDefault()` starts from the
  default and changes only what follows.
- **Each option replaces its own setting** (DL-188): `AllowResolvers` the usable set, `Allow` the global allow-list,
  `AllowFor(s)` the list for `s`, `Confine` the roots, and `RequireTrust` and `FilePermissions` their checks. Within one
  policy the last one wins, and `InheritDefault()` anywhere in the list means "start from the default". So
  `InheritDefault(), Allow(x)` replaces the default's global list instead of adding to it.
- **A fresh policy,** one without `InheritDefault()`, starts from nothing: no resolver is usable unless listed, and
  there are no allow-lists, no confinement, no trust requirement and no permission check. A policy that lists no
  resolvers forbids every include.
- **Validation** (DL-189): a `WithScopePolicy` for a scope that is never declared, and an `AllowResolvers` or `AllowFor`
  naming a scheme with no registered resolver, are `*OptionError`s, checked once all options are applied.
- **Within one policy,** allow-lists combine as described in [Allow-lists](#allow-lists): a reference must match the
  global list and its scheme's list.
- **Registered vs usable:** registering a resolver makes it *available*. Only policies that list it in `AllowResolvers`
  can *use* it. In `mise.Preset`, the default policy allows every registered resolver. In `misehardened.Preset`, the
  default policy allows only `file`, so a remote resolver the app registers is usable only in scopes whose policy names
  it. That policy must also give the resolver an allow-list and must require pinned references.
- **Which scope judges an include:** the scope of the file that wrote it. Nested includes (`cosmiconfig.Preset`) are
  judged by the scope of the root file, so an include never gains trust by being reached through another file.
- **Explicit config files** ([Explicit config files](#explicit-config-files)) are judged by the policy of the scope
  whose name they take: `project` in the mise layouts. With no scope marked `ReplacedByConfigFiles()`, the default
  policy judges them (DL-190).
- **Bootstrap files** get their scope's permission check and trust requirement. One that fails either is skipped with a
  warning, so an untrusted repository's bootstrap file can't choose the env (DL-190).
- **Only the app sets policies, in code.** Bootstrap files and env vars can't, as with
  [Choosing scopes](#choosing-scopes).
- **`Describe()`** shows each scope's effective policy (`ScopeInfo.Policy`). A rejected reference fails the load, and
  the failed result's report lists it as a `LayerFailed` layer whose `Reason` names the scope whose policy rejected it
  and the rule, for example "rejected by the project policy: outside the confinement roots" (DL-194).

**Preset defaults:**

| Preset              | Default policy (all scopes)                                                         |
| ------------------- | ----------------------------------------------------------------------------------- |
| No preset (engine default) | Every registered resolver, everything allowed, no confinement, no permission check (DL-189). |
| `mise.Preset`        | Every registered resolver, everything allowed, no confinement, no permission check.  |
| `misehardened.Preset`  | `file` only, `Confine("{config_root}", "{user}", "{system}")`, `FilePermissions(StrictModes)`. |
| `cosmiconfig.Preset` | `file` only, no confinement, no permission check.                                   |

### Caching

Remote files are cached under `{user cache dir}/{name}/includes`:

| Reference                                                  | Cache behaviour                                         |
| ---------------------------------------------------------- | ------------------------------------------------------- |
| [Pinned](#remote-references) (full git commit id, OCI digest, https integrity fragment) | Cached forever; never fetched again. |
| Unpinned (branch, tag, plain URL)                          | Fetched again once the TTL (default 1 hour) has passed. |

**How entries are stored** (DL-206):

- **Content** is stored under the SHA-256 of the content itself, for every scheme. A commit id can't be the key, because
  two files in one commit share it. The fetched identity (the commit id, the OCI manifest digest, or the SHA-256 of http
  content) is report metadata.
- **An index** maps each reference to its entry: the identity, the content's SHA-256, the media type, when it was
  fetched, and whether it's pinned. It's keyed by the SHA-256 of the normalised reference string, so no part of a URL or
  path ever becomes part of a file name. The index is what lets the TTL be checked before fetching, and a failed fetch
  fall back to a stale copy.
- **The real filesystem:** the cache uses the OS filesystem through `os.Root`, never the
  [filesystem hook](#filesystem-hook), which is read-only. Directories are created 0700 and files 0600, and every file
  is written by atomic rename.
- **A cache that can't be used,** because it can't be opened, read or written, is skipped with a `WarnIncludeCache`
  warning, and the load fetches. Content that doesn't match its recorded SHA-256 counts as missing.
- **No eviction yet:** each file is at most the include size limit. Pruning is a
  [future idea](../planning/future-ideas.md).

**Freshness** (DL-207):

- **The TTL** runs from the last successful fetch, by the loader's clock. Pinned entries are never fetched again. A TTL
  of 0 always fetches, so the cache is then only a fallback.
- **Revalidation** (DL-229): the index entry also stores the fetch's validator. Once the TTL has passed, the loader
  passes it to the resolver, and `ErrNotModified` restarts the TTL and uses the cached copy, shown as `revalidated` in
  the report. A `*FetchError` while revalidating falls back to the stale copy, as for any fetch.
- **Fetch failures:** only a `*FetchError` (a network or server failure, or a timeout) falls back to a cached copy,
  which is used with a `WarnStaleInclude` warning on the layer. A 404, a policy rejection or a bad reference fails the
  load, because they mean the config has changed. With no cached copy, the `*FetchError` fails the load.
- **Rechecked on use:** allow-lists and pinning are checked before the lookup, as for any reference, and cached content
  is checked again for the size limit, the integrity fragment and the format. Included files never consult the trust
  store (DL-184), cached or not.
- **Options:** `WithIncludeCache(dir, ttl)` sets the cache dir (`""` keeps the default) and the TTL. A relative dir or a
  negative TTL is an `*OptionError`. `WithoutIncludeCache()` turns caching off, and with it the stale fallback.
- **User cache dir:** from the host: `$XDG_CACHE_HOME` on Linux, else the host's equivalent of Go's `os.UserCacheDir()`.
  Tests set `WithIncludeCache(t.TempDir(), ttl)` or `WithoutIncludeCache()`.

### `misehardened.Preset`

```go
// core: the layout (no dependencies)
var MiseHardenedLayout = MiseLayout.Extend(
	cascadingconfig.WithoutRemoteIncludes(),
	// … plus the permission, confinement and pinning rules below
)

// package presets/misehardened (the layout plus TOML, YAML and fsnotify; no remote resolvers)
var Preset = cascadingconfig.MiseHardenedLayout.Extend( /* decoders, notifier */ )
```

`misehardened.Preset` is `cascadingconfig.MiseHardenedLayout` with TOML, YAML and fsnotify added. The layout itself is
`MiseLayout` with the changes below.

- **Remotes off:** no remote resolvers are registered. A remote reference is a **load error** (a 501), not a silent
  skip, so a config that depends on a remote base fails loudly.
- **Turning remotes back on:** an app can register a resolver again, but only per scope, through a policy that names it,
  gives it an allow-list, and requires [pinned references](#remote-references): a full git commit id, an OCI digest, or
  an https integrity fragment, as in mise's paranoid mode. Unpinned references are a load error (see
  [Per-scope policies](#per-scope-policies)). `WithoutRemoteIncludes()` enforces this (DL-205): it removes every remote
  resolver registered before it, and marks the loader hardened, so a policy that allows a remote scheme without an
  allow-list (global or `AllowFor`) and `RequirePinned()` is an `*OptionError` at `New`.
- **File permissions (Unix):** the [filesystem hook](#filesystem-hook) ignores config files and directories, with a
  warning, if they're writable by group or other, or owned by someone other than the current user or root. This is like
  SSH's `StrictModes`, and the check is exported as `StrictModes`. The warning names the file and the reason, for
  example "`/srv/app/myapp.toml` ignored: writable by group". Ignored files appear in `Describe()`. Windows ACLs aren't
  checked yet.
- **Local include confinement:** the `file` resolver's `Confine("{config_root}", "{user}", "{system}")` option. A local
  include that resolves elsewhere, after following symlinks, is a load error (see [Local files](#local-files)).
- **Scopes:** all four are on by default, and the app narrows them with `WithScopes(...)` (see
  [Choosing scopes](#choosing-scopes)).

## Filesystem hook

Every read goes through one filesystem: the main search, the bootstrap search and the `file` resolver. The default reads
the OS filesystem. An app or a test can supply its own with `WithFileSystem(fsys)`.

- **`fs.FS` compatibility:** `MountFS(dir, fsys)` mounts any `fs.FS` (such as `fstest.MapFS` or `embed.FS`) at the
  absolute directory `dir`, so a test can run the whole search against an in-memory tree.
- **Symlinks are followed.** A candidate that's a symlink is resolved, and the target is opened from its own directory,
  as a new root. A dotfiles setup such as `~/.config/{name} -> ~/dotfiles/{name}` therefore works, even though `os.Root`
  refuses links that escape it. Dedup ([3.1](config-locations.md#31-tiers)) and `Confine` apply to the **target**.
- **Regular files only.** A FIFO, device, socket or directory where a config file is expected is skipped with a warning.
  Opening a FIFO would block forever, so a cloned repo could otherwise hang the loader. A broken symlink or a symlink
  loop is skipped with a warning too. None of them is ever opened (DL-110).
- **Unreadable files:** a searched file that can't be read, for example because of its permissions or an I/O error, is
  skipped with a warning and the status `LayerUnreadable` (D16). An explicit file that can't be read is an `*IOError`.
- **Missing directories** load nothing, without a warning. So does a path that exists but isn't a directory, such as a
  binary named after the app in the project directory, or a single-file `~/.{name}` where a pattern looks inside a
  `.{name}` directory. It's logged at Debug (DL-262).
- **Empty files** are kept in the layer list with the status `LayerEmpty`, and merge nothing.
- **Errors:** an oversized file and a parse error fail the load. Every broken file is reported, joined, and the others
  are still read.
- **A file reached twice,** by identity, is loaded once at its highest position, with the scope and trust of its most
  trusted route (DL-110).
- **Size limit:** a local file larger than the limit is a `*TooLargeError` (413). The default is 10 MiB, and
  `WithMaxFileSize(n)` changes it. Remote includes have their own limit ([Traversal protection](#traversal-protection)).
- **Name matching is case-sensitive,** and done against the directory listing rather than by opening each candidate
  path. On case-insensitive filesystems (macOS, Windows), `MyApp.TOML` therefore doesn't match `myapp.toml`, the same as
  in `cctest.FS` and on Linux.
- **Duplicates are detected by file identity** (`os.SameFile`, or the filesystem's equivalent), not by path string, so
  hard links and differently spelled paths count as one file.
- **Permission policy** (DL-191): `FilePermissions(f)` takes a function that gets a `FileCheck`: the path, its
  `fs.FileInfo`, whether it's the containing directory, the scope, the current user's uid and the host's `GOOS`. It's
  called for **every** file read, whether searched, explicit, included or a bootstrap file, and for its containing
  directory; for a symlink, the target's directory. Higher ancestors aren't checked. The owner is read through an
  `Owner() (uid int, ok bool)` method on the file info, so `cctest.FS` can fake it and platforms without owners report
  `ok == false`. The function returns `PermissionAllow`, `PermissionIgnore` or `PermissionError`, with a reason:
  - **Ignore** skips a searched file as `LayerIgnored`, with a `WarnFilePermissions` warning and the reason. A file
    named directly, an include or an explicit file, is a `*PolicyError` (403) instead, because it's never silently
    skipped (DL-181).
  - **Error** is a `*PolicyError` (403).

  It can be set per scope (see [Per-scope policies](#per-scope-policies)). For example, an app can require `system`
  files to be owned by root and `project` files to be owned by the user.
- **`StrictModes`** is the built-in check, as in SSH: it ignores a file or directory that is writable by group or other,
  except a directory with the sticky bit, or that is owned by neither the current user nor root. A file whose owner
  isn't known is allowed, and so is everything on Windows, where ACLs aren't checked yet.

## Trust store

mise's `mise trust` means that a project's config is approved once and remembered after that. We support the same idea
as a per-scope policy option, not as a separate hook:

```go
type TrustStore interface {
	// Trusted reports whether path (symlinks resolved) has been approved.
	// digest is the file's content hash, so a store can require re-approval
	// after the file changes.
	Trusted(ctx context.Context, path, digest string) (bool, error)
}
```

A scope's policy then requires it:

<!-- example -->
```go
cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
	cascadingconfig.InheritDefault(),
	cascadingconfig.RequireTrust(store),
)
```

- **Untrusted files** are skipped with a `WarnUntrusted` warning, and listed in `Describe()` as `LayerUntrusted`. An
  untrusted explicit file is a `*PolicyError` (403) instead, because a file named directly is never silently skipped
  (DL-181). The loader never prompts the user; asking for approval and recording it is the app's job, through its store.
- **Which files are checked** (DL-192): in a scope with `RequireTrust`, its searched, explicit and bootstrap files.
  Included files take their includer's verdict, and the store isn't asked about them.
- **When:** after the size and permission checks, and before decoding. An untrusted file is never decoded, so one that
  doesn't parse doesn't fail the load.
- **Store errors** fail the load, as a `*BaseError` (500) whose `Op` is `"trust store"`.
- **No store is shipped.** We don't write files (see [Non-goals](#non-goals)), so a persistent store is the app's
  concern. A `TrustPaths(globs...)` store, equivalent to mise's `trusted_config_paths`, is provided because it needs no
  writes. Its entries follow the same rules as the [`Glob` matcher](#allow-lists), applied to absolute slash paths after
  resolving symlinks, with `~` expanded. An entry without wildcards trusts that path and everything beneath it, as
  `trusted_config_paths` trusts whole trees ([3.12](config-locations.md#312-trust)). A store has no host to ask, so `~`
  is the process's home directory, not `cctest.Host`'s.
- **The digest** is the lowercase hex SHA-256 of the file's raw bytes, before decoding or any byte-order-mark stripping.
  So an app can compute it with `sha256sum` and pre-populate its store.
- **Defaults:** no preset requires trust. `mise.Preset` trusts every file, as D18 decided. `misehardened.Preset` relies
  on its other protections, and apps add `RequireTrust` where they want approval.

## Environment variables

These are `mise.Preset`'s bindings. `misehardened.Preset` uses the same ones. `{NAME}` is the app name in upper case,
with every character outside `[A-Z0-9]` replaced by `_`.

| Variable                          | Sets                                                                 | Value                |
| --------------------------------- | -------------------------------------------------------------------- | -------------------- |
| `{NAME}_CONFIG_FILE`              | Explicit project files; replaces the project scope ([Explicit config files](#explicit-config-files)) | path list |
| `{NAME}_CONFIG_DIR`               | User dir (default `$XDG_CONFIG_HOME/{name}`, else `~/.config/{name}`) | path                |
| `{NAME}_GLOBAL_CONFIG_FILE`       | One file as the whole user scope; disables the override scope        | path                 |
| `{NAME}_SYSTEM_CONFIG_DIR`        | System dir ([Platform directories](#platform-directories))          | path                 |
| `{NAME}_SYSTEM_CONFIG_FILE`       | One file as the whole system scope                                   | path                 |
| `{NAME}_ENV`                      | Selected environments, later wins                                   | comma list           |
| `{NAME}_CEILING_PATHS`            | Extra exclusive walk ceilings                                       | path list            |
| `{NAME}_IGNORED_CONFIG_PATHS`     | Paths never loaded                                                  | path list            |
| `{NAME}_OVERRIDE_CONFIG_FILENAMES` | Replaces Layer A's names and removes Layer C, as in mise             | comma list           |
| `{NAME}_CONFIG_FORMATS`           | Format precedence ([File format precedence](#file-format-precedence)) | comma list         |

- **List separators:** a "path list" uses the OS separator (`:`, or `;` on Windows), from the host, so a fake Windows
  host uses `;`. Empty entries are dropped. A "comma list" uses `,`, with surrounding spaces trimmed (DL-123).
- **Combined with options:** `{NAME}_CEILING_PATHS` and `{NAME}_IGNORED_CONFIG_PATHS` add to `WithCeilingPaths` and
  `WithIgnoredPaths`, because both only add ceilings or ignored paths. Every other binding is overridden by its option.
- **Empty values** count as unset.
- **Relative paths** are resolved against the current directory, and a leading `~` is expanded.
- **Ignored paths** match a file, or every file below a directory, after resolving symlinks. They aren't globs. A
  matching candidate is listed as `LayerIgnored` with a reason, but no warning, because the user asked for it. Explicit
  files are never ignored (DL-127).
- **Override filenames** replace the shared patterns of every `Walk` scope with the listed names, in order, and remove
  its local patterns; env and env-local patterns stay (DL-125).
- **Override filenames** are full names, and may use `{ext}`. For example, `custom.{ext}` matches `custom.toml` and
  `custom.yaml`.
- **Single-file variables** (`{NAME}_GLOBAL_CONFIG_FILE`, `{NAME}_SYSTEM_CONFIG_FILE`) behave like
  [explicit config files](#explicit-config-files): a missing file is a `*NotFoundError` (404), and no `.local` or
  `.{env}` variants are added.
- **`XDG_CONFIG_HOME`** is read as the base of the default user dir. It isn't a binding of ours.
- **Keys** (DL-98) are of type `Binding`, a string type whose constants hold the suffix: `BindEnv` is `"ENV"` and
  `BindConfigFormats` is `"CONFIG_FORMATS"`. Untyped constants convert, so `WithoutEnvBinding("ENV")` also compiles, and
  a custom scope's key is any other suffix, such as `"MOUNTED_DIR"`.
- **Declaring and renaming:** a layout declares the bindings it supports. `WithEnvBinding(key, name)` declares a binding
  or renames it, and an empty name means the default `{NAME}_<suffix>`. For example,
  `WithEnvBinding(BindEnv, "MYAPP_CONFIG_ENV")` avoids a clash with an app that already uses `MYAPP_ENV` for something
  else.
- **Turning off:** any binding can be turned off, for example `WithoutEnvBinding(BindEnv)`. Turning off a built-in
  binding that isn't declared does nothing; an unknown key is an `*OptionError`, which catches typos.
- **Checks:** a variable name must match `[A-Za-z_][A-Za-z0-9_]*`, and two bindings can't share one variable. Either
  mistake is an `*OptionError`.
- **`CosmiconfigLayout`** declares only `{NAME}_CONFIG_FILE` and `{NAME}_CONFIG_FORMATS`, because cosmiconfig itself has
  no env vars.
- **`Describe()`** lists each binding, its variable name, and whether it is set.

## Explicit config files

Like a CLI's `--config` flag, the app or the user can name files directly:

<!-- example -->
```go
cascadingconfig.WithConfigFiles(flagValues...) // usually fed from a --config flag
```

or with `{NAME}_CONFIG_FILE=/path/a.toml:/path/b.yaml`.

- **What it replaces (`mise.Preset`):** the project scope only. The directory walk is skipped, but the system, user and
  override scopes still apply. Generally, the files replace the scope marked `ReplacedByConfigFiles()`, at its position,
  even when it isn't selected, and take `TrustProject` and that scope's name. With no marked scope, they go above every
  scope (DL-126). An explicit file's config root is its scope's `ConfigRoot`, if it has one, and otherwise the file's
  own directory.
- **Errors:** a missing file is a `*NotFoundError` (404), an unregistered format an `*UnsupportedFormatError` (415), and
  a file that can't be read, or a directory, an `*IOError`.
- **What it replaces (`cosmiconfig.Preset`):** everything. This matches cosmiconfig's `load(path)`, which reads only
  that file.
- **No variants:** the files are loaded as given. `.local` and `.{env}` siblings aren't added.
- **Order:** with several files, they are applied in the order given, and the later file wins.
- **Empty entries are dropped,** so `WithConfigFiles("")`, from an empty flag, means *not set* (DL-99).
- **Format** comes from each file's extension. Explicit files accept **any registered decoder**, whatever the format
  order, which only affects searching. So `--config x.toml` loads even if `{NAME}_CONFIG_FORMATS` leaves TOML out. A
  format with no registered decoder is an `*UnsupportedFormatError` (415). Nothing has to be registered just in case.
- **Missing files:** a file that doesn't exist is an error, as with cosmiconfig's `load(path)`.
- **Includes** in the explicit files work as usual.
- **Precedence of the setting:** `WithConfigFiles` beats `{NAME}_CONFIG_FILE`, because the option normally carries a
  command-line flag, which should beat the environment.
- **Bootstrap files** are still searched as usual: from the start dir in the mise presets, and in the current directory
  only in `cosmiconfig.Preset`, as cosmiconfig's meta config is.

## Platform directories

| Scope    | Linux / macOS                                     | Windows                                                                  |
| -------- | ------------------------------------------------- | ------------------------------------------------------------------------ |
| `system` | `/etc/{name}`                                     | `%ProgramData%\{name}`, falling back to `C:\ProgramData\{name}` if the variable is unset |
| `user`   | `$XDG_CONFIG_HOME/{name}`, else `~/.config/{name}` | the same: `%XDG_CONFIG_HOME%\{name}`, else `%USERPROFILE%\.config\{name}` |

The user dir follows mise on every OS (D4) in `mise.Preset` and `misehardened.Preset`. Only the system dir differs on
Windows.

- **Setting them** (DL-112): `WithPlatformDirs(XDGDirs)` gives the table above. A scope uses them through the `{user}`
  and `{system}` placeholders in `FixedDir`. Using a placeholder without `WithPlatformDirs` is an `*OptionError`.
- **Overrides:** `{NAME}_CONFIG_DIR` and `{NAME}_SYSTEM_CONFIG_DIR`, when declared, replace `{user}` and `{system}`. A
  relative value resolves against the current directory, and a leading `~` is expanded.
- **No home directory:** if the user dir can't be worked out, scopes that use it are skipped with a `WarnNoHome`
  warning.

`cosmiconfig.Preset` uses cosmiconfig's `env-paths` locations for its user dir instead
([2.3](config-locations.md#23-global-config-directory)), with cosmiconfig's global filenames (`config`, `config.{ext}`):

| OS      | `cosmiconfig.Preset` user dir                      |
| ------- | -------------------------------------------------- |
| Linux   | `$XDG_CONFIG_HOME/{name}`, else `~/.config/{name}` |
| macOS   | `~/Library/Preferences/{name}`                     |
| Windows | `%APPDATA%\{name}\Config`                          |

## Value overrides (env vars and flags)

Single config values can be overridden from environment variables or command-line flags. This is a pair of opt-in
**custom scope kinds**, not a built-in scope. No preset includes them, because how variable names map to keys depends on
the app's schema.

<!-- example -->
```go
mise.Preset.Extend(
	cascadingconfig.WithScope("env",
		cascadingconfig.EnvVars("MYAPP_"), // MYAPP_SERVER__PORT=8080 → server.port = "8080"
		cascadingconfig.After(cascadingconfig.ScopeOverride),
	),
	cascadingconfig.WithScope("flags",
		cascadingconfig.Values(flagTree), // the app's parsed flags, as a tree
		cascadingconfig.After("env"),
	),
	cascadingconfig.WithScopes(cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser, cascadingconfig.ScopeProject,
		cascadingconfig.ScopeOverride, "env", "flags"),
)
```

**`EnvVars(prefix, opts...)`:**

- **Default mapping:** the prefix is stripped, the name is lowercased, and `__` separates nesting levels. So
  `MYAPP_SERVER__PORT` gives `server.port`, and `MYAPP_LOG_LEVEL` gives `log_level`.
- **Why `__`:** a single `_` is too common inside key names to separate levels.
- **Custom mapping:** `MapEnv(func(name, value string) (Path, any, bool))` replaces the default, for example to parse
  lists or numbers. Without it, every value is a **string**, and any type conversion happens when the result is decoded.
  `MapEnv` is an `EnvVarsOption`, a kind of its own (DL-113). Returning `false` skips the variable. A value outside the
  canonical types is a bug in the app, a code-500 `*BaseError`.
- **Edge cases** (DL-113): a name with an empty segment, such as `MYAPP_A____B`, is skipped with a `WarnEnvVarSkipped`
  warning. Variables apply in name order, so when two clash, such as `MYAPP_A` and `MYAPP_A__B`, the later name wins,
  with a `WarnEnvVarClash` warning. The prefix is matched case-sensitively, except on Windows. An empty prefix is an
  `*OptionError`.
- **Discovery bindings are excluded:** variables that are bound to discovery knobs, such as `MYAPP_CONFIG_DIR` or
  `MYAPP_ENV` (see [Environment variables](#environment-variables)), are never read as values, even though they share
  the prefix.
- **Read on every load:** variables are read each time a load or [reload](#hot-reload) runs. A change to them doesn't
  *trigger* a reload, because env vars can't be watched; only a manual `Reload` picks one up straight away.

**`Values(tree)`** takes a ready-made tree, typically built from the app's parsed flags. It may also be a function that
returns a tree, so a reload can re-read it: `ValuesFunc(f)` calls `f` on every load (DL-113). An error from `f`, or a
value outside the canonical types, is a code-500 `*BaseError` whose `Op` is `ValuesFunc`.

- **Copied when called** (DL-103), so changing the tree afterwards has no effect.
- **Canonical values only:** a value outside the [canonical types](api.md#tree-and-values), such as an `int` from flag
  parsing, is an `*OptionError` at `New` naming the key.

**Rules for both kinds:**

- **Placement:** by convention they sit above `override`, with flags above env vars, matching the usual "flag > env >
  file" order. Placement is up to the app, like any [custom scope](#custom-scopes).
- **Reserved keys aren't interpreted:** a value that maps to the include key or `_unset` is ignored, with a warning. A
  value scope can't pull in files or remove keys.
- **Trust:** both kinds default to `TrustUser`, so no `Trust(...)` is needed. They have no bootstrap files and no
  includes, so trust only shows up in `Describe()`.
- **Provenance:** `Describe()` attributes each value to its source, for example "`server.port` set by
  `MYAPP_SERVER__PORT`". An `EnvVars` scope is one layer, and `Origin.EnvVar` names the variable behind each key
  (DL-152).
- **No keys, no content** (DL-264): a value layer that supplies no keys, such as an `EnvVars` scope with no matching
  variable, has the status `LayerEmpty`, as do the defaults scope and a custom scope's tree when they're empty. Its
  warnings stay on it, and it never counts for `RequireConfig`. A file is empty when it holds no document, so a file
  holding `{}` is `LayerLoaded`; a value scope has no document, so no keys is empty.

## Transform hooks

Apps can rewrite config trees with transform hooks, for example to expand templates such as `{{ config_root }}` or
`${HOME}`. **No template language is built in.**

<!-- example -->
```go
cascadingconfig.WithLayerTransform(func(
	ctx context.Context, l cascadingconfig.Layer, tree cascadingconfig.Tree,
) (cascadingconfig.Tree, error) {
	return expandVars(tree, l.ConfigRoot) // the app's own expansion, e.g. os.Expand
})
cascadingconfig.WithResultTransform(func(ctx context.Context, tree cascadingconfig.Tree) (cascadingconfig.Tree, error) {
	return tree, validate(tree)
})
```

- **`WithLayerTransform`:**
  - **When:** runs on **each layer** after it is decoded and its keyed table taken, and before its include key is read
    and `_unset` applied ([Includes](#includes)).
  - **What it gets:** the layer's metadata (path, scope, config root, format, env). That gives it each file's own config
    root, which a transform after merging couldn't know.
  - **Coverage** (DL-193): every layer that merges, including included layers, value scopes and defaults. Never
    bootstrap files, which have `WithBootstrapTransform`, and never layers that are duplicates, ignored, untrusted,
    empty or not chosen by a first-match search (DL-76).
- **`WithResultTransform`** runs once on the merged result, before the `RequireConfig` checks and before `Decode` checks
  for unknown keys. It is the counterpart of cosmiconfig's `transform` option. `OriginOf` describes the merge, not the
  transform, so a key that a result transform adds has no origin.
- **Order:** several transforms run in registration order.
- **Return values:** `nil` from a layer transform merges nothing, and the layer stays `LayerLoaded`. `nil` from a result
  transform is an empty result. A returned tree must hold only canonical values, as a decoder's must.
- **Errors:** an error, or a tree with a non-canonical value, is a `*TransformError` (422). For a layer transform, its
  `Path` is the file; a result transform's has no `Path`.
- **Purity:** transforms must be deterministic. They run again on every [reload](#hot-reload), and reload decides
  whether anything changed by comparing results.

## Hot reload

### Prior art

| Library | What it watches                       | Symlinks (k8s ConfigMap)            | Debounce                  | On parse error                         | Concurrency                     |
| ------- | ------------------------------------- | ----------------------------------- | ------------------------- | -------------------------------------- | ------------------------------- |
| Viper   | Parent dir of **one** file            | Re-resolves the target on every event | None                      | Logs; keeps the old config; still calls the callback | Not safe; can `os.Exit` from the library |
| koanf   | Parent dir of **one** file, per provider | Re-resolves the target on every event | Drops duplicates < 5 ms   | Error passed to the callback; watching stops | Left to the app                 |

**fsnotify** is the de-facto Go watcher. Its main constraints:

- **No recursive watching:** each directory needs its own watch.
- **No polling backend,** and **no events at all** on NFS, SMB, FUSE, `/proc` or `/sys`.
- **Atomic saves:** editors save by writing a temporary file and renaming it over the original, which drops a watch on
  the file itself. So watch the **parent directory** and filter by name.
- **Chmod noise:** Spotlight, anti-virus and backup tools generate it, so ignore Chmod events.
- **Limits:** on Linux, inotify watches are capped by `max_user_watches` and `max_user_instances`. On macOS, kqueue
  needs one file descriptor per watched directory.

Our case is harder than either library's: discovery spans many directories and many candidate files that
**don't exist yet**. For example, creating `myapp.local.toml` must trigger a reload.

### Design

**API** (decided in [api.md](api.md#load-and-watch)):

<!-- example -->
```go
w, err := cascadingconfig.Watch[Config](ctx, loader, func(c cascadingconfig.Change[Config]) {
	// c.Old, c.New: decoded snapshots; c.Diff: changed keys with provenance; c.Err: reload failure
}, cascadingconfig.WithDebounce(100*time.Millisecond, time.Second))
defer w.Close()
cfg := w.Current() // lock-free read of the latest good snapshot
```

**Watch set** (DL-224):

- **Directories, not files.** A watched load records every directory it lists or tries to list: each walk directory,
  each pattern's directory within it (such as `.config/{name}/` and `{name}/conf.d/`), glob directories, the user,
  system and platform dirs, and the bootstrap locations. Each directory comes with a filter: the names searched for
  there.
- **Files named directly:** the directory of each explicit config file and of each local include is watched, filtered to
  that file's name.
- **Symlink targets:** for a symlinked file, the directory of its final target is watched too, filtered to the target's
  name, so editing a target somewhere else, such as `~/dotfiles/myapp.toml`, is seen.
- **Missing directories:** for a directory that doesn't exist yet, the loader watches its nearest existing ancestor,
  filtered to the next path segment, however far up that is. A missing `/etc/myapp` watches `/etc` for `myapp`. Once the
  directory appears, the reload re-plans the set.
- **Filtering:** an event is relevant only if its name passes its directory's filter. Notifiers drop Chmod events.
- **Symlink swaps:** for any event in a directory that holds a symlinked candidate, the symlink is resolved again, and a
  changed target counts as a change. This covers Kubernetes ConfigMap `..data` swaps, as Viper and koanf do.
- **Re-planned after every reload,** because a reload can change the ceiling, the selected envs, the formats or the
  folder fragments. A failed reload re-plans from the previous set plus whatever it recorded, so a reload that fails in
  bootstrap still watches everything the last one did.
- **Custom scopes** (DL-239): a custom scope's `Path` sources are watched like explicit files, so edits to them are seen
  without `Watchable`. A scope that implements `Watchable` also gets the directories `WatchDirs` returns watched with no
  name filter, so any event in them reloads; that's how new files appear. `WatchDirs` is called after `Discover` on
  every watched load, with the same `Discovery`, and never by a plain `Load`. A relative directory, or an error, is a
  `WarnWatchFailed` warning, and the rest of the set is planned as usual. A missing directory is watched through its
  nearest existing ancestor.

**Reloading:**

- **Debounced** (DL-228): events are coalesced on the trailing edge, with a 100 ms quiet period and at most 1 s of
  delay, so one save means one reload. Each event restarts the quiet period, but a burst reloads no later than the
  maximum after its first event. `WithDebounce(quiet, maxWait)` changes both; a `quiet` of 0 reloads on every event.
- **A reload works exactly like an initial load:** it decodes into a fresh value, never the previous snapshot, with
  defaults applied the same way ([api.md](api.md#defaults)).
- **Full re-discovery,** not an incremental patch. Bootstrap, discovery, policies (permissions, confinement, trust),
  transforms and merging all run again. Precedence and dedup depend on the whole set of files, and config is small.
- **Last good config wins:** if a reload fails (a parse error, a policy violation or a transform error), `Current()`
  keeps the last good snapshot. The handler gets `Change.Err`, a `WarnReloadFailed` warning is logged, and the next
  event retries. A half-written file therefore never replaces good config. The loader never exits the process.
- **Callbacks only on real change:** the handler runs only when the merged result changes. Touching a file or editing a
  comment doesn't trigger it. The exceptions (DL-222): every failed reload calls it, and so does the first good reload
  after a failure, even with no change, so the app can clear its error state.
- **One at a time** (DL-223): one goroutine runs a reload, then the handler, then the next reload. While the handler
  runs, events keep coalescing, but the next reload waits for it to return. `Current()` is updated before the handler is
  called.
- **Manual reload:** `w.Reload(ctx)` forces one, for example on `SIGHUP`. It also re-reads env vars and `Values`
  functions, which can't be watched. It takes any pending events with it, cancelling the debounce timer.

**What can't be watched by events:**

| Source                                 | How it refreshes                                                                                       |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Remote includes                        | Pinned references never change. Unpinned ones are re-checked in the background only with `WithRemotePoll(d, matchers...)`, by a conditional fetch: an ETag, `git ls-remote` or the OCI manifest digest (DL-229, DL-230). Otherwise they refresh on reloads, under the [cache rules](#caching). |
| NFS, SMB and FUSE mounts; watcher limits reached | Built-in stat polling, every 2 s by default (DL-226). `WithPolling()` polls everything, and any directory whose watch fails is polled automatically, with a `WarnWatchFailed` warning. |
| Env vars, `Values`                     | Re-read on every reload, but changes to them never trigger one. Use a manual `Reload`.                  |
| Embedded files, `embed.FS`             | Never; they're static.                                                                                  |
| Custom `Scope` implementations         | `Path` sources are watched as files. New sources need the optional `Watchable` interface; without it, they appear only on a reload. `Tree` sources refresh only on a reload. |

**Polling** (DL-226):

- **What's compared:** at every interval, each polled directory is listed through the
  [filesystem hook](#filesystem-hook), and each entry its filter matches is compared with the previous scan: type, size,
  mode, modification time and identity (`FileSystem.SameFile`). Files that were loaded are also hashed with SHA-256,
  which catches a same-size edit within the modification time's granularity. They're small, because `WithMaxFileSize`
  caps them. The first scan is the baseline.
- **Interval:** `WithPollInterval(d)` sets it for all polling, 2 s by default.
- **Without a notifier,** everything is polled. That's the documented behaviour, so it's logged at debug level, not
  warned.

**Remote polling** (DL-230):

- **Per source:** `WithRemotePoll(d, matchers...)` polls the unpinned remotes the matchers match, or every unpinned
  remote with no matchers. The matchers are the allow-list ones, and see the normalised reference, so `Prefix("git::")`
  selects every git include and `Prefix("oci::ghcr.io/")` one registry. When rules overlap, the most recently given
  wins. A `d` of 0 means "don't poll these". Remote polling is off unless a rule turns it on.
- **A poll** revalidates each matched remote the last good load used, ignoring the cache TTL. Changed content is stored
  in the cache, and a reload is queued through the debounce. If nothing changed, nothing else happens. Without the cache
  there's nowhere to keep validators, so each poll is a full reload instead.
- **Gentle on servers:** each reference's next poll is jittered within ±10% of its interval, so instances started
  together don't poll in step. A failed poll (a `*FetchError`, such as a 429 or 503) doubles that reference's interval,
  up to 8 times the configured one, and the next success resets it. A reference used by several files is checked once
  per poll.
- **File events** still don't contact servers within the cache TTL.

**Event sources** (DL-225):

- **No fsnotify in the core.** `WithNotifier(func() (Notifier, error))` takes a factory, called once per watcher,
  because a notifier's events have one reader. `Close` closes that watcher's notifier. A factory error makes everything
  poll, with a warning, and `Watch` still succeeds.
- **The contract:** `Events` carries the absolute path, in the `FileSystem`'s namespace, of any created, written,
  removed or renamed entry. Chmod-only events are dropped by the notifier. Every error on `Errors` is a warning plus a
  reload, because events may have been lost. A closed `Events` channel makes the whole set poll, with a warning. A
  failed `Add` makes that one directory poll, with a warning.
- **`watch/fsnotify`** (DL-232) supplies `fsnotify.New`, which the ready-to-use presets register.
- **Tests** use `cctest.Notifier` and `cctest.Clock` (DL-231), so reload behaviour can be tested without real files or
  timers.

**Warnings** (DL-227): `WarnWatchFailed` (`"watch-failed"`) when a directory or the notifier falls back to polling, or
the notifier reports an error, and `WarnReloadFailed` (`"reload-failed"`) for a failed reload. The watcher has no
report, so they go to the logger, with the code as an attribute. A handler that panics is logged at error level.

**Settled by the implementation** (DL-235):

- **Everything goes through the debounce:** a change found by polling, a remote poll that found new content, and a
  notifier error each count as an event.
- **Directories that can't be opened** for any reason but not existing count as existing, so they're watched or polled
  rather than skipped. That's how a directory a watch fails on is polled until it can be read.
- **Glob directory segments** skip hidden directories, as discovery does.
- **Polled directories are scanned again after every reload,** so the next poll compares against what the reload saw.
  Beside a symlinked file, every entry is scanned, so a `..data` swap is seen.
- **A notifier factory that returns nil** counts as one that failed.
- **A remote poll** that gets new content with the same SHA-256 as the cached copy doesn't reload. One that fails with
  any error but a `*FetchError`, or finds no cached copy, reloads, so the reload reports what's wrong.

**Security on reload:** every check runs again. A file that becomes group-writable is ignored on the next reload, with a
warning, and a revoked trust approval takes effect there too.

## Non-goals

- **Writing config files** is out of scope for now. That covers choosing a write target, as mise does for `mise use` or
  `--global`; creating files; and editing them. The loader only reads. mise's write-only settings, such as
  `default_config_filename`, aren't carried over.
- **Network-level policy (SSRF).** The loader has no built-in rules about which addresses remote includes may reach,
  such as loopback, link-local or cloud-metadata ranges. Apps that need them have two hooks:
  - **Allow-list matchers** see the reference as written. A `MatchFunc` can reject hostnames, or IPs written in the URL,
    such as `http://169.254.169.254/`. Redirect targets are checked again, so a redirect to a written-out IP is caught
    too. Matchers run before DNS, so they can't see what a hostname resolves to.
  - **A custom HTTP client:** `httpresolver.New(httpresolver.WithClient(c))` builds the `https` resolver on the app's
    own `*http.Client`. Its dialer (`net.Dialer.Control`) can check each resolved IP at connect time, which also covers
    DNS rebinding.
- **JSONC** (JSON with comments and trailing commas) is out of scope for now. It's listed in
  [future-ideas.md](../planning/future-ideas.md).
- **Combined-environment files** such as `myapp.dev.linux.toml`, which would load only when every environment they name
  is active, are not planned. They may be considered later, or never. Adding them would have to be **opt-in**, because
  today `dev.linux` is an ordinary single environment name, as in mise.

---

## Decision log

Entries 42 to 80 came from the [planning review](decisions/0001-planning-review.md), which records each finding and why
it was decided as it was.

There are no open questions.

**Decided:**

| # | Question                                      | Decision                                                                                                      |
| - | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| 1 | Presets                                       | Ship `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset`, and let users define their own, including by extending a preset (see [Decisions](#overview)). |
| 2 | Default preset                                | `mise.Preset`, with the [deviations](#deviations-from-mise) listed above.                                      |
| 3 | Same name in several formats                  | Merge them all, with TOML applied last by default (see [File format precedence](#file-format-precedence)).     |
| 4 | Overriding the format order                   | `{NAME}_CONFIG_FORMATS`, which beats the bootstrap `formats` key, which beats the app's setting, which beats the preset. |
| 5 | Default walk limit                            | Stop after `~`, or at `/` when outside `~` (see [Directory walk](#directory-walk)).                            |
| 6 | Env-var bindings                              | On by default, each one opt-out-able, and all of them visible through `Describe()`.                           |
| 7 | Bootstrap file name                           | Each preset names its own (see [Bootstrap file](#bootstrap-file)).                                           |
| 8 | Arrays when merging                           | Replaced by the higher layer. Concatenation is opt-in (see [Merging](#merging)).                              |
| 9 | [Divergence](config-locations.md#4-divergences) defaults (D1–D18) | Confirmed as written.                                                                                       |
| 10 | Schema-specific merge behaviour              | Per-key merge rules, set by the app only (see [Per-key merge rules](#per-key-merge-rules)).                   |
| 11 | Unsetting keys                               | Supported with the reserved `_unset` key (renameable). YAML/JSON `null` is a real value that replaces lower values when merging (see [Unsetting keys](#unsetting-keys), [Null values](#null-values-yaml-and-json)). |
| 12 | Include key and remote includes              | Each preset names its key. Remotes are fetched through resolver hooks: `http(s)`, `git` and `oci` are built in, with allow-lists of matchers or matcher funcs (see [Includes](#includes)). |
| 13 | Hardened preset                              | `MiseHardenedLayout` extends `MiseLayout` and disables remote includes; `misehardened.Preset` adds TOML, YAML and fsnotify (see [`misehardened.Preset`](#misehardenedpreset)). |
| 14 | System dir on Windows                        | `%ProgramData%\{name}` (see [Platform directories](#platform-directories)).                                   |
| 15 | Env-var list                                 | As listed in [Environment variables](#environment-variables). Every binding is renameable.                      |
| 16 | Explicit config files                        | `WithConfigFiles` / `{NAME}_CONFIG_FILE` replace the project scope only in the mise presets, and everything in `cosmiconfig.Preset` (see [Explicit config files](#explicit-config-files)). |
| 17 | Writing config files                         | Out of scope for now (see [Non-goals](#non-goals)).                                                          |
| 18 | Remotes with no allow-list (`mise.Preset`)    | All allowed, as in mise (see [Allow-lists](#allow-lists)).                                                   |
| 19 | Remote caching                               | Pinned references are cached forever, unpinned ones get a 1-hour TTL, and a stale copy is used with a warning if a fetch fails (see [Caching](#caching)). |
| 20 | `misehardened.Preset` hardening                 | Permission check with a warning, local include confinement, pinned remotes if re-enabled (see [`misehardened.Preset`](#misehardenedpreset)). |
| 21 | Choosing scopes                              | Opt-in: each preset has a default set, and the app replaces it with `WithScopes(...)`, in code only. `mise.Preset` and `misehardened.Preset` default to all four (see [Choosing scopes](#choosing-scopes)). |
| 22 | Warnings                                     | Logged through `*slog.Logger` (default `slog.Default()`) and recorded per layer (see [Warnings and logging](#warnings-and-logging)). |
| 23 | Local files                                  | A built-in `file` resolver handles local includes, and one shared filesystem hook carries every read and the permission policy (see [Local files](#local-files), [Filesystem hook](#filesystem-hook)). |
| 24 | Trust per scope                              | All resolver and permission rules can be set per scope. A scope's policy replaces the default as a whole (see [Per-scope policies](#per-scope-policies)). |
| 25 | Custom scopes                                | Supported, built from the existing kinds or the `Scope` interface, placed with `Before`/`After`, project trust by default (see [Custom scopes](#custom-scopes)). |
| 26 | Bootstrap scope limits                       | Keys that hide or skip files act only in their own scope and less-trusted ones; `env` is the exception. `misehardened.Preset` project bootstrap files may only set `env` and `formats` (see [Bootstrap file](#bootstrap-file), [Scope trust](#scope-trust)). |
| 27 | mise migration flags                         | Use mise's end-state behaviour: platform envs and `conf.d` env fragments are always on, and the settings are dropped (see [Deviations from mise](#deviations-from-mise)). |
| 28 | Trust hook                                   | Folded into per-scope policies as `RequireTrust(store)` (see [Trust store](#trust-store)).                      |
| 29 | Scope modifiers                              | Both `WithScopes(...)` (replace the set) and `WithoutScopes(...)` (remove from it), applied in order across `Extend` calls (see [Choosing scopes](#choosing-scopes)). |
| 30 | Environment selection                        | `WithEnvs(...)` > `{NAME}_ENV` > bootstrap `env`. Platform envs rank below selected envs. The order within a directory is shared < env < local < env-local (see [Environments](#environments)). |
| 31 | Several environments in one filename         | Follow mise: names are matched literally (dots allowed) and never combined, and unmatched files are silently not loaded. Combined-environment files may be considered later, opt-in, or never (see [Environments](#environments), [Non-goals](#non-goals)). |
| 32 | Safe environment names                       | Names must give a valid filename and can't escape the searched directory. Path separators, control characters and Windows-reserved characters are rejected, joined paths are checked with `filepath.IsLocal` and opened through `os.Root`, and dots stay valid (see [Safe environment names](#safe-environment-names)). |
| 33 | Traversal in remote resolvers               | The loader normalises and validates every reference before allow-listing, checks every fetched result, resolves relative references against the remote origin, and builds cache paths from hashes. `git` reads blobs without a worktree, `oci` reads tar entries without extracting, and both accept regular files only (see [Traversal protection](#traversal-protection)). |
| 34 | Requests to internal networks (SSRF)       | No built-in network policy. Allow-list matchers cover hostnames and written-out IPs, and a custom `http.Client` on the `https` resolver covers resolved IPs (see [Non-goals](#non-goals)). |
| 35 | Value overrides                              | Opt-in custom scope kinds: `EnvVars(prefix)` (`__` for nesting, discovery bindings excluded) and `Values(tree)` for flags. No preset includes them (see [Value overrides](#value-overrides-env-vars-and-flags)). |
| 36 | Templates                                    | No built-in language. `WithLayerTransform` runs on each layer, with its metadata, before merging; `WithResultTransform` runs on the merged result (see [Transform hooks](#transform-hooks)). |
| 37 | Hot reload: strategy                         | Watch directories where candidates could appear; debounce, then re-run discovery and merging in full; keep the last good snapshot on failure (see [Hot reload](#hot-reload)). |
| 38 | Hot reload: remote includes                  | Background polling is opt-in with `WithRemotePoll(interval)`. Otherwise remotes refresh only on reloads, under the cache rules. |
| 39 | Hot reload: filesystems without events       | Built-in stat polling (2 s default), used with `WithPolling(interval)` and automatically for any directory whose watch fails, with a warning. Network filesystems aren't detected automatically. |
| 40 | Hot reload: callbacks                        | Only when the merged result changes, or a reload fails. `WithReloadOnAnyEvent()` opts in to every reload. Callbacks are serialised. |
| 41 | Hot reload: debounce                         | A 100 ms quiet period and at most 1 s of delay, both configurable.                                                  |
| 42 | `cosmiconfig.Preset` default scopes          | The start dir only, as cosmiconfig's default `none` strategy. The `user` scope and the walk come with `WithSearchStrategy(Global)` (see [Search strategies](#search-strategies)). |
| 43 | Array default                                | `WithArrayMerge(Replace)` by default; `cosmiconfig.Preset` sets `Append`, matching `mergeImportArrays` (see [Merging](#merging)). |
| 44 | Node.js-specific sources                     | Not supported: no `package.json`/`package.yaml`, no `packageProp`, no default `package.json` root marker. Users can add them with `Embedded` and `WithRootMarker` (see [Deviations from cosmiconfig](#deviations-from-cosmiconfig)). |
| 45 | `cosmiconfig.Preset` user dir                | cosmiconfig's `env-paths` locations (see [Platform directories](#platform-directories)).                      |
| 46 | Extensionless files                          | Decoded by the format `WithExtensionlessFormat` names. `cosmiconfig.Preset` uses YAML if registered, otherwise JSON. |
| 47 | YAML extensions                              | `.yaml` and `.yml` are both YAML. The format order lists formats. Within YAML, `.yaml` ranks above `.yml` (see [File format precedence](#file-format-precedence)). |
| 48 | Dates and times                              | Strings in the tree, in RFC 3339 form, for every format. koanf's `TextUnmarshaler` hook parses them into `time.Time` fields (DL-148). This differs from koanf with TOML, which hands `time.Time` through (see [Format dialects](#format-dialects)). |
| 49 | Integers                                     | `int64` only for literals with no fraction or exponent that fit; out of range is a `*ParseError`.             |
| 50 | YAML dialect                                 | YAML 1.2 core schema. Keys become strings. Several documents, and unknown tags, are `*ParseError`s. Alias expansion is capped. |
| 51 | Duplicate keys                               | Each format's own semantics: JSON keeps the last and logs a warning; TOML and YAML raise a `*ParseError`.       |
| 52 | Dialects and encoding                        | Strict JSON (JSONC is a [future idea](../planning/future-ideas.md)); TOML as the pinned go-toml accepts; UTF-8 only, with a leading byte-order mark stripped. |
| 53 | Empty files and the top level                | Empty means whitespace or comments only, or a null document; `{}` is not empty. The top level must be a map.   |
| 54 | Requiring config                             | `RequireConfig(level, scopes...)`, combinable. The default level counts any source; flags narrow it to files and non-empty layers, globally or per scope ([api.md](api.md#requiring-config)). |
| 55 | Pinned references                            | A full git commit id, an OCI digest, or an https `#sha256=` integrity fragment that's verified after download (see [Remote references](#remote-references)). |
| 56 | Matchers                                     | `Glob`: `*` within a segment, `**` across. `Regexp` is fully anchored. The normalised reference string is documented per scheme (see [Allow-lists](#allow-lists)). |
| 57 | Trust-store details                          | `TrustPaths` uses the `Glob` rules on resolved absolute paths. The digest is the hex SHA-256 of the raw bytes (see [Trust store](#trust-store)). |
| 58 | Cache lookup                                 | An index keyed by the SHA-256 of the normalised reference. http identity is the content's SHA-256. `WithoutIncludeCache()` turns caching off. Cached content is rechecked (see [Caching](#caching)). |
| 59 | Isolating git                                | No system or global git config; credential helpers opt-in; git 2.24 or newer; fetching the ref when fetching by commit is refused; `WithProtocols` for tests. |
| 60 | OCI artifact forms                           | Per-file (`oras push`) and tar layers; the last layer wins; decompression is capped; credentials and plain HTTP are opt-in options. |
| 61 | A file reached by two routes                 | Precedence from the higher route; trust, policy and scope from the more trusted route; both recorded (see [Scope trust](#scope-trust)). |
| 62 | Symlinks                                     | Followed. The target is opened from its own directory, and dedup and `Confine` apply to the target (see [Filesystem hook](#filesystem-hook)). |
| 63 | Special files                                | Only regular files are read; others are skipped with a warning.                                              |
| 64 | Local file size                              | A `*TooLargeError` above 10 MiB by default; `WithMaxFileSize` changes it.                                     |
| 65 | Permission check targets                     | The symlink target and its containing directory. The owner comes from an `Owner()` method on the file info.  |
| 66 | Name matching and dedup                      | Case-sensitive, against the directory listing; duplicates by file identity.                                  |
| 67 | Formats for explicit files and includes      | Any registered decoder, whatever the format order. An unregistered format is a `*UnsupportedFormatError`.    |
| 68 | Relative paths                               | Env vars: against the current directory. Bootstrap files: against the file's directory. `~` expanded in both. |
| 69 | Ignored paths, override names, single-file vars | Ignored paths match a file or a directory prefix after symlinks; override names may use `{ext}`; single-file vars behave like explicit files. |
| 70 | Faking the host in tests                     | An internal host (env, home, cwd, GOOS, GOARCH, UID), replaceable only through `cctest.Host` ([api.md](api.md#test-helpers)). |
| 71 | Layout options                               | Every built-in layout setting has a public option ([api.md](api.md#layout-options)); exact signatures at M3's review. |
| 72 | Defaults                                     | Every reload works like an initial load: each decodes into a fresh value. Defaults are a scope below all others, built from a Go value with `WithDefaults(v)`; config can clear them, and `OriginOf` traces them. No `SetDefaults()` method ([api.md](api.md#defaults)). |
| 73 | Empty `With…s` calls                         | `WithEnvs()` and `WithConfigFiles()` with no arguments mean "not set"; empty `WithScopes()` and `WithFormats()` are errors. |
| 74 | Option naming exceptions                     | `WithProbes`, `WithFlatPaths` and `WithUnknownKeys` are single settings despite the *s*.                     |
| 75 | Error types for the remaining cases          | Cancellation keeps `errors.Is` working (codes 499 and 504); `*IOError` (500); `*UnsupportedSchemeError` (501); a forbidden scheme is a `*PolicyError`; malformed reserved keys are a `*ParseError`; callback errors are wrapped. A failed `Load` still returns a `*Result` with its report ([api.md](api.md#errors)). |
| 76 | Concurrency and ownership                    | Callbacks must be safe for concurrent use; callers and callbacks get their own copies of trees; transforms run only on layers that merge ([api.md](api.md#concurrency-and-ownership)). |
| 77 | Watch lifecycle                              | `ctx` bounds the watcher; `Close` is idempotent and safe from the handler; `Reload` returns an error; a change is a value difference; handler panics are recovered ([api.md](api.md#load-and-watch)). |
| 78 | Bootstrap key handling                       | Unknown keys warn; keys outside the limits are dropped with a warning; wrong types are an `*OptionError`; no includes or `_unset` (see [Bootstrap file](#bootstrap-file)). |
| 79 | Include behaviour                            | Top level only, and removed from the result; once per include chain; none from embedded or value sources; trust inherited from the includer (see [Includes](#includes)). |
| 80 | Platform names                               | `unix` on every `GOOS` except windows, plan9, js and wasip1; `darwin` becomes `macos` and `amd64` becomes `x64`; everything else keeps its Go name (see [Environments](#environments)). |
| 81 | How decoders report empty files and warnings | `Decode` returns a `Document` with `Tree`, `Empty` and `Warnings` ([api.md](api.md#extension-interfaces)). |
| 82 | Rules every format shares                    | `DecodeDocument` applies them around any decoder: byte-order mark, UTF-8, `*ParseError` with position, canonical types. A non-canonical type is a decoder bug (500). |
| 83 | Warnings                                     | A `Warning` type with a `WarningCode` (string), file path, key path and message. The full report format is settled in M5. |
| 84 | Building errors in other modules             | Each specific error type defines its own fixed `Code()`; the base `BaseError` has exported fields (it can't be named `Error`, because a field and a method can't share a name); `NewError` builds base errors with a code ([api.md](api.md#errors)). |
| 85 | Path parsing                                 | The empty string is the root; empty segments are errors; segments can be double-quoted as in TOML, and `String()` quotes where needed, so paths round-trip ([api.md](api.md#path)). |
| 86 | Where the merge engine lives                 | In the root package, because it uses the public types. Its tests call one unexported entry point and check results only through `Result.Tree` and `OriginOf`, so they carry over to `Load`. |
| 87 | Writing strategies                           | `MergeStrategy` constants `DeepMerge`, `Replace`, `Append`, `Prepend`; a custom `MergeFunc` has its own option, `WithMergeFunc` (see [Per-key merge rules](#per-key-merge-rules)). |
| 88 | Rule matching                                | Ambiguous means same length, same wildcard count, and some key matches both; the same path again replaces the rule; no root rule; explicit rules can mismatch, the default merge never fails. |
| 89 | Errors from merge functions                  | A `*MergeError` (422) wrapping the app's error, not a 500 callback error; a non-canonical return value is a 500. Functions get copies. |
| 90 | Naming both files in a mismatch              | `*MergeError` has `Key` and `Lower`; `Path` is the higher layer's file. Every mismatch is reported, joined ([api.md](api.md#errors)). |
| 91 | Shape of `Origin`                            | `Layer`, `Kind`, `Elements` and `History`, with kinds including `OriginMerged` and `OriginUnset`; history restarts when an enclosing table is replaced ([api.md](api.md#result-report-and-origin)). |
| 92 | `_unset` edge cases                          | Malformed values and `_unset` inside arrays are `*ParseError`s; a table holding only `_unset` doesn't set its key; a no-op unset leaves no trace (see [Unsetting keys](#unsetting-keys)). |
| 93 | Several files in one slot                    | `FormatConflict` constants `ConflictMerge`, `ConflictFirst`, `ConflictFail` (not `ConflictError`, which is the error type's name); `ConflictFail` fires on a second file, even of the same format; `*ConflictError` lists the files; skipped files warn with `WarnFormatConflict`. |
| 94 | What M3 covers                               | The option machinery, plus only the options whose behaviour can work now. Each later milestone settles its own option signatures at its test review and adds them to the layouts. Until M4a the layouts have no scopes. |
| 95 | JSON in the core layouts                     | The JSON parser moves to `internal/json`, which doesn't import the core. The layouts wrap it in a private decoder, and `format/json` wraps the same parser, so its API and behaviour are unchanged. |
| 96 | Testing options                              | In-package tests read the loader's private config where nothing public shows an effect yet; errors and layouts are tested from outside; misplaced option kinds are checked by building programs under `testdata/compile`. |
| 97 | Creating and naming presets                  | `NewPreset(name, opts...)`; `Extend` keeps the name; `ExtendAs(name, opts...)` names an extension and records its base; a zero `Preset` is no preset ([api.md](api.md#preset-and-extend)). |
| 98 | Binding keys                                 | `type Binding string` holding the suffix; `WithEnvBinding(key, name)` declares or renames, with an empty name meaning the default; unknown keys, bad names and shared variables are errors; `CosmiconfigLayout` declares `CONFIG_FILE` and `CONFIG_FORMATS` (see [Environment variables](#environment-variables)). |
| 99 | Checking names                               | The app name: not empty, `.` or `..`, at most 255 bytes, none of the rejected characters. `WithEnvs` names get the same check at `New`. Empty entries are dropped in `WithEnvs` and `WithConfigFiles`. |
| 100 | Placing custom scopes                       | Anchors must already be defined; `After(x)` and `Before(x)` insert next to `x`; neither means above every scope so far; both, a clash or a duplicate in `WithScopes` is an error (see [Custom scopes](#custom-scopes)). |
| 101 | Formats and decoders as options             | `WithFormats` takes the env var's syntax; duplicates and formats with no decoder are errors, checked after every option; `WithDecoder` replaces a format's decoder; an extension claimed twice is an error. |
| 102 | Merge options                               | Generic `WithMergeRule` and `WithMergeFunc`; bad paths, nil functions and ambiguous rules are errors; `WithArrayMerge` takes `Replace`, `Append` or `Prepend`; `WithUnsetKey("")` is an error, and it shares one setting with `WithoutUnset`. |
| 103 | The `Values` kind                           | Copied when called; non-canonical values are an `*OptionError` naming the key; trust `TrustUser`. `ValuesFunc` and the other kinds come in M4a. |
| 104 | Option errors                               | Each problem is an `*OptionError` whose `Op` is the option's name; all problems from one `New` are joined. |
| 105 | A layout's default format order             | `WithDefaultFormats`, which may name formats with no decoder; they're skipped. `WithFormats` stays strict and replaces it (see [File format precedence](#file-format-precedence)). |
| 106 | Splitting M4                                | Four parts: M4a filesystem and loading; M4b patterns, environments and the walk, with `MiseLayout`'s scopes; M4c formats and bootstrap; M4d `CosmiconfigLayout` ([implementation-plan.md](../planning/implementation-plan.md#m4-discovery)). |
| 107 | Loading before reports                      | `Loader.Load(ctx)` returns a `*Result` with `Tree()` and `OriginOf`. Tests read the private layer list until `Report` arrives in M5. |
| 108 | Paths and fake hosts                        | `cctest.Host.GOOS` picks OS-dependent defaults; paths are joined with the real OS's rules. Owners in `cctest.FS` arrive with the permission policy in M7. |
| 109 | The `cctest` API                            | `NewFS` and its methods, `Host` and `WithHost`, and `Golden` with `-update` ([api.md](api.md#test-helpers)). |
| 110 | Reading rules                               | Unreadable searched files are `LayerUnreadable` with a warning; non-regular files and broken symlinks are skipped unopened; missing dirs are silent; empty files are `LayerEmpty`; a file reached twice loads once, at its highest position, with its most trusted route (see [Filesystem hook](#filesystem-hook)). |
| 111 | `FixedDir` and patterns                     | `FixedDir(dir, patterns...)` with `Pattern{Template}`; `{name}`, `{user}`, `{system}` and `~` in `dir`, `{name}` and `{ext}` in templates; patterns lowest first; later milestones add fields to `Pattern` (see [Custom scopes](#custom-scopes)). |
| 112 | Platform directories                        | `WithPlatformDirs(XDGDirs)` replaces `WithUserDir` and `WithSystemDir`; the dir bindings override it; no home skips the user dir with a warning (see [Platform directories](#platform-directories)). |
| 113 | Value scope kinds                           | `ValuesFunc` runs on every load, with 500s for its errors; `EmbeddedFS` reads an `fs.FS`; `EnvVars` takes `EnvVarsOption`s such as `MapEnv`, and its edge cases are settled; value scopes drop the unset key with a warning (see [Value overrides](#value-overrides-env-vars-and-flags)). |
| 114 | The file-reading check                      | A selected file-reading scope with an empty format order is an `*OptionError`. |
| 115 | Options settled in M4a                      | `WithFileSystem`, `MountFS`, `WithMaxFileSize` and `WithLogger`; a nil filesystem or logger, or a size of zero or less, is an `*OptionError`; warnings are logged at Warn level with their code and file. |
| 116 | Golden tests in M4a                         | They cover `FixedDir` scopes; the worked example moves to M4b. |
| 117 | Pattern fields                              | `Template`, `Variant`, `Root` and `Key`; `{env}` exactly for the env variants; patterns grouped by variant within a directory (see [Patterns](#patterns)). |
| 118 | Globs                                       | `*` within one segment: no dots in a file name, no leading dot in a directory name; alphabetical; one slot per stem. |
| 119 | Config roots                                | `RootSearchDir` and `RootFileDir`, and the `ConfigRoot(dir)` scope option; layers record root, pattern, variant and env. |
| 120 | Keys inside files                           | `Pattern.Key` replaces the `Embedded` kind; a missing key means not found, a non-table value is a `*ParseError`. |
| 121 | Choosing environments                       | `WithEnvs`, then `{NAME}_ENV`; a repeated name keeps its later position; platform envs below; unsafe names and filenames are `*OptionError`s at `Load` (see [Environments](#environments)). |
| 122 | The walk                                    | `Walk(patterns...)`, `WithStartDir`, `WithWalkStop` (`StopAtHome` or `StopAtRoot`) and `WithCeilingPaths`; root markers come with M4d (see [Directory walk](#directory-walk)). |
| 123 | Path lists                                  | The host's list separator; empty entries dropped; relative entries against the current directory; `~` expanded. |
| 124 | `MiseLayout`'s scopes                       | system, user, project and override, as listed in [`MiseLayout`'s scopes](#miselayouts-scopes). |
| 125 | Bindings that replace or disable scopes     | `ReplacedByFile(b)` and `DisabledBy(b)`; override filenames replace a walk's shared patterns and remove its local ones. |
| 126 | Explicit files                              | The `ExplicitFile(paths...)` kind, and `ReplacedByConfigFiles()` marking the scope they replace; errors 404, 415 and `*IOError` (see [Explicit config files](#explicit-config-files)). |
| 127 | Ignored paths                               | `WithIgnoredPaths` with the env var; matches are `LayerIgnored` without a warning; explicit files are never ignored. |
| 128 | A moved user directory                      | The walk drops files under the default user directory when `{NAME}_CONFIG_DIR` moves it, as mise does. |
| 129 | Golden tests in M4b                         | The worked example, a full directory with layers A–D, the user and override scopes, and walks from inside and outside `~`. |
| 130 | Sharing mise's global patterns              | `MiseGlobalPatterns()` returns them, a fresh copy each call, for the system and user scopes and for apps' own scopes, such as a mounted config directory. A package can't hold them, because `Pattern` lives in the core. |
| 131 | Declaring bootstrap files                   | The scope option `BootstrapFiles(patterns...)` searches with the scope's own kind; `Pattern.SkipHomeAndRoot` for `Walk`; unselected scopes' bootstrap files aren't read (see [Bootstrap file](#bootstrap-file)). |
| 132 | Bootstrap keys and limits                   | `BootstrapKey` constants for the five keys; `WithBootstrapKeys`, `WithoutBootstrapKey` and `WithBootstrapLimit(scope, keys...)`. |
| 133 | Reading bootstrap files                     | Ordered like config files, a higher file replacing a key; list values; paths relative to the file; unknown keys and dropped keys warn; wrong types are `*OptionError`s at `Load`; the bootstrap walk uses only option and env-var ceilings. |
| 134 | Bootstrap precedence and transforms         | Env var > bootstrap > option > preset for single settings, with `WithEnvs` first for `env`; adding lists combine; `WithBootstrapTransform` for per-value control, with `*TransformError`s. |
| 135 | Scope limits by trust                       | Hiding keys only act on scopes at most as trusted as the file; each scope takes `formats` from the highest file at least as trusted; ignored entries never apply to a more trusted scope, with a warning. |
| 136 | `{NAME}_CONFIG_FORMATS`                     | Implemented with `WithFormats`' syntax; an unknown format or one with no decoder is an `*OptionError` at `Load` named after the variable. |
| 137 | Bootstrap files in results                  | Tests read a private list until M5's report; bootstrap files never merge into the config. |
| 138 | The layouts' bootstrap files                | `MiseLayout`'s system, user and project files as mise has them, all five keys; `MiseHardenedLayout` limits project files to `env` and `formats`; no extra `.config/{name}/` location. |
| 139 | First-match resolution                      | `WithResolution(FirstMatch)` loads only the highest-precedence non-empty file; files below it aren't decoded (see [Search strategies](#search-strategies)). |
| 140 | Walk stops and search strategies            | `StopAtStart` and `StopAtMarker` join the walk stops; `WithStopDir` generalises `StopAtHome`; `WithSearchStrategy(SearchNone \| SearchProject \| SearchGlobal)` is cosmiconfig shorthand. |
| 141 | Choosing a file's format                    | `WithFileFormat(matcher, formats...)` with `Extension`, `FileName`, `Glob`, `Regexp` and `MatchFunc`; extensions are built-in rules checked last; it replaces `WithExtensionlessFormat` (see [Choosing a file's format](#choosing-a-files-format)). |
| 142 | The meta config                             | A `meta` scope at `{cwd}` with a `Key`, and its `cosmiconfig` table as a bootstrap file with `searchPlaces` and `mergeSearchPlaces`; `loaders` and `searchStrategy` only warn. |
| 143 | `EnvPathsDirs`                              | cosmiconfig's `env-paths` user directory on each OS; no system directory. |
| 144 | Explicit files replace everything           | `WithConfigFilesOnly()`: no scope is searched when explicit files are set; bootstrap files are still read. |
| 145 | `$import`                                   | Deferred, and possibly never supported, depending on how it fits the include security and trust model. |
| 146 | `CosmiconfigLayout`'s scopes                | `user`, `project` and `meta`, with first-match resolution and `SearchNone` (see [Deviations from cosmiconfig](#deviations-from-cosmiconfig)). |
| 147 | Golden tests in M4d                         | One directory with every search place, each strategy's walk, `SearchGlobal` reaching the user directory, and the meta file's key winning. |
| 148 | Dates when decoding                         | koanf's three hooks and no fourth: `time.Time` implements `UnmarshalText`, so the `TextUnmarshaler` hook already parses RFC 3339 strings, and the koanf parity tests cover dates ([api.md](api.md#decoding)). |
| 149 | Custom decoder configs                      | `WithDecoderConfig` replaces our hooks, as in koanf; `DecodeHook()` returns ours to compose; the config is copied; a `Metadata` in it is an `*OptionError`, since concurrent decodes would share it. |
| 150 | Decode errors                               | One `*DecodeError` per key, with `Key`, the file in `Path` and the scope, joined; messages never quote values; the layer blamed is the value's own: an element's, a merged table's latest, `defaults`, or the variable. |
| 151 | Unknown keys                                | Recorded on the `Result` from every decode through it, once each, under a lock; `UnknownKeysIgnore` silences them; keys inside arrays are named as mapstructure names them (`servers[0].hots`); an unused table is listed as its leaf keys. |
| 152 | Env-var provenance                          | `Origin.EnvVar` names the variable behind a key from `EnvVars`, including through `MapEnv`; the scope stays one layer. |
| 153 | The defaults scope                          | `ScopeDefaults`, always the lowest and always selected, removable with `WithoutScopes`; a second `WithDefaults` replaces the first; the tag is read after every option; a non-nil pointer to a zero value is included; invalid values are `*OptionError`s naming the field. |
| 154 | First match and value layers                | First-match resolution chooses between files only; value layers, defaults included, always merge at their own position (see [Search strategies](#search-strategies)). |
| 155 | Load stages                                 | Settings, then discovery and reading, then merging, then requirements; each stage reports all its errors, joined; a failed stage stops the load ([api.md](api.md#errors)). |
| 156 | Failed loads                                | Broken files are `LayerFailed`, with a `Reason` built from the error's fields, never the parser's text; `Decode` and `DecodeAt` on a failed result return the load's error. |
| 157 | What `RequireConfig` counts                 | Empty value layers count as nothing; empty files count except with `RequireNonEmpty`; skipped layers never count; explicit files count for the scope they replace, or every scope under `WithConfigFilesOnly`; an unselected scope is an `*OptionError` ([api.md](api.md#requiring-config)). |
| 158 | The report's shape                          | `ScopeInfo`, `EnvInfo`, `Probe` and `BindingInfo` as in [api.md](api.md#result-report-and-origin); `Report.Bootstrap`; `Preset` is the preset's name; `LayerDuplicate` for a file's other route; `snake_case` JSON with enums as strings; `String()` grouped by scope like `mise config ls`. |
| 159 | Deferred from M5                            | `ScopeInfo`'s policy arrives in M7 and `Layer.Remote` in M8; the transform checks of DL-76 move to M7, with transforms. |
| 160 | `KoanfProvider` and `DecodeAt`              | `(*Loader).KoanfProvider()` loads with `context.Background()` on each `Read`; the koanf parity tests load through it; `DecodeAt` on a missing path leaves `v` untouched; a bad path or a non-pointer `v` is a 400 `*BaseError`. |
| 161 | Decoding dependencies                       | mapstructure v2.5.0 is the core module's one requirement; koanf v2.4.1 is pinned in the test-only `internal/compat` module. |
| 162 | Format modules before the first release     | `format/toml` and `format/yaml` replace the core with the repository's copy until the first release, as `internal/compat` does; M11 swaps in the released version and adds a check that refuses a `replace` in a released module. |
| 163 | How YAML values get their types             | goccy parses; the YAML 1.2 core schema's rules type unquoted values, because goccy and yaml.v3 both keep YAML 1.1 leftovers and disagree with each other. `WithLegacyNumbers()` opts in to 1.1 integers; it's tentative, and may be dropped if it complicates the decoder, since a custom decoder can do the same ([decision record 0002](decisions/0002-yaml-and-toml-dialects.md)). |
| 164 | YAML tags                                   | Only the core schema's tags, judged after expanding `%TAG` handles; any other is a `*ParseError`, as the spec treats an unresolved tag. A tag forces its type. |
| 165 | YAML dates                                  | The core schema has no timestamp type, so a YAML date stays a string as written, as goccy already does; one in RFC 3339 form still decodes into `time.Time`. |
| 166 | YAML keys                                   | Resolved by the core schema, then printed, as goccy does; keys equal after resolution are duplicates, as the spec says, and so are keys that clash only once printed, because the tree can't hold both. |
| 167 | Merge keys                                  | Supported, as a deliberate YAML 1.1 extension: the table's own keys win, then earlier list entries, as the merge-key spec says. |
| 168 | YAML aliases                                | At most 10,000 values from aliases and merge keys per document, set with `WithMaxAliasValues(n)`; going over, or a self-alias, is a `*ParseError`. |
| 169 | TOML dates and times                        | Offset date-times as `RFC3339Nano` text; local date-times, dates and times as their TOML text; the values are parsed as go-toml parses them (DL-48). |
| 170 | Parse error messages                        | Never quote the file: YAML errors keep goccy's short message and the position, without its excerpt. |
| 171 | Library panics                              | Each decoder recovers a panic inside its library's parse call and returns a `*ParseError`; a panic in the decoder's own code isn't recovered, so it surfaces as a bug. |
| 172 | Format module API                           | `toml.Decoder()`, and `yaml.Decoder(opts...)` with `WithLegacyNumbers` and `WithMaxAliasValues`; formats, extensions and media types as in [api.md](api.md#settled-in-m6). |
| 173 | Dependency budget                           | `scripts/check-deps.sh` builds throwaway consumers for each setup whose modules exist, and checks their counts; it runs in lefthook's pre-push. |
| 174 | Conformance                                 | Each format's spec is the guiding principle, and deviations are deliberate and documented. Measuring conformance with yaml-test-suite, toml-test and JSONTestSuite is deferred to M12, the last milestone. |
| 175 | YAML directives                             | A document of directives belongs to the next document; `%YAML 1.2` is accepted, 1.1 and later 1.x are parsed as 1.2 with a `yaml.WarnVersion` warning, and 2.x is a `*ParseError`; `%TAG` handles are expanded. goccy put each directive in a document of its own, which the one-document rule wrongly rejected. |
| 176 | Matchers                                    | `MatchFunc` keeps its string signature and, in an allow-list, gets the normalised reference string; `MatchRef(func(IncludeRef) bool)` is for allow-lists only; a new `Prefix` works in both; every M4d matcher works in allow-lists; `Glob` gains `?` for one character other than `/`. |
| 177 | Turning off local includes                  | `WithoutIncludeResolver("file")` with an include key is an `*OptionError` at `New`, as api.md's bare-loader rule says; a scope forbids local includes with a policy whose `AllowResolvers` leaves out `"file"`, which gives a 403. |
| 178 | The include key and nesting                 | `WithIncludeKey(key, IncludeFlat \| IncludeNested)` and `WithoutIncludeKey()`; an empty key, or the unset key, is an `*OptionError`. The mise layouts use `"include"` with `IncludeFlat`. `CosmiconfigLayout` has no include key while `$import` is deferred (DL-145), so nesting and cycles are tested through a custom layout. |
| 179 | Including a file twice                      | One rule for every file: it loads once, at its highest position, and its other routes are `LayerDuplicate` (DL-110). Reaching an ancestor of the same chain again is a `*CycleError` (508). This replaces "once per include chain" and "first rank". |
| 180 | Confinement                                 | `Confine(roots ...string)` with `FixedDir`'s placeholders plus `{config_root}`; roots are resolved through symlinks, and a missing root matches nothing; "under" means the root or below it, by whole segments; it applies to includes only. |
| 181 | Files named directly                        | An included or explicit file that a permission policy would ignore, or that is untrusted, is a `*PolicyError` (403), because a file named directly is never silently skipped. Searched files are skipped with a warning. |
| 182 | `file` resolver and the M7/M8 split         | The `file` resolver is part of the loader, registered by default and not replaceable, the one built-in that isn't an ordinary implementation of its interface. M7 adds `IncludeRef`, `WithoutIncludeResolver`, and a 501 for every other scheme; `IncludeResolver`, `Fetched`, `WithIncludeResolver`, `RequirePinned`, reference normalisation, `WithMaxIncludeSize`, the cache and `WithoutRemoteIncludes` are settled in M8. |
| 183 | Local references                            | Remote only with `name::` or a `scheme://` of two or more letters; `file://` with an empty host or `localhost` is local; anything else is a path; a leading `~/` is the home directory. |
| 184 | Include values and metadata                 | An empty list includes nothing; an empty string, a non-string element or another type is a `*ParseError`. An included layer takes `Scope`, `Variant`, `Env`, `Trust` and `ConfigRoot` from its chain's root file; its `Path` is as joined, `Pattern` is empty, and `IncludedBy` is the direct includer. The key is read at the top of a keyed table. Formats follow `WithFileFormat` rules and extensions, ignoring the format order. |
| 185 | Include errors                              | Errors about a reference have the including file as `Path` and `Op` `"include"`, and quote the reference; errors inside the included file have its own `Path`, and its `LayerFailed` entry shows `IncludedBy`. |
| 186 | Steps for one layer                         | Decode, take the keyed table, run the layer transforms, read and remove the include key, then apply `_unset`. |
| 187 | Where includes work                         | `FixedDir`, `Walk`, `ExplicitFile` and custom-scope files; ordinary data in `EmbeddedFS`, value scopes and bootstrap files; never a first-match result. The includer's `_unset` removes keys from its included files, which rank below it. |
| 188 | Combining policy options                    | Each policy option replaces its own setting, and the last one wins; `InheritDefault()` anywhere starts from the default; a fresh policy allows no resolver and checks nothing; `WithDefaultPolicy` and `WithScopePolicy` are single settings. |
| 189 | Policy defaults and validation              | The engine default allows every registered resolver; `MiseHardenedLayout` allows `file` only, confined to `{config_root}`, `{user}` and `{system}`, with `StrictModes`. An undeclared scope, or a scheme with no registered resolver, is an `*OptionError`. |
| 190 | Bootstrap and explicit files under policies | Bootstrap files get their scope's permission check and trust requirement, and are skipped with a warning when they fail; explicit files are judged by the policy of the scope whose name they take, or the default policy. |
| 191 | File permissions                            | `FilePermissions(func(FileCheck) (Permission, string))`, called for every file read and its containing directory; `StrictModes` is exported; *ignore* gives `LayerIgnored` and `WarnFilePermissions`, *error* a 403. `cctest.FS.Chown` sets an owner, and a file never chowned has none. |
| 192 | Trust store details                         | `LayerUntrusted` and `WarnUntrusted`; a store error is a 500; searched, explicit and bootstrap files are checked, after size and permissions and before decoding; `TrustPaths` entries without wildcards trust whole trees, as mise's do. |
| 193 | Transforms                                  | Layer transforms run on every layer that merges, value scopes and defaults included; `nil` merges nothing; returned trees must be canonical; the result transform runs before `RequireConfig` and the unknown-key check, and `OriginOf` describes the merge. |
| 194 | Policies in the report                      | `ScopeInfo.Policy *PolicyInfo`; a rejected reference is a `LayerFailed` layer naming the policy's scope and the rule; `Report.String()` marks included layers "included by" and adds a `policies:` section. |
| 195 | The M8 split                                | M8 becomes M8a (the loader's remote includes, normalisation, pins, the cache, `cctest.Clock` and `httpresolver`), M8b (`gitresolver`) and M8c (`ociresolver`), each with its own gap list, review and PR. Each part's parsers get fuzz tests from the start. |
| 196 | Registering resolvers                       | `WithIncludeResolver(r)` maps each of its schemes to `r`, and the last registration wins; a nil resolver, no schemes, an invalid scheme or `file` is an `*OptionError`; `WithoutIncludeResolver` removes one scheme. |
| 197 | Reference forms                             | The form, not the resolver, decides the parsing: `scheme://` is a URL; `oci::` an image reference, with an explicit registry and `latest` by default; any other `name::` the getter form, with a required path and an optional ref that follows git's ref-name rules. |
| 198 | `IncludeRef` and `Fetched`                  | `IncludeRef` gains `Ref`, `MaxSize` and `Check`; `Fetched` is `{Content, MediaType, Identity}`, and the loader picks the format from the reference's path first, then the media type; an empty identity is the content's SHA-256. `WithMaxIncludeSize` is 1 MiB by default. |
| 199 | Resolver errors and timeouts                | Errors with a cascadingconfig code keep their type, and others become a `*FetchError` (504 for a timeout); `WithIncludeTimeout(d)`, 30 seconds by default, bounds each fetch; fetches run one at a time, in walk order. |
| 200 | URL normalisation                           | Hosts must be ASCII, so IDNA adds no dependency; scheme and host are lowercased, a trailing dot and the default port removed, dot segments resolved and escapes normalised; encoded `.`, `/` and `\`, a raw `\`, control characters and userinfo are rejected, except an ssh user name; the query is kept. |
| 201 | Pins                                        | The loader decides pins from the form: an integrity fragment, a full git commit id, or an OCI digest. It strips and verifies the integrity fragment itself, and checks `RequirePinned()` before fetching. |
| 202 | What allow-lists see                        | `Extension` and `FileName` see the reference's file path, and the other matchers the whole normalised string. There's no address filtering, and references are shown unredacted, so credentials belong in resolver options. |
| 203 | Remote layers                               | A remote file is identified by its normalised reference, so a reference listed twice is fetched once; `Layer.Remote` holds its scheme, normalised reference, identity, pin and cache state; `WarnStaleInclude` marks a stale copy. |
| 204 | Relative references in remote files         | They resolve against the file's remote origin; climbing out, `~/` and `file://` are `*ReferenceError`s. Built in M8a, for layouts with `IncludeNested`. |
| 205 | Hardened remotes and the layouts            | `WithoutRemoteIncludes()` removes the remote resolvers registered before it, and makes a policy that allows a remote scheme without an allow-list and `RequirePinned()` an `*OptionError`. No layout registers a remote resolver, because they import the core; `mise.Preset` does, in M10. `PolicyInfo.RequirePinned` shows the pin rule. |
| 206 | Cache storage                               | Content is stored under its own SHA-256, so files in one commit don't collide, and the identity is report metadata; the cache uses the real OS filesystem, with modes 0700 and 0600; a cache that can't be used gives `WarnIncludeCache`; there's no eviction yet. |
| 207 | Cache freshness                             | `WithIncludeCache(dir, ttl)`, where a TTL of 0 always fetches; only a `*FetchError` falls back to a stale copy; cached content is rechecked for size, integrity fragment and format, never the trust store, which includes don't consult. |
| 208 | `httpresolver`                              | `New(WithClient(c))`, with the client copied; at most 5 redirects, none to `http`, each checked with `IncludeRef.Check`; only a 200 is content, 404 and 410 are `*NotFoundError`s, and other statuses are 502s; the identity is the body's SHA-256. |
| 209 | `cctest.Clock`                              | `NewClock`, `Now`, `Advance` and `Set`, passed to a loader with `cctest.WithClock`; M9 adds timers. |
| 210 | Include limits                              | `WithMaxIncludeDepth(n)`, 16 by default, and `WithMaxIncludes(n)`, 1000 by default, bound every load's includes, local and remote; going deeper is a `*CycleError` (508), and more references, duplicates counted, a `*TooLargeError` (413). |
| 211 | Fetching with git                           | A fresh bare repo per fetch; a shallow fetch with protocol v2, then `rev-parse --end-of-options`, `ls-tree`, `cat-file -s` against the size limit and `cat-file blob`. When a server refuses a commit id, every branch and tag is fetched without blobs. SHA-256 repositories are supported. The identity is the commit id. |
| 212 | Isolating git                               | Every `GIT_*` variable is removed, since `GIT_CONFIG_COUNT` injects config past `GIT_CONFIG_NOSYSTEM`; global config is an empty file; prompts, hooks, submodules and filters are off; ssh runs in batch mode. The repo URL's scheme is checked against `WithProtocols` before git runs, a `*PolicyError` if it isn't allowed. |
| 213 | git and redirects                           | git never follows redirects itself. For http and https repos, the resolver follows them first with Go's client, at most 5 hops, none to http, each through `IncludeRef.Check`, and git fetches from the final URL; `WithClient` sets the client. With git's default, a redirect silently reached another host. |
| 214 | gitresolver's API and errors                | `New(WithProtocols, WithCredentialHelper, WithGit, WithClient)`. A missing ref or path is a `*NotFoundError`, a symlink, submodule or directory a `*ReferenceError`, a large file a `*TooLargeError`, and anything else a `*FetchError` with git's first error line. Tests use `git http-backend` through `net/http/cgi`. |
| 215 | Reading OCI artifacts                       | The tag or digest is resolved to a manifest digest first, which is the identity; OCI image and Docker v2 manifests only, capped at 4 MiB. Titled layers are files, whatever their media type (oras's default for a file is the tar type), unless oras's unpack annotation marks a directory tar; untitled tar layers (plain or gzip) are read as entries; all in manifest order, with image whiteouts; zstd and indexes are rejected. Entries with absolute names, `..` or `\` fail the fetch. |
| 216 | OCI caps                                    | `WithMaxLayerSize(n)`, 64 MiB by default, caps each blob's declared size, download and decompressed layer; the matched file keeps the include size limit. This replaces capping decompression at the include size limit, which ruled out image-style artifacts with more than 1 MiB of files. |
| 217 | ociresolver's API and network               | `New(WithCredentials, WithPlainHTTP, WithClient, WithMaxLayerSize)`, with the package's own `Credential` so oras types stay out; no Docker config is read. Registry redirects are followed without allow-list checks, at most 5 hops, never to http, since content is digest-verified. oras-go v2.6.4, measured by a new core-oci setup. |
| 218 | ociresolver's errors and tests              | Missing things are `*NotFoundError`s; indexes, unsupported layers, malformed entries and non-regular files `*ReferenceError`s; caps `*TooLargeError`s; anything else `*FetchError`s. Tests use a hand-written in-memory registry; `FuzzTarEntries` fuzzes the layer reader. |
| 219 | M9 and custom scopes                        | The `Scope` interface, and with it `Watchable`, was never scheduled. It gets its own part, M9b, after M9; until then a custom scope can't be written, and M9 watches only the built-in kinds. |
| 220 | The watcher's API                           | `Watch[T]` and `Loader.Watch` return a `*Watcher[T]` with `Current`, `Reload` and `Close`; a failed first load returns a nil watcher and the error; a nil handler is allowed; `Close` returns the notifier's error, then nil; after `Close` or a cancelled `ctx`, `Reload` is a 499 `*BaseError` and `Current` keeps the last snapshot. Snapshots are shared and read-only, because a generic `T` can't be deep-copied cheaply. |
| 221 | `KeyDiff`                                   | `{Path, Kind, Old, New, OldOrigin, NewOrigin}` with `DiffAdded`, `DiffRemoved` and `DiffChanged`; leaves only, an array is one leaf, and a table that becomes a scalar, or the reverse, is one `DiffChanged`; sorted by path; canonical values, deep-copied; `String()` shows no values. |
| 222 | Failed reloads and recovery                 | A failed reload's `Change` has `Old` and `New` both the last good snapshot, the failed `Result` and `Err`. Every failed reload calls the handler, and so does the first good reload after one, even with no change. |
| 223 | Handler calls                               | One goroutine runs a reload, then the handler, then the next reload; events keep coalescing while the handler runs; `Current` is updated first. Go has no goroutine identity, so `Close` and `Reload` recognise the handler's goroutine by its id from `runtime.Stack`: from inside, `Close` doesn't wait, and `Reload`'s handler call follows the current one. From outside, `Reload` waits for its handler call. A manual `Reload` cancels the debounce. |
| 224 | The watch set                               | Recorded by each watched load: every directory it listed or tried to list, with a filter of the names searched there; the directories of explicit files, local includes and symlink targets, filtered to the file's name; for a missing directory, its nearest existing ancestor, filtered to the next segment, with no limit. A failed reload re-plans from the old set plus what it recorded. |
| 225 | Notifiers                                   | `WithNotifier(func() (Notifier, error))`, a factory called once per watcher, because events have one reader. Events carry absolute paths in the `FileSystem`'s namespace; notifiers drop Chmod. An error is a warning and a reload; a closed `Events` channel polls everything; a failed `Add` polls that directory; a failed factory polls everything, and `Watch` still succeeds. |
| 226 | Polling                                     | Each polled directory is listed, and matching entries compared by type, size, mode, modification time and `SameFile`; loaded files are also hashed. `WithPollInterval(d)` sets the interval, 2 s by default, and `WithPolling()` polls everything. This replaces DL-39's `WithPolling(interval)`, which left the fallback interval fixed. Polling without a notifier is logged at debug level. |
| 227 | Watcher warnings                            | `WarnWatchFailed` (`"watch-failed"`) and `WarnReloadFailed` (`"reload-failed"`), logged with the code as an attribute, since the watcher has no report; a handler panic is logged at error level. |
| 228 | Debounce rules                              | `WithDebounce(quiet, maxWait)`: each event restarts the quiet period, and a burst reloads no later than `maxWait` after its first event; `quiet` must be at least 0 and `maxWait` at least `quiet`, else an `*OptionError` from `Watch`; a `quiet` of 0 reloads on every event. |
| 229 | Conditional fetches                         | `Fetched.Validator` and `IncludeRef.Validator`, with `ErrNotModified`: http uses the ETag, else `Last-Modified`; git runs `ls-remote` and compares the commit; OCI compares the manifest digest before any blob. The cache stores the validator and revalidates after the TTL (`"revalidated"` in the report). The loader keeps validators only if printable ASCII and at most 1 KiB. |
| 230 | Remote polling                              | `WithRemotePoll(d, matchers...)`, per source: allow-list matchers on the normalised reference, the most recent rule winning, 0 meaning never, off by default. A poll revalidates the matched unpinned remotes, ignoring the TTL, and queues a reload only on a change; without the cache, it's a full reload. ±10% jitter, and failures back off up to 8 times the interval. This replaces DL-38's single interval. |
| 231 | `cctest` timers and notifier                | The internal clock gains `AfterFunc`. `Clock.Advance` fires due timers in order and returns once the watcher has handled them, reload and handler call included. `cctest.NewNotifier()` has `Send`, which returns once the watcher has taken the event, `SendError`, `FailAdd`, `Watched` and `New`. Tests turn off remote-poll jitter through `cctest`. |
| 232 | `watch/fsnotify`                            | `fsnotify.New() (cascadingconfig.Notifier, error)`, used as `WithNotifier(fsnotify.New)`; drops Chmod, reports `ErrEventOverflow`, idempotent `Close`. Requires the current `golang.org/x/sys`, not fsnotify's v0.13.0. Real-filesystem tests cover an editor's rename, a ConfigMap swap and the fallback; a core-fsnotify dependency setup is measured. |
| 233 | Comparing values                            | A change is any difference in the canonical tree, strings compared exactly, and NaN equals NaN. Dates and times are RFC 3339 strings in the tree, so DL-77's "`time.Time` values are compared as instants" has nothing to apply to: the same instant written differently is a change, because a string field sees new text. |
| 234 | The shape of a `Diff`                       | The pure difference of the two trees' leaves, where a leaf is a scalar, an array or an empty table: changed, removed or added by path, sorted segment by segment. It replaces DL-221's "a table that becomes a scalar is one `DiffChanged`", and it's the only rule tested in which every changed key has its own entry; entries nest when a value changes shape ([decision record 0003](decisions/0003-watch-diff-shape.md)). |
| 235 | Details the M9 implementation settled      | Polls, remote polls with new content and notifier errors all count as events, through the debounce; notifier errors are `WarnWatchFailed`; a directory that can't be opened but exists is watched or polled; glob directory segments skip hidden directories; polled directories are rescanned after each reload; git's `ls-remote` matches a ref as `fetch` does; `cctest.Clock.Timers` reports the waiting timers. |
| 236 | Custom scope kinds                          | `Custom(s Scope) ScopeKind` adapts an app's `Scope`; a nil one is an `*OptionError`. Default trust `TrustProject`, and it needs a decoder. The built-in kinds stay sealed and don't implement `Scope`, which corrects api.md. The report's kind is `"Scope"`, and its policy is shown. |
| 237 | `Discovery`                                 | `Name`, `Scope`, `StartDir`, `Home`, `Envs`, `PlatformEnvs`, `Formats`, `FS`, `LookupEnv` and `Logger`, all through the loader's host so `cctest` fakes apply. |
| 238 | Sources                                     | Exactly one of an absolute `Path` and a `Tree`; a given `Format` must be registered; a broken `Source` is a 500 naming the scope; a coded `Discover` error keeps its code, and others are 500s. `Path` sources join the pipeline like searched files, with the file's directory as config root, and a missing one is a `*NotFoundError`; `Tree` sources are like `Values`. Callbacks can run concurrently. |
| 239 | Watching custom scopes                      | `Path` sources are watched like explicit files. `Watchable.WatchDirs`, called after `Discover` on watched loads only, adds directories with no name filter; missing ones go through their nearest existing ancestor; a relative directory or an error is `WarnWatchFailed`. Name filters are a future idea. |
| 240 | Custom scopes in the report and tests       | `Path` layers look like searched files without a `Pattern`, `Tree` layers have no `Path`, and custom scopes record no probes. `FuzzCustomSources` checks that random sources never panic and fail only with 500s. |
| 241 | Details the M9b implementation settled     | A `Path` source is cleaned before it's read, so dot segments work as for explicit files; a file listed twice is a duplicate; permission and trust checks skip a `Path` source with a warning, as for a searched file, rather than failing the load as for an explicit one; every source is checked before any loads. |
| 242 | M10 in two parts                            | M10a specifies precedence in the core, with conformance tests and an oracle fuzz test; M10b is the presets module, plus a check that each preset keeps its layout's order. Each part has its own gap list, review and PR. |
| 243 | The precedence spec                         | [precedence.md](../precedence.md) is normative for the order of layers: one sort key (scope, walk directory, variant group, environment, pattern, glob match, format, extension), then rules for what the key can't say, each with an ID (`PR-n`), and the built-in layouts' tables. design.md defers to it on order. |
| 244 | The precedence oracle                       | A test-only oracle computes the expected order from the spec alone: its own copy of the layout tables and the sort key, never `layouts.go` or loader internals. A drift test parses precedence.md's tables and checks them against the oracle's. |
| 245 | Precedence conformance tests                | At least one named test per `PR-n` rule, and the spec's worked examples as tests with hand-written expected orders. Includes, bootstrap-chosen settings, value scopes and Windows are covered here, outside the fuzz test. |
| 246 | `FuzzPrecedence`                            | Random layouts (built-in or random patterns), hosts, start dirs, environments, format orders, conflict policies and file sets with decoys, empty files and symlinked duplicates; the report's order, statuses and merged `top` key must match the oracle. JSON's decoder stands in for TOML and YAML. Instead of a red run, six deliberate mutations of the ordering code must each be caught within a minute. |
| 247 | Explicit-only loads keep value scopes       | Under `WithConfigFilesOnly()`, explicit files replace only scopes that search for files, at the place of the highest of them; `Values`, `ValuesFunc`, `EnvVars` and the defaults still take their places, as DL-153 and DL-154 say, so flags above them still beat the file. Until now they were dropped too. |
| 248 | The ready-to-use presets                    | `mise.Preset` is `MiseLayout` with TOML, YAML, `httpresolver`, `gitresolver`, `ociresolver` and `WithNotifier(fsnotify.New)`; `misehardened.Preset` is `MiseHardenedLayout` with TOML, YAML and fsnotify; `cosmiconfig.Preset` is `CosmiconfigLayout` with YAML and fsnotify. Resolvers take their default options. Each is built with `ExtendAs` under its own name, a package variable that's never reassigned. |
| 249 | The presets module                          | It requires the core, `format/toml`, `format/yaml`, `ociresolver` and `watch/fsnotify` through `replace` directives until the first release (DL-162). |
| 250 | Doc examples                                | The usage snippets in design.md and api.md, 16 of their 46 Go blocks, each marked `<!-- example -->`, are Go `Example` functions in the presets module, each holding its snippet unchanged plus its setup; a drift test checks every snippet appears in one. A wrong snippet is fixed in the doc. Snippets' `import` lines are left out of the check, and a snippet with top-level declarations is a compiled `package main`. The 30 signature listings are checked against the API in M11. |
| 251 | Checks on the presets                       | Each preset ranks a rich fixture exactly as its layout does; each registered format loads; in `mise.Preset`, `http` fetches end to end and `git` and `oci` reach their resolvers (a 502 for an unreachable host, not a 501); `misehardened.Preset` rejects remotes with a 501; each preset's default notifier reloads a real directory. |
| 252 | The presets' dependency budget              | `check-deps.sh` gains a setup per preset package: `misehardened` 5/5/5/9 and `mise` 9/9/9/11 as measured in M6, and `cosmiconfig` 4/4/4/9, measured in M10b: its graph lists the presets module's nine, like `misehardened`. |
| 253 | M11 in stages                               | M11 is documentation, the API and examples, in four stages: M11a the documentation structure, M11b the API audit and guard, M11c the examples and M11d the README and user guide. Releasing, with package documentation, licences, CI, tags and making the repository public, is a later stage of its own. |
| 254 | Documentation structure                     | Four homes: user docs in `docs/`, design records (design.md, api.md, config-locations.md and the decision records) in `docs/design/`, plans (implementation-plan.md and future-ideas.md) in `docs/planning/`, and contributor docs (project-layout.md) in `docs/contributing/`, with names unchanged; precedence.md stays in `docs/`. Each doc says which kind it is, `docs/README.md` indexes them and pre-push checks every link ([decision record 0004](decisions/0004-documentation-structure.md)). |
| 255 | When a decision gets a record               | Every decision gets a row here. It also gets a decision record when alternatives were weighed with evidence, when it's hard to reverse (the public API's shape, the module layout, the docs' structure), or when its recommendation was contested; the row then links to the record. Existing rows aren't backfilled. |
| 256 | Checking the design records' listings       | `internal/apicheck`, a test-only module, checks every Go block in design.md and api.md not marked `<!-- example -->` against the real packages, with `go/types` on `go list -export` data. A line `// package <path>[, <path>…]`, paths from the repository root and `.` for the core, optionally followed by a note in parentheses, sends the declarations after it to each package named; with none, they're the core's. Functions and methods compare parameter and result types and type parameters, not parameter names; constants their type and value; variables their type; every declaration its kind. A struct listing may show some of its fields; an interface listing names every exported method. A block that doesn't parse is a finding, so usage code goes in `<!-- example -->` blocks (18 of them, up from 16). A block marked `<!-- api superseded DL-n -->` is skipped. Names in prose are triaged by hand once. |
| 257 | The API guard                               | A file per released package under `api/`, mirroring the package paths, lists its exported API one declaration per line, sorted: functions, methods (the pointer type's method set), types with each exported field or interface method on its own line, sealed interfaces marked, constants with values, variables with types. A test checks every released package against its file both ways; `-update` rewrites the files, so each API change is a deliberate diff. |
| 258 | Plumbing kept out of the public API         | Found by reading the first `api/` files. `cctest.Clock` and `cctest.Notifier` hand the loader unexported adapters, through `WithClock` and `Notifier.New`, so the methods only the loader needs (`AfterFunc`, and the notifier's `Add`, `Remove`, `Events`, `Errors` and `Handled`) aren't methods of the public types. `Notifier.Close` stays public: a test calls it to simulate a notifier that stops (DL-225). The resolvers' `Option` becomes a sealed interface, like the core's and `format/yaml`'s, instead of a function of an unexported type. |
| 259 | Uncached tests before a push                | go test's cache re-checks only files inside the tested module, so a test that reads another module's code, `docs/` or `api/` (`internal/apicheck`, the presets' doc examples) can pass from the cache after they change: removing six `cctest` methods showed as a cached pass. The pre-push hook's latest-Go run uses `-count=1`; the minimum-Go run stays cached, since it reruns whenever code changes. |
| 260 | The examples                                | Five programs in `examples/`, one user story each: `basic`, `cli`, `watch`, `secrets` and `trusted`. Each is `package main` with its work in `run(ctx, args, stdout, opts ...Option)`, ships the config it reads under an app name of its own (`ccx-…`), has a README, and is tested by running `run` in a `cctest` world built from its shipped files, against golden output. They use the standard library's `flag` and no third-party modules. |
| 261 | Defining a scope selects it                 | `WithScope` adds the scope it defines to the current selection; `WithScopes` still replaces the selection and `WithoutScopes` removes from it, in order. A preset's optional, off-by-default scope is defined, then removed with `WithoutScopes`. It replaces "defining a scope doesn't select it", which made an app restate the layout's whole selection to add one scope; the examples found it (M11c). |
| 262 | A non-directory on a search path            | When opening a scope's directory, or listing a directory on a pattern's path, fails with anything other than "doesn't exist", the loader checks the path: something there that isn't a directory is skipped like a missing directory, logged at Debug; any other failure stays an `unreadable` warning. A binary named after the app, or a legacy `~/.{name}` file, no longer warns on every load. |
| 263 | Log levels                                  | Per warning code: Error for a watch handler's panic; Info for `untrusted`, `format-conflict` and `no-home`; Warn for the rest, including codes from other modules. Debug traces loads, files, remote includes and the watcher, each record with an `op` attribute and stable attributes for it, since messages aren't API. `WithLogLevel(slog.Leveler)` drops records below a level before the logger, and custom scopes get the filtered logger; a nil level is an `*OptionError` (see [Warnings and logging](#warnings-and-logging)). |
| 264 | Empty value layers                          | A layer from `Values`, `ValuesFunc`, `EnvVars`, the defaults scope or a custom scope's tree that supplies no keys is `LayerEmpty`, not `LayerLoaded`, with its warnings kept; `RequireConfig` still never counts it. |
| 265 | The README                                  | What the library does, its features, install, a quick start, choosing a preset, the modules and their dependencies, a pre-1.0 note, links and the licence. The quick start is `examples/quickstart/main.go`, a complete program with its own test, embedded whole after a `<!-- quickstart -->` marker and checked to match the file exactly; the other presets get a line each with a link to their example. Badges wait for the release stage. |
| 266 | The user guide                              | Task-oriented pages in `docs/guide/`, with an index: getting started, where config comes from, decoding, merging, includes, security, watching, provenance and logging, testing your app, and errors. precedence.md stays the reference for order. Each usage snippet is marked `<!-- example -->` and held unchanged in an `Example` function in `examples/guide`, run on `cctest`. One checker, `internal/docsnippet`, serves the guide and the design records, whose 18 snippets stay in `presets/mise`, with a snippet count per doc. |
| 267 | Names in user docs                          | Every backticked Go identifier in the README, `docs/guide/` and precedence.md names something in a released package: a package-level name, a method or a field, qualified by its package's name or not. All-capitals names (environment variables) are skipped, as are names qualified by a package outside this repository, such as `slog.LevelWarn`; anything else not found needs an entry in the check's allow-list. Design records are exempt. The check is `TestUserDocNames` in `internal/apicheck`. |
| 268 | M11d in two stages                          | M11d1: the README, the quick start, the checks, the guide's index and its first three pages. M11d2: the other seven pages. Each is reviewed and merged on its own. |

---

## Loader model

This is everything above, reduced to the dimensions a preset must be able to set. Built-in and user presets are the same
type. It is not an API yet.

- **Scopes**, ordered lowest → highest (see [Scope terminology](#scope-terminology)). Each scope is one of:
  - a *fixed dir* (`system`, `user`, `override`), with its own filename pattern list
  - a *walk* (`project`: start dir, direction of application, stop and ceiling rules, a root marker), with a
    per-directory pattern list
  - an *explicit file* (`load(path)`, `WithConfigFiles`, `{NAME}_CONFIG_FILE`), replacing a scope's search
  - an *embedded* file (a key path inside a manifest)

  Users can add custom scopes (placed with `Before`/`After`) and select scopes with `WithScopes`/`WithoutScopes`.
  Reordering or renaming the built-in scopes means defining a new preset. The four built-in names are conventions, not a
  closed set.
- **Patterns** are path templates with `{name}`, `{env}` and `{ext}`, single-segment globs (expanded alphabetically) and
  folder fragments. Each pattern carries its *layer*: shared, env, local or env-local.
- **Variants** are an ordered env list, from a resolver chain (flag → env var → bootstrap file), plus an optional
  platform-env list.
- **Resolution** is a policy: `FirstMatch` (cosmiconfig) or `MergeAll` (mise). The merge strategy deep-merges objects
  and replaces arrays, and [per-key merge rules](#per-key-merge-rules) can override it.
- **Includes:**
  - a key name, set per preset
  - path resolution relative to the including file
  - rank relative to the includer
  - whether nesting is allowed, with cycle detection
  - resolvers, keyed by scheme, including the built-in `file` resolver
  - allow-lists, both global and per scheme
  - per-scope policies
- **Preset composition:** `Extend` derives a preset from another and overrides individual settings.
- **Bootstrap:** an optional pre-search with its own scopes, filenames and allow-list of discovery keys, all named by
  the preset.
- **Per-layer metadata:** the path, the source tier, the config root, the matched pattern, the env, whether the layer is
  empty, and its trust state. This is what makes `config ls`-style diagnostics possible.
- **Filesystem:** one hook for every read, compatible with `fs.FS`, with a permission policy per scope.
- **Filters:** ignored paths, dedup after resolving symlinks (keeping the highest position), and skipping dotfiles in
  `conf.d`.
- **Trust:** a trust level for each scope, used by the bootstrap limits, and approval through `RequireTrust(store)` in
  per-scope policies.
- **Env-var bindings** map each knob above to `{NAME}_…`. Each binding can be renamed or turned off individually.
