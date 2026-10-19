# Config file locations

This document lists every place that [cosmiconfig] and [mise] look for configuration, the order they look in, and how
they combine what they find. It is the research behind `cascadingconfig`'s default search behaviour and the extension
points our loader exposes. The decisions themselves are recorded in [design.md](design.md).

Behaviour was checked against source as well as the published docs:

| Tool        | Version checked | Source of truth                                                                    |
| ----------- | --------------- | ---------------------------------------------------------------------------------- |
| cosmiconfig | v10.0.1         | `src/defaults.ts`, `src/Explorer.ts`, `src/ExplorerBase.ts`, `src/index.ts`         |
| mise        | v2026.10.15     | `src/config/mod.rs`, `src/config/miserc.rs`, `crates/mise-util/src/{env,file}.rs`  |

Where the docs and the source disagree, this document follows the source and says so.

[cosmiconfig]: https://github.com/cosmiconfig/cosmiconfig
[mise]: https://mise.jdx.dev/configuration.html

## Notation

| Token    | Meaning                                                                                       |
| -------- | --------------------------------------------------------------------------------------------- |
| `{name}` | The application name: `moduleName` in cosmiconfig, the literal `mise` in mise.                |
| `{env}`  | A selected config environment, such as `production` (mise only).                             |
| `{ext}`  | A file extension that a registered decoder handles.                                           |
| `*`      | A glob over one path segment, expanded in lexical (alphabetical) order.                       |
| `~`      | The user's home directory.                                                                    |

Precedence is always written **lowest → highest** ("later wins"), which is the order a merging loader applies layers in.
For cosmiconfig, which stops at the first match, the lists are written in **search order** instead, and the first file
found wins.

---

## 1. The two models at a glance

The two tools solve different problems, and almost every difference below comes from this one:
**cosmiconfig finds one file. mise merges many files.**

| Concern                    | cosmiconfig                                                       | mise                                                                                  |
| -------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Result                     | The **first** file found. Nothing else is read.                   | **Every** file found, deep-merged by section.                                          |
| Direction of the walk      | Upwards from the start dir; the nearest file wins by being found first. | Every ancestor, applied root → cwd; the nearest file wins by being applied last. |
| Default walk               | **None.** Only the start dir (v9+).                               | From the cwd all the way to `/`.                                                      |
| Walk stop                  | `stopDir` (inclusive, default `~`), or a `package.{json,yaml}` marker (inclusive). | `ceiling_paths` (exclusive). No default ceiling.                    |
| Files per directory        | 27 candidates, first match wins.                                  | Up to ~40 patterns, all of them loaded.                                               |
| User-global config         | Only with `searchStrategy: 'global'`, after the walk.             | Always loaded, below project config.                                                  |
| System config              | None.                                                             | `/etc/mise`, lowest precedence.                                                       |
| Forced-override tier       | None.                                                             | `~/.config/mise/config.override.toml`, highest precedence.                            |
| Personal (uncommitted) files | None.                                                           | `*.local.toml`, beating shared files in the same directory.                            |
| Environment variants       | None.                                                             | `*.{env}.toml`, selected by `MISE_ENV` / `-E`.                                         |
| Embedded in a manifest     | A `{name}` key in `package.json` / `package.yaml`.                | None.                                                                                 |
| Formats                    | JSON, YAML, JS/TS (and extensionless files parsed as YAML).       | TOML only, plus the `.tool-versions` compatibility format.                            |
| Includes                   | `$import` key: deep merge, arrays concatenated.                   | `include` key: per-section merge, including file wins.                                 |
| Settings for discovery itself | Meta config in `.config/config.{ext}` or `package.json`, cwd only. | `.miserc.toml`, searched cwd → `~`, then global, then system.                     |
| Environment variables      | None.                                                             | Many (see [3.11](#311-environment-variables)).                                        |
| Trust model                | None.                                                             | Project config must be trusted before some features run.                              |

---

## 2. cosmiconfig

### 2.1 Search places within one directory

The default list, in search order (`getDefaultSearchPlaces`). The first candidate that exists **and** yields a non-empty
config wins.

```text
package.json                  # only the `{name}` property is read
.{name}rc                     # no extension: parsed as YAML (YAML ⊃ JSON)
.{name}rc.json
.{name}rc.yaml
.{name}rc.yml
.{name}rc.js
.{name}rc.ts
.{name}rc.cjs
.{name}rc.mjs                 # async API only
.{name}rc.cts
.{name}rc.mts                 # async API only
.config/{name}rc
.config/{name}rc.json
.config/{name}rc.yaml
.config/{name}rc.yml
.config/{name}rc.js
.config/{name}rc.ts
.config/{name}rc.cjs
.config/{name}rc.mjs          # async API only
.config/{name}rc.cts
.config/{name}rc.mts          # async API only
{name}.config.js
{name}.config.ts
{name}.config.cjs
{name}.config.mjs             # async API only
{name}.config.cts
{name}.config.mts             # async API only
```

Notes:

- A tool can replace this list with the `searchPlaces` option. The user can prepend entries through the meta config
  ([2.5](#25-meta-config)).
- `package.yaml` is **not** in the default list, but any search place whose basename is `package` (such as
  `package.yaml`) gets the property-lookup treatment ([2.4](#24-packagejson--packageyaml)).
- `{name}.config.{json,yaml}` is **not** a default place. Only the JS/TS extensions are listed.
- The file's extension picks the loader (`loaders` option, with key `noExt` for files that have no extension). A loader
  that returns `null` means "nothing here, keep searching".

### 2.2 Directory traversal (`searchStrategy`)

| Strategy  | Directories visited, in order                                                                                       | Default when                 |
| --------- | ------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| `none`    | Start dir only.                                                                                                     | `stopDir` not set (default). |
| `project` | Start dir, then each parent, stopping **after** the first dir that contains `package.json` or `package.yaml`, or at `/`. | Never; must be chosen.       |
| `global`  | Start dir, then each parent up to and **including** `stopDir` (default `os.homedir()`), or up to `/` if `stopDir` is not an ancestor. Then the global config dir ([2.3](#23-global-config-directory)). | `stopDir` is set. |

- The start dir is the `search(from)` argument, which defaults to `process.cwd()`.
- Setting `stopDir` together with any strategy other than `global` is an error.
- Only `global` ever looks at the user-global directory. `project` and `none` never do.
- cosmiconfig v8 and earlier walked upwards by default. v9 changed the default to `none`, so tools now opt in to
  walking.

### 2.3 Global config directory

This is used only with `searchStrategy: 'global'`, after the directory walk. The directory is
`envPaths({name}, { suffix: '' }).config`:

| OS      | Directory                                         |
| ------- | ------------------------------------------------- |
| Linux   | `$XDG_CONFIG_HOME/{name}`, else `~/.config/{name}` |
| macOS   | `~/Library/Preferences/{name}`                    |
| Windows | `%APPDATA%\{name}\Config`                         |

These are the files checked in the global directory, in search order (`globalConfigSearchPlaces`). The project search
places are **not** used here.

```text
config                        # no extension: YAML
config.json
config.yaml
config.yml
config.js
config.ts
config.cjs
config.mjs                    # async API only
config.cts
config.mts                    # async API only
```

> **Go note:** `os.UserConfigDir()` does not match either tool. On macOS it returns `~/Library/Application Support` and
> on Windows `%AppData%`. cosmiconfig uses `~/Library/Preferences` and `%APPDATA%\{name}\Config`, and mise uses
> `~/.config` on every OS. We need our own resolver.

### 2.4 `package.json` / `package.yaml`

- Only the value at `packageProp` is read. It defaults to `{name}`, and a dotted path (`"configs.myapp"`) or array
  (`["configs", "foo.bar"]`) addresses nested keys.
- A top-level key that contains a dot is matched literally first: `"one.two"` matches `{"one.two": …}`.
- If the property is missing, the file counts as "not found" and the search continues in the same directory.

### 2.5 Meta config

Before any search, cosmiconfig looks for a file that configures **cosmiconfig itself**. It looks
**only in `process.cwd()`**, not in the `search(from)` dir, and never walks upwards. It checks, in order:

```text
package.json                  # `cosmiconfig` property
package.yaml                  # `cosmiconfig` property
.config/config.json
.config/config.yaml
.config/config.yml
.config/config.js
.config/config.ts
.config/config.cjs
.config/config.mjs
.config/config.cts
.config/config.mts
```

- The `cosmiconfig` key may set `searchPlaces`, which can use the `{name}` placeholder. Those entries are **prepended**
  to the tool's list. Setting `mergeSearchPlaces: false` replaces the tool's list instead.
- `loaders` and `searchStrategy` are rejected with an error. Other options are "use at your own risk".
- **The meta file is also a config location.** If a meta file was found, its `{name}` property (`packageProp`) is
  checked *before* the normal search, and a non-empty value there wins outright. So `.config/config.json` can hold
  `{"cosmiconfig": {…}, "myapp": {…}}`, and the `myapp` key becomes the config, in the cwd only.

### 2.6 `$import`

- A loaded config may contain `$import`, a string or a list of paths resolved relative to the importing file.
- Imports are merged in order, and the importing file's own keys are applied last: `mergeAll([...imports, own])`.
- Merging is **deep for plain objects**. Arrays are **concatenated** by default (`mergeImportArrays: true`). Any other
  value is replaced.
- Self-imports and import cycles are errors.

### 2.7 Edge rules

- **Empty files:** whitespace-only files are skipped, and the search continues (`ignoreEmptySearchPlaces: true`). An
  empty result reports `isEmpty: true`.
- **Unreadable candidates:** `ENOENT`, `EISDIR`, `ENOTDIR` and `EACCES` are treated as "not here". Any parse error fails
  the whole search.
- **Explicit path:** `load(path)` reads one file with no search. It still applies `packageProp` to `package.*` files,
  and it still follows `$import`.
- **No match:** the search returns `null`, not an error.
- **Result:** `{ config, filepath, isEmpty? }`, describing exactly one file.

---

## 3. mise

### 3.1 Tiers

mise loads every file it finds. Tiers are applied **lowest → highest**:

```text
1. system     /etc/mise/…                         (MISE_SYSTEM_CONFIG_DIR)
2. global     ~/.config/mise/…                    (MISE_CONFIG_DIR)
3. project    every dir from `/` (or below the ceiling) down to the cwd
4. override   ~/.config/mise/config.override.toml (beats all project config)
```

A file reachable by more than one route is loaded **once**, after resolving symlinks, at its **highest** position.
Example: when the cwd is under `~`, `~/.config/mise/config.toml` also matches the project pattern
`.config/mise/config.toml` for the directory `~`, so it ranks as a project-tier file of `~`.

### 3.2 Filenames within one project directory

From `SHARED_CONFIG_FILENAMES`, `LOCAL_OVERRIDE_CONFIG_FILENAMES` and `env_config_patterns` in `src/config/mod.rs`. The
list is **lowest → highest**, and every file that exists is loaded. Globs expand alphabetically, so a later name wins.

#### Layer A. Shared files

```text
.tool-versions                          # asdf-compatible format
.config/{name}/conf.d/*.toml
.config/{name}/conf.d/*/{name}.toml     # "folder fragment"
.config/{name}/config.toml
.config/{name}/{name}.toml
.config/{name}.toml
.{name}/conf.d/*.toml
.{name}/conf.d/*/{name}.toml
.{name}/config.toml
{name}/conf.d/*.toml
{name}/conf.d/*/{name}.toml
{name}/config.toml
{name}.toml
.{name}.toml                            # dotfile beats the plain name
```

#### Layer B. Environment files

Repeated for each selected `{env}` in selection order, so a later env wins:

```text
.config/{name}/conf.d/*/{name}.{env}.toml
.config/{name}/config.{env}.toml
.config/{name}.{env}.toml
{name}/conf.d/*/{name}.{env}.toml
{name}/config.{env}.toml
{name}.{env}.toml
.{name}/conf.d/*/{name}.{env}.toml
.{name}/config.{env}.toml
.{name}.{env}.toml
```

#### Layer C. Local (personal, uncommitted) files

```text
.config/{name}/conf.d/*/{name}.local.toml
.config/{name}/config.local.toml
.config/{name}/{name}.local.toml
.config/{name}.local.toml
.{name}/conf.d/*/{name}.local.toml
.{name}/config.local.toml
{name}/conf.d/*/{name}.local.toml
{name}/config.local.toml
{name}.local.toml
.{name}.local.toml
```

#### Layer D. Environment-local files

Layer B's patterns with `{env}` replaced by `{env}.local`, repeated per env.

The net rule within a directory is **shared < env < local < env-local**. The docs' summary is
`mise.toml < mise.{env}.toml < mise.local.toml < mise.{env}.local.toml`.

Notes:

- **Divergence between mise's docs and source:** the environments page doesn't list `.mise.{env}.toml` or
  `.config/mise/config.{env}.toml`, but the source loads both.
- **Order differs between layers:** in Layer B, `{name}/…` comes before `.{name}/…`, but in Layers A and C, `.{name}/…`
  comes before `{name}/…`. This is a quirk of the source, and we should reproduce it in the mise preset rather than
  "fix" it.
- **`conf.d`:** files and folders whose names start with `.` are skipped. Folder fragments are not searched recursively.
  Single-file fragments with an env suffix (`conf.d/*.{env}.toml`) are opt-in through `env_conf_d` until mise 2027.8.10,
  when they become the default.
- **Platform environments:** when `auto_env` is on, `unix`, `{os}` and `{os}-{arch}` are prepended to the env list, at
  the lowest precedence. It is off by default until mise 2027.6.0.
- **Replacing the names:** `MISE_OVERRIDE_CONFIG_FILENAMES` replaces all of Layer A's TOML names and **removes** Layer C
  entirely. Layers B and D still load. `MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES` replaces `.tool-versions`.

### 3.3 Directory traversal

- mise visits **every ancestor** of the cwd, applies them **root → cwd**, and loads every matching file in each one.
- The ceiling is set by `MISE_CEILING_PATHS` (a path list) or by `ceiling_paths` in `.miserc.toml`. The walk stops
  **before** the first ceiling dir it meets. The ceiling dir itself and everything above it are not loaded.
- With no ceiling, the walk goes all the way to `/`. `~` and its parents are ordinary project dirs.
- There is no project-root marker for config discovery, unlike cosmiconfig's `package.json`.
- Directories in `ignored_config_paths` are skipped, and so are files the user has untrusted with `--ignore`.

### 3.4 Global directory

- The directory is `$MISE_CONFIG_DIR`, else `$XDG_CONFIG_HOME/mise`, else `~/.config/mise`. The same rule applies on
  **every OS**; mise does not use `~/Library`.
- Setting `MISE_GLOBAL_CONFIG_FILE` (legacy alias `MISE_CONFIG_FILE`) uses exactly that one file as the whole global
  tier and **disables the override tier**.
- If `MISE_CONFIG_DIR` points somewhere other than the default, files under the *default* `~/.config/mise` that the
  project walk finds are dropped.
- The config root (see [3.10](#310-config-root)) is `~`, or `MISE_GLOBAL_CONFIG_ROOT`.

The global directory has its own, flatter layout. From `config_files_with_incoming`, **lowest → highest**:

```text
conf.d/*.toml
conf.d/*/{name}.toml
config.toml
{name}.toml
  per {env}:  conf.d/*/{name}.{env}.toml, config.{env}.toml, {name}.{env}.toml
conf.d/*/{name}.local.toml
config.local.toml
{name}.local.toml
  per {env}:  conf.d/*/{name}.{env}.local.toml, config.{env}.local.toml, {name}.{env}.local.toml
```

`~/.tool-versions` is added to the global tier only when `MISE_USE_TOML=false`. Otherwise it is still picked up as a
project-tier file when the walk passes through `~`.

### 3.5 System directory

- The directory is `$MISE_SYSTEM_CONFIG_DIR` (legacy `MISE_SYSTEM_DIR`), else `/etc/mise`.
- It uses the same layout as the global directory ([3.4](#34-global-directory)).
- `MISE_SYSTEM_CONFIG_FILE` replaces the whole system tier with one file.

### 3.6 Override tier

These files live in the global directory and are applied **after all project config**:

```text
config.override.toml
config.{env}.override.toml      # per selected env, later env wins
```

They exist so that a user's own choices can beat a project's `mise.local.toml`. The tier is disabled by
`MISE_GLOBAL_CONFIG_FILE`. There is no system-tier equivalent.

### 3.7 Bootstrap file (`.miserc.toml`)

Some settings change **how discovery works**: which envs are selected, the ceiling, ignored paths, and filename
overrides. They can't live in `mise.toml`, because they're needed before `mise.toml` is found. mise reads them first
from a separate, restricted file. Search order, **highest first**:

```text
1. cwd, then each parent up to and including ~ (or / if outside ~),
   stopping before any MISE_CEILING_PATHS dir:
     .miserc.local.toml
     .miserc.toml
     .config/miserc.toml      # not read in ~ or /
2. ~/.config/mise/miserc.local.toml
   ~/.config/mise/miserc.toml
3. /etc/mise/miserc.toml
```

- **Allowed keys:** `env`, `auto_env`, `env_conf_d`, `ceiling_paths`, `ignored_config_paths`,
  `override_config_filenames`, `override_tool_versions_filenames`.
- **Merging:** each file overrides only the keys it sets.
- **Precedence:** `MISE_ENV` and `-E` beat every `.miserc.toml`.
- **Ceiling:** the bootstrap walk honours only the `MISE_CEILING_PATHS` env var, not `ceiling_paths` from a miserc file,
  to avoid recursion.

This is mise's counterpart to cosmiconfig's meta config ([2.5](#25-meta-config)).

### 3.8 Environment selection

Highest precedence first:

1. `-E` / `--env`
2. `MISE_ENV`
3. `env = [...]` in `.miserc.toml`

Several envs can be given, comma-separated (`ci,test`), and a later env wins. Selecting an env never sets environment
variables by itself.

**Environment names** (checked against `main` at `b756710`, 2026-10-19, with `auto_env` and `env_conf_d` on):

- **Not validated:** the value is only split on commas. Dots are allowed on purpose: "Environment names themselves may
  contain dots; active files are selected by exact glob patterns" (`conf_d_file_environment` in `src/config/mod.rs`).
- **Matched literally, never combined:** `mise.dev.linux.local.toml` loads only when an environment named exactly
  `dev.linux` is selected. Neither `-E dev` on Linux nor `-E dev,linux` loads it.
- **Some filenames are ambiguous:** `mise.dev.local.toml` is the local file of `dev`, or the env file of `dev.local`.
  Whichever name is selected decides.
- **No warning for unmatched files:** a file that no selected environment matches is silently not loaded. The only
  related warnings belong to the switch-over periods (dotted `conf.d` names, and platform files before `auto_env` is on
  by default), and they stop when the defaults change.
- **`conf.d` fragments** split at the first dot once `env_conf_d` is on, so `conf.d/tools.dev.linux.toml` belongs to env
  `dev.linux`.
- **Explicit platform names:** a platform name that is also selected explicitly (`-E linux`) is removed from the
  automatic list, so it ranks as a selected environment.

### 3.9 Includes

- A top-level `include = [...]` names local `.toml` paths (relative to the including file), `git::…` or `oci::…`.
- An included file ranks **directly below** the file that includes it, and a later entry beats an earlier one.
- Includes can't nest. Some sections (`[settings]`, `[tasks]`, …) are forbidden in an included file.
- Remote includes are cached, and an included file inherits the including file's trust.

### 3.10 Config root

Each file has a `config_root`, the directory its relative paths resolve against:

- For project files, it is the **project directory**, even for `.config/mise/config.toml` or `.mise/config.toml`.
- **Exceptions:** `.config/mise/mise.toml` and `.config/mise/mise.local.toml` use `.config/mise/` as their root.
- Each `conf.d/<folder>/` is its own root.
- For the global tier, it is `~` (or `MISE_GLOBAL_CONFIG_ROOT`).

Our loader should attach the config root to every loaded layer, because consumers need it to resolve relative paths.

### 3.11 Environment variables

| Variable                                | Effect                                                              |
| --------------------------------------- | ------------------------------------------------------------------- |
| `MISE_CONFIG_DIR`                       | Global directory (default `$XDG_CONFIG_HOME/mise`).                 |
| `XDG_CONFIG_HOME`                       | Base of the default global directory (default `~/.config`).          |
| `MISE_GLOBAL_CONFIG_FILE` / `MISE_CONFIG_FILE` | Use one global file; disables the override tier.            |
| `MISE_GLOBAL_CONFIG_ROOT`               | Config root for global files (default `~`).                          |
| `MISE_SYSTEM_CONFIG_DIR` / `MISE_SYSTEM_DIR` | System directory (default `/etc/mise`).                        |
| `MISE_SYSTEM_CONFIG_FILE`               | Use one system file.                                                |
| `MISE_ENV`                              | Selected envs, comma-separated.                                     |
| `MISE_CEILING_PATHS`                    | Exclusive walk ceilings (OS path-list separator).                   |
| `MISE_IGNORED_CONFIG_PATHS`             | Paths never loaded.                                                 |
| `MISE_OVERRIDE_CONFIG_FILENAMES`        | Replace the per-directory TOML names.                                |
| `MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES` | Replace `.tool-versions`.                                           |
| `MISE_DEFAULT_CONFIG_FILENAME`          | An extra per-directory name, placed alongside `mise.toml`; also the file mise creates when writing. |
| `MISE_AUTO_ENV` / `MISE_ENV_CONF_D`     | Feature flags described in [3.2](#32-filenames-within-one-project-directory). |

### 3.12 Trust

- Project-tier files are subject to trust. mise asks the user to trust a project config (`mise trust`) before using it.
- A declined file can be persistently ignored (`mise trust --ignore`). `trusted_config_paths` pre-trusts whole trees.
- Untrusted projects can't fetch remote includes.
- Global, system and override files are trusted by path.

cosmiconfig has no equivalent, even though it executes JS config files.

---

## 4. Divergences

These are the places where "mirror both tools" is not possible with a single behaviour. Each one becomes a preset
option, and each preset follows its own tool. The last column is `mise.Preset`, which is also the default. Every entry
in it is confirmed, and the detail behind each one is in
[design.md](design.md).

| #  | Topic                       | cosmiconfig                                              | mise                                                    | `mise.Preset` (default)                              |
| -- | --------------------------- | -------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------- |
| D1 | **Combining files**         | First match wins.                                        | Merge every file.                                       | **Merge** (the package is called *cascading*config). Keep first-match as a `Resolve` policy. |
| D2 | **Default walk**            | `none` (start dir only).                                 | cwd → `/`.                                              | Walk up to and including `~`, or to `/` when the start dir is outside `~` (see [Directory walk](design.md#directory-walk)). |
| D3 | **Walk stop**               | `stopDir`, inclusive, default `~`; `package.*` marker, inclusive. | `ceiling_paths`, exclusive, no default.        | Support both inclusive *stop* and exclusive *ceiling*. Default: stop at `~`, inclusive. `{NAME}_CEILING_PATHS` adds ceilings. |
| D4 | **Global dir location**     | `env-paths`: `~/Library/Preferences/{name}` on macOS, `%APPDATA%\{name}\Config` on Windows. | `$XDG_CONFIG_HOME/{name}` or `~/.config/{name}` everywhere. | XDG everywhere, as mise does. Offer an `env-paths`-compatible resolver. |
| D5 | **Global dir position**     | After the walk; only reached if nothing nearer matched.  | Below every project file.                               | These are the same rule under each tool's resolve policy: global is the lowest user tier. |
| D6 | **Global filenames**        | `config`, `config.{ext}`                                 | `config.toml`, `{name}.toml`, `conf.d/…`, `.local`, `{env}` | Layout from mise; extension set from the formats we register. |
| D7 | **Per-directory names**     | `.{name}rc[.{ext}]`, `.config/{name}rc[.{ext}]`, `{name}.config.{ext}` | `{name}.toml`, `.{name}.toml`, `{name}/config.toml`, `.{name}/config.toml`, `.config/{name}.toml`, `.config/{name}/config.toml`, `conf.d` | mise's layout with any registered extension. rc-style names via `cosmiconfig.Preset`. |
| D8 | **Formats**                 | JSON, YAML, extensionless YAML, JS/TS                    | TOML, `.tool-versions`                                  | Pluggable decoders keyed by extension. Ship JSON, YAML and TOML. JS/TS are not portable to Go. When one slot matches several formats, all are merged with TOML applied last (it wins) by default; the order can be overridden with `{NAME}_CONFIG_FORMATS` (see [File format precedence](design.md#file-format-precedence)). |
| D9 | **Manifest property**       | `package.json` / `package.yaml` `{name}` key             | None.                                                   | Not in any preset, because it is Node.js-specific. Users can add a manifest source with the `Embedded` scope kind, for example `package.json#{name}` or `pyproject.toml#tool.{name}`. |
| D10 | **System tier**            | None.                                                    | `/etc/{name}`                                           | Include it, as mise does. On Windows, `%ProgramData%\{name}` (see [Platform directories](design.md#platform-directories)). |
| D11 | **Override tier**          | None.                                                    | `config.override.toml`                                  | Include it, as mise does.                           |
| D12 | **Local / env variants**   | None.                                                    | `.local`, `.{env}`, `.{env}.local`                      | Include them, as mise does. The env selector is configurable. |
| D13 | **Includes**               | `$import`: deep-merge objects, concatenate arrays.       | `include`: per-section rules, included file ranks just below the includer. | Deep-merge objects; arrays **replace**. Used for every merge (see [Merging](design.md#merging)). `$import`-style array concatenation is opt-in. Remote includes go through resolver hooks with allow-lists (see [Includes](design.md#includes)). |
| D14 | **Discovery settings**     | Meta config, cwd only; can prepend `searchPlaces`.       | `.miserc.toml`, walked; can set env, ceiling, ignores, filenames. | A small search of its own, run before the main one. Each preset names its bootstrap files and allowed keys (see [Bootstrap file](design.md#bootstrap-file)). |
| D15 | **Empty files**            | Skipped; search continues.                               | Loaded (a no-op layer).                                 | Either policy gives the same merge result. Keep empty layers, flagged, for diagnostics. |
| D16 | **Unreadable candidate**   | `EACCES` etc. silently skipped.                          | Not documented.                                         | Skip with a diagnostic; a parse error is fatal.     |
| D17 | **Env-var overrides**      | None.                                                    | `MISE_*`                                                | On by default: `{NAME}_*` derived from the app name, each one renameable and opt-out-able. `Describe()` lists every active binding (see [Environment variables](design.md#environment-variables)). |
| D18 | **Trust**                  | None.                                                    | Project files untrusted by default.                     | Trust everything by default (we don't execute config). Approval is the opt-in `RequireTrust(store)` policy option (see [Trust store](design.md#trust-store)). |
