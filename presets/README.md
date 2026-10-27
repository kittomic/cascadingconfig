# cascadingconfig/presets

Ready-to-use presets for [cascadingconfig](https://github.com/kittomic/cascadingconfig), each a core layout with its
formats, resolvers and file watcher:

- **`mise.Preset`:** layered system, user and project config, as [mise](https://mise.jdx.dev/configuration.html)
  searches, in TOML, YAML and JSON, with remote includes.
- **`misehardened.Preset`:** the same for config from places you don't control: no remote includes, and strict
  permissions.
- **`cosmiconfig.Preset`:** the first config file found wins, as
  [cosmiconfig](https://github.com/cosmiconfig/cosmiconfig) searches.

```console
go get github.com/kittomic/cascadingconfig/presets
```

See the [README](https://github.com/kittomic/cascadingconfig#quick-start) for a quick start, and the
[guide](https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/).

cascadingconfig isn't affiliated with or endorsed by mise or cosmiconfig; the names say whose search each layout
follows.

Part of [cascadingconfig](https://github.com/kittomic/cascadingconfig), under the [Apache License 2.0](LICENSE).
