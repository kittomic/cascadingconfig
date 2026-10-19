# cascadingconfig

A Go library that discovers, merges and decodes layered configuration files, in the style of [mise] and
[cosmiconfig].

**Status: in development.** Nothing is released yet, and the API may change.

## Design

- [Design](docs/design.md): how config files are found, merged and trusted.
- [API](docs/api.md): the public Go API.
- [Project layout](docs/project-layout.md): the modules, and what each one depends on.
- [Implementation plan](docs/implementation-plan.md): the milestones.
- [Research](docs/config-locations.md): how mise and cosmiconfig search for config.

## Licence

[Apache License 2.0](LICENSE).

[mise]: https://mise.jdx.dev/configuration.html
[cosmiconfig]: https://github.com/cosmiconfig/cosmiconfig
