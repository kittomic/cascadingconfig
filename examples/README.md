# Examples

Five small programs, each a realistic starting point for one kind of app. Each runs as it is: `cd` into its directory
and `go run .`. It reads the config files shipped beside it, under an app name of its own (`ccx-…`), so nothing in your
own config directories gets in the way.

| Example                 | The story                                              | What it uses                                          |
| ----------------------- | ------------------------------------------------------ | ----------------------------------------------------- |
| [basic](basic/)         | A tool with defaults, project config, environments and environment-variable overrides | `mise.Preset`, `WithDefaults`, `EnvVars`, `Describe` |
| [cli](cli/)             | Flags beat files, and users can ask where a setting came from | `cosmiconfig.Preset`, `Values`, `WithConfigFiles`, `OriginOf` |
| [watch](watch/)         | A long-running service that picks up edits             | `Watch[T]`, `Change.Diff`, failed reloads             |
| [secrets](secrets/)     | Secrets from a mounted directory, rotated without a restart | a custom `Scope`, `Watchable`, `Load[T]`         |
| [trusted](trusted/)     | Project config applied only once you approve it        | `misehardened.Preset`, `RequireTrust`, a `TrustStore` |

Each example's README says what to try. Each has tests that run it on an in-memory filesystem with a fake host, from the
same files it ships, and compare its output with the golden files in its `testdata/`.
