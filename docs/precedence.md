# Precedence of load locations

This document is **normative**. It defines the order in which a load applies its layers: which files are candidates,
where each one ranks, and what takes a place in the order. design.md explains why the layouts look as they do. Where it
describes order, this document decides. Tests cite rules by ID (`PR-1`, `PR-2`, …), as they cite DL rows.

The order is **lowest precedence first**. Layers are merged in that order, so a later layer wins wherever two set the
same key, subject to the [merge rules](design.md#merging).

---

## Definitions

- **Layer:** one thing that merges: a file, an included file, or a value scope's tree.
- **Candidate:** a file a scope's patterns match in a directory it searches.
- **Slot:** one pattern in one directory, with every placeholder but `{ext}` filled in. With a `*` glob in the file
  name, each distinct stem is its own slot; without `{ext}`, each matching name is.
- **Position:** a layer's place in the order. Every candidate the load considers is listed in the report at its
  position, but only layers with the status `LayerLoaded` merge.

## Inputs

The order depends only on these, each chosen as design.md describes:

| Input                         | Where it comes from                                                                                      |
| ----------------------------- | -------------------------------------------------------------------------------------------------------- |
| The scopes and their patterns | The layout or preset, `WithScope`, and `WithScopes` / `WithoutScopes` for the selection (DL-100)          |
| The host                      | `GOOS`, `GOARCH`, the home dir, the current dir and the env vars, through the loader's host               |
| The start dir                 | `WithStartDir`, else the current dir                                                                     |
| The walk stop and ceilings    | `WithWalkStop`, `WithSearchStrategy`, `WithStopDir`, `WithRootMarker`, `WithCeilingPaths`, `{NAME}_CEILING_PATHS` and bootstrap `ceiling_paths` |
| The selected environments     | `WithEnvs`, else `{NAME}_ENV`, else bootstrap `env` (design.md: Environments)                             |
| The format order              | `{NAME}_CONFIG_FORMATS`, else bootstrap `formats` (per scope, PR-7), else `WithFormats`, else the layout's default |
| The conflict policy           | `WithFormatConflict`                                                                                     |
| The platform directories      | `WithPlatformDirs`, `{NAME}_CONFIG_DIR` and `{NAME}_SYSTEM_CONFIG_DIR`                                   |
| Explicit files                | `WithConfigFiles`, else `{NAME}_CONFIG_FILE`                                                             |
| Scope replacements            | `ReplacedByFile` and `DisabledBy` bindings, such as `{NAME}_GLOBAL_CONFIG_FILE`                          |
| The resolution                | `WithResolution`                                                                                         |
| What's skipped                | Ignored paths, file permissions and the trust store                                                      |
| The files themselves          | What exists, what's empty, what's a symlink to what                                                      |

## The order of candidates

Candidates are ordered by one key, compared component by component, **lowest first**. PR-1 to PR-7 define the
components; the rules after them say what the key can't.

### PR-1: Scopes

- **Rank:** scopes rank in the order they're defined. `WithScope(…, After(x))` places a scope directly above `x`,
  `Before(x)` directly below it, and neither above every scope defined so far (DL-100).
- **Defaults:** the defaults scope, `ScopeDefaults`, is always lowest (DL-153).
- **Selection doesn't reorder:** only selected scopes take part, and `WithScopes` lists don't change the rank.
- **`DisabledBy(b)`:** a scope whose binding `b` is set takes no part.
- **`ReplacedByFile(b)`:** when `b` is set, the scope's place holds exactly the file `b` names, loaded as an explicit
  file (PR-9) with the scope's name and trust.

### PR-2: Directories within a scope

- **`FixedDir`** searches one directory: its template with `~`, `{user}`, `{system}`, `{cwd}` and `{name}` filled in.
  `{NAME}_CONFIG_DIR` replaces `{user}` and `{NAME}_SYSTEM_CONFIG_DIR` replaces `{system}`, when the layout declares
  them. A directory that can't be worked out, such as `{user}` with no home, is skipped with a warning.
- **`Walk`** searches from the start dir up through each parent:
  - **Ceilings** are exclusive. A ceiling, and everything above it, is never searched. A start dir that is a ceiling
    searches nothing.
  - **Stops:** `StopAtStart` searches the start dir only; `StopAtHome` stops after the stop dir (`WithStopDir`, else
    `~`), or at `/` if the stop dir isn't the start dir or an ancestor of it; `StopAtMarker` stops after the first
    directory that holds a root marker, or at `/`; `StopAtRoot` stops at `/`.
  - **Order:** the directories apply **root-most first**, so the nearest one wins.
  - **`SkipHomeAndRoot`:** a pattern with it isn't searched in `~` or `/`.
  - **A moved user directory** (DL-128): when `{NAME}_CONFIG_DIR` moves `{user}`, a walk drops every candidate under the
    default user directory. They aren't listed at all.
- **`ExplicitFile`** loads its files as explicit files (PR-9), at its scope's place.
- **`EmbeddedFS`** searches the root of its `fs.FS`.

### PR-3: Variant groups within a directory

Within one directory, every pattern belongs to one group, and the groups rank:

1. **shared** (`VariantShared`)
2. **env** (`VariantEnv`)
3. **local** (`VariantLocal`)
4. **env-local** (`VariantEnvLocal`)

So every local file ranks above every shared and env file in the same directory, whatever its environment.

### PR-4: Environments within a group

The env and env-local groups are searched once per environment, in this order:

1. **The platform environments,** in the order `unix`, `{os}`, `{os}-{arch}`:
   - `unix` applies on every `GOOS` except `windows`, `plan9`, `js` and `wasip1`.
   - `{os}` is `GOOS`, with `darwin` named `macos`.
   - `{arch}` is `GOARCH`, with `amd64` named `x64`.
   - A platform environment that's also selected is dropped from this list.
2. **The selected environments,** in the order selected. A name selected twice keeps only its later place.

A template's `{env}` is filled in with the name exactly as written. Names are never combined.

### PR-5: Patterns within a group

Within one group and one environment, patterns rank in the order the scope lists them. Bootstrap `searchPlaces`
(cosmiconfig's layout) are added after every walk pattern, so they rank highest, unless `mergeSearchPlaces = false`
makes them replace the walk's patterns.

### PR-6: Glob matches within a pattern

A template's segments are matched one at a time:

- **A `*` in a directory segment** matches any directory whose name doesn't start with a dot. Matches are taken in byte
  order of their names, and everything below the first match ranks below everything below the second.
- **A `*` in the file name** matches a non-empty run of characters with no dots. Each distinct stem is one slot, and
  slots rank in byte order of their stems. With several `*`s, stems compare by their captures in turn.
- **A template without `{ext}`** makes each matching name its own slot, in byte order of the names.
- **No match is case-insensitive,** on any filesystem.

### PR-7: Formats within a slot

- **The order:** a slot's files rank by **format**, the lowest-ranked format first, so the highest-ranked format applies
  last and wins. Within one format, the files rank by **extension**, in the reverse of the order the decoder lists its
  extensions (highest first), so YAML's `.yml` ranks below `.yaml`.
- **Which formats a scope searches:** only the formats in its format order, and only extensions of registered decoders.
  A `WithFileFormat` rule decides a file's format first. An `{ext}` slot otherwise uses the format whose extension
  filled `{ext}`.
- **Per-scope order:** `{NAME}_CONFIG_FORMATS`, when set, is every scope's order. Otherwise a scope uses the `formats`
  of the highest-precedence bootstrap file whose trust is at least the scope's own, else the app's or the layout's order
  (DL-135).
- **Conflicts:** under `ConflictMerge`, every file in a slot takes its place. Under `ConflictFirst`, only the
  highest-ranked file loads, and the others are `LayerIgnored` with `WarnFormatConflict`. Under `ConflictFail`, a slot
  with more than one file fails the load with a `*ConflictError`.

## Rules beyond the key

### PR-8: Reading candidates

- **Statuses:** a candidate whose file can't be used still has its position, with a status other than `LayerLoaded`.
  Such files never merge:
  - ignored paths: `LayerIgnored`
  - file permission checks: `LayerIgnored`, with a warning
  - the trust store: `LayerUntrusted`
  - broken symlinks and non-regular files: `LayerIgnored`
  - unreadable files: `LayerUnreadable`
  - empty files: `LayerEmpty`
- **Symlinks:** a symlinked candidate is read through its target, at the symlink's position.

### PR-9: Explicit files

- **Which files:** `WithConfigFiles`, else `{NAME}_CONFIG_FILE`, with empty entries dropped. The files apply in the
  order given, so the later one wins.
- **Where they go:**
  - **A marked scope:** they take the place of the scope marked `ReplacedByConfigFiles()`, even if it isn't selected,
    which then searches nothing.
  - **No marked scope:** they rank above every scope.
- **Metadata:** they have `TrustProject`, and the marked scope's name, or none.
- **`WithConfigFilesOnly()`:** explicit files replace every scope that searches for files: `FixedDir`, `Walk`,
  `ExplicitFile`, `EmbeddedFS` and custom scopes. Value scopes (`Values`, `ValuesFunc`, `EnvVars` and the defaults)
  still take their places (DL-247).
- **No variants:** an explicit file never brings its `.local` or `.{env}` siblings.

### PR-10: Includes

- **Rank:** an included file ranks **directly below** the file that includes it. Several entries rank in the order
  listed, so a later entry wins, and all of them rank below the includer.
- **Recursion:** an included file's own includes rank directly below it in turn.
- **Metadata:** an included layer takes its scope, variant, environment, trust and config root from the root file of its
  chain (DL-184).

### PR-11: One file, reached twice

- **Identity:** a file is the same file when the filesystem says so (`SameFile`), whatever path reached it.
- **Where it ranks:** it takes **only its highest position**. Its other routes are listed as `LayerDuplicate`.
- **Trust:** the layer takes the scope and trust of its **most trusted** route.
- **Remote files:** remote includes are the same file when their normalised references are equal.

### PR-12: Value layers

- **Values, ValuesFunc and EnvVars:** each scope is one layer at its scope's place.
- **The defaults:** one layer, lowest.
- **Custom scopes:** a `Tree` source is one layer, in the order the scope returns it.
- **Includes:** value layers never include.

### PR-13: First-match resolution

Under `WithResolution(FirstMatch)`:

- **The choice:** of the files that aren't included, the highest-precedence one that is loaded and not empty is
  **chosen**, with its includes.
- **Files below it** are `LayerIgnored`, and aren't decoded.
- **Errors:** a file above it that fails, such as one that doesn't parse, fails the load.
- **Value layers** still merge at their own places, above or below the chosen file (DL-154).

### PR-14: Bootstrap files

Bootstrap files never merge. They're ordered by the same rules, PR-1 to PR-7, among themselves. A key set in a higher
bootstrap file replaces the same key from a lower one, except `ceiling_paths` and `ignored_config_paths`, which add
(DL-134).

---

## The built-in layouts

Each table lists a scope's patterns **in precedence order**, lowest first: by group (PR-3), then in the order listed
(PR-5). `RootFileDir` patterns make the file's own directory its config root; the others use the searched directory. The
drift test (DL-244) reads these tables and checks them against the oracle's own copy.

`MiseHardenedLayout` ranks everything exactly as `MiseLayout` does: it only adds policies, which skip files (PR-8).

### `MiseLayout`

Scopes, lowest first: `system`, `user`, `project`, `override`. Formats `toml`, `yaml`, `json`, under `ConflictMerge`.

#### `MiseLayout` scope `system`: `FixedDir("{system}")`

| #  | Template                              | Variant   | Root      |
| -- | ------------------------------------- | --------- | --------- |
| 1  | `conf.d/*.{ext}`                      | shared    | search    |
| 2  | `conf.d/*/{name}.{ext}`               | shared    | file      |
| 3  | `config.{ext}`                        | shared    | search    |
| 4  | `{name}.{ext}`                        | shared    | search    |
| 5  | `conf.d/*.{env}.{ext}`                | env       | search    |
| 6  | `conf.d/*/{name}.{env}.{ext}`         | env       | file      |
| 7  | `config.{env}.{ext}`                  | env       | search    |
| 8  | `{name}.{env}.{ext}`                  | env       | search    |
| 9  | `conf.d/*/{name}.local.{ext}`         | local     | file      |
| 10 | `config.local.{ext}`                  | local     | search    |
| 11 | `{name}.local.{ext}`                  | local     | search    |
| 12 | `conf.d/*.{env}.local.{ext}`          | env-local | search    |
| 13 | `conf.d/*/{name}.{env}.local.{ext}`   | env-local | file      |
| 14 | `config.{env}.local.{ext}`            | env-local | search    |
| 15 | `{name}.{env}.local.{ext}`            | env-local | search    |

Replaced by the file `{NAME}_SYSTEM_CONFIG_FILE` names, when set.

#### `MiseLayout` scope `user`: `FixedDir("{user}")`

The same 15 patterns as `system`, with the config root `~`. Replaced by the file `{NAME}_GLOBAL_CONFIG_FILE` names, when
set.

#### `MiseLayout` scope `project`: `Walk`

| #  | Template                                      | Variant   | Root      |
| -- | --------------------------------------------- | --------- | --------- |
| 1  | `.config/{name}/conf.d/*.{ext}`               | shared    | search    |
| 2  | `.config/{name}/conf.d/*/{name}.{ext}`        | shared    | file      |
| 3  | `.config/{name}/config.{ext}`                 | shared    | search    |
| 4  | `.config/{name}/{name}.{ext}`                 | shared    | file      |
| 5  | `.config/{name}.{ext}`                        | shared    | search    |
| 6  | `.{name}/conf.d/*.{ext}`                      | shared    | search    |
| 7  | `.{name}/conf.d/*/{name}.{ext}`               | shared    | file      |
| 8  | `.{name}/config.{ext}`                        | shared    | search    |
| 9  | `{name}/conf.d/*.{ext}`                       | shared    | search    |
| 10 | `{name}/conf.d/*/{name}.{ext}`                | shared    | file      |
| 11 | `{name}/config.{ext}`                         | shared    | search    |
| 12 | `{name}.{ext}`                                | shared    | search    |
| 13 | `.{name}.{ext}`                               | shared    | search    |
| 14 | `.config/{name}/conf.d/*.{env}.{ext}`         | env       | search    |
| 15 | `.config/{name}/conf.d/*/{name}.{env}.{ext}`  | env       | file      |
| 16 | `.config/{name}/config.{env}.{ext}`           | env       | search    |
| 17 | `.config/{name}.{env}.{ext}`                  | env       | search    |
| 18 | `{name}/conf.d/*.{env}.{ext}`                 | env       | search    |
| 19 | `{name}/conf.d/*/{name}.{env}.{ext}`          | env       | file      |
| 20 | `{name}/config.{env}.{ext}`                   | env       | search    |
| 21 | `{name}.{env}.{ext}`                          | env       | search    |
| 22 | `.{name}/conf.d/*.{env}.{ext}`                | env       | search    |
| 23 | `.{name}/conf.d/*/{name}.{env}.{ext}`         | env       | file      |
| 24 | `.{name}/config.{env}.{ext}`                  | env       | search    |
| 25 | `.{name}.{env}.{ext}`                         | env       | search    |
| 26 | `.config/{name}/conf.d/*/{name}.local.{ext}`  | local     | file      |
| 27 | `.config/{name}/config.local.{ext}`           | local     | search    |
| 28 | `.config/{name}/{name}.local.{ext}`           | local     | file      |
| 29 | `.config/{name}.local.{ext}`                  | local     | search    |
| 30 | `.{name}/conf.d/*/{name}.local.{ext}`         | local     | file      |
| 31 | `.{name}/config.local.{ext}`                  | local     | search    |
| 32 | `{name}/conf.d/*/{name}.local.{ext}`          | local     | file      |
| 33 | `{name}/config.local.{ext}`                   | local     | search    |
| 34 | `{name}.local.{ext}`                          | local     | search    |
| 35 | `.{name}.local.{ext}`                         | local     | search    |
| 36 | `.config/{name}/conf.d/*.{env}.local.{ext}`   | env-local | search    |
| 37 | `.config/{name}/conf.d/*/{name}.{env}.local.{ext}` | env-local | file |
| 38 | `.config/{name}/config.{env}.local.{ext}`     | env-local | search    |
| 39 | `.config/{name}.{env}.local.{ext}`            | env-local | search    |
| 40 | `{name}/conf.d/*.{env}.local.{ext}`           | env-local | search    |
| 41 | `{name}/conf.d/*/{name}.{env}.local.{ext}`    | env-local | file      |
| 42 | `{name}/config.{env}.local.{ext}`             | env-local | search    |
| 43 | `{name}.{env}.local.{ext}`                    | env-local | search    |
| 44 | `.{name}/conf.d/*.{env}.local.{ext}`          | env-local | search    |
| 45 | `.{name}/conf.d/*/{name}.{env}.local.{ext}`   | env-local | file      |
| 46 | `.{name}/config.{env}.local.{ext}`            | env-local | search    |
| 47 | `.{name}.{env}.local.{ext}`                   | env-local | search    |

The walk stops at `~`, inclusive (`StopAtHome`). The scope is the one explicit files replace (PR-9).

#### `MiseLayout` scope `override`: `FixedDir("{user}")`

| # | Template                        | Variant | Root   |
| - | ------------------------------- | ------- | ------ |
| 1 | `config.override.{ext}`         | shared  | search |
| 2 | `config.{env}.override.{ext}`   | env     | search |

The config root is `~`. The scope takes no part when `{NAME}_GLOBAL_CONFIG_FILE` is set.

#### `MiseLayout` bootstrap files

Searched in the selected scopes only, by the same rules, lowest first: `system` (`{name}rc.{ext}`), then `user`
(`{name}rc.{ext}`, `{name}rc.local.{ext}`), then `project`, in each walk directory: `.config/{name}rc.{ext}` (not in `~`
or `/`), `.{name}rc.{ext}`, `.{name}rc.local.{ext}`.

### `CosmiconfigLayout`

Scopes, lowest first: `user`, `project`, `meta`. Formats `json`, `yaml`, under `ConflictFirst`, with `FirstMatch`
resolution (PR-13) and `WithConfigFilesOnly()` (PR-9). The project walk is `StopAtStart` unless `WithSearchStrategy`
changes it, and `user` is selected only by `WithSearchStrategy(SearchGlobal)`.

#### `CosmiconfigLayout` scope `user`: `FixedDir("{user}")`

| # | Template         | Variant | Root   |
| - | ---------------- | ------- | ------ |
| 1 | `config.{ext}`   | shared  | search |
| 2 | `config`         | shared  | search |

#### `CosmiconfigLayout` scope `project`: `Walk`

| # | Template                  | Variant | Root   |
| - | ------------------------- | ------- | ------ |
| 1 | `.config/{name}rc.{ext}`  | shared  | search |
| 2 | `.config/{name}rc`        | shared  | search |
| 3 | `.{name}rc.{ext}`         | shared  | search |
| 4 | `.{name}rc`               | shared  | search |

#### `CosmiconfigLayout` scope `meta`: `FixedDir("{cwd}")`

| # | Template                 | Variant | Root   |
| - | ------------------------ | ------- | ------ |
| 1 | `.config/config.{ext}`   | shared  | search |

The file's `{name}` key is the config. The same file's `cosmiconfig` key is the scope's bootstrap file.

---

## Worked examples

These are conformance tests (DL-245), each with its expected order written by hand from the rules above.

### Example 1: environments in one directory (PR-3, PR-4)

`MiseLayout`, Linux x64, `MYAPP_ENV=dev`, every file in `~/work/app`, which is the start dir:

```text
myapp.toml                    shared
myapp.unix.toml               env        platform: unix, linux, linux-x64
myapp.linux.toml              env
myapp.linux-x64.toml          env
myapp.dev.toml                env        selected
myapp.local.toml              local
myapp.unix.local.toml         env-local
myapp.linux.local.toml        env-local
myapp.linux-x64.local.toml    env-local
myapp.dev.local.toml          env-local  highest
```

### Example 2: the walk and a file reached twice (PR-1, PR-2, PR-11)

`MiseLayout` on macOS arm64, home `/home/me`, start dir `/home/me/work/app`, no environments selected, no XDG variables,
so `{user}` is `/home/me/.config/myapp`. The files:

```text
/etc/myapp/config.toml
/home/me/.config/myapp/config.toml
/home/me/myapp.toml
/home/me/work/myapp.toml
/home/me/work/app/myapp.toml
/home/me/work/app/.myapp.toml
/home/me/.config/myapp/config.override.toml
```

The walk searches `/home/me`, `/home/me/work` and `/home/me/work/app`, in that order. `config.toml` in the user
directory is also the walk's pattern 3 (`.config/{name}/config.{ext}`) in `/home/me`, so it's one file reached twice: it
ranks at the walk's place, which is higher, with `user`'s scope and trust, and its `user` route is a duplicate. The
report lists, lowest first:

```text
/etc/myapp/config.toml                        system      loaded
/home/me/.config/myapp/config.toml            user        LayerDuplicate
/home/me/.config/myapp/config.toml            user        loaded, at the walk's place in /home/me (pattern 3)
/home/me/myapp.toml                           project     loaded (pattern 12)
/home/me/work/myapp.toml                      project     loaded
/home/me/work/app/myapp.toml                  project     loaded (pattern 12)
/home/me/work/app/.myapp.toml                 project     loaded (pattern 13), ranks above myapp.toml (PR-5)
/home/me/.config/myapp/config.override.toml   override    loaded
```

### Example 3: a slot and globs (PR-6, PR-7)

`MiseLayout`, one walk dir, formats `toml`, `yaml`, `json`, `ConflictMerge`:

```text
.myapp/conf.d/b.yaml
.myapp/conf.d/a.json
.myapp/conf.d/a.toml
.myapp/conf.d/a.yml
.myapp/conf.d/a.yaml
.myapp/conf.d/x/myapp.toml
.myapp/conf.d/w/myapp.json
```

The order: stem `a` before stem `b`, and within `a` the lowest format first; then pattern 7's directories `w` before
`x`:

```text
.myapp/conf.d/a.json
.myapp/conf.d/a.yml
.myapp/conf.d/a.yaml
.myapp/conf.d/a.toml
.myapp/conf.d/b.yaml
.myapp/conf.d/w/myapp.json
.myapp/conf.d/x/myapp.toml
```
