# Decoding

A load merges every layer into one tree of plain values. Decoding turns that tree into your own types, with
[mapstructure](https://github.com/go-viper/mapstructure) and the same defaults as
[koanf](https://github.com/knadh/koanf), so a struct written for koanf decodes the same way here.

## Into a struct

Fields are matched by their `koanf` tag:

<!-- example -->
```go
type Config struct {
	Server struct {
		Host    string        `koanf:"host"`
		Port    int           `koanf:"port"`
		Timeout time.Duration `koanf:"timeout"`
	} `koanf:"server"`
	Tags []string `koanf:"tags"`
}
```

- **Types are converted where it's safe.** A string from an environment variable fills an `int` or a `bool`, a duration
  is written `"30s"` or `"7d"`, a date-time is RFC 3339 text and fills a `time.Time`, and any type with an
  `UnmarshalText` method decodes from a string.
- **Another tag:** `WithTag("config")` matches a different struct tag.
- **Your own conversions:** `WithDecodeHook` adds a hook, a function of the source type, the field's type and the value,
  which runs before the default hooks, so it can handle a type of yours or override a default.
- **Stricter decoding:** `WithoutWeakTyping()` turns off conversions such as `"8080"` into an `int`, and
  `WithZeroFields()` lets a `null` clear a value already set in Go.

There are three ways to decode:

- **`Load[T](ctx, loader)`** loads and decodes into a fresh `T` in one call.
- **`res.Decode(&cfg)`** decodes a `*Result` you already have, such as one you also want to `Describe`.
- **`DecodeAt(res, key, &v)`** decodes one part of the tree, for a package that only owns its own section:

<!-- example -->
```go
var server ServerConfig
if err := cascadingconfig.DecodeAt(res, "server", &server); err != nil {
	return err
}
```

## Defaults

`WithDefaults` takes a value of your config type. Its non-zero fields become the lowest layer of all, so every file and
variable can override them, and a key's origin shows `defaults` until one does:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithDefaults(Config{
	Tags: []string{"default"},
}))
```

Supply defaults this way, not by pre-filling the struct you decode into: then config can remove a default, with `null`
or the unset key, and `Load` always starts from a fresh value, so a reload never keeps a value that config has since
removed.

## Unknown keys

A key that matches no field is usually a typo. By default it's a warning, logged and listed in the report, naming the
key and the file that set it. `WithUnknownKeys` makes it an error, or ignores it:

<!-- example -->
```go
_, err := cascadingconfig.Load[Config](ctx, loader)
var de *cascadingconfig.DecodeError
if errors.As(err, &de) && de.Unknown {
	fmt.Printf("%s: %s matches no field\n", de.Path, de.Key)
}
```

Each key that fails to decode is a `*DecodeError` of its own, with `Key` and the file in `Path`, joined into one error.
Values are never part of the message, since config can hold secrets.

## Requiring config

By default a load with nothing found succeeds, and every field keeps its default. `RequireConfig` makes a load fail with
a `*NotFoundError` unless something qualifying loaded:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset,
	cascadingconfig.RequireConfig(cascadingconfig.RequireFile, cascadingconfig.ScopeProject))
```

- **`RequireAny`** is met by anything that loaded: a file, even an empty one, environment variables, flags or defaults.
- **`RequireFile`** counts only files, and **`RequireNonEmpty`** leaves out empty ones; combine them with `|`.
- **Scopes:** with none named, one qualifying layer anywhere is enough. With scopes, each one named needs its own.
