# Watching

A watched loader reloads when config changes, so a long-running service picks up an edit without a restart. It watches
every file a load reads, the directories where new files could appear, and, if you ask, remote includes.

## Watching a typed config

<!-- example -->
```go
w, err := cascadingconfig.Watch(ctx, loader, func(ctx context.Context, c cascadingconfig.Change[Config]) {
	if c.Err != nil {
		log.Printf("reload failed, keeping the last good config: %v", c.Err)
		return
	}
	for _, d := range c.Diff {
		log.Printf("config changed: %s", d) // the key and where its value came from, never the value
	}
})
if err != nil {
	return err // the first load failed
}
defer w.Close()
cfg := w.Current() // the latest good config, safe from any goroutine
```

- **`Watch`** loads once, returns a `*Watcher`, and calls the handler after each reload that changes the merged values.
  `loader.Watch` is the same for the untyped tree.
- **`Current`** returns the latest good config. Read it whenever you need config, rather than keeping a copy.
- **`Reload`** reloads now, such as on `SIGHUP`, or after changing environment variables, which can't be watched.
- **`Close`** stops watching. Cancelling `ctx` does too.

## Changes

A `Change[T]` carries:

- **`Old` and `New`:** the config before and after, each decoded into a fresh value.
- **`Diff`:** each changed key, as a `KeyDiff` with its `Kind` (`DiffAdded`, `DiffRemoved` or `DiffChanged`), its
  `Path`, its old and new values, and the origin of each. A key is a leaf: a scalar, an array or an empty table. When a
  table becomes a scalar, its keys are removed and the scalar added. A `KeyDiff` prints without its values, since config
  can hold secrets.
- **`Result`:** the reload's full result, for its report.
- **`Err`:** why a reload failed.

**A failed reload,** such as a file saved with a syntax error, keeps the last good config: `Current` doesn't change, the
handler gets `Err` with `Old` and `New` both the last good config, and a `reload-failed` warning is logged. The handler
is called again by the next good reload, even if nothing changed since the last good one, so you know it recovered.

**The handler** runs on the watcher's own goroutine, one call at a time; events that arrive meanwhile are coalesced into
the next reload. It may call `Reload` or `Close`. `WithReloadOnAnyEvent()` calls it after every reload, changed or not.

## Events and polling

- **Notifiers:** the presets watch with fsnotify. A layout from the core module has no notifier, so it polls instead,
  unless you add one with `WithNotifier(fsnotify.New)`.
- **Polling:** a directory that can't be watched is polled, every 2 seconds by default. `WithPollInterval` changes that,
  and `WithPolling()` polls everything, for filesystems where events don't arrive, such as some network mounts.
- **Debounce:** an editor's save is often several writes, so events are coalesced. Each event restarts a quiet period of
  100 ms, and a burst reloads no later than 1 s after its first event. `WithDebounce(quiet, maxWait)` changes both.
- **What's watched:** each loaded file, each directory searched, so a new file is noticed, and the nearest existing
  directory above one that's missing. Kubernetes-style ConfigMap updates, which swap a symlink, are seen too.

Watch options go on the `Watch` call, because watching is optional:

<!-- example -->
```go
w, err := cascadingconfig.Watch(ctx, loader, handle,
	cascadingconfig.WithDebounce(250*time.Millisecond, 2*time.Second),
	cascadingconfig.WithRemotePoll(10*time.Minute), // re-check unpinned remote includes
)
```

## Remote includes

Remote files don't send events, so a watcher only re-checks them if you ask, with `WithRemotePoll(d, matchers...)`:
every unpinned remote include, or those the matchers match, every `d`. A check asks the server whether the file changed,
and reloads only if it did. Checks are spread out by a little random jitter, and back off when a server fails. Use a
long interval for servers you don't want to load, and matchers to poll some sources more often than others:

<!-- example -->
```go
w, err := cascadingconfig.Watch(ctx, loader, handle,
	cascadingconfig.WithRemotePoll(time.Hour),
	cascadingconfig.WithRemotePoll(time.Minute, cascadingconfig.Prefix("https://config.acme.example/")),
)
```

When rules overlap, the one given last wins. Pinned references never change, so they're never checked.
