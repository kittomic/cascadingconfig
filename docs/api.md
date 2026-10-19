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
  registered. A format without a decoder is simply not searched. This only applies to the *layout's default* order. An
  order set by the app, an env var or a bootstrap file still errors on an unregistered format
  ([design.md](design.md#file-format-precedence)).
- **Names in design.md:** design.md uses the same names: `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset`
  for the ready-to-use presets, and the `…Layout` names for the core layouts.

### Modules and packages

| Import path (module root in **bold**)                          | Package        | Contents                                                                                         | Third-party dependencies                         |
| -------------------------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------ |
| **`github.com/kittomic/cascadingconfig`**                      | `cascadingconfig` | `New`, `Load[T]`, `Watch[T]`, `Result`, `Report`, errors, options, merging, decoding, layouts, the `file` resolver, the filesystem hook, stat polling and the reload logic | `go-viper/mapstructure/v2`                     |
| `…/cascadingconfig/format/json`                                 | `json`         | JSON decoder, registered by every layout                                                        | none (standard library)                          |
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
// Minimal: mise's layout, TOML only, no remotes, no fsnotify.
// Module graph: cascadingconfig, mapstructure, cascadingconfig/format/toml, go-toml.
import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
)

loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseHardenedLayout.Extend(
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
  null semantics, `FormatConflict: merge`, the `koanf` tag, warnings for unknown keys, `slog.Default()`, and the
  debounce timings.
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
type Preset struct { /* name, and an ordered list of options */ }

func (p Preset) Extend(opts ...Option) Preset
func (p Preset) Name() string
```

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
  - **`With…` for a list entry** (`WithDecoder`, `WithMergeRule`, `WithLayerTransform`): it is added.
  - **`With…s` replaces a whole set** (`WithScopes`, `WithFormats`, `WithEnvs`, `WithConfigFiles`).
  - **`Without…` removes** (`WithoutScopes`, `WithoutIncludeResolver`, `WithoutEnvBinding`, `WithoutUnset`).
  - **`Require…` turns on a check** (`RequireConfig`).
- **Grouping:** `Options(opts...)` bundles several options into one, so an app can share a group.
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

- **Watch options go on the `Watch` call,** not on the loader, because watching is optional. A loader that's never
  watched never sees them. The file-change *source* (`WithNotifier`) is the exception and stays on the loader, because
  it belongs to the filesystem hook, and presets register it.
- **One policy option set for both levels.** `WithDefaultPolicy(policyOpts...)` and
  `WithScopePolicy(scope, policyOpts...)` take exactly the same options.

### Option catalogue

| Area          | Options                                                                                                                     |
| ------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Scopes        | `WithScopes`, `WithoutScopes`, `WithScope`, `WithStartDir` (default: the current directory at each `Load`), `WithWalkStop`, `WithCeilingPaths`, `WithIgnoredPaths` |
| Environments  | `WithEnvs`                                                                                                                  |
| Formats       | `WithDecoder`, `WithFormats`, `WithFormatConflict`                                                                          |
| Files         | `WithConfigFiles`, `WithFileSystem`, `WithNotifier`                                                                         |
| Merging       | `WithMergeRule`, `WithUnsetKey`, `WithoutUnset`, `WithLayerTransform`, `WithResultTransform`                                |
| Includes      | `WithIncludeResolver`, `WithoutIncludeResolver`, `WithoutRemoteIncludes`, `WithIncludeCache`, `WithDefaultPolicy`, `WithScopePolicy` |
| Env bindings  | `WithEnvBinding`, `WithoutEnvBinding`, `WithoutBootstrapKey`                                                                |
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
| Default hooks   | day-suffix durations (`"7d"`), `StringToTimeDurationHookFunc`, `encoding.TextUnmarshaler` | The same three, in the same order. koanf's hooks are unexported, so we reimplement them with the same behaviour. |
| Weak typing     | `WeaklyTypedInput: true`                                                     | The same, so env-var strings such as `"8080"` decode into `int`. |
| Custom decoder  | `UnmarshalConf.DecoderConfig` replaces the defaults                          | `WithDecoderConfig(*mapstructure.DecoderConfig)`, with the same semantics. We set `Result`, and `TagName` falls back to `koanf` if empty. |
| Flat paths      | `UnmarshalConf.FlatPaths`                                                    | `WithFlatPaths()`.                                   |
| Key delimiter   | `.` by default                                                               | `.`, as in [merge-rule paths](design.md#per-key-merge-rules). |

**Using koanf directly:** `loader.KoanfProvider()` returns a value whose `Read()` gives the merged tree. Its
`ReadBytes()` returns an error, because the tree has no single byte form. It satisfies `koanf.Provider` by method set
alone, so cascadingconfig doesn't import koanf. An app can then run `k.Load(loader.KoanfProvider(), nil)`, use our
discovery and merging, and keep its koanf decoding code.

**Unknown keys** are keys in the tree that no struct field consumes:

- **Default: ignore, with a warning.** Each unknown key is logged through the [logger](design.md#warnings-and-logging),
  with the file that set it, for example "`sever.port` (set by `./myapp.toml`) matches no field". It is also recorded in
  the report.
- **Strict mode:** `WithUnknownKeys(cascadingconfig.UnknownKeysError)` makes them a `*DecodeError`, listing every
  unknown key with its origin.
- **How they're found:** through mapstructure's `Metadata.Unused`. If the app supplies its own `DecoderConfig` with
  `Metadata`, both are filled. If the app sets `ErrorUnused` itself, mapstructure's error is wrapped in a
  `*DecodeError`.

**Decode errors carry provenance.** For example: "`server.port`: expected int, got string `"80a"`, set by
`~/work/app/myapp.local.toml`".

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
| That config cleared a default         | Ship the default in a **defaults scope** (`EmbeddedFS`, [design.md](design.md#custom-scopes)), not as a pre-filled struct value. A higher `null` or `_unset` then removes it from the tree, and a pointer field reads `nil`. |
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
- **`w.Current()`** returns the latest good snapshot, lock-free. `w.Reload(ctx)` forces a reload, and `w.Close()` stops
  watching.
- **Untyped watching:** `loader.Watch(ctx, func(cascadingconfig.Change[cascadingconfig.Tree]))`.

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

// Error is the base type that every specific error embeds.
type Error struct {
	code  Code
	Op    string // "load", "decode", "include", "reload", …
	Path  string // the file or reference involved, if any
	Scope string // the scope involved, if any
	Err   error  // the underlying cause
}

func (e *Error) Code() Code     { return e.code }
func (e *Error) Unwrap() error  { return e.Err }
func (e *Error) Error() string  { /* "cascadingconfig: <op> <path>: <cause>" */ }

// Specific errors embed *Error and add their own fields and sentinel.
type ParseError struct {
	*Error
	Format       string
	Line, Column int
}

var ErrParse = errors.New("cascadingconfig: parse error")

func (e *ParseError) Is(target error) bool { return target == ErrParse }

// CodeOf returns the code of the first cascadingconfig error in err's chain, or 0.
func CodeOf(err error) Code
```

**Error types:**

| Type                      | Code | Sentinel               | When                                                                        |
| ------------------------- | ---- | ---------------------- | --------------------------------------------------------------------------- |
| `*OptionError`            | 400  | `ErrInvalidOption`     | Invalid loader configuration: unknown scope, invalid environment name, bad `formats` list, ambiguous merge rules. Returned by `New`, or at load time for values from env vars or bootstrap files. |
| `*ParseError`             | 400  | `ErrParse`             | A file isn't valid TOML, YAML or JSON.                                      |
| `*ReferenceError`         | 400  | `ErrInvalidReference`  | An include reference fails validation, including [traversal](design.md#traversal-protection). |
| `*PolicyError`            | 403  | `ErrPolicy`            | Blocked by policy: allow-list, trust store, permission policy in error mode, confinement, unpinned remote. |
| `*NotFoundError`          | 404  | `ErrNotFound`          | An explicit config file or an include target doesn't exist.                 |
| `*ConflictError`          | 409  | `ErrConflict`          | Several formats in one slot with `FormatConflict: error`.                   |
| `*TooLargeError`          | 413  | `ErrTooLarge`          | A remote include is over the size limit.                                    |
| `*UnsupportedFormatError` | 415  | `ErrUnsupportedFormat` | No decoder for an extension or `Content-Type`.                              |
| `*DecodeError`            | 422  | `ErrDecode`            | Decoding into a struct failed: type mismatch, or unknown keys in strict mode. |
| `*MergeError`             | 422  | `ErrMerge`             | A merge rule met the wrong type, or a `Func` rule failed.                   |
| `*TransformError`         | 422  | `ErrTransform`         | A transform hook returned an error.                                         |
| `*FetchError`             | 502 / 504 | `ErrFetch`        | A remote include couldn't be fetched (504 for a timeout).                   |
| `*CycleError`             | 508  | `ErrIncludeCycle`      | An include cycle (`cosmiconfig.Preset`, which allows nesting).               |

**Several errors in one load:**

- **All of them are returned,** joined with `errors.Join`, so one run reports every broken file, not just the first.
- **Inspecting them:** `errors.Is` and `errors.As` already search joined errors. `CodeOf` returns the first code it
  finds, and `cascadingconfig.Errors(err)` returns the individual errors.

**No config found is not an error.** A load that finds no files gives an empty result, as cosmiconfig and mise do.
`RequireConfig()` turns it into a `*NotFoundError` (404).

Warnings are not errors. They go to the logger and the report.

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
  scope's effective policy, and the env-var bindings.

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
| integer          | `int64`                    | JSON integers too. The JSON decoder uses `UseNumber` and converts whole numbers.         |
| float            | `float64`                  |                                                                                         |
| string           | `string`                   |                                                                                         |
| date-time with offset | `time.Time`           | TOML offset date-times, YAML timestamps.                                                |
| local date or time | `string`                 | TOML local dates and times, in their RFC 3339 text form, so nothing is lost. Decode hooks parse them. |
| array            | `[]any`                    |                                                                                         |
| table / map      | `map[string]any`           | Keys must be strings. A YAML map with non-string keys is a `*ParseError`.                |

### `Path`

```go
type Path []string

// PathSpec is accepted wherever the API takes a key path.
type PathSpec interface{ ~string | ~[]string }

func ParsePath(s string) (Path, error) // "server.tls" → Path{"server", "tls"}
func (p Path) String() string          // the dotted form
```

- **Two forms, one parameter.** Functions that take a path are generic over `PathSpec`, so both
  `WithMergeRule("tools.*", Replace)` and `WithMergeRule(Path{"plugins", "foo.bar"}, Replace)` work. The string form is
  dot-separated. The list form is for keys that contain dots.
- **Wildcards:** in merge rules, a segment of exactly `*` matches one key. A literal `*` key can't be addressed by a
  rule, which is a rare enough case to accept.

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
	Remote     *Remote     // set for remote includes: scheme, normalised reference, fetched identity
	IncludedBy string      // Path of the including layer, if any
	Status     LayerStatus // Loaded, Empty, Ignored, Untrusted, PermissionDenied, Unmatched, …
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
	Preset   string
	Scopes   []ScopeInfo   // selected and unselected, with kind, trust and effective policy
	Envs     EnvInfo       // selected and platform environments, with where each came from
	Formats  []string      // effective format order
	Layers   []Layer       // loaded and skipped, lowest precedence first
	Probes   []Probe       // locations searched where nothing was found, only with WithProbes
	Bindings []BindingInfo // env-var name, what it controls, whether it is set
	Warnings []Warning
}

type Origin struct {
	Layer   Layer        // the layer that set the current value
	Kind    OriginKind   // Set, Appended, Prepended, Null, Func
	History []OriginStep // earlier layers that set, unset or nulled it
}
```

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
	Extensions() []string  // "toml"
	MediaTypes() []string  // "application/toml", for http(s) includes without an extension
	Decode(data []byte) (Tree, error) // canonical values only
}

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
