# Getting started

`cascadingconfig` reads an app's configuration from many places at once: a system directory, the user's config
directory, the project the app runs in, environment variables, flags. It merges them in a defined order and decodes the
result into your own struct.

## A first load

A **loader** is built once, from the app's name and a **preset**, and loads as often as you like:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset)
if err != nil {
	return err
}
cfg, err := cascadingconfig.Load[Config](ctx, loader)
```

The name fills `{name}` in every place the preset searches, so `myapp` looks for files such as `/etc/myapp/config.toml`,
`~/.config/myapp/config.toml` and `myapp.toml` in the working directory or any directory above it. `Load` decodes into a
fresh `Config` each time: see [decoding](decoding.md).

## Presets and layouts

A **preset** says where to search, which formats to read, and how to merge. Three are ready to use, in the `presets`
module:

- **`mise.Preset`** searches as [mise] does: system, user and project config, with environment-specific files. It reads
  TOML, YAML and JSON, fetches remote includes, and watches files for changes.
- **`cosmiconfig.Preset`** searches as [cosmiconfig] does: the first config file found wins.
- **`misehardened.Preset`** is `mise.Preset` for config from places you don't control: no remote includes, and files
  that others can write to are skipped.

Each is built from a **layout** in the core module, such as `cascadingconfig.MiseLayout`, which brings no dependencies
beyond JSON. Start from a layout when you want the fewest dependencies, and add only the formats you need. Every preset
can be extended with options, without changing it:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithEnvs("dev"))
```

## Scopes and layers

A preset searches in **scopes**, from lowest precedence to highest. `mise.Preset` has four: `system`, `user`, `project`
and `override`. Each file a scope finds becomes a **layer**, and layers merge in order, so a later layer wins wherever
two set the same key. Within the project scope, the walk applies directories root-most first, so the file nearest the
working directory wins. [Where config comes from](sources.md) covers each scope, and [precedence](../precedence.md)
gives the exact rules.

A load's result says what it found:

<!-- example -->
```go
res, err := loader.Load(ctx)
if err != nil {
	return err
}
for _, layer := range res.Describe().Layers {
	fmt.Println(layer.Scope, layer.Status, layer.Path)
}
```

A layer's status says what happened to it: `loaded`, or skipped and why, such as `empty`, `ignored` or `untrusted`.
`Describe` also lists the scopes, the environments, the warnings and more; `OriginOf` traces a single key.

## Next

- [Where config comes from](sources.md), to add environment variables, flags or a `--config` file.
- [Decoding](decoding.md), for defaults and unknown keys.
- The [examples](../../examples/), for complete programs.

[mise]: https://mise.jdx.dev/configuration.html
[cosmiconfig]: https://github.com/cosmiconfig/cosmiconfig
