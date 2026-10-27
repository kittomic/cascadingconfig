package cascadingconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
)

func (ld *loading) closeRoots() {
	for _, r := range ld.roots {
		if r != nil {
			_ = r.Close()
		}
	}
	if ld.cache != nil {
		_ = ld.cache.Close()
	}
}

// root opens dir, once per load. A missing directory is silently skipped; any other failure is a warning.
func (ld *loading) root(dir string) (Root, bool) {
	if r, ok := ld.roots[dir]; ok {
		return r, r != nil
	}
	r, err := ld.c.fsys.OpenRoot(dir)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case ld.underNonDir(dir):
			ld.notADir(dir)
		default:
			ld.warnings = append(ld.warnings, Warning{
				Code: WarnUnreadable, Path: dir, Message: fmt.Sprintf("%s can't be searched: %v", dir, err),
			})
		}
		ld.roots[dir] = nil
		return nil, false
	}
	ld.roots[dir] = r
	return r, true
}

// resolveDir fills in a FixedDir directory: ~, {user} or {system}, then {name}. It returns false, with a warning, when
// the directory can't be worked out.
func (ld *loading) resolveDir(tpl string, s *scopeDef) (string, bool) {
	c := ld.c
	base, rest := "", tpl
	switch {
	case strings.HasPrefix(tpl, "{user}"):
		rest = tpl[len("{user}"):]
		dir, ok := ld.platformDir(BindConfigDir, func() (string, error) { return c.dirs.userDir(c.host, c.name) }, s)
		if !ok {
			return "", false
		}
		base = dir
	case strings.HasPrefix(tpl, "{system}"):
		rest = tpl[len("{system}"):]
		dir, ok := ld.platformDir(BindSystemConfigDir, func() (string, error) { return c.dirs.systemDir(c.host, c.name), nil }, s)
		if !ok {
			return "", false
		}
		base = dir
	case strings.HasPrefix(tpl, "{cwd}"):
		rest = tpl[len("{cwd}"):]
		cwd, err := c.host.Cwd()
		if err != nil {
			ld.addError(&IOError{BaseError: BaseError{Op: "load", Scope: string(s.name), Err: err}})
			return "", false
		}
		base = cwd
	case isHomePath(tpl):
		home, err := c.host.Home()
		if err != nil {
			ld.noHome(s, err)
			return "", false
		}
		base, rest = home, tpl[1:]
	}
	return filepath.Clean(base + filepath.FromSlash(expand(rest, c.name, "", ""))), true
}

// platformDir returns a platform directory: the binding's value when it is declared and set, or else def's.
func (ld *loading) platformDir(b Binding, def func() (string, error), s *scopeDef) (string, bool) {
	c := ld.c
	if v, ok := c.vars[b]; ok {
		if val, ok := c.host.LookupEnv(v); ok && val != "" {
			return ld.hostPath(val, s)
		}
	}
	dir, err := def()
	if err != nil {
		ld.noHome(s, err)
		return "", false
	}
	return dir, true
}

// hostPath resolves a path from an env var: ~ is expanded, and a relative path is relative to the current directory.
func (ld *loading) hostPath(p string, s *scopeDef) (string, bool) {
	h := ld.c.host
	if isHomePath(p) {
		home, err := h.Home()
		if err != nil {
			ld.noHome(s, err)
			return "", false
		}
		return filepath.Join(home, p[1:]), true
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), true
	}
	cwd, err := h.Cwd()
	if err != nil {
		ld.addError(&IOError{BaseError: BaseError{Op: "load", Path: p, Scope: scopeName(s), Err: err}})
		return "", false
	}
	return filepath.Join(cwd, p), true
}

func scopeName(s *scopeDef) string {
	if s == nil {
		return ""
	}
	return string(s.name)
}

func (ld *loading) noHome(s *scopeDef, err error) {
	msg := fmt.Sprintf("a path with ~ was skipped: %v", err)
	if s != nil {
		msg = fmt.Sprintf("the scope %q was skipped: %v", s.name, err)
	}
	for _, w := range ld.warnings {
		if w.Code == WarnNoHome && w.Message == msg {
			return
		}
	}
	ld.warnings = append(ld.warnings, Warning{Code: WarnNoHome, Message: msg})
}

// underNonDir reports whether dir, which couldn't be opened, is or lies under something that isn't a directory, such as
// a binary named after the app (DL-262). It checks the path itself, not the error, which differs between systems.
func (ld *loading) underNonDir(dir string) bool {
	for p := dir; ; {
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		r, err := ld.c.fsys.OpenRoot(parent)
		if err != nil {
			p = parent
			continue
		}
		info, err := r.Stat(filepath.Base(p))
		_ = r.Close()
		return err == nil && !info.IsDir()
	}
}

// underNonDirIn reports whether sub, a slash path in root that couldn't be listed, is or lies under something that
// isn't a directory (DL-262).
func underNonDirIn(root Root, sub string) bool {
	parts := strings.Split(sub, "/")
	for i := range parts {
		info, err := root.Stat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return false
		}
		if !info.IsDir() {
			return true
		}
	}
	return false
}

// notADir logs a directory skipped because something on its path isn't a directory, at Debug (DL-262).
func (ld *loading) notADir(dir string) {
	ld.c.log().LogAttrs(ld.ctx, slog.LevelDebug, dir+" isn't a directory, so it's skipped",
		slog.String("op", "not-a-dir"), slog.String("path", dir))
}

// realPath resolves p's symlinks, if it exists.
func (ld *loading) realPath(p string) string {
	if resolved, err := ld.c.fsys.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// within reports whether p is dir or below it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
