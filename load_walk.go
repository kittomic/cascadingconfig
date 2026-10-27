package cascadingconfig

import (
	"path/filepath"
	"slices"
)

// scopeRoot returns the config root for a scope's RootSearchDir patterns in dir.
func (ld *loading) scopeRoot(s *scopeDef, dir string) (string, bool) {
	if s.configRoot == "" {
		return dir, true
	}
	return ld.hostPath(s.configRoot, s)
}

// walkDirs returns the directories a walk searches, root-most first. The bootstrap walk honours only the option's
// and the env var's ceilings; the main walk adds the bootstrap files' ceilings.
func (ld *loading) walkDirs(s *scopeDef, bootstrap bool) ([]string, bool) {
	if ld.walked && !bootstrap {
		return ld.walk, true
	}
	c, h := ld.c, ld.c.host
	cwd, err := h.Cwd()
	if err != nil {
		ld.addError(&IOError{BaseError: BaseError{Op: "load", Scope: string(s.name), Err: err}})
		return nil, false
	}
	start := cwd
	if c.startDir != "" {
		var ok bool
		if start, ok = ld.hostPath(c.startDir, s); !ok {
			return nil, false
		}
	}
	ceilings := map[string]bool{}
	list := slices.Clone(c.ceilings)
	if v := ld.binding(BindCeilingPaths); v != "" {
		list = append(list, ld.pathList(v)...)
	}
	for _, p := range list {
		if abs, ok := ld.hostPath(p, s); ok {
			ceilings[filepath.Clean(abs)] = true
		}
	}
	if !bootstrap {
		for _, p := range ld.settings.ceilings {
			ceilings[filepath.Clean(p)] = true
		}
	}
	stop, stopErr := h.Home()
	if c.stopDir != "" {
		var ok bool
		if stop, ok = ld.hostPath(c.stopDir, s); !ok {
			return nil, false
		}
		stopErr = nil
	}
	var dirs []string
	for d := filepath.Clean(start); !ceilings[d]; {
		dirs = append(dirs, d)
		if c.walkStop == StopAtStart ||
			c.walkStop == StopAtHome && stopErr == nil && d == filepath.Clean(stop) ||
			c.walkStop == StopAtMarker && ld.hasMarker(d) {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	slices.Reverse(dirs)
	if bootstrap {
		return dirs, true
	}

	if c.dirs != nil {
		if v := ld.binding(BindConfigDir); v != "" {
			def, err1 := c.dirs.userDir(h, c.name)
			moved, ok := ld.hostPath(v, s)
			if err1 == nil && ok && filepath.Clean(def) != filepath.Clean(moved) {
				ld.dropUnder = filepath.Clean(def)
			}
		}
	}
	ld.walk, ld.walked = dirs, true
	return dirs, true
}

// hasMarker reports whether dir contains an entry named by WithRootMarker.
func (ld *loading) hasMarker(dir string) bool {
	root, ok := ld.root(dir)
	if !ok {
		return false
	}
	for _, e := range ld.readDir(root, dir, ".") {
		if slices.Contains(ld.c.markers, e.Name()) {
			return true
		}
	}
	return false
}

// isHomeOrRoot reports whether dir is ~ or a filesystem root.
func (ld *loading) isHomeOrRoot(dir string) bool {
	if filepath.Dir(dir) == dir {
		return true
	}
	home, err := ld.c.host.Home()
	return err == nil && filepath.Clean(home) == dir
}
