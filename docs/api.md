# cascadingconfig API

This document designs the public Go API for the loader described in [design.md](design.md). Each section is marked as
decided or proposed. Names and signatures may still be refined while the API is built.

## Overview

```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset /*, opts... */)

// typed: the main use case
cfg, err := cascadingconfig.Load[Config](ctx, loader)

// untyped, or both
res, err := loader.Load(ctx)
tree := res.Tree()     // merged tree: map[string]any
err = res.Decode(&cfg) // decode it whenever you like
report := res.Describe()

// optional: watch for changes
w, err := cascadingconfig.Watch[Config](ctx, loader, func(c cascadingconfig.Change[Config]) { /* ... */ })
defer w.Close()
```

- **`New` and `NewWithPreset` validate every option** and returns an error straight away, so a misconfigured loader
  fails at start-up, not on the first `Load`.
- **Typed entry points are free functions** (`Load[T]`, `Watch[T]`), because Go methods can't have type parameters.

## Package layout

**Status: decided** (see [Package layout decisions](#package-layout-decisions)). The repository tree, tooling and
measured dependency counts are in [project-layout.md](project-layout.md).

The goal is that **consumers pay only for what they use**.

### Rules

1. **A third-party dependency gets its own module** (its own `go.mod`). That keeps it out of a consumer's module graph
   entirely: their `go.mod` and `go.sum`, vulnerability scans and SBOMs.
2. **An optional standard-library-only piece gets its own package** within the core module. Go compiles only the
   packages you import, so it stays out of your build unless you use it.
3. **The core module has one dependency:** `go-viper/mapstructure/v2`, which has none of its own, because struct
   decoding is part of the core API.

### Layouts vs presets

A preset bundles two things:

- **A layout:** scopes, filename patterns, precedence, variants, policies and bootstrap files. A layout needs nothing
  beyond the standard library.
- **Capabilities:** format decoders and resolvers (TOML, YAML, git, OCI) and the file-watching backend. Capabilities
  bring dependencies.

So **layouts live in the core, and ready-to-use presets live in a separate module** that adds the capabilities:

| Core layout (no dependencies)      | Ready-to-use preset (separate module) | Adds                                                            |
| ---------------------------------- | ------------------------------------- | --------------------------------------------------------------- |
| `cascadingconfig.MiseLayout`       | `mise.Preset`                         | TOML, YAML, http(s), git, OCI, fsnotify                         |
| `cascadingconfig.MiseHardenedLayout` | `misehardened.Preset`                   | TOML, YAML, fsnotify (no remotes)                               |
| `cascadingconfig.CosmiconfigLayout`| `cosmiconfig.Preset`                  | YAML, fsnotify (no TOML, no remotes, as cosmiconfig)            |

- **Format order with missing decoders:** a layout's default format order names formats that may not have a decoder
  registered. A format without a decoder is simply not searched. This only applies to the *layout's default* order, set
  with `WithDefaultFormats` (DL-105). An order set by the app's `WithFormats`, an env var or a bootstrap file still
  errors on an unregistered format ([design.md](design.md#file-format-precedence)).
- **Names in design.md:** design.md uses the same names: `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset`
  for the ready-to-use presets, and the `…Layout` names for the core layouts.

### Modules and packages

| Import path (module root in **bold**)                          | Package        | Contents                                                                                         | Third-party dependencies                         |
| -------------------------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------ |
| **`github.com/kittomic/cascadingconfig`**                      | `cascadingconfig` | `New`, `Load[T]`, `Watch[T]`, `Result`, `Report`, errors, options, merging, decoding, layouts, the `file` resolver, the filesystem hook, stat polling and the reload logic | `go-viper/mapstructure/v2`                     |
| `…/cascadingconfig/format/json`                                 | `json`         | JSON decoder. Its parser, in `internal/json`, is also what every layout registers (DL-95)        | none (standard library)                          |
| `…/cascadingconfig/httpresolver`                                | `httpresolver` | `http`/`https` include resolver                                                                  | none (standard library)                          |
| `…/cascadingconfig/gitresolver`                                 | `gitresolver`  | `git` include resolver, run through the `git` program                                            | none (needs `git` installed at run time)         |
| **`…/cascadingconfig/format/toml`**                             | `toml`         | TOML decoder                                                                                     | `pelletier/go-toml/v2`                           |
| **`…/cascadingconfig/format/yaml`**                             | `yaml`         | YAML decoder                                                                                     | `goccy/go-yaml`                                  |
| **`…/cascadingconfig/ociresolver`**                             | `ociresolver`  | `oci` include resolver                                                                           | `oras.land/oras-go/v2`                           |
| **`…/cascadingconfig/watch/fsnotify`**                          | `fsnotify`     | Event-based file watching                                                                        | `fsnotify/fsnotify`                              |
| **`…/cascadingconfig/presets`**                                 | `mise`, `misehardened`, `cosmiconfig` | The ready-to-use presets                                                | all of the above                                 |

- **Watching stays in the core.** `Watch[T]`, debouncing, full reloads and the stat-polling fallback need only the
  standard library. The fsnotify module only supplies the *event source*, through the filesystem hook's notifier
  ([design.md](design.md#hot-reload)). Without it, watching polls (every 2 s by default). The ready-to-use presets
  register fsnotify.
- **git runs as a command,** not through `go-git`. That keeps it dependency-free, and the
  [traversal design](design.md#traversal-protection) already uses git commands (`cat-file`, `rev-parse`,
  `GIT_ALLOW_PROTOCOL`). The cost is that the `git` program must be installed.

### Examples

```go
// Batteries included: everything mise does.
import "github.com/kittomic/cascadingconfig/presets/mise"

loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset)
```

```go
// Minimal: mise's layout, TOML only. Core layouts register no remotes and no fsnotify.
// Module graph: cascadingconfig, mapstructure, cascadingconfig/format/toml, go-toml.
import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
)

loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout.Extend(
	cascadingconfig.WithDecoder(toml.Decoder()),
))
```

### Costs of several modules

- **Versioning:** each module is tagged separately, for example `format/toml/v0.1.0`. Each one requires a compatible
  core version, so a breaking core change means releasing several modules.
- **Local development** uses a `go.work` file, so the modules build against each other unreleased.
- **Tooling:** `go test ./...`, `golangci-lint run ./...`, `go mod tidy` and `govulncheck ./...` don't cross module
  boundaries. The lefthook commands and CI must loop over the modules (`go work` can list them).

### Package layout decisions

1. **Layout names.** **Decided:**

   | Core layout                        | Ready-to-use preset   | Import path                    |
   | ---------------------------------- | --------------------- | ------------------------------ |
   | `cascadingconfig.MiseLayout`       | `mise.Preset`         | `…/presets/mise`               |
   | `cascadingconfig.MiseHardenedLayout` | `misehardened.Preset` | `…/presets/misehardened`     |
   | `cascadingconfig.CosmiconfigLayout`| `cosmiconfig.Preset`  | `…/presets/cosmiconfig`        |

   - **"Hardened", not "Secure":** "Secure" would imply that `mise.Preset` is insecure. It isn't; it trusts files and
     fetches remotes, as mise does. The hardened variant adds no remotes, strict permissions and confined includes.
   - **The `Layout` suffix** marks a core value that brings no formats or remotes beyond JSON.
   - **One type:** layouts and presets are both of type `Preset`.
2. **One presets module or one per preset.** **Decided:** one module for now, perhaps revisited later
   ([project-layout.md](project-layout.md#decisions)). Measured: an app that imports only `presets/misehardened` gets
   the same 5 modules in its `go.mod`, `go.sum` and build as with separate modules. Only `go list -m all` grows, from 5
   to 9.
3. **Libraries.** **Decided:** `pelletier/go-toml/v2` and `goccy/go-yaml`. Neither has dependencies, and both report
   line and column positions, which `*ParseError` needs. Their maintenance status and licences are to be confirmed
   before the first release.
4. **git as a command** rather than `go-git`. **Decided:** yes. It needs no Go dependencies, and the
   [traversal design](design.md#traversal-protection) already relies on git commands. The `git` program must be
   installed at run time.

## Building a loader

**Status: decided.**

### `New` and `NewWithPreset`

```go
func New(name string, opts ...Option) (*Loader, error)
func NewWithPreset(name string, preset Preset, opts ...Option) (*Loader, error)
```

- **`name`** is the app name, `{name}` in every pattern. It must pass the
  [safe-name check](design.md#safe-environment-names), and `{NAME}`, the env-var prefix, is derived from it.
- **`NewWithPreset`** starts from a preset. Its `opts` are applied after the preset, so `NewWithPreset(n, p, a, b)`
  behaves exactly like `NewWithPreset(n, p.Extend(a, b))`.
- **`New`** starts from nothing but the engine defaults. It's up to the app to add scopes, decoders and everything else
  (see [A loader without a preset](#a-loader-without-a-preset)).
- **Validation happens here.** Every option is checked. All the problems found are returned together, joined into
  `*OptionError`s (code 400), so a misconfigured loader fails at start-up.
- **The returned `*Loader` is immutable.** It is safe for concurrent `Load` calls. Each `Load` reads the current
  directory, env vars and files afresh.

### A loader without a preset

Settings come from two places:

- **Engine defaults** live in the core and apply even without a preset: deep merging with arrays replaced, `_unset`,
  null semantics, `ConflictMerge` for same-slot files, the `koanf` tag, warnings for unknown keys, `slog.Default()`, and
  the debounce timings.
- **Layout settings** come only from a preset: scopes and their patterns, bootstrap files, the include key, decoders and
  format order, resolvers, policies, and env-var bindings.

`New` has only the engine defaults. Without layout settings, most failures would be **silent**:

| Missing                        | What happens                                                                                   | Silent?            |
| ------------------------------ | ---------------------------------------------------------------------------------------------- | ------------------ |
| Scopes                         | Nothing is searched. `Load` returns an empty result, and the app runs on zero values.          | Yes                |
| Built-in scope definitions     | `WithScopes(ScopeUser)` names an undefined scope.                                              | No: error at `New` |
| Decoders                       | Only formats with a decoder are searched, so files exist but never load.                       | Yes                |
| An include key                 | `include = [...]` is plain data and ends up in the result.                                     | Yes                |
| The `file` resolver (with an include key) | Every local include fails.                                                          | No                 |
| Env-var bindings               | `MYAPP_CONFIG_DIR` and the rest have no effect.                                                | Yes                |
| Bootstrap files                | `.myapprc.toml` is never read.                                                                 | Yes                |

**Decided:** `New` and `NewWithPreset` reject the combinations that can only be mistakes, as `*OptionError`s:

1. **No scopes at all.**
2. **A file-reading scope with no registered decoder.** `New` doesn't register JSON implicitly, so a bare loader stays
   bare.
3. **An include key with no `file` resolver.**

Everything else, such as no env bindings or no bootstrap files, is a legitimate minimal setup. `Describe()` reports "no
preset" and lists the empty layout settings, so what the loader was configured to do is visible.

### `Preset` and `Extend`

```go
type Preset struct { /* name, base, and an ordered list of options */ }

func NewPreset(name string, opts ...Option) Preset
func (p Preset) Extend(opts ...Option) Preset
func (p Preset) ExtendAs(name string, opts ...Option) Preset
func (p Preset) Name() string
```

- **Creating and naming** (DL-97):
  - **`NewPreset(name, opts...)`** creates a preset from nothing.
  - **`Extend`** keeps the receiver's name.
  - **`ExtendAs(name, opts...)`** creates a named extension and records the receiver as its base. That's how
    `MiseHardenedLayout` extends `MiseLayout`, and `misehardened.Preset` extends `MiseHardenedLayout`.
  - **A zero `Preset{}`** has no name and no options, so `NewWithPreset(n, Preset{})` is the same as `New(n)`.
- **A preset is a name plus an ordered list of options.** Nothing is applied until `New`, which runs the preset's
  options, then the extensions, then `New`'s own `opts`, in that order, onto a fresh configuration.
- **That makes "applied in order" literally true.** `WithScopes(A, B, C)` followed by `WithoutScopes(B)` selects `A` and
  `C`, because the options run one after another.
- **`Extend` never changes its receiver.** It returns a new `Preset` with a copied option list, so shared package-level
  presets such as `mise.Preset` can't be altered through an extension.
- **`Name()`** appears in `Describe()`, for example "`misehardened` (extends `MiseHardenedLayout`)".
- **Layouts and ready-to-use presets are the same type,** as decided in [Package layout](#package-layout).

### Option rules

- **Sealed.** `Option` is an interface with an unexported method, so only this package can create options. The set can
  grow later without breaking anyone. Other modules don't need their own options: they provide *values* that core
  options take, as in `cascadingconfig.WithDecoder(toml.Decoder())`.
- **Naming says how an option combines:**
  - **`With…` for a single setting** (`WithLogger`, `WithTag`): the last one wins.
  - **`With…` for a list entry** (`WithDecoder`, `WithMergeRule`, `WithMergeFunc`, `WithLayerTransform`): it is added. A
    merge rule or function for a path that already has one replaces it.
  - **`With…s` replaces a whole set** (`WithScopes`, `WithFormats`, `WithEnvs`, `WithConfigFiles`).
    - **Called with no arguments,** `WithEnvs()` and `WithConfigFiles()` mean *not set*, so an app can always pass its
      flag values, and an empty flag leaves `{NAME}_ENV` and `{NAME}_CONFIG_FILE` in charge. `WithScopes()` and
      `WithFormats()` with no arguments are `*OptionError`s, because an empty set can only be a mistake.
    - **Exceptions:** `WithProbes`, `WithFlatPaths` and `WithUnknownKeys` end in *s* but set a single setting. They
      follow the single-setting rule.
  - **`Without…` removes** (`WithoutScopes`, `WithoutIncludeResolver`, `WithoutEnvBinding`, `WithoutUnset`).
  - **`Require…` turns on a check** (`RequireConfig`).
- **Grouping:** `Options(opts...)` bundles several options into one, so an app can share a group.
- **Errors** (DL-104): each problem is an `*OptionError` whose `Op` is the option's name, for example
  `cascadingconfig: WithFormats: unknown format "xml"`. Every problem from one `New` is returned, joined.
- **Empty entries are dropped** in `WithEnvs` and `WithConfigFiles`, as in `{NAME}_ENV`. So `WithConfigFiles("")`, from
  an empty flag, means *not set* (DL-99).
- **Programmatic values only.** Options set the app's values. Env vars and bootstrap files override them where design.md
  says so, for example [format precedence](design.md#file-format-precedence). Options never read the environment
  themselves.

### Option kinds

Options that only make sense in one place get their own type, so the compiler rejects them anywhere else:

| Type           | Passed to                                    | Examples                                                                                    |
| -------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `Option`       | `New`, `Preset.Extend`                       | everything in the catalogue below                                                           |
| `ScopeKind`    | `WithScope(name, kind, …)`                   | `FixedDir`, `Walk`, `ExplicitFile`, `Embedded`, `EmbeddedFS`, `EnvVars`, `Values`             |
| `ScopeOption`  | `WithScope(name, kind, …)`                   | `Before`, `After`, `Trust`                                                                  |
| `PolicyOption` | `WithDefaultPolicy`, `WithScopePolicy`       | `InheritDefault`, `AllowResolvers`, `Allow`, `AllowFor`, `RequirePinned`, `Confine`, `RequireTrust`, `FilePermissions` |
| `Matcher`      | `Allow`, `AllowFor`                          | `Prefix`, `Glob`, `Regexp`, `MatchFunc`                                                      |
| `WatchOption`  | `Watch`, `Watch[T]`                          | `WithDebounce`, `WithPolling`, `WithRemotePoll`, `WithReloadOnAnyEvent`                     |
| `EnvVarsOption` | `EnvVars(prefix, …)`                        | `MapEnv`                                                                                    |

- **Watch options go on the `Watch` call,** not on the loader, because watching is optional. A loader that's never
  watched never sees them. The file-change *source* (`WithNotifier`) is the exception and stays on the loader, because
  it belongs to the filesystem hook, and presets register it.
- **One policy option set for both levels.** `WithDefaultPolicy(policyOpts...)` and
  `WithScopePolicy(scope, policyOpts...)` take exactly the same options.

### Layout options

The built-in layouts are built from these options, so a user's layout can do everything they do. **Proposed shapes.**
Each one's exact signature is settled at the test review of the milestone that implements it, which also adds it to the
layouts (DL-94).

| Area           | Options                                                                                                   |
| -------------- | --------------------------------------------------------------------------------------------------------- |
| Resolution     | **Settled** (DL-139): `WithResolution(MergeAll \| FirstMatch)`                                            |
| Patterns       | `Pattern(template, variant, root)` inside a scope kind. `template` uses `{name}`, `{env}` and `{ext}`; `variant` is `VariantShared`, `VariantEnv`, `VariantLocal` or `VariantEnvLocal`; `root` says how the config root is derived (the scope dir, or the file's own dir). |
| Includes       | `WithIncludeKey(key, nesting)`, where `nesting` allows or forbids includes in included files              |
| Bootstrap      | **Settled** (DL-131, DL-132): `BootstrapFiles(patterns...)` on a scope, `WithBootstrapKeys`, `WithoutBootstrapKey`, `WithBootstrapLimit` and `WithBootstrapTransform` |
| Limits         | `WithMaxIncludeSize(n)` for remote includes (default 1 MiB), alongside `WithMaxFileSize(n)` for local files |
| Directories    | **Settled** (DL-112): `WithPlatformDirs(XDGDirs)` for the mise layouts, and `EnvPathsDirs` for the cosmiconfig layout (M4d). It replaces the proposed `WithUserDir` and `WithSystemDir` pair. |
| Scope kinds    | `Values(tree)` for a fixed tree, and `ValuesFunc(func(ctx context.Context) (Tree, error))` for one read on every load |
| Env bindings   | **Settled** (DL-98): `type Binding string`, with constants such as `BindConfigDir` (`"CONFIG_DIR"`), `BindEnv` and `BindConfigFormats`. `WithEnvBinding(BindEnv, "MYAPP_CONFIG_ENV")` declares or renames a binding; an empty name means the default `{NAME}_<suffix>`. A custom scope can declare its own binding. |

### Settled signatures

These are the options whose behaviour exists so far, with their signatures settled (DL-94). The rest of the catalogue
follows in later milestones.

```go
func Options(opts ...Option) Option

// Scopes (DL-100)
func WithScopes(names ...ScopeName) Option
func WithoutScopes(names ...ScopeName) Option
func WithScope(name ScopeName, kind ScopeKind, opts ...ScopeOption) Option
func Before(scope ScopeName) ScopeOption
func After(scope ScopeName) ScopeOption
func Trust(level TrustLevel) ScopeOption
func Values(tree Tree) ScopeKind // DL-103

// Formats (DL-101)
func WithDecoder(d Decoder) Option
func WithFormats(formats ...string) Option
func WithDefaultFormats(formats ...string) Option // a layout's default order: formats with no decoder are skipped
func WithFormatConflict(policy FormatConflict) Option

// Merging (DL-102)
func WithMergeRule[P PathSpec](path P, strategy MergeStrategy) Option
func WithMergeFunc[P PathSpec](path P, f MergeFunc) Option
func WithArrayMerge(strategy MergeStrategy) Option // Replace, Append or Prepend
func WithUnsetKey(key string) Option
func WithoutUnset() Option

// Environments and files
func WithEnvs(names ...string) Option
func WithConfigFiles(paths ...string) Option

// Env bindings (DL-98)
type Binding string

const (
	BindConfigFile              Binding = "CONFIG_FILE"
	BindConfigDir               Binding = "CONFIG_DIR"
	BindGlobalConfigFile        Binding = "GLOBAL_CONFIG_FILE"
	BindSystemConfigDir         Binding = "SYSTEM_CONFIG_DIR"
	BindSystemConfigFile        Binding = "SYSTEM_CONFIG_FILE"
	BindEnv                     Binding = "ENV"
	BindCeilingPaths            Binding = "CEILING_PATHS"
	BindIgnoredConfigPaths      Binding = "IGNORED_CONFIG_PATHS"
	BindOverrideConfigFilenames Binding = "OVERRIDE_CONFIG_FILENAMES"
	BindConfigFormats           Binding = "CONFIG_FORMATS"
)

func WithEnvBinding(b Binding, name string) Option
func WithoutEnvBinding(b Binding) Option
```

#### Settled in M4a

```go
// Loading (DL-107)
func (l *Loader) Load(ctx context.Context) (*Result, error)

// Files (DL-115)
func WithFileSystem(fsys FileSystem) Option
func MountFS(dir string, fsys fs.FS) FileSystem // fsys appears at the absolute dir; nothing else exists
func WithMaxFileSize(n int64) Option           // default 10 MiB
func WithLogger(l *slog.Logger) Option         // default slog.Default()

// Directories (DL-112)
type PlatformDirs interface{ /* sealed */ }

var XDGDirs PlatformDirs // user: $XDG_CONFIG_HOME/{name}, else ~/.config/{name}; system: /etc/{name} or %ProgramData%\{name}

func WithPlatformDirs(d PlatformDirs) Option

// Scope kinds (DL-111, DL-113)
type Pattern struct {
	Template string // a relative file path with {name} and {ext}; later milestones add fields
}

func FixedDir(dir string, patterns ...Pattern) ScopeKind // dir: absolute, or starting with ~, {user} or {system}
func EmbeddedFS(fsys fs.FS, patterns ...Pattern) ScopeKind
func ValuesFunc(f func(ctx context.Context) (Tree, error)) ScopeKind
func EnvVars(prefix string, opts ...EnvVarsOption) ScopeKind
func MapEnv(f func(name, value string) (Path, any, bool)) EnvVarsOption

// Layer statuses so far: LayerLoaded, LayerEmpty, LayerIgnored, LayerUnreadable (DL-110)
// Warning codes so far: WarnDuplicateKey, WarnFormatConflict, WarnUnreadable, WarnNotAFile, WarnBrokenSymlink,
// WarnReservedKey, WarnEnvVarSkipped, WarnEnvVarClash, WarnNoHome
```

#### Settled in M4b

```go
// Patterns (DL-117 to DL-120)
type Pattern struct {
	Template string
	Variant  Variant
	Root     RootMode
	Key      string
}

type RootMode int // RootSearchDir (the default), RootFileDir

// Scope kinds and scope options (DL-122, DL-125, DL-126)
func Walk(patterns ...Pattern) ScopeKind
func ExplicitFile(paths ...string) ScopeKind
func ConfigRoot(dir string) ScopeOption
func ReplacedByFile(b Binding) ScopeOption
func DisabledBy(b Binding) ScopeOption
func ReplacedByConfigFiles() ScopeOption

// mise's global-directory patterns, as MiseLayout's system and user scopes use them (DL-130). Each call returns a
// fresh copy.
func MiseGlobalPatterns() []Pattern

// The walk and ignored paths (DL-122, DL-127)
type WalkStop int // StopAtHome (the default), StopAtRoot

func WithStartDir(dir string) Option
func WithWalkStop(s WalkStop) Option
func WithCeilingPaths(paths ...string) Option
func WithIgnoredPaths(paths ...string) Option
```

- **`Embedded` is gone,** replaced by `Pattern.Key` (DL-120).

#### Settled in M4c

```go
// Bootstrap files (DL-131 to DL-134)
func BootstrapFiles(patterns ...Pattern) ScopeOption // FixedDir and Walk only

// Pattern gains a field:
//	SkipHomeAndRoot bool // Walk only: not searched in ~ or /

type BootstrapKey string

const (
	BootstrapEnv                     BootstrapKey = "env"
	BootstrapFormats                 BootstrapKey = "formats"
	BootstrapCeilingPaths            BootstrapKey = "ceiling_paths"
	BootstrapIgnoredConfigPaths      BootstrapKey = "ignored_config_paths"
	BootstrapOverrideConfigFilenames BootstrapKey = "override_config_filenames"
)

func WithBootstrapKeys(keys ...BootstrapKey) Option
func WithoutBootstrapKey(key BootstrapKey) Option
func WithBootstrapLimit(scope ScopeName, keys ...BootstrapKey) Option
func WithBootstrapTransform(f func(ctx context.Context, l Layer, t Tree) (Tree, error)) Option

// Warning codes added: WarnBootstrapUnknownKey, WarnBootstrapKeyDropped
```

- **`WithBootstrap` is gone,** replaced by `BootstrapFiles` and the key options (DL-131, DL-132).

#### Settled in M4d

```go
// Resolution (DL-139)
type Resolution int // MergeAll (the default), FirstMatch

func WithResolution(r Resolution) Option

// Walk stops and search strategies (DL-140); WalkStop gains StopAtStart and StopAtMarker
func WithStopDir(dir string) Option
func WithRootMarker(names ...string) Option

type SearchStrategy int // SearchNone, SearchProject, SearchGlobal

func WithSearchStrategy(s SearchStrategy) Option

// Choosing a file's format (DL-141)
func WithFileFormat(m Matcher, formats ...string) Option
func Extension(exts ...string) Matcher
func FileName(glob string) Matcher
func Glob(pattern string) Matcher
func Regexp(expr string) Matcher
func MatchFunc(f func(path string) bool) Matcher

// Directories and explicit files (DL-143, DL-144); FixedDir gains the {cwd} placeholder
var EnvPathsDirs PlatformDirs

func WithConfigFilesOnly() Option

// Bootstrap keys added (DL-142): BootstrapSearchPlaces ("searchPlaces"), BootstrapMergeSearchPlaces ("mergeSearchPlaces")
```

- **`WithExtensionlessFormat` is gone,** replaced by `WithFileFormat` (DL-141).
- **Sealed kinds.** `Option`, `ScopeKind`, `ScopeOption`, `EnvVarsOption`, `PolicyOption`, `Matcher` and `WatchOption`
  are interfaces with unexported methods. The last three exist as types now, so the compiler already rejects them in the
  wrong place, and get their options in later milestones.
- **The rules for each group** are in design.md: [Choosing scopes](design.md#choosing-scopes),
  [Custom scopes](design.md#custom-scopes), [File format precedence](design.md#file-format-precedence),
  [Per-key merge rules](design.md#per-key-merge-rules), [Environment variables](design.md#environment-variables) and
  [Value overrides](design.md#value-overrides-env-vars-and-flags).

#### Settled in M5

```go
// Decoding (DL-148 to DL-151)
func Load[T any](ctx context.Context, l *Loader) (T, error)
func (r *Result) Decode(v any) error
func DecodeAt[P PathSpec](r *Result, path P, v any) error
func DecodeHook() mapstructure.DecodeHookFunc // koanf's three hooks, composed

func WithTag(tag string) Option
func WithDecoderConfig(dc *mapstructure.DecoderConfig) Option
func WithFlatPaths() Option

type UnknownKeys int // UnknownKeysWarn (the default), UnknownKeysError, UnknownKeysIgnore

func WithUnknownKeys(u UnknownKeys) Option

// Defaults (DL-153)
const ScopeDefaults ScopeName = "defaults"

func WithDefaults(v any) Option

// Requirements (DL-157)
type Require uint

const RequireAny Require = 0
const (
	RequireFile Require = 1 << iota
	RequireNonEmpty
)

func RequireConfig(level Require, scopes ...ScopeName) Option

// Reports (DL-158, DL-160)
func (r *Result) Describe() *Report
func WithProbes() Option
func (l *Loader) KoanfProvider() *KoanfProvider

type KoanfProvider struct{ /* … */ }

func (p *KoanfProvider) Read() (map[string]any, error)
func (p *KoanfProvider) ReadBytes() ([]byte, error)

// Added to existing types
//   Origin.EnvVar (DL-152)
//   DecodeError.Key, DecodeError.Unknown (DL-150, DL-151)
//   LayerFailed, LayerDuplicate (DL-156, DL-158); WarnUnknownKey (DL-151)
//   String and MarshalText on Variant, TrustLevel, LayerStatus and OriginKind; MarshalText and UnmarshalText on Path
```

- **An empty tag, a nil `DecoderConfig`, or one with `Metadata`,** is an `*OptionError`, as is an unknown `UnknownKeys`
  value or `Require` flag.
- **`Load[T]`** decodes into a fresh `T`, which can be any type mapstructure decodes into, such as a struct, a pointer
  to one, or a map.

### Option catalogue

| Area          | Options                                                                                                                     |
| ------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Scopes        | `WithScopes`, `WithoutScopes`, `WithScope`, `WithPlatformDirs`, `WithDefaults`, `WithStartDir` (default: the current directory at each `Load`), `WithWalkStop`, `WithCeilingPaths`, `WithIgnoredPaths`, `WithSearchStrategy`, `WithStopDir`, `WithRootMarker` |
| Environments  | `WithEnvs`                                                                                                                  |
| Formats       | `WithDecoder`, `WithFormats`, `WithDefaultFormats`, `WithFormatConflict`, `WithFileFormat`                                                       |
| Files         | `WithConfigFiles`, `WithFileSystem`, `WithNotifier`, `WithMaxFileSize`                                                      |
| Merging       | `WithMergeRule`, `WithMergeFunc`, `WithArrayMerge`, `WithUnsetKey`, `WithoutUnset`, `WithLayerTransform`, `WithResultTransform`              |
| Includes      | `WithIncludeResolver`, `WithoutIncludeResolver`, `WithoutRemoteIncludes`, `WithIncludeCache`, `WithoutIncludeCache`, `WithDefaultPolicy`, `WithScopePolicy` |
| Env bindings  | `WithEnvBinding`, `WithoutEnvBinding`                                                                                       |
| Bootstrap     | `WithBootstrapKeys`, `WithoutBootstrapKey`, `WithBootstrapLimit`, `WithBootstrapTransform`                                  |
| Decoding      | `WithTag`, `WithDecoderConfig`, `WithFlatPaths`, `WithUnknownKeys`                                                          |
| Behaviour     | `RequireConfig`, `WithLogger`, `WithProbes`                                                                                 |

### Example

```go
import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

var appPreset = mise.Preset.Extend(
	cascadingconfig.WithScopes(cascadingconfig.ScopeUser, cascadingconfig.ScopeProject),
	cascadingconfig.WithMergeRule("plugins.*", cascadingconfig.Replace),
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.AllowResolvers("file"),
	),
)

func main() {
	loader, err := cascadingconfig.NewWithPreset("myapp", appPreset,
		cascadingconfig.WithConfigFiles(flagConfigFiles...),
		cascadingconfig.WithEnvs(flagEnvs...),
	)
	// …
	cfg, err := cascadingconfig.Load[Config](ctx, loader)
	// …
	w, err := cascadingconfig.Watch[Config](ctx, loader, onChange,
		cascadingconfig.WithDebounce(200*time.Millisecond, 2*time.Second),
	)
}
```

### Decisions

| # | Question                         | Decision                                                                                                 |
| - | -------------------------------- | -------------------------------------------------------------------------------------------------------- |
| 1 | `New`'s signature                | The preset is optional: `New(name, opts...)` starts bare, and `NewWithPreset(name, preset, opts...)` starts from a preset. Both reject the three [mistaken combinations](#a-loader-without-a-preset). |
| 2 | Applying presets                 | Only at `New` / `NewWithPreset`. A `Preset` is an ordered option list.                                   |
| 3 | Sealed options                   | Yes: an unexported interface method. Other modules supply values, not options.                           |
| 4 | Separate option kinds            | Yes: `ScopeKind`, `ScopeOption`, `PolicyOption`, `Matcher`, `WatchOption`.                               |
| 5 | Watch options                    | On the `Watch` call, except `WithNotifier`, which stays on the loader.                                   |
| 6 | Package-level presets            | Variables (`mise.Preset`), documented as "never reassign".                                               |
| 7 | Policy options                   | `WithDefaultPolicy(...)` replaces `WithIncludeAllow` and `WithIncludeAllowFor`, and takes the same options as `WithScopePolicy`. design.md is updated. |

## Results

**Status: decided.**

- **The merged tree is the core result.** Merging, `_unset`, null handling, provenance and hot-reload diffing all need
  it anyway, so exposing it costs nothing extra.
- **Struct decoding is a thin layer** on top of the tree.
- **Both are available:** `Load[T]` decodes straight into `T`. `loader.Load` returns a `*Result`, with `Tree()`,
  `Decode(&v)` and `Describe()`. `cascadingconfig.DecodeAt(res, "server", &s)` decodes one subtree, like koanf's
  `Unmarshal(path, …)`. It is a function rather than a method because it takes a generic path ([Core types](#path)).
- **The tree type** is a plain nested `map[string]any`, the same shape koanf's `Provider.Read` returns. Keys are never
  flattened.

## Decoding

**Status: decided.** The aim is that a struct already annotated for koanf decodes the same way here, unchanged.

**Compatibility with koanf** (checked against koanf v2.4.1, `koanf.go` `UnmarshalWithConf`):

| Aspect          | koanf v2.4.1                                                                 | cascadingconfig                                      |
| --------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------- |
| Decoder library | `github.com/go-viper/mapstructure/v2` (v2.4.0)                               | The same library.                                    |
| Struct tag      | `koanf`; `UnmarshalConf.Tag` overrides it                                    | `koanf` by default; `WithTag("config")` overrides it. |
| Default hooks   | day-suffix durations (`"7d"`), `StringToTimeDurationHookFunc`, `encoding.TextUnmarshaler` | The same three, in the same order (DL-148). koanf's hooks are unexported, so we reimplement them with the same behaviour. `DecodeHook()` returns them composed. |
| Dates           | go-toml hands TOML date-times through as `time.Time`                          | Every date-time is an RFC 3339 string in the tree ([design.md](design.md#format-dialects)). The `TextUnmarshaler` hook already parses it into a `time.Time` field, because `time.Time` implements `UnmarshalText`, and a `string` field gets the text. A date-only or local date-time string into `time.Time` is a `*DecodeError`. Through `KoanfProvider`, koanf's own decoding sees the same strings and decodes them the same way. |
| Weak typing     | `WeaklyTypedInput: true`                                                     | The same, so env-var strings such as `"8080"` decode into `int`. |
| Custom decoder  | `UnmarshalConf.DecoderConfig` replaces the defaults                          | `WithDecoderConfig(*mapstructure.DecoderConfig)`, with the same semantics (DL-149): the app's config, hooks included, replaces ours, so an app that wants day durations composes `DecodeHook()` into its own. The config is copied when the option is applied. We set `Result`, and `TagName` falls back to `koanf` if empty. A `Metadata` in it is an `*OptionError`, because concurrent decodes would all write to it; unknown keys are in the report and the error instead. |
| Flat paths      | `UnmarshalConf.FlatPaths`                                                    | `WithFlatPaths()`.                                   |
| Key delimiter   | `.` by default                                                               | `.`, as in [merge-rule paths](design.md#per-key-merge-rules). |

**Using koanf directly:** `loader.KoanfProvider()` returns a value whose `Read()` runs a load and gives the merged tree
(DL-160). It loads with `context.Background()`, as koanf's own providers have no context, and returns any load error.
Its `ReadBytes()` returns an error, because the tree has no single byte form. It satisfies `koanf.Provider` by method
set alone, so cascadingconfig doesn't import koanf. An app can then run `k.Load(loader.KoanfProvider(), nil)`, use our
discovery and merging, and keep its koanf decoding code.

**`DecodeAt`** (DL-160) decodes the subtree at a path. A path that isn't in the tree is not an error, and `v` is left
untouched, as in koanf. A path string that doesn't parse, or a `v` that isn't a non-nil pointer, is a programming error:
a `*BaseError` with code 400. On a failed result, `Decode` and `DecodeAt` return the load's error again.

**Unknown keys** are keys in the tree that no struct field consumes (DL-151):

- **Default: ignore, with a warning.** Each unknown key is logged through the [logger](design.md#warnings-and-logging),
  with the file that set it, for example "`sever.port` (set by `./myapp.toml`) matches no field". It is also recorded in
  the report, as a `WarnUnknownKey` warning.
- **Strict mode:** `WithUnknownKeys(cascadingconfig.UnknownKeysError)` makes each unknown key a `*DecodeError` with
  `Unknown` set, naming the key and its origin. They're returned joined.
- **Silent:** `WithUnknownKeys(cascadingconfig.UnknownKeysIgnore)` neither logs nor records them, for apps that decode
  one tree into several partial structs.
- **Recorded on the result.** Unknown keys are found when decoding, after the load, so the `Result` records those of
  every decode made through it, once each, under a lock. `Describe()` returns a snapshot. `Load[T]` decodes once.
- **Inside arrays,** a key is named as mapstructure names it, for example `servers[0].hots`, and attributed to that
  element's layer.
- **How they're found:** through mapstructure's `Metadata.Unused`. If the app sets `ErrorUnused` itself, mapstructure's
  error is wrapped in a `*DecodeError`.

**Decode errors carry provenance, never values** (DL-150). Each key that fails is its own `*DecodeError`, with `Key` set
to the key, `Path` to the file that set it and `Scope` to its scope, and several are joined. For example:
"`server.port`: expected int, got string, set by `~/work/app/myapp.local.toml`". The value is left out, because config
often holds secrets and errors end up in logs. Mapstructure's own error, which does quote it, is still reachable through
`Unwrap`. The file named is the value's own layer: an array element's layer for an element, the latest layer for a table
several layers merged into, `defaults` for a default, and the variable (`Origin.EnvVar`) for a key from `EnvVars`.

**`null` and defaults set in Go code.** Decoding keeps koanf's mapstructure setting, `ZeroFields: false`:

- **Null leaves a field untouched.** So if the app pre-fills its struct with defaults before decoding, a `null` in
  config keeps the Go default rather than clearing it.
- **Only Go-side defaults are affected.** Within the tree, null still replaces lower values
  ([design.md](design.md#null-values-yaml-and-json)). A default that lives in a config layer is cleared correctly.
- **`_unset` also leaves it untouched:** the key is absent from the tree, so mapstructure never visits the field.
- **Pointer fields don't tell null from unset.** With no Go-side default, a `*int` stays `nil` after a `null`, after an
  `_unset` and when the key was never set. It only tells "has a value" from "has none". A pre-filled pointer keeps its
  default in all three cases.

| To know…                              | Use                                                                                                  |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| null, unset or never set              | The tree: a key present with a nil value is null, and an absent key is unset or never set. `OriginOf` shows kind `Null`, or an unset step in its history. |
| That config cleared a default         | Supply defaults with [`WithDefaults`](#defaults), not as a pre-filled struct value. A higher `null` or `_unset` then removes them from the tree, and a pointer field reads `nil`. |
| That `null` cleared a Go-side default | `WithDecoderConfig` with `ZeroFields: true`. Null then sets the field to its zero value. `_unset` still keeps the default, because the key is absent. `ZeroFields` also empties map fields before decoding into them, so pre-filled map entries are replaced, not merged. |

There is **no three-state field type** (such as a `Nullable[T]`). The tree and `OriginOf` already tell null, unset and
never-set apart, so one isn't needed.

## Load and Watch

**Status: decided.**

- **`Load` is the default** use case. Watching is optional.
- **`Watch` takes a handler function.** It does the first load synchronously and returns an error if that fails. After
  that it calls the handler only on [changes and failed reloads](design.md#hot-reload).

```go
type Change[T any] struct {
	Old, New T       // decoded snapshots; New is the last good config if Err != nil
	Result *Result   // the new tree and report
	Diff   []KeyDiff // changed keys, with old and new origins
	Err    error     // non-nil for a failed reload
}
```

- **A decode failure during a reload is a failed reload.** The last good config is kept, and the handler gets `Err`.
- **Every reload works exactly like an initial load.** Each load and reload decodes into a **fresh** value, never into
  the previous snapshot. Otherwise a key deleted from a file would keep its old value, because mapstructure leaves
  fields it doesn't visit untouched. Defaults are applied the same way on every load and reload.

### Defaults

**Status: decided** (DL-72).

Defaults are a **scope**: the lowest layer of every load. `WithDefaults` builds that scope from a Go value:

```go
cascadingconfig.WithDefaults(Config{
	Port: 8080,
	Log:  LogConfig{Level: "info"},
})
```

- **Converted once, at `New`.** The value is turned into a tree using the same struct tags as decoding (`koanf` by
  default). Only non-zero fields are included, because a fresh `T` already holds zeros. Values are converted to the
  [canonical forms](#tree-and-values): for example, a `time.Duration` becomes `"5s"`, a `time.Time` becomes its RFC 3339
  text, and a type that implements `encoding.TextMarshaler` becomes its text.
- **Below every other scope.** It's part of every load and reload, so `Load[T]`, `Watch[T]`, `res.Decode` and
  `KoanfProvider` all see the same defaults.
- **Config can clear a default** with `null` or `_unset`, like any other layer.
- **Traceable:** `Describe()` lists the defaults scope, and `OriginOf` reports "set by defaults".
- **Trust:** `TrustSystem`, because the values come from the app's own code. They aren't a file, so
  `RequireConfig(RequireFile)` doesn't count them.
- **The scope** (DL-153) is `ScopeDefaults` (`"defaults"`), of kind `Defaults` in the report:
  - **Always the lowest, and always selected,** so a later `WithScopes` can't silently drop it. This is the one
    exception to "a custom scope needs `WithScopes`". `WithoutScopes(ScopeDefaults)` still removes it.
  - **A name clash** with a scope the app defines is the usual "already defined" `*OptionError`.
  - **A second `WithDefaults`** replaces the first.
  - **The struct tag** is `WithTag`'s, or `WithDecoderConfig`'s `TagName`, read after every option is applied, so the
    order of options doesn't matter.
  - **Zero values:** a nil pointer is left out, but a non-nil pointer to a zero value is included. That is how an app
    sets an explicit zero default.
  - **Invalid values** are `*OptionError`s naming the field: a nil value, a value that isn't a struct, a pointer to one
    or a map with string keys, a map with other keys, a func, chan or complex value, and a `uint64` beyond the `int64`
    range.
  - **All-zero defaults** give an empty layer, which counts as nothing found for `RequireConfig`.
- **Pre-filling still works** with `res.Decode(&cfg)`, for apps that prefer it. It isn't available to `Load[T]` or
  `Watch[T]`, which always decode into a fresh value, so `WithDefaults` is the documented route.
- **One mechanism.** There's no `SetDefaults()` method or `Defaulter` interface.
- **`w.Current()`** returns the latest good snapshot, lock-free. `w.Reload(ctx)` forces a reload, and `w.Close()` stops
  watching.
- **Untyped watching:** `loader.Watch(ctx, func(cascadingconfig.Change[cascadingconfig.Tree]))`.

**Lifecycle:**

- **`ctx` bounds the watcher.** Cancelling it stops watching, as `Close` does.
- **`Close`** is idempotent. It waits for a running handler to return, and it's safe to call from inside the handler,
  where it returns without waiting.
- **`Reload(ctx)`** returns an `error`. It calls the handler only if the reload changed something or failed.
- **What counts as a change:** a difference in the merged values. `time.Time` values are compared as instants (`Equal`),
  and NaN equals NaN. A value that moves to another file with the same value isn't a change, unless
  `WithReloadOnAnyEvent` is set.
- **Panics:** a handler that panics is recovered. The panic is logged as an error, and watching continues.
- **One at a time:** handler calls never overlap.

## Concurrency and ownership

**Status: decided.**

- **The loader is safe for concurrent use.** Several goroutines can call `Load` at once, alongside a watcher.
- **App callbacks must be too.** Transforms, matchers, `MergeFunc`s, `ValuesFunc`, `MapEnv`, permission policies, trust
  stores, and custom scopes, decoders and resolvers can all be called from several goroutines at once.
- **Callers own what they get.** Every callback receives its own deep copy of any tree, so it can modify it freely.
  `Result.Tree()` also returns a copy, so changing it can't affect the report, a watch snapshot or another caller.
- **Transforms run only on layers that will merge.** Layers that are ignored, untrusted, empty, or not chosen by a
  first-match search are never transformed.

## Errors

**Status: decided.**

1. **Functions return plain `error`.**
2. **The concrete value is a typed error with a code.** Codes follow HTTP status codes where that makes sense. Each
   specific error type embeds the base type, which defines the code method.
3. **Each error type has a sentinel**, so callers can use `errors.Is` for the kind and `errors.As` for the details.

```go
type Code int

// Coder is implemented by every cascadingconfig error.
type Coder interface {
	error
	Code() Code
}

// Code constants, for CodeOf comparisons.
const (
	CodeBadRequest           Code = 400
	CodeForbidden            Code = 403
	CodeNotFound             Code = 404
	CodeConflict             Code = 409
	CodeTooLarge             Code = 413
	CodeUnsupportedMediaType Code = 415
	CodeUnprocessable        Code = 422
	CodeCanceled             Code = 499 // nginx's "client closed request"
	CodeInternal             Code = 500
	CodeNotImplemented       Code = 501
	CodeBadGateway           Code = 502
	CodeGatewayTimeout       Code = 504
	CodeLoopDetected         Code = 508
)

// BaseError is the base type that every specific error embeds. Its fields are exported, so other modules (resolvers,
// decoders) can build cascadingconfig errors themselves. It isn't called Error: a struct can't have a field and a method
// with the same name, so an embedded field named Error would hide the Error() method.
type BaseError struct {
	Op    string // "load", "decode", "include", "reload", …
	Path  string // the file or reference involved, if any
	Scope string // the scope involved, if any
	Err   error  // the underlying cause
	code  Code   // set only by NewError; specific types define their own Code
}

// NewError builds a base error for situations with no specific type, such as cancellation (CodeCanceled) or a failed
// app callback (CodeInternal).
func NewError(code Code, op, path string, err error) *BaseError

func (e *BaseError) Code() Code    // the code given to NewError
func (e *BaseError) Unwrap() error // e.Err
// Error returns "cascadingconfig: <op> <path>: <cause>", or "cascadingconfig: <op>: <cause>" without a path.
func (e *BaseError) Error() string

// Specific errors embed BaseError (by value) and define their own Code, so each type's code is fixed.
type ParseError struct {
	BaseError
	Format       string
	Line, Column int // 1-based; 0 when the decoder doesn't know
}

func (e *ParseError) Code() Code { return CodeBadRequest }
func (e *ParseError) Is(target error) bool { return target == ErrParse }
func (e *ParseError) Error() string // "cascadingconfig: <op> <path>:<line>:<col>: <cause>" when the position is known

var ErrParse = errors.New("cascadingconfig: parse error")

// MergeError names both layers' files.
type MergeError struct {
	BaseError        // Path is the higher layer's file
	Key   Path       // where in the tree
	Lower string     // the lower layer's file
}

// ConflictError lists the files in a slot under ConflictFail.
type ConflictError struct {
	BaseError          // Path is the highest-ranked file
	Files []string     // every file in the slot, highest-ranked first
}

// Built from another module, for example:
err := &cascadingconfig.FetchError{
	BaseError: cascadingconfig.BaseError{Op: "fetch", Path: ref, Err: cause},
	Timeout: true, // Code() is then CodeGatewayTimeout; otherwise CodeBadGateway
}

// CodeOf returns the code of the first cascadingconfig error in err's chain, or 0.
func CodeOf(err error) Code

// Errors splits err into its individual errors: any error with an Unwrap() []error method, such as one from
// errors.Join, is split, recursively. A single error is returned on its own, and nil gives nil.
func Errors(err error) []error
```

**Error types:**

| Type                      | Code | Sentinel               | When                                                                        |
| ------------------------- | ---- | ---------------------- | --------------------------------------------------------------------------- |
| `*OptionError`            | 400  | `ErrInvalidOption`     | Invalid loader configuration: unknown scope, invalid environment name, bad `formats` list, ambiguous merge rules. Returned by `New`, or at load time for values from env vars or bootstrap files. |
| `*ParseError`             | 400  | `ErrParse`             | A file isn't valid TOML, YAML or JSON, breaks a [dialect rule](design.md#format-dialects), or has a malformed reserved key (`_unset = 5`, `include = 3`). |
| `*ReferenceError`         | 400  | `ErrInvalidReference`  | An include reference fails validation, including [traversal](design.md#traversal-protection). |
| `*PolicyError`            | 403  | `ErrPolicy`            | Blocked by policy: allow-list, a scheme the policy doesn't allow, trust store, permission policy in error mode, confinement, unpinned remote. |
| `*NotFoundError`          | 404  | `ErrNotFound`          | An explicit config file or an include target doesn't exist.                 |
| `*ConflictError`          | 409  | `ErrConflict`          | More than one file in one slot with `ConflictFail`.                         |
| `*TooLargeError`          | 413  | `ErrTooLarge`          | A remote include is over the size limit.                                    |
| `*UnsupportedFormatError` | 415  | `ErrUnsupportedFormat` | No decoder for an extension or `Content-Type`.                              |
| `*DecodeError`            | 422  | `ErrDecode`            | Decoding into a struct failed: a type mismatch, or an unknown key in strict mode. One per key, with `Key` and `Unknown` set, and the file that set it in `Path`. |
| `*MergeError`             | 422  | `ErrMerge`             | A merge rule met the wrong type, or a `MergeFunc` returned an error.        |
| `*TransformError`         | 422  | `ErrTransform`         | A transform hook returned an error.                                         |
| `*FetchError`             | 502 / 504 | `ErrFetch`        | A remote include couldn't be fetched (504 for a timeout).                   |
| `*CycleError`             | 508  | `ErrIncludeCycle`      | An include cycle (`cosmiconfig.Preset`, which allows nesting).               |
| `*IOError`                | 500  | `ErrIO`                | Reading a file failed for a reason other than "not found", such as permission denied, or a directory where a file was expected. |
| `*UnsupportedSchemeError` | 501  | `ErrUnsupportedScheme` | An include uses a scheme with no registered resolver.                       |

**Other errors:**

- **Cancellation:** a load stopped by its context returns an error for which `errors.Is(err, context.Canceled)` or
  `errors.Is(err, context.DeadlineExceeded)` holds. It's a `*BaseError`, with code 499 for a cancellation, following
  nginx's "client closed request", and 504 for a deadline.
- **Errors from app callbacks** (trust stores, `ValuesFunc`, `MapEnv`, custom `Scope`s, matchers) are wrapped in a
  `*BaseError`, code 500, whose `Op` names the callback and whose `Unwrap` returns the app's error. Transform errors
  keep their own type, `*TransformError`.

**A failed load still returns a result** (DL-156). `loader.Load` returns a non-nil `*Result` alongside the error. Its
report shows what was found, what loaded and what broke, which is what a diagnostic command needs. Its `Tree()` is
`nil`, so half-merged config can't be used by mistake. `Load[T]` returns the zero `T` and the error; use `loader.Load`
when the report is needed.

- **A file that broke** is listed as `LayerFailed`. Its `Reason` is built from the error's fields, such as "parse error
  at line 3, column 7", never from the parser's message, which can quote the file's contents.
- **Decoding a failed result** with `Decode` or `DecodeAt` returns the load's error again.

**Several errors in one load** (DL-155):

- **A load runs in stages:** settings (the format order, bootstrap files and environments), then discovery and reading
  every file, then merging, then the `RequireConfig` checks.
- **Each stage reports every error it finds,** joined with `errors.Join`, so one run reports every broken file, not just
  the first.
- **A failed stage stops the load.** Merging only the layers that did decode would report misleading merge errors, and a
  requirement can't be judged without the files.
- **Inspecting them:** `errors.Is` and `errors.As` already search joined errors. `CodeOf` returns the first code it
  finds, and `cascadingconfig.Errors(err)` returns the individual errors.

**No config found is not an error.** A load that finds nothing gives an empty result, as cosmiconfig and mise do.
[`RequireConfig`](#requiring-config) turns it into a `*NotFoundError` (404).

Warnings are not errors. They go to the logger and the report.

## Requiring config

**Status: decided.**

By default, a load that finds nothing isn't an error. `RequireConfig` makes it one, at a level the app chooses:

```go
type Require uint

// RequireAny counts any loaded layer, of any kind. It's the most forgiving level.
const RequireAny Require = 0

// Flags that narrow what counts. Combine them with |.
const (
	RequireFile     Require = 1 << iota // only layers read from files: discovery, explicit files and includes
	RequireNonEmpty                     // empty layers don't count
)

func RequireConfig(level Require, scopes ...ScopeName) Option
```

- **The default level is forgiving.** `RequireConfig(RequireAny)` is satisfied by anything that loaded: a config file,
  an empty file, flags, env-var overrides, or a built-in defaults scope.
- **Flags narrow it:**
  - **`RequireFile`:** only real files count. Flags (`Values`), env-var overrides (`EnvVars`), compiled-in defaults
    (`Embedded`, `EmbeddedFS`) and `WithDefaults` don't.
  - **`RequireNonEmpty`:** files that are [empty](design.md#format-dialects) don't count.
  - `RequireFile | RequireNonEmpty` means "at least one real, non-empty config file".
- **Global or per scope.** With no scopes, the requirement is global: one qualifying layer anywhere is enough. With
  scopes, **each** listed scope must have a qualifying layer. For example, `RequireConfig(RequireFile, ScopeSystem)`
  means "a system config file must exist".
- **Several requirements combine.** Each `RequireConfig` option adds a requirement, and all of them must hold:

```go
cascadingconfig.RequireConfig(cascadingconfig.RequireAny),                                  // something, anywhere
cascadingconfig.RequireConfig(cascadingconfig.RequireFile|cascadingconfig.RequireNonEmpty,  // and a real system file
	cascadingconfig.ScopeSystem),
```

- **What counts** (DL-157):
  - **Empty value layers don't count at all.** A value scope with no keys, such as an `EnvVars` scope that matched no
    variable or all-zero defaults, counts as nothing found. Otherwise `RequireAny` would pass whenever such a scope is
    selected.
  - **Empty files** count for `RequireAny` and `RequireFile`, but not with `RequireNonEmpty`. `{}` isn't empty.
  - **Skipped layers don't count:** ignored, unreadable, failed and duplicate ones, files a first-match search didn't
    choose, and bootstrap files.
  - **Explicit files** count for the scope they replace, through `ReplacedByConfigFiles` or `ReplacedByFile`. Under
    `WithConfigFilesOnly`, they count for every scope a requirement lists.
- **Errors:** each unmet requirement is a `*NotFoundError` (404) that names the requirement, with `Op` `RequireConfig`.
  Several are joined, like other load errors. An unknown scope, or one that isn't selected, is an `*OptionError` at
  `New`.

## Describe

**Status: decided.**

`res.Describe()` returns a `*Report`:

- **Structured data,** which also prints as text (`String()`, like `mise config ls`) and encodes as JSON
  (`MarshalJSON`).
- **Loaded and skipped files by default.** Each entry has its path, scope, variant, env, format and config root, its
  status (loaded, empty, ignored, untrusted, bad permissions), the reason for any skip, and its warnings. A remote layer
  also shows its reference and fetched identity.
- **Searched locations where nothing was found** are listed only with `WithProbes()`, because that list can be long.
- **Per-key lookup:** `cascadingconfig.OriginOf(res, "server.port")` returns the layer that set the value, or the layers
  for `Append`/`Prepend`, plus any `_unset` or null history.
- **Context:** the report also lists the selected and unselected scopes, the active environments, the format order, each
  scope's effective policy (from M7, DL-159), the bootstrap files and the env-var bindings.
- **Duplicates** (DL-158): a file reached by two routes is listed at the position it loaded from, and the other route is
  a `LayerDuplicate` entry whose `Reason` names the route that was kept.
- **Unknown keys** found by decodes through the result are in `Warnings`, as `WarnUnknownKey` (DL-151).
- **JSON** uses `snake_case` field names, and enums encode as strings (`"loaded"`, `"project"`, `"env_local"`) through
  `MarshalText`. A `Path` encodes as its dotted string. Empty fields are left out.
- **Text:** `String()` prints a header (preset, formats, environments), then the scopes, then the bootstrap files and
  layers, one line each, grouped by scope and lowest first, as `mise config ls` does, then the bindings and the
  warnings. A layer with no path, such as a value scope's, is named after its scope.

## Core types

**Status: decided.**

### `Tree` and values

```go
type Tree = map[string]any
```

- **An alias, not a named type.** That way a `Tree` *is* a `map[string]any`. It passes straight to koanf, mapstructure,
  `encoding/json` and the app's own code, with no conversion.
- **Canonical values.** Every decoder must produce only these types, so that values from different formats merge,
  compare and diff consistently. A TOML `port = 80` and a JSON `"port": 80` must be equal.

| Kind             | Go type                    | Notes                                                                                   |
| ---------------- | -------------------------- | --------------------------------------------------------------------------------------- |
| null             | `nil`                      | YAML and JSON only.                                                                     |
| boolean          | `bool`                     |                                                                                         |
| integer          | `int64`                    | Only literals with no fraction or exponent that fit. JSON `1.0` is a float. Out of range is a `*ParseError`. |
| float            | `float64`                  |                                                                                         |
| string           | `string`                   |                                                                                         |
| date or time     | `string`                   | Every format, in RFC 3339 form, including TOML offset and local date-times ([design.md](design.md#format-dialects)). |
| array            | `[]any`                    |                                                                                         |
| table / map      | `map[string]any`           | Keys are strings. YAML non-string keys are turned into strings. The top level must be a map. |

### `Path`

```go
type Path []string

// PathSpec is accepted wherever the API takes a key path.
type PathSpec interface{ ~string | ~[]string }

func ParsePath(s string) (Path, error) // "server.tls" → Path{"server", "tls"}
func (p Path) String() string          // the dotted form, quoting segments where needed
```

- **Two forms, one parameter.** Functions that take a path are generic over `PathSpec`, so both
  `WithMergeRule("tools.*", Replace)` and `WithMergeRule(Path{"plugins", "foo.bar"}, Replace)` work. The string form is
  dot-separated. The list form is for keys that contain dots.
- **Parsing rules:**
  - **The empty string** is the root path, `Path{}`.
  - **Empty segments are an error:** `"a..b"`, `".a"` and `"a."` are `*OptionError`s where they come from options.
  - **Quoting:** a segment can be double-quoted, as in TOML dotted keys: `plugins."foo.bar"` is
    `Path{"plugins", "foo.bar"}`. Inside quotes, `\"` and `\\` escape a quote and a backslash.
  - **`String()`** quotes any segment that is empty or contains `.`, `"` or `\`, so every `Path` round-trips through
    `ParsePath(p.String())`.
- **Wildcards:** in merge rules, a segment of exactly `*` matches one key. A literal `*` key can't be addressed by a
  rule, which is a rare enough case to accept.

### `MergeStrategy`, `MergeFunc` and `FormatConflict`

```go
type MergeStrategy int // DeepMerge, Replace, Append, Prepend

// MergeFunc is a custom merge, set with WithMergeFunc. It gets copies of both values, and runs only when both layers
// set the path. An error it returns becomes a *MergeError.
type MergeFunc func(path Path, lower, higher any) (any, error)

type FormatConflict int // ConflictMerge (the default), ConflictFirst, ConflictFail
```

The rules for using them are in design.md's [Per-key merge rules](design.md#per-key-merge-rules) and
[File format precedence](design.md#file-format-precedence).

### `ScopeName`, `Variant` and `TrustLevel`

```go
type ScopeName string

const (
	ScopeSystem   ScopeName = "system"
	ScopeUser     ScopeName = "user"
	ScopeProject  ScopeName = "project"
	ScopeOverride ScopeName = "override"
)

type Variant int  // VariantShared, VariantEnv, VariantLocal, VariantEnvLocal
type TrustLevel int // TrustProject < TrustUser < TrustSystem
```

Custom scopes use their own `ScopeName`. Untyped string constants convert implicitly, so
`WithScopes(ScopeUser, "mounted")` compiles.

### `Layer`

`Layer` is the metadata for one source. It is passed to layer transforms, carried in errors, and listed in the report.

```go
type Layer struct {
	Path       string      // file path, or the reference as written for a remote include
	Scope      ScopeName
	Variant    Variant
	Env        string      // for VariantEnv and VariantEnvLocal
	Format     string      // "toml", "yaml", "json", …
	ConfigRoot string
	Pattern    string      // the pattern that matched, e.g. ".config/{name}/config.{ext}"
	Trust      TrustLevel
	Remote     *Remote     // set for remote includes: scheme, normalised reference, fetched identity (M8, DL-159)
	IncludedBy string      // Path of the including layer, if any
	Status     LayerStatus // LayerLoaded, LayerEmpty, LayerIgnored, LayerUnreadable, LayerFailed, LayerDuplicate, …
	Reason     string      // why the layer was skipped, if it was
	Warnings   []Warning
}
```

- **Metadata only.** A `Layer` never contains the layer's tree, so it is safe to log and to keep.
- **Order:** layers are always listed lowest precedence first, matching the order they merge in.

### `Result`, `Report` and `Origin`

```go
func (r *Result) Tree() Tree
func (r *Result) Decode(v any) error
func (r *Result) Describe() *Report

// Functions, not methods, because they take a generic path and Go methods can't have type parameters.
func DecodeAt[P PathSpec](r *Result, path P, v any) error
func OriginOf[P PathSpec](r *Result, path P) (Origin, bool) // "OriginOf" because the type is called Origin

type Report struct {
	Preset    string        // the name of the preset given to NewWithPreset, or ""
	Scopes    []ScopeInfo   // selected and unselected, lowest precedence first
	Envs      EnvInfo       // selected and platform environments, with where they came from
	Formats   []string      // effective format order
	Bootstrap []Layer       // the bootstrap files read, lowest precedence first
	Layers    []Layer       // loaded and skipped, lowest precedence first
	Probes    []Probe       // locations searched where nothing was found, only with WithProbes
	Bindings  []BindingInfo // every declared binding
	Warnings  []Warning     // warnings that belong to no layer, and unknown keys
}

type ScopeInfo struct {
	Name     ScopeName
	Kind     string // "FixedDir", "Walk", "ExplicitFile", "EmbeddedFS", "Values", "ValuesFunc", "EnvVars", "Defaults", "Scope"
	Trust    TrustLevel
	Selected bool
	Skipped  string // why a selected scope wasn't searched, such as "disabled by MYAPP_X" or "replaced by explicit files"
	// M7 adds the effective policy (DL-159).
}

type EnvInfo struct {
	Selected []string // lowest precedence first
	Source   string   // "WithEnvs", the variable's name, or the bootstrap file's path; "" when none are selected
	Platform []string // lowest precedence first
}

type Probe struct {
	Scope   ScopeName
	Dir     string
	Pattern string // the pattern searched, with {env} filled in
}

type BindingInfo struct {
	Binding Binding
	Var     string // the variable's name, such as MYAPP_ENV
	Set     bool   // never the value
}

func (r *Report) String() string
func (r *Report) MarshalJSON() ([]byte, error)

type Origin struct {
	Layer    Layer        // the latest layer to set, merge into, null or unset the value
	Kind     OriginKind   // what that layer did
	EnvVar   string       // the variable that set the value, for keys from EnvVars (DL-152)
	Elements []Layer      // OriginAppended and OriginPrepended only: each element's layer, in order
	History  []OriginStep // the earlier steps, oldest first
}

type OriginStep struct {
	Layer Layer
	Kind  OriginKind
}

// OriginSet, OriginMerged (a table more than one layer contributed to), OriginAppended, OriginPrepended, OriginNull,
// OriginFunc, OriginUnset
type OriginKind int
```

- **What `OriginOf` reports** (DL-91):
  - **A key in the result:** the latest step and its history. A function's result is attributed to the higher layer,
    with the lower one in the history. Keys inside a table a function returned have kind `OriginFunc` too.
  - **A key whose latest event was an unset:** kind `OriginUnset`, so `Describe()` can say "unset by `<file>`".
  - **Anything else** gives `false`: keys never set, the root, and path strings that don't parse.
  - **History restarts** when an enclosing table is replaced, set to null or unset. That event is in the table's own
    history, not repeated on every key below it.
  - **A no-op unset,** of a key that wasn't there, leaves no step.
  - **`EnvVar`** (DL-152) names the variable that set the value, for keys from an `EnvVars` scope, including through
    `MapEnv`. A scope is still one layer.

- **No config values in the report.** `Report.String()`, its JSON, `Origin` and the `String()` of a hot-reload `Diff`
  contain paths, keys and layer metadata, never values, because config often holds secrets. Values are available only
  through `Tree()`, `Decode` and the `Old`/`New` fields of `KeyDiff`. Env-var bindings report whether a variable is set,
  not its value.

### Extension interfaces

These are the contract between the core and the other modules. A change to one of them means releasing every module that
implements it.

```go
// Decoder parses one format. Implemented by format/json, format/toml, format/yaml.
type Decoder interface {
	Format() string        // "toml"
	Extensions() []string  // "toml"; for YAML "yaml", "yml": listed in rank order, highest first
	MediaTypes() []string  // "application/toml", for http(s) includes without an extension
	Decode(data []byte) (Document, error) // canonical values only
}

// Document is one decoded file.
type Document struct {
	Tree     Tree      // nil when Empty
	Empty    bool      // whitespace, comments only, or a null document; {} is not empty
	Warnings []Warning // for example a JSON duplicate key
}

// DecodeDocument runs a decoder with the rules every format shares: it strips a leading UTF-8 byte-order mark, rejects
// input that isn't UTF-8, turns any decoder error into a *ParseError (with the position, if the error has one), and
// checks that only canonical value types came out. A decoder that produces a non-canonical type is a bug in that
// decoder, reported as a *BaseError with CodeInternal. The loader decodes every file through it, and decoder modules
// test against it.
func DecodeDocument(d Decoder, path string, data []byte) (Document, error)

// Warning is a non-fatal problem. Warnings go to the logger and the report.
type Warning struct {
	Code    WarningCode // what kind of problem
	Path    string      // the file; DecodeDocument fills it in when a decoder leaves it empty
	Key     Path        // where in the file, if known
	Message string
}

type WarningCode string

const WarnDuplicateKey WarningCode = "duplicate-key" // more codes are added by the milestones that need them

// PositionError is implemented by decode errors that know where they happened.
// The loader turns any error into a *ParseError and copies the position when it's available.
type PositionError interface {
	error
	Position() (line, column int)
}

// IncludeResolver fetches includes. Implemented by the file resolver, httpresolver, gitresolver and ociresolver.
// (Defined in design.md: Resolver hooks.)
type IncludeResolver interface {
	Schemes() []string
	Resolve(ctx context.Context, ref IncludeRef) (*Fetched, error)
}

// FileSystem carries every read (design.md: Filesystem hook).
type FileSystem interface {
	OpenRoot(dir string) (Root, error)              // os.Root-style, confined to dir
	EvalSymlinks(path string) (string, error)
	SameFile(a, b fs.FileInfo) bool                 // file identity, for dedup
}

// OwnerInfo is implemented by the fs.FileInfo values a FileSystem returns, where the platform has owners. It arrives
// with the permission policy in M7 (DL-108).
type OwnerInfo interface {
	Owner() (uid int, ok bool)
}

type Root interface {
	fs.StatFS
	fs.ReadDirFS
	Close() error
}

// Notifier supplies file-change events for watching. Implemented by watch/fsnotify.
type Notifier interface {
	Add(dir string) error
	Remove(dir string) error
	Events() <-chan string // the path of a changed entry
	Errors() <-chan error
	Close() error
}

// Scope is the escape hatch for custom discovery (design.md: Custom scopes).
type Scope interface {
	Discover(ctx context.Context, d Discovery) ([]Source, error)
}

// Watchable is optionally implemented by a Scope that can report what to watch.
type Watchable interface {
	WatchDirs(ctx context.Context, d Discovery) ([]string, error)
}

// Discovery is what the loader passes to a Scope: app name, start dir, selected envs,
// format order, the filesystem, and the logger.
type Discovery struct { /* … */ }

// Source is one candidate a Scope found, lowest precedence first.
type Source struct {
	Path    string    // read through the FileSystem, so permission checks and confinement apply
	Tree    Tree      // or an already-decoded tree, for sources that aren't files (EnvVars, Values)
	Variant Variant
	Env     string
	Format  string    // empty: from Path's extension
	ConfigRoot string
}
```

- **The top-level-map rule lives in each decoder,** because a `Tree` can only hold a map. The other shared rules live in
  `DecodeDocument`, so no decoder has to repeat them.
- **Built-in kinds use the same interfaces.** `FixedDir`, `Walk` and the rest implement `Scope`, and the built-in
  decoders and resolvers implement `Decoder` and `IncludeResolver`. Nothing built in is special-cased.
- **Files go through the pipeline.** A `Source` with a `Path` is read by the loader, through the filesystem hook, so a
  custom scope gets permission checks, symlink handling and dedup for free. Only `Tree` sources skip file handling.

### Core type decisions

| # | Question                     | Decision                                                                                                   |
| - | ---------------------------- | ---------------------------------------------------------------------------------------------------------- |
| 1 | `Tree`                       | An alias for `map[string]any`.                                                                             |
| 2 | Canonical values             | As in the [table](#tree-and-values): `int64` for every integer, local TOML dates and times as strings, string keys only. |
| 3 | Paths                        | One generic `PathSpec` parameter. `DecodeAt` and `OriginOf` are therefore package functions taking the `*Result`. |
| 4 | Values in reports            | Never. Reports, origins and diff text carry paths, keys and metadata only.                                 |
| 5 | Extension interfaces         | As sketched. Before 1.0 they evolve only by adding optional interfaces, as `Watchable` and `PositionError` do. |

## Test helpers

**Status: decided** ([implementation-plan.md](implementation-plan.md#decisions)).

The core module includes a public `cctest` package, so that the other modules' tests, and app authors' own tests, can
exercise the loader without real files or timers:

- **`cctest.FS`:** an in-memory filesystem implementing the [`FileSystem`](#extension-interfaces) interface, with
  symlinks, permissions and owners (DL-109):
  - **`NewFS(files map[string]string)`** builds a tree from absolute paths and contents, creating directories as needed.
  - **Methods:** `WriteFile`, `Mkdir`, `Symlink`, `Link` (a hard link), `Mkfifo`, `Chmod` and `Remove`. Owners arrive in
    M7 (DL-108).
  - **Safe for concurrent use,** so watch tests can change it while a watcher reads it.
- **`cctest.Notifier`:** a fake `Notifier` whose events the test triggers.
- **`cctest.Clock`:** a fake clock for debounce, TTLs and polling. The loader's clock is internal, and `cctest` is the
  only way to replace it, so the public API has no `WithClock` option.
- **`cctest.Host`:** a fake host: env vars, home dir, current dir, `GOOS`, `GOARCH` and the current user's UID. The
  loader reads all of these through an internal host interface, never from the process directly, and `cctest` is the
  only way to replace it. So tests can run in parallel, and Windows and platform-environment behaviour can be tested on
  any machine.
- **How fakes reach a loader:** options are sealed, so `cctest` returns ready-made core `Option`s through an internal
  hook, for example `cctest.WithHost(cctest.Host{GOOS: "windows", Env: map[string]string{…}})`. The filesystem uses the
  public `WithFileSystem`.
- **What `Host.GOOS` changes** (DL-108): the defaults that depend on the OS, such as `%ProgramData%` instead of `/etc`,
  `USERPROFILE` instead of `HOME` for a real host, and case-insensitive env-var prefixes. Paths are still joined with
  the real OS's rules, so a Windows test checks where a directory came from, not its separators.
- **`cctest.Golden(t, file, got)`** compares `got` with a file under `testdata`, and rewrites the file when the test
  runs with `-update`. It's used for reports and discovery orders.
