# trusted: don't apply config from a repo you haven't approved

A config file in a cloned repository was written by whoever wrote the repository. If your tool runs commands, deploys or
fetches things based on its config, applying that file unseen is a risk. `ccx-trusted` uses `misehardened.Preset` and
requires the project's config files to be approved first, as `mise trust` does. An approval covers the file's exact
content, so if the file changes, it needs approving again.

## Run it

```console
$ go run .
deploying to local with 1 replicas
skipped …/examples/trusted/ccx-trusted.toml: not trusted; run "ccx-trusted trust" to approve it
$ go run . trust
trusted …/examples/trusted/ccx-trusted.toml
$ go run .
deploying to staging with 3 replicas
```

The library also logs an `untrusted` notice at Info for each skipped file, which the default logger prints to standard
error. An app that reports skipped files itself, as this one does, can drop such notices with
`cascadingconfig.WithLogLevel(slog.LevelWarn)`.

## Things to try

- **Edit an approved file:** change `replicas` in `ccx-trusted.toml`. The next run skips the file again until you run
  `go run . trust`.
- **Look at the approvals:** they're kept in `ccx-trusted/trusted.json` in your user config directory, as each file's
  path and the SHA-256 of the approved content. `-trust-file` keeps them elsewhere, such as a scratch file while you
  experiment.
- **User config needs no approval:** a file in `~/.config/ccx-trusted/` is yours, so it applies without trust. Only the
  project scope requires it.

## What it shows

- `misehardened.Preset`: no remote includes, includes confined to their own directories, and files skipped when other
  users can write to them.
- `WithScopePolicy` with `InheritDefault` and `RequireTrust`, adding a trust check to the project scope while keeping
  the hardened rules.
- A `TrustStore`: one method, `Trusted(ctx, path, digest)`, called before a file is decoded, so an untrusted file is
  never parsed.
- `LayerUntrusted` in the report, for telling users what was skipped.
