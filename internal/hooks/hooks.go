// Package hooks lets cctest reach the core's internals without making them public API: the core fills in the hooks
// when it initialises, and cctest calls them.
package hooks

import "time"

// Host describes a fake host. cctest.Host has the same fields.
type Host struct {
	GOOS, GOARCH string
	Env          map[string]string
	Home, Cwd    string
	UID          int
}

// WithHost returns a cascadingconfig.Option, as an any, that replaces the loader's host. The core sets it.
var WithHost func(h Host) any

// Clock is the loader's clock: the time, and timers for debounce and polling. cctest.Clock implements it.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a timer from Clock.AfterFunc.
type Timer interface {
	// Stop prevents the timer from firing, and reports whether it did.
	Stop() bool
}

// WithClock returns a cascadingconfig.Option, as an any, that makes the loader use c, and turns off remote-poll jitter
// so poll times are exact. The core sets it.
var WithClock func(c Clock) any

// Handled is implemented by a fake notifier that needs to know when the watcher has finished with each event or error
// it sent, including any reload that follows straight away. The watcher calls Handled once for each.
type Handled interface {
	Handled()
}
