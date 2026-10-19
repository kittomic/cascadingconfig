package cascadingconfig

// PlatformDirs says where the user and system directories are, for the {user} and {system} placeholders in FixedDir.
type PlatformDirs interface {
	platformDirs()
}

// XDGDirs puts the user directory at $XDG_CONFIG_HOME/{name}, else ~/.config/{name}, on every OS, as mise does, and the
// system directory at /etc/{name}, or %ProgramData%\{name} on Windows. Never reassign it.
var XDGDirs PlatformDirs = xdgDirs{}

type xdgDirs struct{}

func (xdgDirs) platformDirs() {}

// systemDir returns the system directory for app name on h.
func (xdgDirs) systemDir(h host, name string) string {
	_, _ = h, name // stub
	return ""
}

// WithPlatformDirs sets where the user and system directories are. {NAME}_CONFIG_DIR and {NAME}_SYSTEM_CONFIG_DIR,
// when declared, override them.
func WithPlatformDirs(d PlatformDirs) Option {
	_ = d // stub
	return optionFunc(func(*config) {})
}
