# cascadingconfig/watch/fsnotify

File-change events for [cascadingconfig](https://github.com/kittomic/cascadingconfig)'s watcher, from
[fsnotify](https://github.com/fsnotify/fsnotify).

```console
go get github.com/kittomic/cascadingconfig/watch/fsnotify
```

Set it with `cascadingconfig.WithNotifier(fsnotify.New)`. The presets already do. Without a notifier, a watcher polls.

See [Watching](https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/watching.md).

Part of [cascadingconfig](https://github.com/kittomic/cascadingconfig), under the [Apache License 2.0](LICENSE).
