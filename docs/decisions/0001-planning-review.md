# 0001: Planning review

**Date:** 2026-10-19. **Status:** decided; every finding is resolved and applied to the docs.

This records a review of the planning docs before implementation started. It lists every finding, the recommendation
made, and the decision taken. The resulting behaviour is in [design.md](../design.md), [api.md](../api.md),
[project-layout.md](../project-layout.md) and [implementation-plan.md](../implementation-plan.md). Decision-log entries
DL-42 to DL-80 in design.md come from this review.

**Sources:**

- **A consistency review:** contradictions and stale text across the five docs.
- **A gap review:** questions an implementer would hit.
- **Library tests** run against `goccy/go-yaml` v1.19.2, `pelletier/go-toml/v2` v2.4.3 and `encoding/json`.

**How it was worked through:** one group at a time. Each item had a recommendation, the decision was recorded here, and
the doc changes were applied and checked before the next group.

| Group | Topic                                | Items              | Status |
| ----- | ------------------------------------ | ------------------ | ------ |
| 1     | Fixes that need no decision          | F1–F14             | **applied** |
| 2     | cosmiconfig fidelity                 | A1–A5              | **applied** |
| 3     | Values and formats                   | C1–C7              | **applied** |
| 4     | Remote includes: pins, matchers, cache, git and OCI | B1, P1–P3, Q1, R1–R2 | **applied** |
| 5     | Files and paths                      | J1, K1–K4, L1–L4   | **applied** |
| 6     | API and testability                  | D1, E1, I1, N1–N2  | **applied** |
| 7     | Errors, concurrency and watching     | F-E1, G1, H1       | **applied** |
| 8     | Bootstrap files and includes         | M1, O1             | **applied** |
| 9     | Repository prerequisites             | S1–S4              | **applied** |

---

## Group 1: Fixes that need no decision

These were wording that contradicted decisions already made, so they were approved as a block.

| ID  | Where                                   | Problem                                                                                                       | Fix |
| --- | --------------------------------------- | ------------------------------------------------------------------------------------------------------------- | --- |
| F1  | design.md Hot reload, "API sketch"      | It uses a non-generic `loader.Watch(ctx, func(Change))` and says "the API design will settle the details".    | Use `Watch[T](ctx, loader, fn, opts...)` and `Change[T]`, and link api.md. |
| F2  | design.md Directory walk                | It says `WalkStop(Root)`, but the catalogue has `WithWalkStop`.                                               | Use `WithWalkStop(Root)`. |
| F3  | design.md Hot reload                    | "The OS filesystem uses fsnotify" contradicts the core having no fsnotify dependency.                         | The OS filesystem polls; `watch/fsnotify` supplies events through `WithNotifier`; tests use `cctest.Notifier`. |
| F4  | design.md decision log DL-16; plan M4a  | Explicit files replace "the project scope only".                                                               | Qualify it: in `cosmiconfig.Preset` they replace everything. |
| F5  | design.md Explicit config files         | "Bootstrap files are still searched from the start dir."                                                       | Qualify it per preset: cosmiconfig's meta config is the current directory only. |
| F6  | design.md File format precedence        | "The only override that also changes bootstrap discovery" ignores the app's own option.                       | "The only source outside the app's code that…" |
| F7  | design.md File format precedence        | "A format can only appear in the list if a decoder is registered" contradicts api.md's layout-default exception. | Limit it to orders set by the app, an env var or a bootstrap file. |
| F8  | design.md Safe names, Formats           | "Error at start-up" for env-var and bootstrap values.                                                          | They fail at `Load`. Options fail at `New`. |
| F9  | design.md Value overrides vs Hot reload | "Read again on each reload" vs "only on a manual `Reload`".                                                    | Every reload re-reads them, but only a manual `Reload` is *triggered* by them. |
| F10 | design.md Custom scopes vs Value overrides | Custom scopes default to project trust, yet `EnvVars`/`Values` are "TrustUser" with no `Trust(...)` set.    | A scope kind carries its own default trust; these two default to user trust. |
| F11 | api.md Examples; plan M7                | The "minimal" example uses `MiseHardenedLayout` but calls it plain mise. M7 tests `mise.Preset`, which only exists in M10. | Fix the example's comment, and use `MiseLayout` in M7. |
| F12 | design.md Loader model                  | "Users can add, remove, reorder and rename scopes."                                                            | "Add (placed with `Before`/`After`) and select". There's no reorder or rename. |
| F13 | design.md Environments; config-locations.md | `myapp.dev.local.toml` is called "the *shared* file of `dev.local`".                                         | It's the *env* file of `dev.local`. |
| F14 | Several                                 | Leftovers: "The two presets" (there are three); "proposed in api.md" (decided); "the `GOWORK=off` CI release check" (it runs in lefthook). The project-layout tree is missing `scripts/`, `cctest/`, `examples/` and the `internal/compat` module. | Fix each. |

**Decision:** accepted as a block, and applied to the docs.

---

## Group 2: cosmiconfig fidelity

The cosmiconfig preset claims to mirror cosmiconfig, but differs in five places.

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| A1 | **Default scopes.** design.md gives `user, project`. cosmiconfig's default strategy (`none`) searches only the start dir, and only `global` ever reads the global dir. | Default to the start dir only. The `user` scope and the walk come on with the `Global` strategy. | Accepted. |
| A2 | **`$import` arrays.** cosmiconfig concatenates arrays (`mergeImportArrays: true`). We replace them everywhere, and "concatenation is opt-in" has no option. | Add `WithArrayMerge(Append)`, a default strategy for arrays, and set it in `cosmiconfig.Preset`. | Accepted. |
| A3 | **Search strategies.** There are no options for cosmiconfig's `searchStrategy`, `stopDir`, the `package.json` marker or `packageProp`. That blocks M4c. | Add `WithSearchStrategy(None\|Project\|Global)`, `WithStopDir(dir)` (inclusive), `WithRootMarker("package.json", "package.yaml")` and `WithPackageProp(path)`. A stop dir with a non-global strategy is an `*OptionError`, as in cosmiconfig. | **Changed:** no Node.js-specific sources: no `package.json`/`package.yaml`, no `packageProp`, no default `package.json` root marker. Users can add them (`Embedded`, `WithRootMarker`). The strategy options and stop-dir rules are kept. |
| A4 | **User dir.** design.md says the user dir "follows mise on every OS", but cosmiconfig uses `env-paths`: `~/Library/Preferences/{name}` on macOS and `%APPDATA%\{name}\Config` on Windows. | XDG for the mise presets; `env-paths` locations for `cosmiconfig.Preset`. | Accepted. |
| A5 | **Extensionless `.{name}rc`.** cosmiconfig parses it as YAML, which also accepts JSON. `CosmiconfigLayout` has only JSON, so the file would silently never load. | Add `WithExtensionlessFormat(format)`. The cosmiconfig layout uses YAML if a YAML decoder is registered, and JSON otherwise. | Accepted, plus: `.yaml` and `.yml` are both YAML, and `.yaml` wins at the same precedence (it sorts first alphabetically). |

---

## Group 3: Values and formats

Mostly from the library tests.

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| C1 | **Dates.** `goccy/go-yaml` gives YAML timestamps as **strings**. api.md says `time.Time`. go-toml gives offset date-times as `time.Time` and local ones as `toml.LocalDate` and similar types. | **Every** date-time is an RFC 3339 string in the tree, including TOML's, so the formats agree. Add a string-to-`time.Time` decode hook. That's one hook beyond koanf's defaults: koanf with TOML would hand `time.Time` straight through. | Accepted: dates are strings, which defers the type choice to decoding. The koanf difference is documented. |
| C2 | **Integers.** YAML gives `uint64`, including values above the int64 range. JSON `1.0` and `1e3` are undefined. | An `int64` only when the literal has no fraction or exponent and fits. Out of range is a `*ParseError`. | Accepted. |
| C3 | **YAML dialect.** Tested: `no`, `yes`, `on`, `off` and `y` stay strings (YAML 1.2). Non-string keys (`1:`, `true:`) are quietly turned into strings, which contradicts api.md's "a non-string key is a parse error". Multi-document files, anchors, merge keys and tags are undefined. | YAML 1.2 core schema. Keys are turned into strings, as goccy does, and the parse-error rule is dropped. Several documents in one file are a `*ParseError`. Alias expansion is capped. Unknown tags are errors. | Accepted: YAML 1.2 rules. |
| C4 | **Duplicate keys.** Tested: YAML and TOML both error, but `encoding/json` silently keeps the **last** value. | A `*ParseError` in every format. The JSON decoder adds a check at the token level. | **Changed:** each format's own semantics. JSON keeps the last value and **logs a warning**; TOML and YAML error. |
| C5 | **JSON and TOML dialects.** Comments, trailing commas, TOML 1.0 vs 1.1 and a byte-order mark are undefined. | Strict JSON, with no comments: cosmiconfig uses plain `JSON.parse`. TOML 1.0. A UTF-8 byte-order mark is stripped in every format. | Accepted the refined recommendation, after testing the libraries: strict JSON; TOML as the pinned go-toml accepts, which is TOML 1.1 syntax as tested; a UTF-8 byte-order mark stripped and non-UTF-8 input rejected. JSONC is recorded in [future-ideas.md](../future-ideas.md), out of scope for now. |
| C6 | **"Empty" and the top level.** Is a comments-only file, `{}` or a YAML `~` empty? What does a JSON `[1,2]` at the top level do? First-match search depends on the answer. | Empty means whitespace or comments only, or a document that decodes to null. A top level that isn't a map is a `*ParseError`. | Accepted, with one refinement: `{}` is **not** empty, so an explicit empty config stops a first-match search, as in cosmiconfig. |
| C7 | **What `RequireConfig` counts.** Should it count empty layers, `Values`/`EnvVars`, or a defaults scope (which would mean it never fires)? | Only non-empty layers from files. | **Changed:** `RequireConfig(level, scopes...)`. The default level counts any source; `RequireFile` and `RequireNonEmpty` flags narrow it; it works globally or per scope, and requirements combine. |

---

## Group 4: Remote includes

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| B1 | **Pins.** The hardened per-scope example enables `https`, but only git SHAs and OCI digests count as pinned. Hardened requires pins, so the example can never load. | **git:** a full 40- or 64-hex commit id (an abbreviated id doesn't count). **OCI:** `@sha256:<64 hex>`. **https:** an integrity fragment, `#sha256=<hex>`, verified after download. Fix the example. | Accepted. |
| P1 | **`Glob` semantics.** Does `*` cross `/`? Is there `**`? The `git::https://github.com/acme/*` example depends on it. | `*` matches within one segment and `**` crosses `/`. Document it with examples. | Accepted. |
| P2 | **`Regexp` anchoring, and the normalised reference string.** Is the whole string matched, and does it include the ref and inner path? | `Regexp` is fully anchored. Document the normalised form for each scheme, including the ref and inner path, with examples. | Accepted. |
| P3 | **Trust-store details.** Which glob syntax does `TrustPaths` use? What algorithm and encoding is the digest? | `TrustPaths` uses the same globs as P1. The digest is SHA-256 hex of the raw bytes. | Accepted. |
| Q1 | **The cache's lookup key.** Before a fetch, how is a cached entry found, when cache paths must never derive from the URL? What is the http identity? There's no way to turn caching off. | An index keyed by a hash of the normalised reference, which is allowed because it's hash-only. The http identity is the content's SHA-256. Add `WithIncludeCache(dir, ttl)` and `WithoutIncludeCache()`. Cached content is checked against every policy again. | Accepted. |
| R1 | **Isolating git.** The user's `~/.gitconfig` `url.insteadOf` rewrites URLs *after* our allow-list check. Credential helpers and `includeIf` also run. `--end-of-options` needs git 2.24. Some servers refuse to fetch by SHA. | Isolate git's config (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`), with credential helpers opt-in. Require git 2.24 or newer. Fall back to fetching the ref and checking the commit is reachable. | Accepted, plus `gitresolver.WithProtocols(...)` (default `https`, `ssh`) so tests can reach `file` fixtures. |
| R2 | **OCI artifacts.** `oras push` stores each file as its own blob, named by an annotation, not as a tar layer. Credentials, plain-HTTP test registries, duplicate paths across layers and decompression limits are undefined. | Support both the per-file artifacts and tar layers. The last layer wins. Decompression is capped. Add a credentials option and a plain-HTTP option for tests. | Accepted. |

---

## Group 5: Files and paths

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| J1 | **A file reached two ways.** With the cwd under `~`, the user config is also a project file of `~`, and loads "at its higher position", the project one. Would it then get project trust, so the user's own global config needs project approval? | Precedence comes from the highest route. Trust, policy and `Layer.Scope` come from the most trusted route. `Describe()` records both. | Accepted. |
| K1 | **Symlinks leaving the searched dir,** such as dotfile links. `os.Root` refuses symlinks that escape its root. | Follow them: resolve the link, open the target's directory as a new root, and apply dedup and `Confine` to the target. | Accepted. |
| K2 | **Special files.** A FIFO would make `open` hang (a denial of service from a cloned repo). Also devices and directories. | Only regular files are read. Anything else is skipped with a warning. | Accepted. |
| K3 | **Local file size.** There's no limit, because only remotes have one. | A configurable limit for local files, 10 MiB by default. | Accepted. |
| K4 | **The permission check.** Is it done on the link or the target, and which parent dirs? How is the owner exposed portably, so `cctest.FS` can fake it? | Check the target and its containing directory. Expose the owner through an `Owner() (uid int, ok bool)` interface on the file info. | Accepted. |
| L1 | **Extension matching** on case-insensitive filesystems (APFS, NTFS), and how dedup compares files. | Match case-sensitively, from the directory listing, so `cctest.FS` and macOS agree. Dedup by file identity (`os.SameFile`). | Accepted. |
| L2 | **Formats for explicit files and includes.** Does `--config x.toml` load if `MYAPP_CONFIG_FORMATS` disabled TOML? | Explicit files and includes accept any registered decoder, whatever the format order. | Accepted, clarified: any *registered* decoder. Nothing must be registered; an unregistered format is a 415. |
| L3 | **Relative paths in path-valued settings,** and whether `~` is expanded. | In env vars, relative to the cwd. In bootstrap files, relative to that file's directory. `~` is expanded in both. | Accepted. |
| L4 | **Ignored paths, override filenames, single-file env vars.** Is an ignored path exact, a prefix or a glob? Do override names include an extension? Does a missing `{NAME}_GLOBAL_CONFIG_FILE` give a 404? | Ignored paths match a file, or a directory prefix, after resolving symlinks. Override names may use `{ext}`. Single-file env vars behave like explicit files: a 404 if missing, and no variants. | Accepted. |

---

## Group 6: API and testability

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| D1 | **Injecting the host in tests.** Tests can't set env vars, the home dir, the cwd, GOOS/GOARCH or the UID without touching the whole process, which rules out parallel tests. GOOS can't be faked at all, which blocks the Windows and platform-env tests. | An internal "host" (env lookup, home, cwd, GOOS, GOARCH, UID) that only `cctest` can replace, like the clock. `cctest` returns a core `Option` through an internal hook. | Accepted. |
| E1 | **Options for building layouts.** The built-in layouts need settings with no public option, so they can't be built "on the same API": the include key and nesting, bootstrap files, keys and limits, the resolution policy, patterns with their variant and config root, size limits, dir resolvers, scope-kind signatures, and binding keys. | Add a "Layout options" catalogue with the shapes: `WithIncludeKey`, `WithBootstrap`, `WithResolution`, `Pattern(tmpl, variant, root)`, `WithMaxIncludeSize`, `Values(tree)` and `ValuesFunc(fn)`, plus constants for binding keys. Exact signatures are settled at M3's test review. | Accepted. |
| I1 | **Go-side defaults with `Load[T]`/`Watch[T]`.** Both decode into a fresh `T`, so pre-filled defaults are impossible, and every reload would lose them. | If `*T` has a `SetDefaults()` method (a `Defaulter` interface), it's called before every decode. This adds no options. | **Decided:** every reload works like an initial load (a fresh value each time). Defaults are a scope, `WithDefaults(v)`, built from a Go value; no `SetDefaults()`. |
| N1 | **Empty `With…s` calls.** The plan's own example always passes `WithEnvs(flagEnvs...)`, so with no flags, an empty call would silently override `MYAPP_ENV`. | `WithEnvs()` and `WithConfigFiles()` with no arguments mean "not set". `WithScopes()` with no arguments is an `*OptionError`. | Accepted. |
| N2 | **The naming rule.** `WithProbes`, `WithFlatPaths` and `WithUnknownKeys` look like "`With…s` replaces a set", but aren't sets. | Keep the names, and add an explicit exception to the rule. | Accepted. |

---

## Group 7: Errors, concurrency and watching

| ID   | Finding | Recommendation | Decision |
| ---- | ------- | -------------- | -------- |
| F-E1 | **Errors with no type.** Context cancellation, non-not-found I/O errors (`EACCES`, `EISDIR`), a scheme with no resolver, a scheme the policy forbids, malformed reserved keys (`_unset = 5`, `include = 3`), and errors from callbacks. Also: does a failed `Load` return a partial `*Result`? | `errors.Is(err, context.Canceled)` keeps working. Add `*IOError` (500). A scheme with no resolver is 501; one the policy forbids is a `*PolicyError` (403). Malformed reserved keys are a `*ParseError`. Callback errors are wrapped with `Op`. A failed `Load` still returns a non-nil `*Result` with its report. | Accepted. |
| G1 | **Concurrency and copies.** Concurrent `Load`s and the watcher call app callbacks in parallel. May a transform modify its input? Does `Tree()` share maps with reports and snapshots? | Every callback must be safe for concurrent use. Callbacks get trees they own, as deep copies, and so does `Tree()`. Transforms run only on layers that will merge. | Accepted. |
| H1 | **The Watch lifecycle.** What does `ctx` bound? Is `Close` safe twice, or from inside the handler? What does `Reload` return? What counts as a change (`time.Time`, NaN, a value moving to another file)? Are panics handled? | `ctx` bounds the watcher's lifetime. `Close` is idempotent, waits for a running callback, and is safe to call from inside one. `Reload` returns `error`. A change is a difference in values (`time.Equal`; NaN equals NaN), ignoring which file they came from, unless `WithReloadOnAnyEvent` is set. A panicking handler is recovered and logged. | Accepted. |

---

## Group 8: Bootstrap files and includes

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| M1 | **Bootstrap key handling.** Unknown keys, keys outside the scope limits or hardened's limits, wrong types (`env = "dev"`). Can bootstrap files use includes or `_unset`? | An unknown key is a warning. A key outside the limits is dropped with a warning. A wrong type is an `*OptionError` at load. Bootstrap files support neither includes nor `_unset`. | Accepted. |
| O1 | **Include semantics.** Must the include key be top-level? Is it stripped from the result? What about a file included twice, a diamond, or a file both included and discovered? Do includes work inside `Embedded`/`EmbeddedFS` sources? Do included files inherit trust? | Top-level only, and stripped from the result. Each file is loaded once per include chain. The same file at another rank is allowed and recorded. Included files inherit the includer's trust decision. No includes from `Embedded`/`EmbeddedFS` sources. | Accepted. |

---

## Group 9: Repository prerequisites

| ID | Finding | Recommendation | Decision |
| -- | ------- | -------------- | -------- |
| S1 | **★ No licence.** No milestone chooses one. `.config/rumdl.toml` excludes `LICENSES` and `LICENSE.md`, which suggests the REUSE layout was intended. | **Your choice.** MIT or Apache-2.0 are the usual picks for a library meant for wide use. Apache-2.0 adds an explicit patent grant. | **Apache-2.0.** `LICENSE` in the repo root and every module root (pkg.go.dev reads each module separately). Added in M0. |
| S2 | **Missing repo files:** a README (none before M11), a CHANGELOG or release-notes process for per-module tags, and a `SECURITY.md`. A library focused on attack tests should say how to report vulnerabilities. | Add them in M0: a README stub, `SECURITY.md`, and a CHANGELOG convention based on Conventional Commits, per module. | Accepted. |
| S3 | **Tooling bugs.** `.golangci.yml` points goimports `local-prefixes` at `github.com/kittomic/agentic-platform` (verified). `.config/mise.toml` installs `latest` of every tool, so lint and `govulncheck` results drift between runs. | Fix the prefix in M0, and pin tool versions. | Accepted. |
| S4 | **Platform names.** Only linux, macOS and windows on x64 and arm64 are mapped. Do freebsd and android count as `unix`? What about `386`, `riscv64` and `ppc64le`? | `unix` means every GOOS except windows, plan9, js and wasip1. Architectures other than `x64` (from `amd64`) and `arm64` keep their Go names. Document it as a table. | Accepted. |
