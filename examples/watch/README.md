# watch: a service that picks up edits

A long-running service shouldn't need a restart to change a rate limit. `ccx-watch` stands in for one: it watches every
file `mise.Preset` would load, and on each save prints the keys that changed and the config it now runs with. If an edit
breaks the config, the service says so and carries on with its last good config.

## Run it

```console
$ go run .
serving with port 8080, 100 requests/s, burst 20
watching for changes; press Ctrl-C to stop
```

Then edit `ccx-watch.toml` in another window and save:

```console
  ~ limits.requests_per_second: 100 → 250
now serving with port 8080, 250 requests/s, burst 20
```

## Things to try

- **Break it:** save the file with a syntax error. The service prints `reload failed, keeping the last good config`,
  with the file, line and column, and carries on. The library also logs a `reload-failed` warning to standard error. Fix
  the file, and the next save brings it back.
- **Add and remove keys:** delete `burst`, and the diff shows `- limits.burst (was 20)`; add it back to see `+`.
- **Add a layer while it runs:** create `~/.config/ccx-watch/ccx-watch.toml`, or `ccx-watch.dev.toml` next to the
  project file and restart with `CCX_WATCH_ENV=dev`. New files in watched places are picked up, not just edits.
- **Stop it:** Ctrl-C closes the watcher before the program exits.

## What it shows

- `cascadingconfig.Watch` with a typed handler: `Change[Config]` carries the old and new config, the changed keys
  (`Diff`), and `Err` for a failed reload.
- `Watcher.Current` for the config to start with, and `Watcher.Close` on shutdown.
- Debouncing: an editor's burst of writes for one save becomes one reload.
- `mise.Preset` watches with fsnotify, and falls back to polling where it can't.
