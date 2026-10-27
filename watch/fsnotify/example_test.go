package fsnotify_test

import (
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/watch/fsnotify"
)

// A layout from the core module polls when watched; with fsnotify, a watcher hears about changes as they happen.
func ExampleNew() {
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithNotifier(fsnotify.New))
	if err != nil {
		log.Fatal(err)
	}
	_ = loader // cascadingconfig.Watch(ctx, loader, handler) watches its files
	// Output:
}
