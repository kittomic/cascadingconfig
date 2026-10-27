package cascadingconfig

// Notifier supplies file-change events for watching. Implemented by watch/fsnotify.
type Notifier interface {
	// Add watches dir. A failure makes the watcher poll dir instead, with a warning.
	Add(dir string) error
	// Remove stops watching dir.
	Remove(dir string) error
	// Events carries the absolute path, in the FileSystem's namespace, of any created, written, removed or renamed
	// entry. Chmod-only events are dropped. A closed channel makes the watcher poll everything, with a warning.
	Events() <-chan string
	// Errors carries the notifier's errors. Each is a warning and a reload, because events may have been lost.
	Errors() <-chan error
	Close() error
}

// WithNotifier sets where file-change events come from. newNotifier is called once per watcher, because a notifier's
// events have one reader, and the watcher closes what it returns. If it fails, the watcher polls everything, with a
// warning. Without a notifier, watching polls. A nil function is an *OptionError.
func WithNotifier(newNotifier func() (Notifier, error)) Option {
	return optionFunc(func(c *config) {
		if newNotifier == nil {
			c.fail("WithNotifier", "a nil function")
			return
		}
		c.newNotifier = newNotifier
	})
}
