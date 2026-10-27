# cascadingconfig/format/toml

The TOML decoder for [cascadingconfig](https://github.com/kittomic/cascadingconfig), built on
[go-toml](https://github.com/pelletier/go-toml).

```console
go get github.com/kittomic/cascadingconfig/format/toml
```

Register it with `cascadingconfig.WithDecoder(toml.Decoder())`. The presets already do.

[Conformance](https://github.com/kittomic/cascadingconfig/blob/main/docs/conformance.md) measures it against toml-test.

Part of [cascadingconfig](https://github.com/kittomic/cascadingconfig), under the [Apache License 2.0](LICENSE).
