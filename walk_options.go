package cascadingconfig

// WalkStop says where a walk stops.
type WalkStop int

// Walk stops.
const (
	// StopAtHome stops after ~ when the start directory is inside it, and walks to / otherwise. It is the default.
	StopAtHome WalkStop = iota
	// StopAtRoot always walks to /, as mise does.
	StopAtRoot
	// StopAtStart searches the start directory only, as cosmiconfig's "none" strategy does.
	StopAtStart
	// StopAtMarker walks up to and including the first directory that contains an entry WithRootMarker names, or to
	// /, as cosmiconfig's "project" strategy does.
	StopAtMarker
)

// WithStartDir sets where walks start. By default it is the current directory at each load; a relative dir resolves
// against it.
func WithStartDir(dir string) Option {
	return optionFunc(func(c *config) {
		if dir == "" {
			c.fail("WithStartDir", "an empty start directory")
			return
		}
		c.startDir = dir
	})
}

// WithWalkStop sets where walks stop.
func WithWalkStop(s WalkStop) Option {
	return optionFunc(func(c *config) {
		if s != StopAtHome && s != StopAtRoot {
			c.fail("WithWalkStop", "unknown walk stop %d", int(s))
			return
		}
		c.walkStop = s
	})
}

// WithCeilingPaths adds exclusive walk ceilings: a ceiling is never searched, nor anything above it. They add to
// {NAME}_CEILING_PATHS. Empty entries are dropped.
func WithCeilingPaths(paths ...string) Option {
	paths = dropEmpty(paths)
	return optionFunc(func(c *config) { c.ceilings = append(c.ceilings, paths...) })
}

// WithIgnoredPaths adds paths that are never loaded: a file, or every file below a directory, after resolving
// symlinks. They add to {NAME}_IGNORED_CONFIG_PATHS. Explicit files are never ignored. Empty entries are dropped.
func WithIgnoredPaths(paths ...string) Option {
	paths = dropEmpty(paths)
	return optionFunc(func(c *config) { c.ignored = append(c.ignored, paths...) })
}

// platformEnvs returns the platform environments for a GOOS and GOARCH, lowest precedence first: unix where it
// applies, then {os}, then {os}-{arch}, with mise's names for macOS and x64.
func platformEnvs(goos, goarch string) []string {
	osName, arch := goos, goarch
	if goos == "darwin" {
		osName = "macos"
	}
	if goarch == "amd64" {
		arch = "x64"
	}
	var out []string
	switch goos {
	case "windows", "plan9", "js", "wasip1":
	default:
		out = append(out, "unix")
	}
	return append(out, osName, osName+"-"+arch)
}
