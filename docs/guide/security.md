# Security

Config from your own machine is yours. Config in a repository you cloned was written by whoever wrote the repository,
and if your app runs commands, deploys or fetches things based on its config, applying that file unseen is a risk. This
page covers the tools for config from places you don't control.

## `misehardened.Preset`

`misehardened.Preset` is `mise.Preset` with stricter defaults:

- **No remote includes.** A remote reference is a load error, not a silent skip, so config that depends on a remote base
  fails loudly.
- **File permissions:** on Unix, a config file or directory that group or others can write to, or that's owned by
  someone other than you or root, is skipped with a `file-permissions` warning, as SSH's `StrictModes` does.
- **Confined includes:** a local include must stay within the including file's project, your user config directory or
  the system one, after following symlinks.
- **Limited bootstrap files:** a project's bootstrap files may select environments and formats, nothing else.

It searches the same places as `mise.Preset`, so switching is a one-word change.

## Trusting project config

A trust store decides which files may load, as `mise trust` does. A file it doesn't approve is skipped before it's
decoded, with the status `untrusted` and an Info log record, so an untrusted file can't even fail to parse:

<!-- example -->
```go
preset := misehardened.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
	cascadingconfig.InheritDefault(), // keep the hardened rules, and add trust
	cascadingconfig.RequireTrust(cascadingconfig.TrustPaths("/srv/checkouts/acme")),
))
```

- **`TrustPaths(globs...)`** trusts the files under the paths listed, after resolving symlinks; a path without wildcards
  trusts everything beneath it, as mise's `trusted_config_paths` does.
- **Your own store** is any type with one method, `Trusted(ctx, path, digest)`. The digest is the SHA-256 of the file's
  content, so a store can require approval again after the file changes. The [trusted example](../../examples/trusted/)
  keeps approvals in a JSON file and adds a `trust` command.
- **Which files:** the scope's searched, explicit and bootstrap files. An included file takes its includer's verdict.
- **An explicit file** that isn't trusted is an error, not a skip, because a file you named directly is never silently
  dropped.
- **Your app tells the user.** The loader never prompts. List the skipped files from the report's layers with status
  `LayerUntrusted`, and ask.

## Remote includes, carefully

To fetch remote files in `misehardened.Preset`, register a resolver again and allow it only where you need it, with an
allow-list and pinned references. Without both, `New` refuses the policy:

<!-- example -->
```go
preset := misehardened.Preset.Extend(
	cascadingconfig.WithIncludeResolver(httpresolver.New()),
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeSystem,
		cascadingconfig.InheritDefault(),
		cascadingconfig.AllowResolvers("file", "https"),
		cascadingconfig.AllowFor("https", cascadingconfig.Prefix("https://config.acme.example/")),
		cascadingconfig.RequirePinned(), // each reference needs #sha256=…
	),
)
```

[Includes](includes.md) covers policies and pinning in full.

## Permission checks in other presets

The permission check is a policy option, so any preset can use it, for one scope or every one:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
	cascadingconfig.InheritDefault(),
	cascadingconfig.FilePermissions(cascadingconfig.StrictModes),
))
```

`FilePermissions` takes any function of a `FileCheck`, the file's path, scope, information and owner, that returns
`PermissionAllow`, `PermissionIgnore` with a reason, or `PermissionError`. Windows ACLs aren't checked yet.

## What a config file can't do

Whatever the preset, the loader keeps these rules:

- **No escapes through names.** Environment names and patterns can't climb out of a directory with `..` or a separator.
- **Remote files stay remote.** A relative reference in a remote file resolves within its own site, repository or
  artifact, never to your local files.
- **Nothing blocks or runs.** A FIFO or device where a config file is expected is skipped unopened, git runs without
  hooks, filters or your git config, and archives are read as streams with size limits.
- **Limits:** files are at most 10 MiB by default (`WithMaxFileSize`), includes are bounded in depth and number, and
  every fetch has a timeout.
