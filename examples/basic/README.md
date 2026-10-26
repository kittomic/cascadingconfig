# basic: layered config for a tool

Your tool has built-in defaults. Teams commit a project config file, an environment can add its own file, and anyone can
override a single value with an environment variable. `ccx-basic` loads all of that with `mise.Preset`, decodes it into
a typed `Config`, and prints the result with the layers that made it.

## Run it

```console
$ go run .
server:   localhost:8080
log:      info, text
features: [search]

layers, lowest first (environments: none):
  defaults  (built-in defaults)
  project   …/examples/basic/ccx-basic.toml
```

## Things to try

- **Select an environment:** `CCX_BASIC_ENV=dev go run .` adds `ccx-basic.dev.toml` above `ccx-basic.toml`. The log
  level becomes `debug`, and `features` is replaced, not appended, as mise does with arrays.
- **Override one value:** `CCX_BASIC_SERVER__PORT=9090 go run .`. The prefix is stripped, and `__` separates levels, so
  the variable sets `server.port`. Variables beat every file, and the `env` layer now shows in the list: with no
  matching variable it's empty, and only loaded layers are listed.
- **Add your own layer:** put a `ccx-basic.toml` in `~/.config/ccx-basic/` (or `$XDG_CONFIG_HOME/ccx-basic/`). It loads
  below the project file, so the project wins where both set a key.
- **Remove a default:** add `_unset = ["server.host"]` to `ccx-basic.toml`, and the host falls back to Go's zero value.

## What it shows

- `mise.Preset`, extended with `WithDefaults` for built-in defaults and an `EnvVars` scope for overrides.
- Adding a scope: `WithScope` defines it and selects it, above every scope defined before it.
- `Result.Decode` into a typed struct, and `Result.Describe` for the layers behind it.
