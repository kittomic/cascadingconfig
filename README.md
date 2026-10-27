# cascadingconfig

[![CI](https://github.com/kittomic/cascadingconfig/actions/workflows/ci.yml/badge.svg?branch=wip)](https://github.com/kittomic/cascadingconfig/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/kittomic/cascadingconfig.svg)](https://pkg.go.dev/github.com/kittomic/cascadingconfig)

A Go library that finds, merges and decodes layered configuration files, the way [mise] and [cosmiconfig] do. System,
user and project config, environment-specific files, environment variables and flags are merged in a defined order into
one typed struct, and every value can be traced back to the file or variable that set it.

**Status: pre-1.0.** Releases are v0.x, so until 1.0 a minor version may break the API; each module's CHANGELOG says
what changed.

## Features

- **Layered discovery** in mise's style (system, user, project directories walked upwards, overrides, environments) or
  cosmiconfig's (the first config file found wins), or a layout of your own.
- **Typed decoding** into your structs, with defaults, unknown-key detection and required config.
- **Provenance:** which file, scope or environment variable set each value, and what it overrode.
- **TOML, YAML and JSON**, each following its spec, with JSON built in.
- **Hot reload** with debouncing, typed change diffs, and the last good config kept when an edit breaks it.
- **Includes**, local and remote (https, git, OCI), with allow-lists, pinning and a cache.
- **Hardening** for config from places you don't control: trust stores, permission checks and confinement.
- **Testable:** an in-memory filesystem, a fake host, clock and notifier for your own tests.

## Install

```console
go get github.com/kittomic/cascadingconfig/presets
```

It needs Go 1.26 or later. Linux and macOS are tested on the real OS. Windows paths and behaviour are tested through
`cctest`'s fake host, and the tests that don't depend on Unix paths also run on Windows. The `git` resolver needs the
`git` program, 2.24 or later (2.29 for SHA-256 repositories).

## Quick start

This program loads `myapp`'s config from `/etc/myapp`, `~/.config/myapp`, a `myapp.toml` in the working directory or any
directory above it, and `MYAPP_*` environment variables, then decodes it into a struct. It's
[examples/quickstart](examples/quickstart/).

<!-- quickstart -->
```go
// Command quickstart is the README's quick start: it loads myapp's config with mise.Preset, from /etc/myapp,
// ~/.config/myapp, myapp.toml in this directory or any above it, and MYAPP_* environment variables, and prints it.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the app's configuration.
type Config struct {
	Server struct {
		Port int `koanf:"port"`
	} `koanf:"server"`
	Log struct {
		Level string `koanf:"level"`
	} `koanf:"log"`
}

func main() {
	cfg, err := load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("port %d, log level %s\n", cfg.Server.Port, cfg.Log.Level)
}

// load reads the config. opts are added to the loader's options, such as a test's fake filesystem.
func load(ctx context.Context, opts ...cascadingconfig.Option) (Config, error) {
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, append(opts,
		// MYAPP_SERVER__PORT=9090 sets server.port, above every file.
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_")),
	)...)
	if err != nil {
		return Config{}, err
	}
	return cascadingconfig.Load[Config](ctx, loader)
}
```

```console
$ go run .
port 8080, log level info
$ MYAPP_SERVER__PORT=9090 go run .
port 9090, log level info
```

## Choosing a preset

A preset is a layout, where and how config is searched for, plus the formats, resolvers and file watcher that go with
it.

| Preset                | For                                                                  | Example                          |
| --------------------- | -------------------------------------------------------------------- | -------------------------------- |
| `mise.Preset`         | Most tools: layered system, user and project config, as mise does    | [basic](examples/basic/)         |
| `cosmiconfig.Preset`  | JavaScript-style tools: the first config file found wins             | [cli](examples/cli/)             |
| `misehardened.Preset` | Config from repositories you don't control: no remote includes, strict permissions | [trusted](examples/trusted/) |

Swapping one for another is a one-word change. For the fewest dependencies, start from a layout in the core module, such
as `cascadingconfig.MiseLayout`, and add only the formats you need. The [examples](examples/) show more: reloading on
edits, a custom scope for mounted secrets, and explaining where a setting came from.

## Modules

Each module brings only its own dependencies, so you pay for what you use.

| Module                                            | Adds                                                         | Third-party dependencies |
| ------------------------------------------------- | ------------------------------------------------------------ | ------------------------ |
| `github.com/kittomic/cascadingconfig`             | The loader, the layouts, JSON, http and git includes, `cctest` | mapstructure           |
| `github.com/kittomic/cascadingconfig/format/toml` | TOML                                                         | go-toml                  |
| `github.com/kittomic/cascadingconfig/format/yaml` | YAML                                                         | goccy/go-yaml            |
| `github.com/kittomic/cascadingconfig/ociresolver` | OCI includes, from container registries                      | oras-go                  |
| `github.com/kittomic/cascadingconfig/watch/fsnotify` | File-change events for hot reload                         | fsnotify                 |
| `github.com/kittomic/cascadingconfig/presets`     | `mise.Preset`, `misehardened.Preset` and `cosmiconfig.Preset` | all of the above        |

[Project layout](docs/contributing/project-layout.md#dependency-analysis) measures what each combination pulls in.

## Documentation

- [The guide](docs/guide/): how to load, decode, merge, watch and secure config.
- [Precedence](docs/precedence.md): exactly which files load, and in what order.
- [Conformance](docs/conformance.md): how closely each decoder follows its format's spec, measured against the format's
  test suite.
- [Examples](examples/): five small programs, each a starting point for one kind of app.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), and [docs/README.md](docs/README.md) for the design records and plans.

## Licence

[Apache License 2.0](LICENSE).

[mise]: https://mise.jdx.dev/configuration.html
[cosmiconfig]: https://github.com/cosmiconfig/cosmiconfig
