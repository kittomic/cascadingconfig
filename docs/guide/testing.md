# Testing your app

Your app's config tests shouldn't depend on the machine they run on: a developer's `~/.config` or a CI runner's
environment would change the result. The `cctest` package, in the core module, gives a loader a world of its own: an
in-memory filesystem, a fake host, a clock and a file-change notifier.

## A fake world

<!-- example -->
```go
fsys := cctest.NewFS(map[string]string{
	"/etc/myapp/config.toml":   "[server]\nhost = \"0.0.0.0\"\n",
	"/home/me/work/myapp.toml": "[server]\nport = 8080\n",
})
host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work",
	Env: map[string]string{"MYAPP_ENV": "dev"}}
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
```

- **`NewFS`** takes file paths and contents. They must be absolute on the OS the tests run on, so the Unix paths used
  here work on Linux and macOS, but not on Windows. Directories are implied, and the `FS` has methods for what tests
  need: `WriteFile`, `Remove`, `Mkdir`, `Symlink`, `Chmod`, `Chown` and `Mkfifo`.
- **`WithHost`** replaces everything the loader would ask the process: the OS and architecture, which select platform
  environments and paths, the home and working directories, the user ID for permission checks, and the environment
  variables. Nothing leaks in from the real one.
- **Any `fs.FS`**, such as an `embed.FS` or `fstest.MapFS`, works too: `MountFS(dir, fsys)` mounts it at an absolute
  directory.

## Structuring your app for tests

Build the loader in one function that takes extra options, and pass the world from your tests. The
[examples](../../examples/) all do this:

```go
func load(ctx context.Context, opts ...cascadingconfig.Option) (Config, error) {
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, opts...)
	if err != nil {
		return Config{}, err
	}
	return cascadingconfig.Load[Config](ctx, loader)
}
```

`main` calls `load(ctx)`, and a test calls `load(ctx, cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))`.

## Watching, without waiting

A watcher normally waits for real time to pass. With a fake clock and notifier, a test moves time itself:

<!-- example -->
```go
clock := cctest.NewClock(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
notifier := cctest.NewNotifier()
loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithFileSystem(fsys),
	cctest.WithHost(host), cctest.WithClock(clock), cascadingconfig.WithNotifier(notifier.New))
```

Then, with the watcher started, edit a file and tell the watcher:

<!-- example -->
```go
fsys.WriteFile("/home/me/work/myapp.toml", []byte("[server]\nport = 9090\n"), 0o644)
notifier.Send("/home/me/work/myapp.toml")
clock.Advance(time.Second) // past the debounce; returns once the reload and the handler have run
```

- **`Send`** returns once the watcher has taken the event, and **`Advance`** once everything that fell due has run, so a
  test needs no sleeps.
- **The notifier** also fails on cue: `SendError` reports an error, `FailAdd` makes watching a directory fail, and
  `Close` stops it, as a broken notifier would. `Watched` lists the directories watched.
- **The clock** also drives the include cache's freshness and remote polling.

## Golden files

`cctest.Golden` compares output with a file in `testdata/`, and rewrites the file when asked: with `CCTEST_UPDATE=1`, or
with an `-update` flag your test package defines. cctest registers no flags of its own, so it never clashes with yours:

```go
var _ = flag.Bool("update", false, "rewrite golden files") // in any _test.go file of the package
```

Then compare:

<!-- example -->
```go
cctest.Golden(t, "report.golden", []byte(res.Describe().String()))
```

Reports name paths in the fake world, so they're the same on every Linux or macOS machine.
