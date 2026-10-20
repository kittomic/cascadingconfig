package cascadingconfig

import "path/filepath"

// PlatformDirs says where the user and system directories are, for the {user} and {system} placeholders in FixedDir.
type PlatformDirs interface {
	userDir(h host, name string) (string, error)
	systemDir(h host, name string) string
}

// XDGDirs puts the user directory at $XDG_CONFIG_HOME/{name}, else ~/.config/{name}, on every OS, as mise does, and the
// system directory at /etc/{name}, or %ProgramData%\{name} on Windows. Never reassign it.
var XDGDirs PlatformDirs = xdgDirs{}

type xdgDirs struct{}

// userDir returns the user directory for app name on h. A relative $XDG_CONFIG_HOME is ignored, as the XDG spec says.
func (xdgDirs) userDir(h host, name string) (string, error) {
	if x, ok := h.LookupEnv("XDG_CONFIG_HOME"); ok && filepath.IsAbs(x) {
		return filepath.Join(x, name), nil
	}
	home, err := h.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", name), nil
}

// systemDir returns the system directory for app name on h.
func (xdgDirs) systemDir(h host, name string) string {
	if h.GOOS() == "windows" {
		base, ok := h.LookupEnv("ProgramData")
		if !ok || base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, name)
	}
	return filepath.Join("/etc", name)
}

// WithPlatformDirs sets where the user and system directories are. {NAME}_CONFIG_DIR and {NAME}_SYSTEM_CONFIG_DIR,
// when declared, override them.
func WithPlatformDirs(d PlatformDirs) Option {
	return optionFunc(func(c *config) {
		if d == nil {
			c.fail("WithPlatformDirs", "nil platform directories")
			return
		}
		c.dirs = d
	})
}
