# cascadingconfig/ociresolver

Fetches [cascadingconfig](https://github.com/kittomic/cascadingconfig) includes from container registries, such as
`oci::ghcr.io/acme/myapp-config:1.4.0`, built on [oras-go](https://github.com/oras-project/oras-go).

```console
go get github.com/kittomic/cascadingconfig/ociresolver
```

Register it with `cascadingconfig.WithIncludeResolver(ociresolver.New())`. `mise.Preset` already does.

See [Includes](https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/includes.md) for references, pinning and
policies.

Part of [cascadingconfig](https://github.com/kittomic/cascadingconfig), under the [Apache License 2.0](LICENSE).
