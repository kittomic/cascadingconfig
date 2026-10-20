package cascadingconfig

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
	_ = r // stub
	return optionFunc(func(*config) {})
}

// WithStopDir sets where a StopAtHome walk stops, inclusive, instead of ~. If dir isn't an ancestor of the start
// directory, the walk goes to /.
func WithStopDir(dir string) Option {
	_ = dir // stub
	return optionFunc(func(*config) {})
}

// WithRootMarker sets the names whose presence in a directory stops a StopAtMarker walk there, inclusive, such as .git
// or go.mod.
func WithRootMarker(names ...string) Option {
	_ = names // stub
	return optionFunc(func(*config) {})
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
	_ = s // stub
	return optionFunc(func(*config) {})
}

// WithConfigFilesOnly makes explicit config files, when they're set, the whole load: no scope is searched, as with
// cosmiconfig's load(path). Bootstrap files are still read.
func WithConfigFilesOnly() Option {
	return optionFunc(func(*config) {})
}
