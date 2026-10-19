# cascadingconfig design

This document records the design decisions for `cascadingconfig`'s config discovery and merging: presets, scopes,
formats, includes, trust and the loader model. The research it builds on is in
[config-locations.md](config-locations.md). That file describes how cosmiconfig and mise search for and combine config
files, and lists where the two diverge.

Notation (`{name}`, `{env}`, `{ext}`, precedence written **lowest → highest**) is defined in
[config-locations.md](config-locations.md#notation).

## Overview

- **The default preset is `MisePreset`.** Where the two tools diverge, it follows mise, except for the deliberate
  changes listed in [Deviations from mise](#deviations-from-mise).
- **We ship three presets.** `MisePreset` and `CosmiconfigPreset` each mirror their tool as closely as Go allows. The
  main exception is that JS/TS config formats aren't supported. `MiseSecurePreset` is `MisePreset` with hardened
  settings (see [`MiseSecurePreset`](#misesecurepreset)).
- **Users can define their own presets.** A preset sets the scopes, the search paths and patterns within each scope, the
  variants, and the precedence. The built-in presets use the same API as user presets, with no special-casing.
- **Presets can extend other presets.** `MiseSecurePreset` is defined as an extension of `MisePreset`, and user presets
  can be built the same way.
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

The two presets, in these terms:

| Preset              | Scopes (lowest → highest)                | Variants          | Resolution                                            |
| ------------------- | ---------------------------------------- | ----------------- | ----------------------------------------------------- |
| `MisePreset`        | system, user, project, override          | all four          | merge all                                             |
| `MiseSecurePreset`  | as `MisePreset`                          | as `MisePreset`   | as `MisePreset`; remote includes disabled             |
| `CosmiconfigPreset` | user, project (searched project-first)   | `shared` only     | first match; the walk is off unless a stop dir is set |

## Environments

Environments select the `env` and `env-local` [variants](#scope-terminology): files like `{name}.{env}.toml` and
`{name}.{env}.local.toml`.

**Selection**, highest precedence first, as in mise (flag > `MISE_ENV` > `.miserc.toml`):

1. `WithEnvs(...)`, the app's option, normally fed from a `--env` / `-E` flag
2. `{NAME}_ENV`, a comma list
3. `env = [...]` in a bootstrap file

**Platform environments** are always active (see [Deviations from mise](#deviations-from-mise)):

- **Names:** `unix` (not on Windows), then `{os}` (`linux`, `macos`, `windows`), then `{os}-{arch}` (such as `linux-x64`
  or `macos-arm64`; `x86_64` becomes `x64` and `aarch64` becomes `arm64`).
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
  selected, and the shared file of `dev.local` when `dev.local` is selected. A name such as `local` can make one file
  match two patterns. For example, `myapp.local.toml` is both the local file and the shared file of env `local`. The
  file is loaded **once**, at its higher-precedence position, by the same dedup rule as
  [3.1](config-locations.md#31-tiers).
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
- **Errors:** an invalid name is an error at start-up from any source (`WithEnvs`, `{NAME}_ENV`, a bootstrap file). The
  error names the source and the offending character.
- **Dotted names stay valid,** so mise-style setups such as `MYAPP_ENV=dev.linux` port unchanged.
- **The app name gets the same check** (`{name}`), when the preset is built, as `env-paths` does for cosmiconfig.

## Choosing scopes

Each preset declares its default set of scopes. Two modifiers change it, and they can be used in any `Extend`:

- **`WithScopes(...)`** *replaces* the current set with exactly the scopes listed.
- **`WithoutScopes(...)`** *removes* the listed scopes from the current set.

```go
// a system daemon: only /etc/{name} (or %ProgramData%\{name})
cascadingconfig.MiseSecurePreset.Extend(
	cascadingconfig.WithScopes(cascadingconfig.ScopeSystem),
)

// everything MiseSecurePreset has, minus system config
cascadingconfig.MiseSecurePreset.Extend(
	cascadingconfig.WithoutScopes(cascadingconfig.ScopeSystem),
)
```

Modifiers apply **in order**, left to right, and across chained `Extend` calls. So
`P.Extend(WithScopes(A, B, C)).Extend(WithoutScopes(B))` selects `A` and `C`.

| Preset              | Default scopes                     |
| ------------------- | ---------------------------------- |
| `MisePreset`        | `system`, `user`, `project`, `override` |
| `MiseSecurePreset`  | `system`, `user`, `project`, `override` |
| `CosmiconfigPreset` | `user`, `project`                  |

- **Selecting, not reordering.** Both modifiers choose which of the preset's scopes are on. Their precedence stays as
  the preset defines it, whatever order they're listed in. A different order is a preset definition, not a scope choice.
- **Unknown scopes are an error.** Naming a scope the preset doesn't define fails when the option is applied, for either
  modifier.
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

**Why `MiseSecurePreset` keeps all four scopes:**

- **Its protections apply in every scope.** They limit what a file can do and decide whether it is trusted: no remotes,
  permission checks, include confinement. Narrowing the scopes adds little on top.
- **Which scopes fit depends on the app.** A CLI wants `user` and `project`; a system daemon wants `system` alone.
- **It stays a drop-in replacement.** Switching from `MisePreset` makes loading stricter without silently ignoring a
  user's own config.

## Custom scopes

Apps can define new scopes alongside `system`, `user`, `project` and `override`. Per-scope policies let each custom
scope carry its own trust level.

```go
cascadingconfig.MisePreset.Extend(
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

- **Kinds:** a custom scope is built from one of the existing kinds: `FixedDir`, `Walk` (with a stop marker, such as a
  workspace root), `ExplicitFile`, `Embedded` (a key inside a manifest), or `EmbeddedFS` / any `fs.FS`. Fully custom
  discovery implements the `Scope` interface, just as resolvers implement `IncludeResolver`.
- **Placement:** set with `Before(scope)` or `After(scope)`. Precedence follows the preset's order with the custom
  scopes slotted in.
- **Names** must be unique. A clash with any scope, built-in or custom, is an error.
- **Selection:** a custom scope is used only if `WithScopes(...)` lists it, as with the built-in scopes.
- **Env-var bindings:** none are created automatically. An app adds them explicitly, for example
  `WithEnvBinding("MOUNTED_DIR", "MYAPP_MOUNTED_CONFIG_DIR")`.
- **Trust:** a custom scope declares its trust level with `Trust(...)`. Without it, it gets project trust, the lowest
  level (see [Scope trust](#scope-trust)).

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

## Warnings and logging

The loader reports every non-fatal problem as a warning through a `*slog.Logger`. The default is `slog.Default()`, and
the app can set its own with `WithLogger(l)`. Examples:

- a config file ignored because of its permissions
  ([`MiseSecurePreset`](#misesecurepreset))
- a stale remote include used because the fetch failed
  ([Caching](#caching))
- the formats in a same-slot conflict, when the `FormatConflict` policy is `first`
  ([File format precedence](#file-format-precedence))

Each warning is also recorded on the layer it concerns, so `Describe()` and tests can see it without capturing logs.

## Directory walk

`MisePreset` walks the `project` scope upwards from the start dir:

- **Start dir inside `~`:** the walk searches `~` itself and then stops. Directories above `~` are never searched.
- **Start dir outside `~`:** for example `/srv/app`, or a container with no home above the cwd. The walk goes all the
  way to `/`.
- **Extra ceilings:** `{NAME}_CEILING_PATHS`, or `ceiling_paths` in the bootstrap file, adds exclusive ceilings on top,
  as in mise.
- **Order:** directories are applied root-most first, so the nearest directory wins.

mise already uses this rule for its `.miserc.toml` walk ([3.7](config-locations.md#37-bootstrap-file-miserctoml)). Using
it for the main search is a deliberate deviation, because real mise walks the main search to `/`. The option
`WalkStop(Root)` restores mise's exact behaviour.

The `user` scope (`~/.config/{name}`) is a separate scope and is not affected by the walk limit.

## Bootstrap file

**Each preset names its own bootstrap files**, including where they are searched and which keys they may set. The
obvious name in one preset is a config file in another: `.myapprc.json` is a normal config file in `CosmiconfigPreset`.
A user-defined preset can name any bootstrap files, or have none.

| Preset              | Bootstrap files, highest first                                                                                                                                                                 | Allowed keys |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `MisePreset`        | Walk from the start dir, with the same stop rule as above: `.{name}rc.local.{ext}`, `.{name}rc.{ext}`, `.config/{name}rc.{ext}` (not read in `~` or `/`). Then `{user}/{name}rc.local.{ext}` and `{user}/{name}rc.{ext}`. Then `{system}/{name}rc.{ext}`. | `.miserc.toml`'s keys except mise's migration flags (`env`, `ceiling_paths`, `ignored_config_paths`, `override_config_filenames`), plus `formats` |
| `MiseSecurePreset`  | As `MisePreset`.                                                                                                                                                                              | As `MisePreset`, except that **project**-scope bootstrap files may only set `env` and `formats`. |
| `CosmiconfigPreset` | The cwd only: the `cosmiconfig` key of `package.json` or `package.yaml`, then of `.config/config.{ext}`.                                                                                       | cosmiconfig's keys (`searchPlaces`, `mergeSearchPlaces`), plus `formats` |

- **Override files:** each key set in a higher bootstrap file replaces the same key from lower files. Lists are not
  merged, as in mise.
- **App config inside the meta file:** in `CosmiconfigPreset`, as in cosmiconfig, the meta file's `{name}` key is also
  checked as app config (see [2.5](config-locations.md#25-meta-config)).
- **Formats:** bootstrap files use the format order from the environment variable, the app's setting or the preset,
  never from a bootstrap file (see [File format precedence](#file-format-precedence)).

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

`MiseSecurePreset` goes further, and project-scope bootstrap files may only set `env` and `formats` (see the table
above).

## Merging

- **Rule:** objects deep-merge. Any other value, **including arrays**, is replaced by the higher layer.
- **Applies everywhere:** the same rule is used across scopes, variants, same-slot formats and includes.
- **Opt-in alternative:** array concatenation, as in cosmiconfig's `$import`, is an opt-in merge strategy.
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
| `Func`      | A custom `func(path Path, lower, higher any) (any, error)`.                    |

**Paths:**

- **Syntax:** a path is a list of key segments. The string form is dot-separated, and `*` matches exactly one segment:
  `tools.*`, `env.*`, `server.tls`.
- **Keys containing dots:** use the list form, for example `Path("plugins", "foo.bar")`.
- **Matching** is case-sensitive, because TOML, YAML and JSON keys all are.

**Matching rules:**

- **Most specific rule wins.** An exact path beats a path with wildcards, fewer wildcards beat more, and a longer path
  beats a shorter one.
- **Ambiguous rules are an error.** If two rules are still tied, the error is reported when the rule is registered, not
  while loading.
- **`Replace` and `Func` cover the whole subtree.** Rules on paths below the matched one are not consulted.
- **Type mismatches:** a rule that meets the wrong type, such as `Append` on a non-array, is a load error. The error
  names both layers' files. Null is never a mismatch (see [Null values](#null-values-yaml-and-json)).

**Where rules come from:**

- **Set by the app in code**, for example `WithMergeRule("tools.*", Replace)`. Merge rules are part of the app's schema
  contract.
- **Not set by presets, bootstrap files or env vars.** Presets describe *discovery*, not schema, so `MisePreset` ships
  with no merge rules. An app that wants mise's section semantics declares them itself.
- **Used for every merge:** across scopes, variants, same-slot formats and includes. One rule set means one result,
  whichever route a file was found by.

**Provenance:** each merged value records the layer it came from. For `Append` and `Prepend`, each element records its
own layer. For `Func`, the value is attributed to both layers. This is what lets `Describe()` answer "where did this
value come from?".

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
- **Missing keys:** unsetting a key that no lower layer set does nothing. It isn't an error, because lower layers vary
  from machine to machine.
- **Arrays:** unsetting removes the whole array. There is no way to remove single items; use a `Replace` rule or
  redefine the array.
- **Merge rules:** removal happens before [per-key merge rules](#per-key-merge-rules) run. Rules, including `Func`, only
  run when both sides still have the path after removal.
- **Never in the result:** `_unset` is stripped from the merged config. `Describe()` records each removal ("`proxy`
  unset by `~/work/app/myapp.local.toml`").
- **Applies everywhere:** across scopes, variants, same-slot formats and includes.
- **Presets:** it is on in all presets, including `CosmiconfigPreset`, where it applies to `$import` merges. This is a
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
  holds whatever per-key rule matches: `DeepMerge`, `Append` and `Prepend` all give way. `Func` is the exception: it
  receives `nil` and decides for itself.
- **Same-slot formats:** null follows the normal order. If `myapp.yaml` has `port: ~` and `myapp.toml` has `port = 80`,
  TOML is applied last, so the result is `80`. If only the YAML file sets `port`, the result is `null`.
- **Decoding into structs:** a null leaves a pointer, slice or map field `nil`, and any other field at its zero value.
  An app that needs to tell "set to null" from "not set" uses pointer fields or reads the merged tree.

## Deviations from mise

`MisePreset` is mise's discovery generalised to any app name, with these deliberate changes:

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
- **The `FormatConflict` policy** can be:
  - `merge`: the default, as described here.
  - `first`: load only the highest-ranked file and report the others as ignored.
  - `error`: fail when a slot matches more than one format.
- **Format ranking only orders files within a slot.** Across different patterns, pattern order decides as usual. For
  example, `myapp.toml` and `.myapp.yaml` are different slots. `.myapp.yaml` ranks higher because its pattern does, even
  though TOML outranks YAML.
- **Globs work the same way.** In `conf.d/*.{ext}`, the slot is the file stem. `conf.d/10-node.yaml` and
  `conf.d/10-node.toml` merge as one pair, with TOML applied last. Each stem's group is applied in alphabetical order,
  whatever its extensions.

**Default order, highest first:**

| Preset              | Format precedence                         | Why                                                                  |
| ------------------- | ----------------------------------------- | -------------------------------------------------------------------- |
| `MisePreset` (default) | `toml`, `yaml`, `yml`, `json`          | TOML wins. mise is TOML-only, so the other formats are additions.    |
| `CosmiconfigPreset` | *(none)*, `json`, `yaml`, `yml`           | cosmiconfig's own search order. *(none)* is the extensionless `.{name}rc`, parsed as YAML. TOML is not in this preset. The preset uses `FormatConflict: first`, because cosmiconfig stops at the first match. |

- `yaml` and `yml` decode the same way, but they are separate extensions. A slot containing both loads both, and `.yaml`
  is applied after `.yml`.
- A format can only appear in the list if a decoder is registered for it.
- Extensions with no decoder never match, so `.js` and `.ts` are simply never found.

**Overriding the order.** The format precedence is part of the loader's initial configuration. It can be set in four
places. Highest precedence first:

| # | Source                                   | Example                      |
| - | ---------------------------------------- | ---------------------------- |
| 1 | Environment variable                     | `MYAPP_CONFIG_FORMATS=yaml,toml` |
| 2 | Bootstrap file (see [D14](config-locations.md#4-divergences)) | `formats = ["yaml", "toml"]` |
| 3 | The app's programmatic option            | `WithFormats("toml", "json")` |
| 4 | Preset default                           | `toml, yaml, yml, json`      |

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
  are the only ones enabled, and an unknown extension is an error.
- **Locking the order:** an app author who wants to fix the order can turn off both overrides, with
  `WithoutEnvBinding("CONFIG_FORMATS")` and `WithoutBootstrapKey("formats")`.

**The environment variable:**

```text
{NAME}_CONFIG_FORMATS=yaml,toml
```

- **Name:** `{NAME}` is the app name in upper case, with any character outside `[A-Z0-9]` replaced by `_`. For example,
  `my-app` gives `MY_APP_CONFIG_FORMATS`.
- **Value:** a comma-separated list of extensions, highest precedence first. Leading dots and surrounding spaces are
  allowed: `.yaml, toml`.
- **Formats not in the list are disabled.** `MYAPP_CONFIG_FORMATS=toml` means only TOML files are found. This matches
  `MISE_OVERRIDE_CONFIG_FILENAMES`, which also *replaces* its list rather than adding to it.
- **An empty value counts as unset**, as in mise.
- **Errors:** an unknown extension, or one with no registered decoder, is an error at loader start-up, not a silent
  skip.
- **Opting out:** the app author can turn the binding off (`WithoutEnvBinding("CONFIG_FORMATS")`), as with every other
  env binding (see D17).
- **Effect on the bootstrap file:** this is the only override that also changes how the bootstrap files themselves are
  found.

## Includes

A config file can pull in other files through an include key. Every reference, local or remote, goes through a resolver
(see [Resolver hooks](#resolver-hooks)). Each preset names its key and chooses which resolvers it registers. Every
preset registers the `file` resolver (see [Local files](#local-files)).

| Preset              | Include key | Remote resolvers registered | Nesting                                   |
| ------------------- | ----------- | --------------------------- | ----------------------------------------- |
| `MisePreset`        | `include`   | `http`/`https`, `git`, `oci` | No: an included file can't include (mise) |
| `MiseSecurePreset`  | `include`   | none, so local files only   | No                                        |
| `CosmiconfigPreset` | `$import`   | none, so local files only (as cosmiconfig) | Yes, with cycle detection (as cosmiconfig) |

**Rules for every include:**

- **Value:** a string or a list of references.
- **Rank:** an included file ranks directly below the file that includes it, and a later entry beats an earlier one.
  This is mise's rule, and it is also how cosmiconfig's `$import` resolves (own keys > last import > first import).
- **Local paths** are relative to the including file. Absolute paths are allowed.
- **Merge behaviour:** `_unset`, `null` and per-key merge rules all work as in any other layer.
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
  placeholder: `ConfigRoot` (the including file's config root), `UserDir` or `SystemDir`. `MiseSecurePreset` sets
  `Confine(ConfigRoot, UserDir, SystemDir)`.
- **Reads** go through the [filesystem hook](#filesystem-hook), so they get its permission policy.
- **Turning it off:** `WithoutIncludeResolver("file")` turns off local includes, after which any local reference is a
  load error.

### Remote references

| Scheme          | Form                                   | Example                                                               |
| --------------- | -------------------------------------- | --------------------------------------------------------------------- |
| `http`, `https` | A URL                                  | `https://config.example.com/myapp/base.toml`                          |
| `git`           | `git::<repo-url>//<path>?ref=<ref>`    | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<image-ref>[//<path>]`            | `oci::ghcr.io/acme/myapp-config:1.4.0`                                |

- **Format:** the file format comes from the path's extension. For `http(s)` with no extension, it comes from
  `Content-Type`.
- **`oci` path:** it defaults to `{name}.toml` at the artifact root.

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
cascadingconfig.WithIncludeAllow( // every scheme
	cascadingconfig.Prefix("https://config.example.com/"),
)
cascadingconfig.WithIncludeAllowFor("git",
	cascadingconfig.Glob("git::https://github.com/acme/*"),
	cascadingconfig.MatchFunc(func(ref cascadingconfig.IncludeRef) bool {
		return ref.From.Scope != cascadingconfig.ScopeProject // only user/system files may fetch
	}),
)
```

- **Matchers:** `Prefix`, `Glob` and `Regexp` match the normalised reference string. `MatchFunc` gets the whole
  `IncludeRef`, including the file that asked for it.
- **Matching:** a reference must match **at least one** entry on each allow-list that applies to it, the global one and
  its scheme's.
- **No allow-list configured:** every reference is allowed in `MisePreset`, as in mise. `MiseSecurePreset` has no
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
   again from step 1. This only matters for presets that allow nesting, because `MisePreset` doesn't.
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

#### `oci`

- **No extraction to disk.** Layers are read as streams, and the requested file is found among the tar entries.
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
cascadingconfig.MiseSecurePreset.Extend(
	cascadingconfig.WithIncludeResolver(cascadingconfig.HTTPSResolver()),
	// Only system config may fetch, and only from the internal config host.
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeSystem,
		cascadingconfig.InheritDefault(),
		cascadingconfig.AllowResolvers("file", "https"),
		cascadingconfig.AllowFor("https", cascadingconfig.Prefix("https://config.acme.internal/")),
	),
	// Project config may only include files inside its own project.
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.Confine(cascadingconfig.ConfigRoot),
	),
)
```

**How policies combine:**

- **The default policy** is made of the app-level settings (`WithIncludeAllow`, `WithIncludeAllowFor` and the rest). It
  applies to every scope that has no policy of its own.
- **A scope policy replaces the default for that scope** as a whole, so a scope can be *more or less* trusted than the
  default. If policies were merged, a scope could only ever narrow the default. `InheritDefault()` starts from the
  default and changes only what follows.
- **Within one policy,** allow-lists combine as described in [Allow-lists](#allow-lists): a reference must match the
  global list and its scheme's list.
- **Registered vs usable:** registering a resolver makes it *available*. Only policies that list it in `AllowResolvers`
  can *use* it. In `MisePreset`, the default policy allows every registered resolver. In `MiseSecurePreset`, the default
  policy allows only `file`, so a remote resolver the app registers is usable only in scopes whose policy names it. That
  policy must also give the resolver an allow-list and must require pinned references.
- **Which scope judges an include:** the scope of the file that wrote it. Nested includes (`CosmiconfigPreset`) are
  judged by the scope of the root file, so an include never gains trust by being reached through another file.
- **Explicit config files** ([Explicit config files](#explicit-config-files)) are judged by the `project` policy.
- **Only the app sets policies, in code.** Bootstrap files and env vars can't, as with
  [Choosing scopes](#choosing-scopes).
- **`Describe()`** shows each scope's effective policy, and for every rejected reference, which scope's policy rejected
  it and why.

**Preset defaults:**

| Preset              | Default policy (all scopes)                                                         |
| ------------------- | ----------------------------------------------------------------------------------- |
| `MisePreset`        | Every registered resolver, everything allowed, no confinement, no permission check.  |
| `MiseSecurePreset`  | `file` only, `Confine(ConfigRoot, UserDir, SystemDir)`, the Unix permission check.   |
| `CosmiconfigPreset` | `file` only, no confinement, no permission check.                                   |

### Caching

Remote files are cached by fetched identity under `{user cache dir}/{name}/includes`:

| Reference                                   | Cache behaviour                              |
| ------------------------------------------- | -------------------------------------------- |
| Pinned (git commit SHA, OCI digest)         | Cached forever; never fetched again.          |
| Unpinned (branch, tag, plain URL)           | Fetched again once the TTL (default 1 hour) has passed. |

- **Fetch failures:** if a fetch fails and a cached copy exists, it is used and a warning is logged. With no cached
  copy, the load fails.
- **Options:** the TTL and the cache dir can be set (`WithIncludeCache(dir, ttl)`), and caching can be turned off.
- **User cache dir:** `$XDG_CACHE_HOME` on Linux, else Go's `os.UserCacheDir()`.

### `MiseSecurePreset`

```go
var MiseSecurePreset = MisePreset.Extend(
	cascadingconfig.WithoutRemoteIncludes(),
)
```

- **Remotes off:** no remote resolvers are registered. A remote reference is a **load error**, not a silent skip, so a
  config that depends on a remote base fails loudly.
- **Turning remotes back on:** an app can register a resolver again, but only per scope, through a policy that names it,
  gives it an allow-list, and requires pinned references: a git commit SHA or an OCI digest, as in mise's paranoid mode.
  Unpinned references are a load error (see [Per-scope policies](#per-scope-policies)).
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

- **`fs.FS` compatibility:** an adapter mounts any `fs.FS` (such as `fstest.MapFS` or `embed.FS`) at a virtual root, so
  a test can run the whole search against an in-memory tree.
- **Symlinks:** the hook resolves them for dedup ([3.1](config-locations.md#31-tiers)) and for `Confine`.
- **Permission policy:** `FilePermissions(policy)` is a function that gets the path, its `fs.FileInfo` (including the
  owner, where the OS reports one) and the scope. It returns *allow*, *ignore with a warning* or *error*. It runs for
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
  writes.
- **Defaults:** no preset requires trust. `MisePreset` trusts every file, as D18 decided. `MiseSecurePreset` relies on
  its other protections, and apps add `RequireTrust` where they want approval.

## Environment variables

These are `MisePreset`'s bindings. `MiseSecurePreset` uses the same ones. `{NAME}` is the app name in upper case, with
every character outside `[A-Z0-9]` replaced by `_`.

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

- **List separators:** a "path list" uses the OS separator (`:`, or `;` on Windows). A "comma list" uses `,`, with
  surrounding spaces trimmed.
- **Empty values** count as unset.
- **`XDG_CONFIG_HOME`** is read as the base of the default user dir. It isn't a binding of ours.
- **Renaming:** any binding can be renamed. For example, `WithEnvBinding("ENV", "MYAPP_CONFIG_ENV")` avoids a clash with
  an app that already uses `MYAPP_ENV` for something else.
- **Turning off:** any binding can be turned off, for example `WithoutEnvBinding("ENV")`.
- **`Describe()`** lists each binding, its variable name, and whether it is set.

## Explicit config files

Like a CLI's `--config` flag, the app or the user can name files directly:

```go
cascadingconfig.WithConfigFiles(flagValues...) // usually fed from a --config flag
```

or with `{NAME}_CONFIG_FILE=/path/a.toml:/path/b.yaml`.

- **What it replaces (`MisePreset`):** the project scope only. The directory walk is skipped, but the system, user and
  override scopes still apply.
- **What it replaces (`CosmiconfigPreset`):** everything. This matches cosmiconfig's `load(path)`, which reads only that
  file.
- **No variants:** the files are loaded as given. `.local` and `.{env}` siblings aren't added.
- **Order:** with several files, they are applied in the order given, and the later file wins.
- **Format** comes from each file's extension. An unknown extension is an error.
- **Missing files:** a file that doesn't exist is an error, as with cosmiconfig's `load(path)`.
- **Includes** in the explicit files work as usual.
- **Precedence of the setting:** `WithConfigFiles` beats `{NAME}_CONFIG_FILE`, because the option normally carries a
  command-line flag, which should beat the environment.
- **Bootstrap files** are still searched from the start dir.

## Platform directories

| Scope    | Linux / macOS                                     | Windows                                                                  |
| -------- | ------------------------------------------------- | ------------------------------------------------------------------------ |
| `system` | `/etc/{name}`                                     | `%ProgramData%\{name}`, falling back to `C:\ProgramData\{name}` if the variable is unset |
| `user`   | `$XDG_CONFIG_HOME/{name}`, else `~/.config/{name}` | the same: `%XDG_CONFIG_HOME%\{name}`, else `%USERPROFILE%\.config\{name}` |

The user dir follows mise on every OS (D4). Only the system dir differs on Windows.

## Value overrides (env vars and flags)

Single config values can be overridden from environment variables or command-line flags. This is a pair of opt-in
**custom scope kinds**, not a built-in scope. No preset includes them, because how variable names map to keys depends on
the app's schema.

```go
cascadingconfig.MisePreset.Extend(
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
- **Discovery bindings are excluded:** variables that are bound to discovery knobs, such as `MYAPP_CONFIG_DIR` or
  `MYAPP_ENV` (see [Environment variables](#environment-variables)), are never read as values, even though they share
  the prefix.
- **Read once per load:** variables are read at load time, and again on each [reload](#hot-reload).

**`Values(tree)`** takes a ready-made tree, typically built from the app's parsed flags. It may also be a function that
returns a tree, so a reload can re-read it.

**Rules for both kinds:**

- **Placement:** by convention they sit above `override`, with flags above env vars, matching the usual "flag > env >
  file" order. Placement is up to the app, like any [custom scope](#custom-scopes).
- **Reserved keys aren't interpreted:** a value that maps to the include key or `_unset` is ignored, with a warning. A
  value scope can't pull in files or remove keys.
- **Trust:** `TrustUser`. They have no bootstrap files and no includes, so trust only shows up in `Describe()`.
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

**API sketch** (the API design will settle the details):

```go
w, err := loader.Watch(ctx, func(c cascadingconfig.Change) {
	// c.Old, c.New: immutable snapshots; c.Diff: changed keys with provenance; c.Err: reload failure
})
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
| Env vars, `Values`                     | Only on a manual `Reload`.                                                                              |
| Embedded files, `embed.FS`             | Never; they're static.                                                                                  |
| Custom `Scope` implementations         | An optional `Watchable` interface; without it, only on a manual `Reload`.                               |

- **Filesystem hook:** watching goes through the [filesystem hook](#filesystem-hook). The OS filesystem uses fsnotify. A
  test filesystem provides a fake notifier, so reload behaviour can be tested without real files or timers.
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
  - **A custom HTTP client:** `HTTPSResolver(WithHTTPClient(c))` builds the `https` resolver on the app's own
    `*http.Client`. Its dialer (`net.Dialer.Control`) can check each resolved IP at connect time, which also covers DNS
    rebinding.
- **Combined-environment files** such as `myapp.dev.linux.toml`, which would load only when every environment they name
  is active, are not planned. They may be considered later, or never. Adding them would have to be **opt-in**, because
  today `dev.linux` is an ordinary single environment name, as in mise.

---

## Decision log

There are no open questions.

**Decided:**

| # | Question                                      | Decision                                                                                                      |
| - | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| 1 | Presets                                       | Ship `MisePreset`, `MiseSecurePreset` and `CosmiconfigPreset`, and let users define their own, including by extending a preset (see [Decisions](#overview)). |
| 2 | Default preset                                | `MisePreset`, with the [deviations](#deviations-from-mise) listed above.                                      |
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
| 13 | Hardened preset                              | `MiseSecurePreset` extends `MisePreset` and disables remote includes (see [`MiseSecurePreset`](#misesecurepreset)). |
| 14 | System dir on Windows                        | `%ProgramData%\{name}` (see [Platform directories](#platform-directories)).                                   |
| 15 | Env-var list                                 | As listed in [Environment variables](#environment-variables). Every binding is renameable.                      |
| 16 | Explicit config files                        | `WithConfigFiles` / `{NAME}_CONFIG_FILE` replace the project scope only (see [Explicit config files](#explicit-config-files)). |
| 17 | Writing config files                         | Out of scope for now (see [Non-goals](#non-goals)).                                                          |
| 18 | Remotes with no allow-list (`MisePreset`)    | All allowed, as in mise (see [Allow-lists](#allow-lists)).                                                   |
| 19 | Remote caching                               | Pinned references are cached forever, unpinned ones get a 1-hour TTL, and a stale copy is used with a warning if a fetch fails (see [Caching](#caching)). |
| 20 | `MiseSecurePreset` hardening                 | Permission check with a warning, local include confinement, pinned remotes if re-enabled (see [`MiseSecurePreset`](#misesecurepreset)). |
| 21 | Choosing scopes                              | Opt-in: each preset has a default set, and the app replaces it with `WithScopes(...)`, in code only. `MisePreset` and `MiseSecurePreset` default to all four (see [Choosing scopes](#choosing-scopes)). |
| 22 | Warnings                                     | Logged through `*slog.Logger` (default `slog.Default()`) and recorded per layer (see [Warnings and logging](#warnings-and-logging)). |
| 23 | Local files                                  | A built-in `file` resolver handles local includes, and one shared filesystem hook carries every read and the permission policy (see [Local files](#local-files), [Filesystem hook](#filesystem-hook)). |
| 24 | Trust per scope                              | All resolver and permission rules can be set per scope. A scope's policy replaces the default as a whole (see [Per-scope policies](#per-scope-policies)). |
| 25 | Custom scopes                                | Supported, built from the existing kinds or the `Scope` interface, placed with `Before`/`After`, project trust by default (see [Custom scopes](#custom-scopes)). |
| 26 | Bootstrap scope limits                       | Keys that hide or skip files act only in their own scope and less-trusted ones; `env` is the exception. `MiseSecurePreset` project bootstrap files may only set `env` and `formats` (see [Bootstrap file](#bootstrap-file), [Scope trust](#scope-trust)). |
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

  Users can add, remove, reorder and rename scopes. The four built-in names are conventions, not a closed set.
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
