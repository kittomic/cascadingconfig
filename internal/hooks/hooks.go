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

// WithClock returns a cascadingconfig.Option, as an any, that makes the loader read the time from now. The core sets
// it.
var WithClock func(now func() time.Time) any
