package cascadingconfig

import "strings"

// Resolution says how the files a load finds combine.
type Resolution int

// Resolutions.
const (
	// MergeAll merges every file, as mise does. It is the default.
	MergeAll Resolution = iota
	// FirstMatch loads only the highest-precedence non-empty file, as cosmiconfig does. Files below it aren't decoded.
	FirstMatch
)

// WithResolution sets how the files a load finds combine.
func WithResolution(r Resolution) Option {
	return optionFunc(func(c *config) {
		if r != MergeAll && r != FirstMatch {
			c.fail("WithResolution", "unknown resolution %d", int(r))
			return
		}
		c.resolution = r
	})
}

// WithStopDir sets where a StopAtHome walk stops, inclusive, instead of ~. If dir isn't an ancestor of the start
// directory, the walk goes to /.
func WithStopDir(dir string) Option {
	return optionFunc(func(c *config) {
		if dir == "" {
			c.fail("WithStopDir", "an empty stop dir")
			return
		}
		c.stopDir = dir
	})
}

// WithRootMarker sets the names whose presence in a directory stops a StopAtMarker walk there, inclusive, such as .git
// or go.mod.
func WithRootMarker(names ...string) Option {
	names = append([]string(nil), names...)
	return optionFunc(func(c *config) {
		for _, n := range names {
			if n == "" || strings.ContainsAny(n, `/\`) {
				c.fail("WithRootMarker", "the root marker %q must be a non-empty name, without separators", n)
				return
			}
		}
		c.markers = names
	})
}

// SearchStrategy is cosmiconfig's searchStrategy, as shorthand for a walk stop and the user scope's selection.
type SearchStrategy int

// Search strategies.
const (
	// SearchNone searches the start directory only, and deselects the user scope.
	SearchNone SearchStrategy = iota
	// SearchProject walks up to a root marker, and deselects the user scope.
	SearchProject
	// SearchGlobal walks up to the stop dir, ~ by default, then searches the user scope, which it selects.
	SearchGlobal
)

// WithSearchStrategy sets a cosmiconfig search strategy. SearchNone means WithWalkStop(StopAtStart), SearchProject
// WithWalkStop(StopAtMarker) and SearchGlobal WithWalkStop(StopAtHome); the user scope's selection changes when the
// option runs.
func WithSearchStrategy(s SearchStrategy) Option {
	return optionFunc(func(c *config) {
		switch s {
		case SearchNone:
			c.walkStop = StopAtStart
			delete(c.selected, ScopeUser)
		case SearchProject:
			c.walkStop = StopAtMarker
			delete(c.selected, ScopeUser)
		case SearchGlobal:
			c.walkStop = StopAtHome
			if c.scope(ScopeUser) != nil {
				c.selected[ScopeUser] = true
			}
		default:
			c.fail("WithSearchStrategy", "unknown search strategy %d", int(s))
		}
	})
}

// WithConfigFilesOnly makes explicit config files, when they're set, the whole load: no scope is searched, as with
// cosmiconfig's load(path). Bootstrap files are still read.
func WithConfigFilesOnly() Option {
	return optionFunc(func(c *config) { c.configFilesOnly = true })
}
