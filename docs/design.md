# cascadingconfig design

This document records the design decisions for `cascadingconfig`'s config discovery and merging: presets, scopes,
formats, includes, trust and the loader model. The research it builds on is in
[config-locations.md](config-locations.md). That file describes how cosmiconfig and mise search for and combine config
files, and lists where the two diverge.

Notation (`{name}`, `{env}`, `{ext}`, precedence written **lowest → highest**) is defined in
[config-locations.md](config-locations.md#notation).

## Overview

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

```go
mise.Preset.Extend(
	// Built-in defaults compiled into the binary, below everything else.
	cascadingconfig.WithScope("defaults",
		cascadingconfig.EmbeddedFS(defaultsFS, "{name}.toml"),
		cascadingconfig.Before(cascadingconfig.ScopeSystem),
		cascadingconfig.Trust(cascadingconfig.TrustSystem),
	),
	// Kubernetes-mounted config, above user config.
	cascadingconfig.WithScope("mounted",
		cascadingconfig.FixedDir("/run/secrets/{name}", "config.{ext}"),
		cascadingconfig.After(cascadingconfig.ScopeUser),
	),
	cascadingconfig.WithScopes(
		"defaults", cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser, "mounted",
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
  implement `IncludeResolver`.
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
- **Selection:** a custom scope is used only if `WithScopes(...)` lists it, as with the built-in scopes. Defining a
  scope doesn't select it.
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
- **`Describe()`** records both routes.

## Warnings and logging

The loader reports every non-fatal problem as a warning through a `*slog.Logger`. The default is `slog.Default()`, and
the app can set its own with `WithLogger(l)`. Examples:

- a config file ignored because of its permissions
  ([`misehardened.Preset`](#misehardenedpreset))
- a stale remote include used because the fetch failed
  ([Caching](#caching))
- the formats in a same-slot conflict, when the `FormatConflict` policy is `first`
  ([File format precedence](#file-format-precedence))

Each warning is also recorded on the layer it concerns, so `Describe()` and tests can see it without capturing logs.

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
  `conf.d/*.{env}.{ext}` reads its environment from the dot. In a directory segment it matches any name that doesn't
  start with a dot. Matches apply in alphabetical order, and each stem is its own slot. Other glob characters are
  literal, and `**` is an `*OptionError`.
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
config of `~`.

## `MiseLayout`'s scopes

`MiseLayout` defines four scopes, lowest precedence first, and selects them all (DL-124). Every pattern uses `{ext}`
where mise has `.toml`.

| Scope      | Kind                  | Patterns                                                                                     | Options                                   |
| ---------- | --------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------- |
| `system`   | `FixedDir("{system}")` | mise's global layout ([3.4](config-locations.md#34-global-directory))                       | `Trust(TrustSystem)`, `ReplacedByFile(BindSystemConfigFile)` |
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
| `mise.Preset`        | Walk from the start dir, with the same stop rule as above: `.{name}rc.local.{ext}`, `.{name}rc.{ext}`, `.config/{name}rc.{ext}` (not read in `~` or `/`). Then `{user}/{name}rc.local.{ext}` and `{user}/{name}rc.{ext}`. Then `{system}/{name}rc.{ext}`. | `.miserc.toml`'s keys except mise's migration flags (`env`, `ceiling_paths`, `ignored_config_paths`, `override_config_filenames`), plus `formats` |
| `misehardened.Preset`  | As `mise.Preset`.                                                                                                                                                                              | As `mise.Preset`, except that **project**-scope bootstrap files may only set `env` and `formats`. |
| `cosmiconfig.Preset` | The cwd only: the `cosmiconfig` key of `.config/config.{ext}`. cosmiconfig also reads `package.json` and `package.yaml`; we don't (see [Deviations from cosmiconfig](#deviations-from-cosmiconfig)). | cosmiconfig's keys (`searchPlaces`, `mergeSearchPlaces`), plus `formats` |

- **Override files:** each key set in a higher bootstrap file replaces the same key from lower files. Lists are not
  merged, as in mise.
- **Relative paths** in a bootstrap file (`ceiling_paths`, `ignored_config_paths`) are resolved against that file's
  directory, as mise does, and a leading `~` is expanded.
- **App config inside the meta file:** in `cosmiconfig.Preset`, as in cosmiconfig, the meta file's `{name}` key is also
  checked as app config (see [2.5](config-locations.md#25-meta-config)).
- **Formats:** bootstrap files use the format order from the environment variable, the app's setting or the preset,
  never from a bootstrap file (see [File format precedence](#file-format-precedence)).
- **Unknown keys** are a warning, so a newer bootstrap file doesn't break an older app.
- **Keys outside the limits** (the scope limits below, or `misehardened.Preset`'s project limits) are dropped, with a
  warning naming the file and the key.
- **Wrong types**, such as `env = "dev"` instead of a list, are an `*OptionError` at `Load`.
- **No includes and no `_unset`.** Bootstrap files are read before the main search, so they stay simple. Either key is
  an unknown key, with a warning.

**Scope limits.** A bootstrap file belongs to the scope it was found in. Any key that can **hide or skip** files only
takes effect in that scope and in less-trusted scopes (see [Scope trust](#scope-trust)). Those keys are
`ignored_config_paths`, `formats`, `ceiling_paths` and `override_config_filenames`. So a project's bootstrap file can't
make the loader skip `/etc/{name}/config.toml`:

- **`ignored_config_paths`:** an entry that points into a more trusted scope is dropped, with a warning naming the
  bootstrap file.
- **`formats`:** from a project bootstrap file, it orders and filters formats in the `project` scope only. More trusted
  scopes keep the order from the env var, the app's setting or the preset.
- **`ceiling_paths` and `override_config_filenames`:** these only ever act on the `project` walk, so they are already
  within the limit for every bootstrap file.
- **`env` is the exception.** It applies in every scope, as in mise. It doesn't hide files; it chooses among variants
  such as `config.{env}.toml` that each scope's own author wrote.

`misehardened.Preset` goes further, and project-scope bootstrap files may only set `env` and `formats` (see the table
above).

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
that several layers contributed to is `Merged` (see [`Origin`](api.md#result-report-and-origin)). This is what lets
`Describe()` answer "where did this value come from?".

#### Example: mise's section semantics as rules

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
| No `package.json` or `package.yaml`: no `{name}` property lookup (`packageProp`), and no `cosmiconfig` key in them for the meta config. | Node.js manifests. Users can add a manifest source with the `Embedded` scope kind, for example `package.json#{name}`. |
| The `Project` search strategy has no default root marker.              | cosmiconfig stops at the first directory containing `package.json`/`package.yaml`. Here the app names its own marker with `WithRootMarker`, for example `.git` or `go.mod`. |
| `_unset` removes inherited keys ([Unsetting keys](#unsetting-keys)).    | cosmiconfig would pass `_unset` through as data.                                     |

### Search strategies

These options mirror cosmiconfig's `searchStrategy` and `stopDir`:

| Strategy                           | Directories searched                                                                                  | Scopes            |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------------- |
| `WithSearchStrategy(None)` (default) | The start dir only.                                                                                 | `project`         |
| `WithSearchStrategy(Project)`      | The start dir, then each parent, up to and **including** the first directory containing a root marker set with `WithRootMarker(names...)`, or up to `/`. | `project` |
| `WithSearchStrategy(Global)`       | The start dir, then each parent, up to and **including** the stop dir (`WithStopDir(dir)`, default `~`), or up to `/` if the stop dir isn't an ancestor. Then the user dir. | `project`, `user` |

- **Invalid combinations are `*OptionError`s,** as in cosmiconfig: `WithStopDir` with a strategy other than `Global`,
  and `Project` with no root marker.
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
| `cosmiconfig.Preset` | *(none)*, `json`, `yaml`                  | cosmiconfig's own search order. *(none)* is the extensionless `.{name}rc`, decoded as set by `WithExtensionlessFormat`: YAML if a YAML decoder is registered, otherwise JSON. TOML is not in this preset. The preset uses `FormatConflict: first`, because cosmiconfig stops at the first match. |

- **The order lists formats, not extensions.** One format can have several extensions: YAML is both `.yaml` and `.yml`.
- **Within a format, extensions have a fixed rank.** For YAML, `.yaml` ranks above `.yml`, because `yaml` sorts first
  alphabetically. A slot containing both loads both under the default `merge` policy, with `.yaml` applied last so it
  wins where they overlap. Under `first`, only `.yaml` loads. Under `error`, the pair is a `*ConflictError`.
- **Extensionless files** (cosmiconfig's `.{name}rc`) are decoded by the format that `WithExtensionlessFormat(format)`
  names. With no such setting, extensionless candidates aren't searched.
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
  others. Lists aren't merged, just as mise's `env` key replaces the inherited list.
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

## Format dialects

What each built-in decoder accepts, and the rules every decoder follows. These were checked against the pinned libraries
(go-toml v2.4.3, goccy/go-yaml v1.19.2, `encoding/json`).

| Rule                  | JSON                                              | TOML                                     | YAML                                      |
| --------------------- | ------------------------------------------------- | ---------------------------------------- | ----------------------------------------- |
| Dialect               | Strict RFC 8259: no comments, no trailing commas, as cosmiconfig's `JSON.parse` | Whatever the pinned go-toml accepts. v2.4.3 already accepts TOML 1.1 syntax (multi-line inline tables, times without seconds, `\e`). | YAML 1.2 core schema: `yes`, `no`, `on`, `off` and `y` stay **strings**. |
| Duplicate keys        | The **last** one wins, as in `JSON.parse` and `encoding/json`, and a **warning** is logged | `*ParseError`, as the TOML spec requires | `*ParseError`, as the YAML spec requires   |
| Non-string map keys   | n/a                                               | n/a                                      | Turned into strings (`1:` becomes `"1"`), as goccy does |
| Several documents     | n/a                                               | n/a                                      | `*ParseError`. goccy would silently keep only the first. |
| Aliases               | n/a                                               | n/a                                      | Allowed, with a cap on expansion, so a hostile file can't expand without bound |
| Unknown tags          | n/a                                               | n/a                                      | `*ParseError`                             |

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
- **Dates and times are strings** in the tree, in RFC 3339 form. That includes TOML's offset and local date-times (a
  TOML `1979-05-27 07:32:00Z` becomes `"1979-05-27T07:32:00Z"`). The choice of type is deferred to decoding, so a
  `string` field gets the text and a `time.Time` field gets a parsed time ([api.md](api.md#decoding)).

## Includes

A config file can pull in other files through an include key. Every reference, local or remote, goes through a resolver
(see [Resolver hooks](#resolver-hooks)). Each preset names its key and chooses which resolvers it registers. Every
preset registers the `file` resolver (see [Local files](#local-files)).

| Preset              | Include key | Remote resolvers registered | Nesting                                   |
| ------------------- | ----------- | --------------------------- | ----------------------------------------- |
| `mise.Preset`        | `include`   | `http`/`https`, `git`, `oci` | No: an included file can't include (mise) |
| `misehardened.Preset`  | `include`   | none, so local files only   | No                                        |
| `cosmiconfig.Preset` | `$import`   | none, so local files only (as cosmiconfig) | Yes, with cycle detection (as cosmiconfig) |

**Rules for every include:**

- **Placement:** the include key only works at the **top level** of a file, as in mise and cosmiconfig. It's removed
  from the result, as `_unset` is. Elsewhere in a file, the key is ordinary data.
- **Value:** a string or a list of references.
- **Rank:** an included file ranks directly below the file that includes it, and a later entry beats an earlier one.
  This is mise's rule, and it is also how cosmiconfig's `$import` resolves (own keys > last import > first import).
- **Local paths** are relative to the including file. Absolute paths are allowed.
- **Formats:** an included file loads with any registered decoder, whatever the format order. A format with no
  registered decoder is an `*UnsupportedFormatError` (415).
- **Merge behaviour:** `_unset`, `null` and per-key merge rules all work as in any other layer.
- **Each file loads once per include chain.** A file included twice along one chain, as in a diamond, loads at its first
  rank. The same file can still appear at another rank through a different chain, or be found directly as well; each
  appearance is recorded in `Describe()`.
- **No includes from embedded sources.** `Embedded` and `EmbeddedFS` sources, and `Values`/`EnvVars`, can't use the
  include key, because there's no file location to resolve a relative reference against. The key is ordinary data there.
- **Trust:** an included file inherits its includer's trust decision, as in mise
  ([3.9](config-locations.md#39-includes)). It's still judged by its includer's scope policy
  ([Per-scope policies](#per-scope-policies)).
- **Errors:** a reference that can't be loaded is a load error naming the including file. Includes are never silently
  skipped, because silently dropping config is worse than failing.

### Local files

Local includes are handled by a built-in `file` resolver. It implements `IncludeResolver` just like the remote
resolvers, and every preset registers it.

- **References:** plain paths (`base.toml`, `../shared/base.toml`, `/etc/acme/base.toml`) and `file://` URLs both go to
  this resolver. A relative path resolves against the including file's directory.
- **Allow-lists:** it accepts the same matchers as the remote resolvers. They are matched against the absolute path
  after symlinks are resolved.
- **`Confine(roots...)`:** the resolved path must lie under one of the roots. A root can be a literal path or a
  placeholder: `ConfigRoot` (the including file's config root), `UserDir` or `SystemDir`. `misehardened.Preset` sets
  `Confine(ConfigRoot, UserDir, SystemDir)`.
- **Reads** go through the [filesystem hook](#filesystem-hook), so they get its permission policy.
- **Turning it off:** `WithoutIncludeResolver("file")` turns off local includes, after which any local reference is a
  load error.

### Remote references

| Scheme          | Form                                   | Example                                                               |
| --------------- | -------------------------------------- | --------------------------------------------------------------------- |
| `http`, `https` | A URL, optionally with `#sha256=<hex>`  | `https://config.example.com/myapp/base.toml#sha256=9f86d0…`           |
| `git`           | `git::<repo-url>//<path>?ref=<ref>`    | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<image-ref>[//<path>]`            | `oci::ghcr.io/acme/myapp-config:1.4.0`                                |

- **Format:** the file format comes from the path's extension. For `http(s)` with no extension, it comes from
  `Content-Type`.
- **`oci` path:** it defaults to `{name}.toml` at the artifact root.

**Pinned references** name content that can't change. A policy with `RequirePinned()` accepts only these:

| Scheme          | Pinned when                                                                                              |
| --------------- | -------------------------------------------------------------------------------------------------------- |
| `git`           | `ref=` is a **full** commit id: 40 hex characters (SHA-1) or 64 (SHA-256). An abbreviated id isn't a pin. |
| `oci`           | The image reference has a digest: `@sha256:<64 hex>`.                                                    |
| `http`, `https` | The URL has an integrity fragment, `#sha256=<64 hex>`. The downloaded bytes are hashed and must match, or the fetch is a `*PolicyError`. The fragment is never sent to the server. |

### Resolver hooks

Remote fetching is done by resolvers. The three built-in resolvers are ordinary implementations of the same interface:

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
	Location string // URL, repo URL or image ref, normalised
	Path     string // path inside a repo or artifact, if any
	Version  string // git ref, tag or digest, if any
	From     Layer  // the including file: path, scope, variant
}

type Fetched struct {
	Content  []byte
	Format   string // extension, e.g. "toml"
	Identity string // canonical identity, e.g. commit SHA or digest
}
```

The app can add a resolver for a new scheme or replace a built-in one with `WithIncludeResolver(r)`, and can remove one
with `WithoutIncludeResolver("git")`.

### Allow-lists

Each resolver can have an allow-list. There is also an allow-list that applies to every scheme:

```go
cascadingconfig.WithDefaultPolicy(
	cascadingconfig.Allow( // every scheme
		cascadingconfig.Prefix("https://config.example.com/"),
	),
	cascadingconfig.AllowFor("git",
		cascadingconfig.Glob("git::https://github.com/acme/*"),
		cascadingconfig.MatchFunc(func(ref cascadingconfig.IncludeRef) bool {
			return ref.From.Scope != cascadingconfig.ScopeProject // only user/system files may fetch
		}),
	),
)
```

- **Matchers:** `Prefix`, `Glob` and `Regexp` match the normalised reference string. `MatchFunc` gets the whole
  `IncludeRef`, including the file that asked for it.
  - **`Glob`:** `*` matches within one `/`-separated segment, and `**` matches across segments. So
    `git::https://github.com/acme/*` matches `…/acme/config.git` but not `…/acme/team/config.git`, and
    `git::https://github.com/acme/**` matches both. `?` matches one character, and there are no other metacharacters.
  - **`Regexp`** is anchored at both ends: it must match the **whole** normalised string.
  - **`Prefix`** is a plain string prefix.

**The normalised reference string** that matchers see:

| Scheme          | Form                                                    | Example                                                                 |
| --------------- | ------------------------------------------------------- | ----------------------------------------------------------------------- |
| `http`, `https` | `scheme://host[:port]/path`. The host is lowercased, the default port and dot segments are removed, and the integrity fragment is dropped. | `https://config.acme.internal/myapp/base.toml` |
| `git`           | `git::<repo-url>//<path>?ref=<ref>`, with the repo URL normalised like an http URL and the inner path cleaned. | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<registry>/<repository>[:<tag>][@<digest>]//<path>`. The registry is lowercased, and the inner path is always present (`{name}.toml` by default). | `oci::ghcr.io/acme/myapp-config:1.4.0//myapp.toml` |
| `file`          | The absolute path, after resolving symlinks.            | `/etc/acme/base.toml`                                                   |

- **Matching:** a reference must match **at least one** entry on each allow-list that applies to it, the global one and
  its scheme's.
- **No allow-list configured:** every reference is allowed in `mise.Preset`, as in mise. `misehardened.Preset` has no
  remotes at all, and requires an allow-list if an app turns them back on.
- **No match is a load error** naming the including file, never a silent skip.
- **Redirects:** the `http(s)` resolver checks the allow-list again for each redirect target.
- **`Describe()`** shows each remote layer's raw reference and its fetched identity.

### Traversal protection

Remote references carry paths and URLs written in config files, so every resolver must stop them from reaching outside
the repo, artifact or origin they name. Some checks are enforced by the **loader**, around every resolver. The rest are
per-scheme rules for the **built-in** resolvers.

#### Enforced by the loader, for every resolver

These checks also cover resolvers that apps write themselves:

1. **Before matching:** the reference is normalised and validated **before** it is matched against allow-lists, so a
   reference can't pass an allow-list in one form and be fetched in another:
   - **URLs:** parsed, dot segments resolved (RFC 3986 §5.2.4), and the host lowercased and IDNA-normalised, with any
     trailing dot and default port removed.
   - **URLs rejected outright:** a URL with userinfo is rejected, because `https://config.acme.internal@evil.example/`
     would otherwise match a `Prefix` on the internal host. So is a URL with a percent-encoded `.`, `/` or `\` in its
     path (`%2e`, `%2f`, `%5c`).
   - **Inner paths** (the `//<path>` of `git::` and `oci::`) are cleaned with `path.Clean` and must then satisfy
     `fs.ValidPath`, which rules out a leading `/` and `..` segments. They must not contain `\`, NUL or control
     characters, and the extension must be one with a registered decoder.
2. **After fetching:** the `Fetched` result is checked. `Format` must match the validated path's extension (or, for
   `http(s)` URLs with no extension, an allowed `Content-Type`). `Content` must be within the size limit, 1 MiB by
   default and configurable.
3. **Relative references inside a remote file** resolve against that file's **remote origin**: the same repo and ref,
   the same artifact, or the same URL origin. They never resolve against the local filesystem, and they are checked
   again from step 1. This only matters for presets that allow nesting, because `mise.Preset` doesn't.
4. **Cache paths** are built from a hash of the fetched identity, never from any part of the URL, repo or path. Cache
   files are written through `os.Root` on the cache dir, by atomic rename.

#### `git`

- **No worktree.** The file is read from the object database (`git cat-file` on `<commit>:<path>`), not from a
  checked-out tree. There are no files on disk for a symlink to redirect.
- **Regular files only:** the tree entry's mode must be `100644` or `100755`. A symlink (`120000`) or submodule
  (`160000`) is rejected, even if it points inside the repo. This is stricter than mise, which canonicalises the path in
  a checkout and rejects it if it escapes or isn't a regular file (`validate_remote_git_path`).
- **No option injection:** the ref is resolved to a full commit SHA with `git rev-parse --verify --end-of-options`, and
  refs starting with `-` are rejected.
- **Transports:** only `https` and `ssh` are allowed (`GIT_ALLOW_PROTOCOL=https:ssh`). That blocks `ext::`, `fd::`,
  `file://` and local-path remotes.
- **No side effects:** fetches are shallow, of that commit only. They use no submodules, no LFS or other filters, and
  `core.hooksPath` is set to an empty directory.
- **Isolated config:** git runs with `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL` pointing at an empty file, so the
  user's and the system's git config never apply. Otherwise a `url.<base>.insteadOf` rule could rewrite a URL *after*
  the allow-list check, and `includeIf` or credential helpers would run. Credential helpers are opt-in, with
  `gitresolver.WithCredentialHelper(...)`.
- **Version:** git 2.24 or newer is required, for `--end-of-options`. An older git is a `*FetchError` that says so.
- **Fetching by commit:** some servers refuse to fetch an arbitrary commit id. Then the resolver fetches the ref
  instead, and checks the commit is reachable from it.
- **Protocols:** `gitresolver.WithProtocols(...)` changes the allowed transports. The default is `https` and `ssh`.
  Tests use it to allow `file` for repositories in temporary directories.

#### `oci`

- **Two artifact forms.** An artifact pushed with `oras push` stores each file as its own blob, named by the
  `org.opencontainers.image.title` annotation. An image-style artifact stores files in tar layers. Both are supported:
  the requested path is matched against the title annotations, and against the entries of tar layers.
- **No extraction to disk.** Layers are read as streams, and the requested file is found among them.
- **Duplicates:** if the same path appears in several layers, the **last** layer wins, as in an image.
- **Decompression is capped** at the include size limit, so a small compressed layer can't expand without bound.
- **Options:** `ociresolver.WithCredentials(...)` supplies registry credentials. By default none are sent, and
  `~/.docker/config.json` isn't read. `ociresolver.WithPlainHTTP(hosts...)` allows plain-HTTP registries, for tests and
  local registries only.
- **Entry matching:** each entry name is cleaned and compared to the validated inner path. An entry with an absolute
  name or a `..` segment fails the whole fetch, even if it isn't the requested file, because it means the artifact is
  malformed or hostile.
- **Regular files only:** the matched entry must be a regular file (`TypeReg`). Symlinks, hard links, devices and
  directories are rejected.
- **Digests:** every blob's digest is verified as it streams, and the manifest is resolved to a digest before anything
  is read.

#### `http` / `https`

- **Redirects:** each hop is normalised and matched against the allow-list again (see [Allow-lists](#allow-lists)).
  There are at most 5 hops, and a redirect from `https` to `http` is rejected.
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
		cascadingconfig.Confine(cascadingconfig.ConfigRoot),
	),
)
```

**How policies combine:**

- **The default policy** is set with `WithDefaultPolicy(...)`, which takes the same options as `WithScopePolicy`. It
  applies to every scope that has no policy of its own.
- **A scope policy replaces the default for that scope** as a whole, so a scope can be *more or less* trusted than the
  default. If policies were merged, a scope could only ever narrow the default. `InheritDefault()` starts from the
  default and changes only what follows.
- **Within one policy,** allow-lists combine as described in [Allow-lists](#allow-lists): a reference must match the
  global list and its scheme's list.
- **Registered vs usable:** registering a resolver makes it *available*. Only policies that list it in `AllowResolvers`
  can *use* it. In `mise.Preset`, the default policy allows every registered resolver. In `misehardened.Preset`, the
  default policy allows only `file`, so a remote resolver the app registers is usable only in scopes whose policy names
  it. That policy must also give the resolver an allow-list and must require pinned references.
- **Which scope judges an include:** the scope of the file that wrote it. Nested includes (`cosmiconfig.Preset`) are
  judged by the scope of the root file, so an include never gains trust by being reached through another file.
- **Explicit config files** ([Explicit config files](#explicit-config-files)) are judged by the `project` policy.
- **Only the app sets policies, in code.** Bootstrap files and env vars can't, as with
  [Choosing scopes](#choosing-scopes).
- **`Describe()`** shows each scope's effective policy, and for every rejected reference, which scope's policy rejected
  it and why.

**Preset defaults:**

| Preset              | Default policy (all scopes)                                                         |
| ------------------- | ----------------------------------------------------------------------------------- |
| `mise.Preset`        | Every registered resolver, everything allowed, no confinement, no permission check.  |
| `misehardened.Preset`  | `file` only, `Confine(ConfigRoot, UserDir, SystemDir)`, the Unix permission check.   |
| `cosmiconfig.Preset` | `file` only, no confinement, no permission check.                                   |

### Caching

Remote files are cached under `{user cache dir}/{name}/includes`:

| Reference                                                  | Cache behaviour                                         |
| ---------------------------------------------------------- | ------------------------------------------------------- |
| [Pinned](#remote-references) (full git commit id, OCI digest, https integrity fragment) | Cached forever; never fetched again. |
| Unpinned (branch, tag, plain URL)                          | Fetched again once the TTL (default 1 hour) has passed. |

**How entries are found:**

- **Content** is stored under its fetched identity: the commit id for git, the blob digest for OCI, and the SHA-256 of
  the bytes for http(s).
- **An index** maps each reference to its identity. It's keyed by the SHA-256 of the normalised reference string, so no
  part of a URL or path ever becomes part of a file name. That's what lets a fetch that fails fall back to a stale copy,
  and lets the TTL be checked before fetching.
- **Rechecked on use:** cached content goes through every policy check again on each load: allow-lists, pinning, the
  size limit and the trust store.
- **Fetch failures:** if a fetch fails and a cached copy exists, it is used and a warning is logged. With no cached
  copy, the load fails.
- **Options:** `WithIncludeCache(dir, ttl)` sets the cache dir and TTL, and `WithoutIncludeCache()` turns caching off.
- **User cache dir:** `$XDG_CACHE_HOME` on Linux, else Go's `os.UserCacheDir()`.

### `misehardened.Preset`

```go
// core: the layout (no dependencies)
var MiseHardenedLayout = MiseLayout.Extend(
	cascadingconfig.WithoutRemoteIncludes(),
	// … plus the permission, confinement and pinning rules below
)

// presets module: the layout plus TOML, YAML and fsnotify (no remote resolvers)
var Preset = cascadingconfig.MiseHardenedLayout.Extend( /* decoders, notifier */ )
```

`misehardened.Preset` is `cascadingconfig.MiseHardenedLayout` with TOML, YAML and fsnotify added. The layout itself is
`MiseLayout` with the changes below.

- **Remotes off:** no remote resolvers are registered. A remote reference is a **load error**, not a silent skip, so a
  config that depends on a remote base fails loudly.
- **Turning remotes back on:** an app can register a resolver again, but only per scope, through a policy that names it,
  gives it an allow-list, and requires [pinned references](#remote-references): a full git commit id, an OCI digest, or
  an https integrity fragment, as in mise's paranoid mode. Unpinned references are a load error (see
  [Per-scope policies](#per-scope-policies)).
- **File permissions (Unix):** the [filesystem hook](#filesystem-hook) ignores config files and directories, with a
  warning, if they're writable by group or other, or owned by someone other than the current user or root. This is like
  SSH's `StrictModes`. The warning names the file and the reason, for example "`/srv/app/myapp.toml` ignored: writable
  by group". Ignored files appear in `Describe()`. Windows ACLs aren't checked yet.
- **Local include confinement:** the `file` resolver's `Confine(ConfigRoot, UserDir, SystemDir)` option. A local include
  that resolves elsewhere, after following symlinks, is a load error (see [Local files](#local-files)).
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
- **Missing directories** load nothing, without a warning.
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
- **Permission policy:** `FilePermissions(policy)` is a function that gets the path, its `fs.FileInfo` (including the
  owner, where the OS reports one) and the scope. For a symlink, it checks the **target and its containing directory**.
  The owner is read through an `Owner() (uid int, ok bool)` method on the file info, so `cctest.FS` can fake it and
  platforms without owners report `ok == false`. It returns *allow*, *ignore with a warning* or *error*. It runs for
  **every** file, whether the search found it or it was included. It can be set per scope (see
  [Per-scope policies](#per-scope-policies)). For example, an app can require `system` files to be owned by root and
  `project` files to be owned by the user.

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

cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
	cascadingconfig.InheritDefault(),
	cascadingconfig.RequireTrust(store),
)
```

- **Untrusted files** are skipped with a warning, and listed in `Describe()` as untrusted. The loader never prompts the
  user; asking for approval and recording it is the app's job, through its store.
- **No store is shipped.** We don't write files (see [Non-goals](#non-goals)), so a persistent store is the app's
  concern. A `TrustPaths(globs...)` store, equivalent to mise's `trusted_config_paths`, is provided because it needs no
  writes. Its globs follow the same rules as the [`Glob` matcher](#allow-lists), applied to absolute paths after
  resolving symlinks.
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

```go
cascadingconfig.WithConfigFiles(flagValues...) // usually fed from a --config flag
```

or with `{NAME}_CONFIG_FILE=/path/a.toml:/path/b.yaml`.

- **What it replaces (`mise.Preset`):** the project scope only. The directory walk is skipped, but the system, user and
  override scopes still apply. Generally, the files replace the scope marked `ReplacedByConfigFiles()`, at its position,
  even when it isn't selected, and take `TrustProject` and that scope's name. With no marked scope, they go above every
  scope (DL-126).
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
  `MYAPP_SERVER__PORT`".

## Transform hooks

Apps can rewrite config trees with transform hooks, for example to expand templates such as `{{ config_root }}` or
`${HOME}`. **No template language is built in.**

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
  - **When:** runs on **each layer** after it is decoded and before `_unset` and merging.
  - **What it gets:** the layer's metadata (path, scope, config root, format, env). That gives it each file's own config
    root, which a transform after merging couldn't know.
  - **Coverage:** included layers are transformed too. Bootstrap files are not.
- **`WithResultTransform`** runs once on the merged result. It is the counterpart of cosmiconfig's `transform` option.
- **Order:** several transforms run in registration order.
- **Errors:** an error is a load error. For a layer transform, the error names the file.
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

```go
w, err := cascadingconfig.Watch[Config](ctx, loader, func(c cascadingconfig.Change[Config]) {
	// c.Old, c.New: decoded snapshots; c.Diff: changed keys with provenance; c.Err: reload failure
}, cascadingconfig.WithDebounce(100*time.Millisecond, time.Second))
defer w.Close()
cfg := w.Current() // lock-free read of the latest good snapshot
```

**Watch set:**

- **Directories, not files.** The loader watches every directory where a candidate file *could* appear: each walk
  directory, and each pattern's parent within it, such as `.config/{name}/` and `{name}/conf.d/`. It also watches the
  user and system dirs, the bootstrap locations, and the directories of explicit config files.
- **Missing directories:** for a directory that doesn't exist yet, the loader watches its nearest existing ancestor
  within the same walk directory. Once the directory appears, the watch set is re-planned.
- **Filtering:** an event is relevant only if its name matches a candidate pattern (including `conf.d` globs) or is a
  directory on the path to one. Chmod events are ignored.
- **Symlink swaps:** for any event in a directory that holds a symlinked candidate, the symlink is resolved again, and a
  changed target counts as a change. This covers Kubernetes ConfigMap `..data` swaps, as Viper and koanf do.
- **Re-planned after every reload,** because a reload can change the ceiling, the selected envs, the formats or the
  folder fragments.

**Reloading:**

- **Debounced:** events are coalesced on the trailing edge, with a 100 ms quiet period and at most 1 s of delay, so one
  save means one reload.
- **A reload works exactly like an initial load:** it decodes into a fresh value, never the previous snapshot, with
  defaults applied the same way ([api.md](api.md#defaults)).
- **Full re-discovery,** not an incremental patch. Bootstrap, discovery, policies (permissions, confinement, trust),
  transforms and merging all run again. Precedence and dedup depend on the whole set of files, and config is small.
- **Last good config wins:** if a reload fails (a parse error, a policy violation or a transform error), `Current()`
  keeps the last good snapshot. The callback gets `Change.Err`, a warning is logged, and the next event retries. A
  half-written file therefore never replaces good config. The loader never exits the process.
- **Callbacks only on real change:** the callback runs only when the merged result changes. Touching a file or editing a
  comment doesn't trigger it.
- **One at a time:** callbacks are serialised, and events keep coalescing while a callback runs.
- **Manual reload:** `w.Reload(ctx)` forces one, for example on `SIGHUP`. It also re-reads env vars and `Values`
  functions, which can't be watched.

**What can't be watched by events:**

| Source                                 | How it refreshes                                                                                       |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Remote includes                        | Pinned references never change. Unpinned ones are re-checked in the background only with `WithRemotePoll(interval)`, using ETags, `git ls-remote` or the OCI manifest digest. Otherwise they refresh on reloads, under the [cache rules](#caching). |
| NFS, SMB and FUSE mounts; watcher limits reached | Built-in stat polling (mtime, size, inode, then a content hash), every 2 s by default. It's used with `WithPolling(interval)`, and automatically for any directory whose watch fails, with a warning.                                                     |
| Env vars, `Values`                     | Re-read on every reload, but changes to them never trigger one. Use a manual `Reload`.                  |
| Embedded files, `embed.FS`             | Never; they're static.                                                                                  |
| Custom `Scope` implementations         | An optional `Watchable` interface; without it, only on a manual `Reload`.                               |

- **Event sources:** the core has no fsnotify dependency. Without a notifier, watching polls. The `watch/fsnotify`
  module supplies file events through `WithNotifier`, which the ready-to-use presets register. Tests use
  `cctest.Notifier` and `cctest.Clock`, so reload behaviour can be tested without real files or timers.
- **Security on reload:** every check runs again. A file that becomes group-writable is ignored on the next reload, with
  a warning, and a revoked trust approval takes effect there too.

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
  [future-ideas.md](future-ideas.md).
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
| 48 | Dates and times                              | Strings in the tree, in RFC 3339 form, for every format. A decode hook parses them into `time.Time` fields. This differs from koanf with TOML, which hands `time.Time` through (see [Format dialects](#format-dialects)). |
| 49 | Integers                                     | `int64` only for literals with no fraction or exponent that fit; out of range is a `*ParseError`.             |
| 50 | YAML dialect                                 | YAML 1.2 core schema. Keys become strings. Several documents, and unknown tags, are `*ParseError`s. Alias expansion is capped. |
| 51 | Duplicate keys                               | Each format's own semantics: JSON keeps the last and logs a warning; TOML and YAML raise a `*ParseError`.       |
| 52 | Dialects and encoding                        | Strict JSON (JSONC is a [future idea](future-ideas.md)); TOML as the pinned go-toml accepts; UTF-8 only, with a leading byte-order mark stripped. |
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
| 106 | Splitting M4                                | Four parts: M4a filesystem and loading; M4b patterns, environments and the walk, with `MiseLayout`'s scopes; M4c formats and bootstrap; M4d `CosmiconfigLayout` ([implementation-plan.md](implementation-plan.md#m4-discovery)). |
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
