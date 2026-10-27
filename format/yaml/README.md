# cascadingconfig/format/yaml

The YAML 1.2 decoder for [cascadingconfig](https://github.com/kittomic/cascadingconfig), built on
[goccy/go-yaml](https://github.com/goccy/go-yaml).

```console
go get github.com/kittomic/cascadingconfig/format/yaml
```

Register it with `cascadingconfig.WithDecoder(yaml.Decoder())`. The presets already do.

[Conformance](https://github.com/kittomic/cascadingconfig/blob/main/docs/conformance.md) measures it against
yaml-test-suite.

Part of [cascadingconfig](https://github.com/kittomic/cascadingconfig), under the [Apache License 2.0](LICENSE).
