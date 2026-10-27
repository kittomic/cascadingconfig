# Changelog: `github.com/kittomic/cascadingconfig`

All notable changes to this module. Entries are built from [Conventional Commits](https://www.conventionalcommits.org/)
whose scope is `core`, and each release is tagged `vX.Y.Z`.

## v0.1.0

The first release. A loader that finds, merges and decodes layered configuration, the way mise and cosmiconfig do:

- **Layouts:** `MiseLayout`, `MiseHardenedLayout` and `CosmiconfigLayout`, built only from exported options, and presets
  of your own with `NewPreset` and `Extend`.
- **Scopes:** system, user, project (walked upwards) and override directories, environment-specific files, explicit
  files, environment variables, values from code, embedded files and custom discovery, with bootstrap files that
  configure the search.
- **Decoding** into structs with defaults, unknown-key handling, required config and decode hooks.
- **Merging:** deep merges, array strategies, per-key rules and functions, null and `_unset`.
- **Includes:** local files, and http, https and git through the `httpresolver` and `gitresolver` packages, with
  policies (allow-lists, pinning, confinement, trust stores and permission checks) and a cache.
- **Watching** with debouncing, typed diffs, polling, remote polls, and the last good config kept on a failed reload.
- **Provenance:** a report of every scope, layer and warning, and `OriginOf` for each key.
- **JSON** built in, and `cctest`: an in-memory filesystem and fake host, clock and notifier for your tests.
