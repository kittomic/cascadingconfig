package cascadingconfig

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
)

// loadExplicit loads files as given, at the current position: no variants, any registered decoder, and errors for
// files that are missing, unreadable or of an unknown format. Explicit files are never ignored.
func (ld *loading) loadExplicit(paths []string, scope ScopeName, trust TrustLevel, s *scopeDef) {
	for _, p := range paths {
		abs, ok := ld.hostPath(p, s)
		if !ok {
			continue
		}
		l := Layer{Path: abs, Scope: scope, Trust: trust, Status: LayerLoaded, ConfigRoot: filepath.Dir(abs)}
		if s != nil && s.configRoot != "" {
			if root, ok := ld.hostPath(s.configRoot, s); ok {
				l.ConfigRoot = root
			}
		}
		ld.readNamed(l, "", true)
	}
}

// readNamed reads a file named directly, at l.Path: an explicit file, or a custom scope's Path source. format, when
// given, overrides the extension. A missing file, an unreadable one or an unknown format is an error. Only a file
// that isn't explicit can be ignored, and only it is skipped, rather than failing the load, by a policy check.
func (ld *loading) readNamed(l Layer, format string, explicit bool) {
	c := ld.c
	abs := l.Path
	base := BaseError{Op: "load", Path: abs, Scope: string(l.Scope)}
	ld.watchFile(abs)
	resolved, err := c.fsys.EvalSymlinks(abs)
	if err != nil {
		base.Err = err
		if errors.Is(err, fs.ErrNotExist) {
			ld.failLayer(l, &NotFoundError{BaseError: base})
		} else {
			ld.failLayer(l, &IOError{BaseError: base})
		}
		return
	}
	if !explicit && ld.ignore(l, resolved) {
		return
	}
	if format == "" {
		var ok bool
		if format, ok = c.fileFormat(filepath.ToSlash(abs)); !ok {
			base.Err = fmt.Errorf("no format rule or registered decoder's extension matches %s", abs)
			ld.failLayer(l, &UnsupportedFormatError{BaseError: base})
			return
		}
	}
	l.Format = format
	root, err := c.fsys.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		base.Err = err
		ld.failLayer(l, &IOError{BaseError: base})
		return
	}
	info, err := root.Stat(filepath.Base(resolved))
	switch {
	case err != nil:
		base.Err = err
	case !info.Mode().IsRegular():
		base.Err = fmt.Errorf("it is %s, not a regular file", describeMode(info.Mode()))
	case info.Size() > c.maxFileSize:
		_ = root.Close()
		ld.failLayer(l, tooLarge(l, info.Size(), c.maxFileSize))
		return
	}
	if base.Err != nil {
		_ = root.Close()
		ld.failLayer(l, &IOError{BaseError: base})
		return
	}
	data, err := readLimited(root, filepath.Base(resolved), c.maxFileSize)
	_ = root.Close()
	if err != nil {
		base.Err = err
		ld.failLayer(l, &IOError{BaseError: base})
		return
	}
	if int64(len(data)) > c.maxFileSize {
		ld.failLayer(l, tooLarge(l, int64(len(data)), c.maxFileSize))
		return
	}
	e := &entry{layer: l, data: data, info: info, file: true, explicit: explicit, real: resolved}
	ld.watchLoaded(abs, resolved)
	if root, err := c.fsys.OpenRoot(filepath.Dir(resolved)); err == nil {
		if di, err := root.Stat("."); err == nil {
			e.dirInfo = di
		}
		_ = root.Close()
	}
	ld.entries = append(ld.entries, e)
}

// read reads one candidate, named by its slash path in root, whose directory entry has type typ.
func (ld *loading) read(root Root, name string, typ fs.FileMode, l Layer, follow bool, key Path) {
	c := ld.c
	r := root
	if follow && len(ld.ignored) > 0 && typ&fs.ModeSymlink == 0 {
		if resolved, err := c.fsys.EvalSymlinks(l.Path); err == nil && ld.ignore(l, resolved) {
			return
		}
	}
	switch {
	case typ&fs.ModeSymlink != 0 && follow:
		ld.watchSymlink(l.Path)
		target, err := c.fsys.EvalSymlinks(l.Path)
		if err != nil {
			ld.skip(l, LayerIgnored, WarnBrokenSymlink, fmt.Sprintf("the symlink is broken: %v", err))
			return
		}
		if ld.ignore(l, target) {
			return
		}
		tr, ok := ld.root(filepath.Dir(target))
		if !ok {
			ld.skip(l, LayerUnreadable, WarnUnreadable, fmt.Sprintf("the directory of the symlink's target, %s, can't be opened", target))
			return
		}
		r, name = tr, filepath.Base(target)
	case !typ.IsRegular():
		ld.skip(l, LayerIgnored, WarnNotAFile, fmt.Sprintf("it is %s, not a regular file", describeMode(typ)))
		return
	}

	info, err := r.Stat(name)
	if err != nil {
		ld.skip(l, LayerUnreadable, WarnUnreadable, err.Error())
		return
	}
	if !info.Mode().IsRegular() {
		ld.skip(l, LayerIgnored, WarnNotAFile, fmt.Sprintf("it leads to %s, not a regular file", describeMode(info.Mode())))
		return
	}
	if info.Size() > c.maxFileSize {
		ld.failLayer(l, tooLarge(l, info.Size(), c.maxFileSize))
		return
	}
	data, err := readLimited(r, name, c.maxFileSize)
	if err != nil {
		ld.skip(l, LayerUnreadable, WarnUnreadable, err.Error())
		return
	}
	if int64(len(data)) > c.maxFileSize {
		ld.failLayer(l, tooLarge(l, int64(len(data)), c.maxFileSize))
		return
	}
	e := &entry{layer: l, data: data, key: key, file: follow}
	if follow {
		e.info, e.real = info, l.Path
		if resolved, err := c.fsys.EvalSymlinks(l.Path); err == nil {
			e.real = resolved
		}
		ld.watchLoaded(l.Path, e.real)
		if di, err := r.Stat(path.Dir(name)); err == nil {
			e.dirInfo = di
		}
	}
	ld.entries = append(ld.entries, e)
}

func readLimited(r Root, name string, limit int64) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func tooLarge(l Layer, size, limit int64) error {
	return &TooLargeError{BaseError: BaseError{
		Op: "load", Path: l.Path, Scope: string(l.Scope),
		Err: fmt.Errorf("the file is at least %d bytes, and the limit is %d", size, limit),
	}}
}

func describeMode(m fs.FileMode) string {
	switch {
	case m.IsDir():
		return "a directory"
	case m&fs.ModeNamedPipe != 0:
		return "a FIFO"
	case m&fs.ModeSocket != 0:
		return "a socket"
	case m&fs.ModeDevice != 0, m&fs.ModeCharDevice != 0:
		return "a device"
	case m&fs.ModeSymlink != 0:
		return "a symlink"
	default:
		return "not a regular file"
	}
}

// decodeEntry decodes one loaded entry, and reports whether it has config to merge. Empty files, and files whose
// pattern key is missing or holds an empty table, have none.
func (ld *loading) decodeEntry(e *entry) (bool, error) {
	if e.layer.Status != LayerLoaded {
		return false, nil
	}
	if e.tree != nil {
		return true, nil
	}
	doc, err := DecodeDocument(ld.c.decoder(e.layer.Format), e.layer.Path, e.data)
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) && pe.Scope == "" {
			pe.Scope = string(e.layer.Scope)
		}
		e.layer.Status, e.layer.Reason = LayerFailed, failReason(err)
		return false, err
	}
	e.layer.Warnings = append(e.layer.Warnings, doc.Warnings...)
	if doc.Empty {
		e.layer.Status = LayerEmpty
		return false, nil
	}
	e.tree = doc.Tree
	if e.key != nil {
		t, ok, err := subtree(doc.Tree, e.key)
		if err != nil {
			pe := &ParseError{
				BaseError: BaseError{Op: "decode", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: err},
				Format:    e.layer.Format,
			}
			e.layer.Status, e.layer.Reason = LayerFailed, failReason(pe)
			return false, pe
		}
		if !ok {
			e.dropped = true
			return false, nil
		}
		if len(t) == 0 {
			e.layer.Status = LayerEmpty
			return false, nil
		}
		e.tree = t
	}
	return true, nil
}

// subtree returns the table at key in t, false if there's nothing there, or an error if the value isn't a table.
func subtree(t Tree, key Path) (Tree, bool, error) {
	var v any = t
	for i, k := range key {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("the key %s isn't a table", key[:i])
		}
		if v, ok = m[k]; !ok {
			return nil, false, nil
		}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("the key %s holds %s, not a table", key, describeType(v))
	}
	return m, true, nil
}
