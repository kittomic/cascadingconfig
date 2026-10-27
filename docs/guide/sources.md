# Where config comes from

A preset decides where config is searched for. This page covers each preset's places, then the sources you add yourself:
environments, environment variables, flags, explicit files and scopes of your own. [Precedence](../precedence.md) has
the exact order of every file.

## `mise.Preset`

Four scopes, lowest precedence first:

| Scope      | Where                                                                | Notes                                       |
| ---------- | -------------------------------------------------------------------- | ------------------------------------------- |
| `system`   | `/etc/{name}`, or `%ProgramData%\{name}` on Windows                  | Machine-wide config                         |
| `user`     | `~/.config/{name}`, or `$XDG_CONFIG_HOME/{name}`                     | The user's own config                       |
| `project`  | The working directory, then each directory above it, up to `~`       | The nearest directory wins                  |
| `override` | `~/.config/{name}/config.override.{ext}`                             | The user's last word, above every project   |

- **File names:** in the `system` and `user` directories, `config.{ext}`, `{name}.{ext}` and `conf.d/*.{ext}`. In each
  project directory, `{name}.{ext}` and `.{name}.{ext}`, the same names in `.config/` (as `.config/{name}.{ext}` or
  `.config/{name}/config.{ext}`), and `{name}/config.{ext}` or `.{name}/config.{ext}`, each with its `conf.d/`
  directory.
- **Formats:** `{ext}` is each registered format's extensions: `toml`, `yaml`, `yml` and `json`. When one place holds
  the same name in two formats, such as `myapp.toml` and `myapp.yaml`, both load, and the higher-ranked format, TOML,
  wins where they disagree. `WithFormatConflict` can load only the first instead, or make it an error.
- **The walk** stops at your home directory, so a project's parent directories are searched but nothing above `~`.
  Outside your home, such as `/srv/app`, it walks to `/`. `WithWalkStop`, `WithCeilingPaths` and `WithStartDir` change
  where it starts and stops.
- **`~/.config/{name}` is found twice** when you work inside your home: as the `user` scope, and by the walk, as project
  config of `~`. A file reached twice loads once, at its highest place, so there it ranks as project config, above the
  `user` scope and any scope placed just above it. With `XDG_CONFIG_HOME` set elsewhere, it doesn't overlap.
- **Local files:** `{name}.local.toml` and the other `.local` names are personal, uncommitted files. They load above the
  shared file in the same place.

## Environments

An **environment** adds files named for it, such as `myapp.dev.toml` next to `myapp.toml`, above the shared files in the
same place. Select environments with `MYAPP_ENV=dev`, or in code:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithEnvs("dev", "staging"))
```

When several are selected, the later one wins. The platform's own environments, such as `unix`, `linux` and `linux-x64`
on a 64-bit Linux machine, are selected too, below the ones you choose.

## Environment variables and flags

Two scope kinds take single values instead of files. `EnvVars` reads variables with a prefix: `MYAPP_SERVER__PORT=8080`
sets `server.port`, with the prefix stripped, the name lowercased, and `__` separating levels. `Values` takes a tree you
build, typically from the flags given on the command line:

<!-- example -->
```go
preset := mise.Preset.Extend(
	cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_")),
	cascadingconfig.WithScope("flags", cascadingconfig.Values(flagTree)),
)
```

`WithScope` defines a scope and selects it, above every scope defined before it, so here flags beat environment
variables, which beat every file. Environment variables are strings; decoding converts them, so `"8080"` fills an `int`.
`ValuesFunc` takes a function instead of a tree, called on every load. Build the flags' tree only from the flags that
were set, with `flag.Visit`, so a flag left out doesn't hide a file's value: the [cli example](../../examples/cli/) does
this.

## Explicit config files

A `--config` flag, or `MYAPP_CONFIG_FILE`, names files to load instead of searching the project:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithConfigFiles(configFlag))
```

Explicit files take the place of the project scope. System, user and value scopes still load, so environment variables
and flags still beat the file. A missing explicit file is an error, not a silent skip.

## `cosmiconfig.Preset`

cosmiconfig's search, without its JavaScript formats:

- **Places**, in the working directory: `.{name}rc`, `.{name}rc.json`, `.{name}rc.yaml` and `.{name}rc.yml`, the same
  names in `.config/`, and the `{name}` key of `.config/config.{ext}`. An `rc` file with no extension is YAML, or JSON
  if YAML isn't registered.
- **The first file found wins**: one file loads, not a merge of all of them.
- **The user directory** (`config.{ext}` where env-paths puts it) and walking up from the working directory are off by
  default, as in cosmiconfig. `WithSearchStrategy(cascadingconfig.SearchGlobal)` turns both on.
- **Explicit files** replace the search, as `WithConfigFiles` does for mise.

## Choosing scopes

`WithScopes` keeps exactly the scopes listed, and `WithoutScopes` removes some. Neither changes their order:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithoutScopes(cascadingconfig.ScopeSystem))
```

## Scopes of your own

A scope of your own searches a place no preset does, such as a directory a container mounts:

<!-- example -->
```go
preset := mise.Preset.Extend(
	cascadingconfig.WithScope("mounted",
		cascadingconfig.FixedDir("/run/config/{name}", cascadingconfig.Pattern{Template: "config.{ext}"}),
		cascadingconfig.After(cascadingconfig.ScopeUser),
	),
)
```

- **Placement:** `After` and `Before` place it next to another scope; with neither, it goes above every scope defined so
  far.
- **Kinds:** `FixedDir` searches one directory, `Walk` walks up from the working directory, `ExplicitFile` loads named
  files, and `EmbeddedFS` searches an `fs.FS` compiled into your app, such as built-in defaults.
- **Anything else:** `Custom` takes a type of your own with a `Discover` method, which returns files or trees. The
  [secrets example](../../examples/secrets/) reads a mounted secrets directory that way.

## Discovery variables

`mise.Preset` reads these variables, where `MYAPP` is your app's name in capitals:

| Variable                     | Does                                                        |
| ---------------------------- | ----------------------------------------------------------- |
| `MYAPP_ENV`                  | Selects environments, comma-separated                       |
| `MYAPP_CONFIG_FILE`          | Explicit files, replacing the project search                |
| `MYAPP_CONFIG_DIR`           | Moves the user directory                                    |
| `MYAPP_SYSTEM_CONFIG_DIR`    | Moves the system directory                                  |
| `MYAPP_CEILING_PATHS`        | Directories the walk stops below                            |
| `MYAPP_IGNORED_CONFIG_PATHS` | Files or directories never loaded                           |
| `MYAPP_CONFIG_FORMATS`       | The formats searched, and their order                       |

An option set in code wins over its variable. `WithBinding` renames a variable and `WithoutBinding` turns one off, for
example when your app already uses `MYAPP_ENV` for something else. `cosmiconfig.Preset` reads only `MYAPP_CONFIG_FILE`
and `MYAPP_CONFIG_FORMATS`.
