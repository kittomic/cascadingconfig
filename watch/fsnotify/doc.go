// Package fsnotify supplies file-change events to cascadingconfig's watcher, using github.com/fsnotify/fsnotify.
//
// Set it with cascadingconfig.WithNotifier(fsnotify.New); the presets already do. Without a notifier, a watcher polls
// instead, and a directory that can't be watched is polled even with one.
//
// See [Watching].
//
// [Watching]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/watching.md
package fsnotify
