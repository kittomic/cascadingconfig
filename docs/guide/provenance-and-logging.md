# Provenance and logging

When a user asks "why is my port 9090?", the answer is in the result: every layer that loaded or was skipped, and the
history of every key. This page covers the report, tracing one key, and what the loader logs.

## The report

`res.Describe()` returns a `*Report` describing the load:

- **`Layers`:** each file and value layer, lowest first, with its scope, path, format, environment, trust and status:
  `loaded`, or why it was skipped, such as `empty`, `ignored`, `unreadable` or `untrusted`, with the reason.
- **`Scopes`:** each scope, whether it's selected, its trust and its policy.
- **`Envs`:** the selected and platform environments, and where the selection came from.
- **`Bootstrap`, `Bindings` and `Formats`:** the bootstrap files read, the discovery variables and whether each is set,
  and the format order.
- **`Warnings`:** every warning, each also recorded on the layer it concerns.
- **`Probes`:** with `WithProbes()`, every place searched where nothing was found.

For people, `report.String()` prints it as text, like `mise config ls`. For tools, it marshals to JSON. A failed load
still returns a result whose report shows what was found and what broke.

## Tracing a key

`OriginOf` says what set a key, and what happened to it before:

<!-- example -->
```go
origin, ok := cascadingconfig.OriginOf(res, "server.port")
if ok {
	fmt.Printf("%s by %s (%s)\n", origin.Kind, origin.Layer.Path, origin.Layer.Scope)
	for _, step := range origin.History {
		fmt.Printf("  earlier: %s by %s\n", step.Kind, step.Layer.Path)
	}
}
```

- **`Kind`** says what the latest layer did: `OriginSet`, `OriginMerged` (a table several layers contributed to),
  `OriginAppended` or `OriginPrepended` (an array a rule combined), `OriginNull`, `OriginFunc` (a merge function's
  result) or `OriginUnset`.
- **`Layer`** is that layer, so its `Path`, `Scope` and `Env` say where it came from.
- **`EnvVar`** names the variable, for a key from an `EnvVars` scope.
- **`Elements`** gives each element's layer, for an array a rule combined.
- **`History`** lists the earlier steps, oldest first.

The [cli example](../../examples/cli/) builds an `-explain` flag on it.

## Logging

The loader logs through a `*slog.Logger`: `slog.Default()`, or the one `WithLogger` sets. Every warning is logged as
well as recorded in the report, at a level that says how much it matters:

- **Error:** a watch handler that panicked, which the watcher recovered from.
- **Warn:** something you wrote or set up was ignored, or may be wrong, such as an unreadable file, an unknown key, a
  file skipped for its permissions, or a failed reload.
- **Info:** expected behaviour worth knowing: an untrusted file skipped, a file in a second format skipped under a
  first-match policy, or no home directory, which is common in containers.
- **Debug:** tracing, for troubleshooting: each load's layers and how long it took, each remote include's cache use, a
  path skipped because it isn't a directory, and each event and reload of a watcher.

`slog.Default()` prints Info and above, so Info records show by default. To log less:

<!-- example -->
```go
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithLogLevel(slog.LevelWarn))
```

- **`WithLogLevel(level)`** drops records below `level` before they reach your logger. A `*slog.LevelVar` changes the
  level while the app runs, such as Debug while you investigate.
- **`WithLogger(slog.New(slog.DiscardHandler))`** silences the loader entirely. The report still has every warning.
- **Custom scopes** log through `Discovery.Logger`, which is filtered the same way.

Each record carries `path` and `scope` where they apply, and a warning's `code`, such as `unknown-key`. Messages aren't
part of the API, but a Debug record's `op` attribute says what it traces, with stable attributes for each:

| `op`        | Attributes                                                      |
| ----------- | --------------------------------------------------------------- |
| `load`      | `loaded` and `skipped`, numbers of layers, and `duration`       |
| `layer`     | `status`, `scope`, and `path` for a file                        |
| `remote`    | `ref`, and `cache`: `fetched`, `cached`, `revalidated` or `stale` |
| `not-a-dir` | `path`, the directory searched                                  |
| `event`     | `path`, from the notifier                                       |
| `reload`    | none: a reload starting                                         |
| `reloaded`  | `changes`, the number of changed keys                           |
| `watch-set` | `dirs`, the number of directories watched or polled             |
| `polling`   | none: polling in place of a notifier                            |
