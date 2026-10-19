# cascadingconfig API

This document designs the public Go API for the loader described in [design.md](design.md). Each section is marked as
decided or proposed. Names and signatures may still be refined while the API is built.

## Overview

```go
loader, err := cascadingconfig.New("myapp", mise.Preset /*, opts... */)

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

- **`New` validates every option** and returns an error straight away, so a misconfigured loader fails at start-up, not
  on the first `Load`.
- **Typed entry points are free functions** (`Load[T]`, `Watch[T]`), because Go methods can't have type parameters.

## Package layout

**Status: proposed.** The open questions are at the end of this section. The repository tree, tooling and measured
dependency counts are in [project-layout.md](project-layout.md).

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

loader, err := cascadingconfig.New("myapp", mise.Preset)
```

```go
// Minimal: mise's layout, TOML only, no remotes, no fsnotify.
// Module graph: cascadingconfig, mapstructure, cascadingconfig/format/toml, go-toml.
import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
)

loader, err := cascadingconfig.New("myapp", cascadingconfig.MiseHardenedLayout.Extend(
	cascadingconfig.WithDecoder(toml.Decoder()),
))
```

### Costs of several modules

- **Versioning:** each module is tagged separately, for example `format/toml/v0.1.0`. Each one requires a compatible
  core version, so a breaking core change means releasing several modules.
- **Local development** uses a `go.work` file, so the modules build against each other unreleased.
- **Tooling:** `go test ./...`, `golangci-lint run ./...`, `go mod tidy` and `govulncheck ./...` don't cross module
  boundaries. The lefthook commands and CI must loop over the modules (`go work` can list them).

### Open questions

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

## Results

**Status: decided.**

- **The merged tree is the core result.** Merging, `_unset`, null handling, provenance and hot-reload diffing all need
  it anyway, so exposing it costs nothing extra.
- **Struct decoding is a thin layer** on top of the tree.
- **Both are available:** `Load[T]` decodes straight into `T`. `loader.Load` returns a `*Result`, with `Tree()`,
  `Decode(&v)`, `DecodeAt("server", &s)` (decode one subtree, like koanf's `Unmarshal(path, …)`) and `Describe()`.
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
- **Telling null from unset:** use pointer fields with no Go-side default.
- **Clearable defaults:** ship them as a defaults scope, such as `EmbeddedFS` ([design.md](design.md#custom-scopes)),
  rather than as pre-filled struct values.

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
- **Per-key lookup:** `res.Origin("server.port")` returns the layer that set the value, or the layers for
  `Append`/`Prepend`, plus any `_unset` or null history.
- **Context:** the report also lists the selected and unselected scopes, the active environments, the format order, each
  scope's effective policy, and the env-var bindings.
