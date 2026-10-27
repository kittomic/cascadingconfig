# Includes

A config file can pull in other files, local or remote, with the `include` key:

```toml
# myapp.toml
include = ["base.toml", "https://config.acme.example/myapp/team.toml"]

[server]
port = 8080
```

`mise.Preset` and `misehardened.Preset` read the `include` key; `cosmiconfig.Preset` has no includes.

## How includes merge

- **Rank:** an included file ranks directly below the file that includes it, and a later entry beats an earlier one. So
  `myapp.toml`'s own keys beat `team.toml`'s, which beat `base.toml`'s.
- **Removing keys:** the including file's `_unset` removes keys from what it includes, so a file can drop a key from its
  own base.
- **One level only:** an included file can't include in turn, as in mise.
- **Top level only:** the key is read at the top of a file, and removed from the result. Elsewhere it's ordinary data.
- **Each file loads once,** however many files include it. A file that includes itself is an error.
- **Never skipped silently:** an include that can't be loaded fails the load, naming the file that asked for it.
- **Trust:** an included file takes its including file's trust decision, and is judged by that file's scope's policy.

## Local files

A relative path resolves against the including file's directory, `~/` against the home directory, and an absolute path
is used as it is. `file://` URLs work too. An included file's format comes from its extension.

## Remote files

`mise.Preset` fetches three kinds of reference:

| Scheme          | Form                                     | Example                                                              |
| --------------- | ---------------------------------------- | -------------------------------------------------------------------- |
| `https`, `http` | A URL                                    | `https://config.acme.example/myapp/base.toml`                        |
| `git`           | `git::<repo-url>//<path>[?ref=<ref>]`    | `git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0` |
| `oci`           | `oci::<registry>/<repository>[:<tag>][@<digest>][//<path>]` | `oci::ghcr.io/acme/myapp-config:1.4.0`          |

- **Pinned references** name content that can't change: a git `ref=` that's a full commit id, an OCI `@sha256:` digest,
  or a URL ending `#sha256=<hex>`, whose downloaded bytes must have that hash.
- **Credentials** belong in the resolver's options, such as `httpresolver.WithClient` with your own transport, never in
  a reference: references show up in reports, errors and logs.
- **Timeouts:** each fetch is bounded by `WithIncludeTimeout`.
- **Relative references** inside a remote file stay within its origin: the same site, repository and ref, or artifact. A
  remote file can never reach your local files.

## Policies

With no policy, `mise.Preset` fetches any reference, as mise does. A policy limits which references are allowed, and
applies to every scope, or with `WithScopePolicy` to one:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithDefaultPolicy(
	cascadingconfig.Allow(cascadingconfig.Prefix("https://config.acme.example/")),
	cascadingconfig.RequirePinned(),
))
```

- **`Allow(matchers...)`** applies to every scheme, and **`AllowFor(scheme, matchers...)`** to one. A reference must
  match each allow-list that applies to it.
- **Matchers:** `Prefix` is a plain string prefix, so end it with `/` for a directory; `Glob` uses `*` within a segment
  and `**` across segments; `Regexp` must match the whole reference; `MatchRef` is a function of the reference and the
  file that asked for it.
- **What matchers see:** each reference in a normal form, such as a URL with its host lowercased and its dot segments
  resolved, so it can't pass in one form and be fetched in another. A local file is its absolute path.
- **`RequirePinned()`** allows only pinned references. **`AllowResolvers("file", "https")`** limits the schemes.
  **`Confine(roots...)`** keeps local includes within the roots, such as `{config_root}`, the including file's own
  project.
- **A rejected reference** is a `*PolicyError`, checked before anything is fetched.

A scope's own policy replaces the default policy for that scope, and on its own starts from nothing: no scheme is usable
until `AllowResolvers` lists it. `InheritDefault()` starts from the default policy instead, and each option after it
replaces its own setting. For example, project files, which come with whatever repository you're in, may include only
local files:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
	cascadingconfig.InheritDefault(),
	cascadingconfig.AllowResolvers("file"),
))
```

## The cache

Remote files are cached under your user cache directory, in `{name}/includes`:

- **Pinned references** are fetched once and kept.
- **Others** are fetched again once an hour, and a server that says the file hasn't changed keeps the cached copy.
- **A failed fetch,** such as a network failure, falls back to the cached copy, with a `stale-include` warning. A
  missing file or a rejected reference still fails the load, because it means the config has changed.

`WithIncludeCache` sets the directory and the time to keep unpinned files, and `WithoutIncludeCache` turns caching off:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithIncludeCache("", 24*time.Hour)) // the default directory
```

A watched loader can also check remote files for changes on a schedule: see [watching](watching.md).
