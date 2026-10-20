package cascadingconfig

// WalkStop says where a walk stops.
type WalkStop int

// Walk stops.
const (
	// StopAtHome stops after ~ when the start directory is inside it, and walks to / otherwise. It is the default.
	StopAtHome WalkStop = iota
	// StopAtRoot always walks to /, as mise does.
	StopAtRoot
)

// WithStartDir sets where walks start. By default it is the current directory at each load; a relative dir resolves
// against it.
func WithStartDir(dir string) Option {
	_ = dir // stub
	return optionFunc(func(*config) {})
}

// WithWalkStop sets where walks stop.
func WithWalkStop(s WalkStop) Option {
	_ = s // stub
	return optionFunc(func(*config) {})
}

// WithCeilingPaths adds exclusive walk ceilings: a ceiling is never searched, nor anything above it. They add to
// {NAME}_CEILING_PATHS.
func WithCeilingPaths(paths ...string) Option {
	_ = paths // stub
	return optionFunc(func(*config) {})
}

// WithIgnoredPaths adds paths that are never loaded: a file, or every file below a directory, after resolving
// symlinks. They add to {NAME}_IGNORED_CONFIG_PATHS. Explicit files are never ignored.
func WithIgnoredPaths(paths ...string) Option {
	_ = paths // stub
	return optionFunc(func(*config) {})
}

// platformEnvs returns the platform environments for a GOOS and GOARCH, lowest precedence first.
func platformEnvs(goos, goarch string) []string {
	_, _ = goos, goarch // stub
	return nil
}
