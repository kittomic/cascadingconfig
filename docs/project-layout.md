# Project layout

This document lays out the repository, its Go modules and the tooling around them. It also measures how many
dependencies each setup pulls in. It puts into practice the [package layout](api.md#package-layout) decided in api.md: a
third-party dependency gets its own module, and an optional standard-library-only piece gets its own package.

**Status: decided.**

## Repository tree

```text
cascadingconfig/                    module github.com/kittomic/cascadingconfig (core)
├── go.mod                          one requirement: github.com/go-viper/mapstructure/v2
├── go.work                         every module below, released or not, for local development
├── cascadingconfig.go …            package cascadingconfig: New, NewWithPreset, Load[T], Watch[T], Result, Report,
│                                   errors, options, layouts, merging, decoding, file resolver,
│                                   filesystem hook, stat polling, reload logic
├── cctest/                         package cctest (core module): in-memory FS, fake notifier, fake clock, golden helpers
├── format/
│   ├── json/                       package json (core module): JSON decoder, encoding/json
│   ├── toml/
│   │   └── go.mod                  module …/format/toml: requires github.com/pelletier/go-toml/v2
│   └── yaml/
│       └── go.mod                  module …/format/yaml: requires github.com/goccy/go-yaml
├── httpresolver/                   package httpresolver (core module): net/http
├── gitresolver/                    package gitresolver (core module): runs the git program
├── ociresolver/
│   └── go.mod                      module …/ociresolver: requires oras.land/oras-go/v2
├── watch/
│   └── fsnotify/
│       └── go.mod                  module …/watch/fsnotify: requires github.com/fsnotify/fsnotify
├── presets/
│   ├── go.mod                      module …/presets: requires all of the above
│   ├── mise/                       package mise:         mise.Preset
│   ├── misehardened/               package misehardened: misehardened.Preset
│   └── cosmiconfig/                package cosmiconfig:  cosmiconfig.Preset
├── internal/                       core-only helpers (the JSON parser, tree copying, path safety, …); the merge
│                                   engine is in the root package, because it uses the public types (DL-86)
│   └── compat/
│       └── go.mod                  module …/internal/compat (test-only, never released): koanf parity tests
├── examples/
│   └── go.mod                      module …/examples (never released)
├── scripts/                        each-module.sh, check-go-versions.sh, check-gowork-off.sh, check-licenses.sh,
│                                   check-deps.sh
├── docs/
├── LICENSE                         Apache-2.0; every module directory above also has its own copy
├── README.md, SECURITY.md
├── .config/, .golangci.yml, lefthook.yml
└── .gitignore
```

## Module boundaries

- **The core never imports another module in this repo.** Dependencies point one way: `format/*`, `ociresolver`,
  `watch/fsnotify` and `presets` require the core, and only `presets` requires the others.
- **Other modules use only the core's public API.** Go's `internal/` rule goes by import path, not module, so
  `…/format/toml` *could* import `…/cascadingconfig/internal/…`. We don't allow it. A released `format/toml` must keep
  working with newer core releases, and internal packages carry no compatibility promise. The interfaces these modules
  implement (`Decoder`, `IncludeResolver`, the filesystem notifier) are public core API.
- **JSON comes with every layout.** It uses only `encoding/json`, so the core layouts register a JSON decoder by
  default. `MiseHardenedLayout` alone can therefore already load JSON files. TOML and YAML are added explicitly, or
  through a preset.
- **Where the JSON parser lives:** `format/json` imports the core, so the core can't import it. The parser lives in
  `internal/json`, which doesn't import the core. The core's layouts wrap it in a private decoder, and `format/json`
  wraps the same parser, so both decode identically (DL-95). `format/json` is a package of the core module, so it may
  use the core's internal packages.
- **Testing:** core tests use JSON and an in-memory filesystem, so they don't need the TOML or YAML modules.
  Cross-module integration tests live in `presets`, which depends on everything.

## Versioning and releases

- **Tags per module:** the core is tagged `vX.Y.Z`, and each other module by its directory, for example
  `format/toml/vX.Y.Z` and `presets/vX.Y.Z`.
- **Before 1.0,** every module stays at `v0`. A breaking core change means a new minor version of the core, plus new
  releases of the modules that implement the changed interfaces.
- **Order:** the core is released first. Other modules then raise their core requirement and are released, with
  `presets` last.
- **`go.work` is committed,** so working in the repo builds every module against its local siblings. It has no effect on
  consumers.
- **Release check:** each module is also built and tested with `GOWORK=off`, from lefthook's pre-push for now and from
  CI later. That catches a module that silently depends on unreleased core changes.
- **Replacing the core:** modules that are never released (`internal/compat`, `examples`) may `replace` the core with
  the repository's copy, so they build with `GOWORK=off` too. Until the first release, so may the modules that will be
  released (`format/toml`, `format/yaml` and the rest), because the repository is private and the core can't be fetched
  (DL-162). From the first release on, a released module never does: M11 swaps in the released version and adds a check.

## Tooling changes

Today the hooks run from the repo root against `./...`. Go commands, `golangci-lint` and `govulncheck` work one module
at a time, so with several modules each command has to loop:

```sh
# list the workspace modules' directories (go.work must exist)
for dir in $(go list -m -f '{{.Dir}}'); do
  (cd "$dir" && go mod tidy -diff)
done
```

| Hook (lefthook.yml)                | Change                                                                                          |
| ---------------------------------- | ----------------------------------------------------------------------------------------------- |
| `go-mod-tidy` (pre-commit)         | Loop over the modules. Also match any `go.mod` or `go.sum`, not only the root ones.             |
| `go-lint` (pre-commit)             | Loop over the modules, each with `--new-from-rev=HEAD`.                                         |
| `go-fmt` (pre-commit)              | No change: it runs on staged files.                                                             |
| `go-test` (pre-push)               | Loop over the modules, with `-race`.                                                            |
| `golangci-lint` (pre-push)         | Loop over the modules.                                                                          |
| `govulncheck` (pre-push)           | Loop over the modules. Then each module is checked against its own dependencies.               |
| `deps` (pre-push, from M6)         | `scripts/check-deps.sh` builds a throwaway consumer for each setup whose modules exist and checks the counts below (DL-173). |

In workspace mode, `go test ./...` from the root may already match packages in every workspace module. That should be
verified when the modules are created, but the loop is used regardless, so that every tool behaves the same way.

CI is deferred ([implementation-plan.md](implementation-plan.md#decisions)). Until it exists, lefthook runs every check,
including the `GOWORK=off` release check, as scripts in `scripts/` that CI will later reuse. The tests run on two Go
versions: the minimum from the `go` directive (Go 1.26, through `mise exec go@1.26 --`) and the latest, so that the
stated minimum is actually tested.

## Dependency analysis

**Method.** Stub modules were built in a scratch workspace, one per proposed module, at the proposed import paths. Each
stub blank-imports its real library. A small consumer app was then created for each setup, and `go mod tidy` was run (Go
1.27.2, 2026-10-19). Four things were counted, excluding `github.com/kittomic/…` itself:

- **go.mod:** third-party requirements written to the consumer's `go.mod`
- **go.sum:** distinct third-party modules in the consumer's `go.sum`
- **Build:** distinct third-party modules compiled into the binary (`go list -deps`)
- **Graph:** modules listed by `go list -m all`, the full module graph that some SBOM tools read

| Setup                                           | go.mod | go.sum | Build | Graph |
| ----------------------------------------------- | -----: | -----: | ----: | ----: |
| Core only (JSON)                                |      1 |      1 |     1 |     1 |
| **Minimal: core + TOML**                        |  **2** |  **2** | **2** | **2** |
| Core + TOML + YAML                              |      3 |      3 |     3 |     3 |
| `misehardened` (TOML, YAML, fsnotify)             |      5 |      5 |     5 |     5 |
| `mise`, everything (adds OCI, http, git)        |      9 |      9 |     9 |    11 |
| `misehardened`, imported from one `presets` module |     5 |      5 |     5 |     9 |

**Modules in each setup**, with the versions `go mod tidy` selected:

| Module                                | Version | Own dependencies    | Core | Minimal | misehardened | mise |
| ------------------------------------- | ------- | ------------------- | :--: | :-----: | :--------: | :--: |
| `github.com/go-viper/mapstructure/v2` | v2.5.0  | none                |  ✓   |    ✓    |     ✓      |  ✓   |
| `github.com/pelletier/go-toml/v2`     | v2.4.3  | none                |      |    ✓    |     ✓      |  ✓   |
| `github.com/goccy/go-yaml`            | v1.19.2 | none                |      |         |     ✓      |  ✓   |
| `github.com/fsnotify/fsnotify`        | v1.10.1 | `golang.org/x/sys`  |      |         |     ✓      |  ✓   |
| `golang.org/x/sys`                    | v0.13.0 | none                |      |         |     ✓      |  ✓   |
| `oras.land/oras-go/v2`                | v2.6.4  | the three below     |      |         |            |  ✓   |
| `github.com/opencontainers/go-digest` | v1.0.0  | none                |      |         |            |  ✓   |
| `github.com/opencontainers/image-spec`| v1.1.1  | (graph only, below) |      |         |            |  ✓   |
| `golang.org/x/sync`                   | v0.24.0 | none                |      |         |            |  ✓   |

**Findings:**

- **The minimal setup needs 2 third-party modules,** `mapstructure` and `go-toml`, and neither has dependencies of its
  own. JSON-only needs just 1.
- **The full `mise` setup's graph lists 2 extra modules,** `russross/blackfriday` v1.6.0 and
  `santhosh-tekuri/jsonschema/v5` v5.3.1. Both come from `opencontainers/image-spec`'s `go.mod`. They are never
  downloaded into `go.sum` and never compiled, but they appear in `go list -m all`.
- **One `presets` module** ([api.md](api.md#package-layout-decisions)): an app that imports only `presets/misehardened`
  gets the same 5 modules in its `go.mod`, `go.sum` and build as with separate modules. But `go list -m all` also lists
  `oras-go`, `go-digest` and `image-spec` (9 instead of 5). So the cost of one module is limited to tools that read the
  full module graph.
- **`golang.org/x/sys` resolves to v0.13.0,** because that's what fsnotify v1.10.1 requires. That version dates from
  2023. `watch/fsnotify` should require a current `x/sys`, so that consumers get it through minimum version selection.
- **The `git` and `http(s)` resolvers add nothing** to any count, because they use only the standard library and the
  `git` program.
- **Caveat:** the stubs blank-import one or two packages from each library. Real code may import more packages from the
  same modules, which changes package counts but not module counts.

## Decisions

| # | Question                    | Decision                                                                                                   |
| - | --------------------------- | ---------------------------------------------------------------------------------------------------------- |
| 1 | The `go` directive          | `go 1.26.0` in every module and in `go.work`: the oldest supported Go release, and the minimum consumers need. `filepath.IsLocal` (Go 1.20) and `os.Root` (Go 1.24) are both older. The dev toolchain stays at the latest Go, through mise. |
| 2 | Committing `go.work`        | Committed, with the `GOWORK=off` release check (lefthook now, CI later).                                                        |
| 3 | JSON in every layout        | Yes. JSON uses only `encoding/json`, so every core layout registers it, through the parser in `internal/json` that `format/json` also wraps (DL-95). |
| 4 | One `presets` module        | One module for now. It may be reconsidered later, for example if SBOM noise from `go list -m all` becomes a problem. |
