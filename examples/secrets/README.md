# secrets: a custom scope for config the layouts can't find

Kubernetes and Docker hand an app its secrets as a mounted directory: one file per setting, holding the value. No config
layout searches a place like that, and the files aren't TOML or YAML. `ccx-secrets` adds a custom scope that reads the
directory into settings, above its config files, and watches it, so a rotated secret is picked up without a restart.
Secrets are never printed, nor is anything derived from them, such as a hash: the app shows each secret's version
instead, which goes up when the value changes.

## Run it

```console
$ go run .
db:  app@localhost, password set, version 1
api: https://api.example.com, token set, version 1
```

`db.host` and `db.user` come from `ccx-secrets.toml`; `db.password` and `api.token` from the files in `secrets/`.

## Things to try

- **Rotate a secret:** run `go run . -watch`, then change `secrets/db.password` in another window. The app prints
  `changed db.password (the secrets scope → the secrets scope)` and `password set, version 2`. The change line comes
  from the library's `KeyDiff`, which never prints values.
- **Point it at a real mount:** `go run . -secrets /run/secrets/ccx-secrets`. A missing directory just means no secrets
  are mounted.
- **Kubernetes layout:** Kubernetes mounts each key as a symlink into a hidden `..data` directory, and rotates by
  swapping `..data`. The scope follows the symlinks and skips names starting with a dot, so this works unchanged.
- **Secrets win:** add `password = "from-file"` under `[db]` in `ccx-secrets.toml`. The secrets scope is defined last,
  so it ranks above every file, and the password stays at version 1.

## What it shows

- A custom scope: a type with `Discover`, returning a `Source` that holds a tree, and plugged in with
  `WithScope("secrets", cascadingconfig.Custom(...))`, which also selects it.
- `Discovery`: the scope reads through `d.FS`, the loader's filesystem, so tests run it on an in-memory one.
- `Watchable`: `WatchDirs` adds the directory to what a watch watches.
- `Load[T]` for a one-shot typed load, and `Watch` with a typed handler.
