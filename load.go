package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Load discovers, reads, decodes and merges every selected scope, and returns the result. Every problem found is
// returned, joined.
func (l *Loader) Load(ctx context.Context) (*Result, error) {
	c := l.cfg
	ld := &loading{ctx: ctx, c: c, roots: map[string]Root{}}
	defer ld.closeRoots()
	for _, f := range c.order {
		ld.order = append(ld.order, c.decoder(f))
	}

	for _, name := range c.scopeOrder() {
		if err := ctx.Err(); err != nil {
			return nil, contextError(err)
		}
		s := c.scope(name)
		s.kind.discover(ld, s)
	}
	if err := ctx.Err(); err != nil {
		return nil, contextError(err)
	}
	ld.dedup()
	layers, err := ld.decode()
	ld.log()
	if err != nil {
		return nil, err
	}
	res, err := merge(c.merge, layers)
	if err != nil {
		return nil, err
	}
	res.found = ld.found()
	res.warnings = ld.warnings
	return res, nil
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(CodeGatewayTimeout, "load", "", err)
	}
	return NewError(CodeCanceled, "load", "", err)
}

// loading is the state of one load.
type loading struct {
	ctx      context.Context
	c        *config
	order    []Decoder // the searched formats' decoders, highest precedence first
	entries  []*entry  // lowest precedence first
	warnings []Warning // warnings that belong to no layer
	roots    map[string]Root
}

// entry is one layer the load considered, or a problem that fails it.
type entry struct {
	layer Layer
	data  []byte      // a file's contents
	tree  Tree        // a value scope's tree
	info  fs.FileInfo // a file's identity in the loader's FileSystem, for dedup; nil for EmbeddedFS
	err   error
}

func (ld *loading) addError(err error) {
	ld.entries = append(ld.entries, &entry{err: err})
}

// skip records a layer that won't load, with a warning.
func (ld *loading) skip(l Layer, status LayerStatus, code WarningCode, reason string) {
	l.Status, l.Reason = status, reason
	l.Warnings = append(l.Warnings, Warning{Code: code, Path: l.Path, Message: l.Path + ": " + reason})
	ld.entries = append(ld.entries, &entry{layer: l})
}

func (ld *loading) closeRoots() {
	for _, r := range ld.roots {
		if r != nil {
			_ = r.Close()
		}
	}
}

// root opens dir, once per load. A missing directory is silently skipped; any other failure is a warning.
func (ld *loading) root(dir string) (Root, bool) {
	if r, ok := ld.roots[dir]; ok {
		return r, r != nil
	}
	r, err := ld.c.fsys.OpenRoot(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
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
	case tpl == "~" || strings.HasPrefix(tpl, "~/") || strings.HasPrefix(tpl, `~\`):
		home, err := c.host.Home()
		if err != nil {
			ld.noHome(s, err)
			return "", false
		}
		base, rest = home, tpl[1:]
	}
	return filepath.Clean(base + filepath.FromSlash(expand(rest, c.name, ""))), true
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
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
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
		ld.addError(&IOError{BaseError: BaseError{Op: "load", Path: p, Scope: string(s.name), Err: err}})
		return "", false
	}
	return filepath.Join(cwd, p), true
}

func (ld *loading) noHome(s *scopeDef, err error) {
	ld.warnings = append(ld.warnings, Warning{
		Code: WarnNoHome, Message: fmt.Sprintf("the scope %q was skipped: %v", s.name, err),
	})
}

// search finds each pattern's files in root, lowest precedence first. dir is root's path, or empty for an fs.FS, whose
// layers are named by their slash path. follow says whether symlinks can be followed through the loader's FileSystem.
func (ld *loading) search(root Root, dir string, patterns []Pattern, s *scopeDef, follow bool) {
	for _, p := range patterns {
		sub := path.Dir(p.Template)
		if sub != "." {
			sub = expand(sub, ld.c.name, "")
		}
		entries, err := root.ReadDir(sub)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				ld.warnings = append(ld.warnings, Warning{
					Code: WarnUnreadable, Path: ld.join(dir, sub), Message: fmt.Sprintf("%s can't be searched: %v", ld.join(dir, sub), err),
				})
			}
			continue
		}
		types := map[string]fs.FileMode{}
		for _, e := range entries {
			types[e.Name()] = e.Type()
		}
		for _, l := range ld.candidates(p.Template, types, dir, sub) {
			l.Scope, l.Trust = s.name, s.trust
			if l.Status != LayerLoaded {
				ld.entries = append(ld.entries, &entry{layer: l})
				continue
			}
			ld.read(root, path.Join(sub, path.Base(filepath.ToSlash(l.Path))), types[path.Base(filepath.ToSlash(l.Path))], l, follow)
		}
	}
}

func (ld *loading) join(dir, slashPath string) string {
	if dir == "" {
		return slashPath
	}
	return filepath.Join(dir, filepath.FromSlash(slashPath))
}

// candidates returns the files that match a template among a directory's entries, lowest precedence first. With
// {ext}, the files form a slot ordered by orderSlot.
func (ld *loading) candidates(tpl string, types map[string]fs.FileMode, dir, sub string) []Layer {
	name := ld.c.name
	if !slices.Contains(placeholders(tpl), "{ext}") {
		file := path.Base(expand(tpl, name, ""))
		if _, ok := types[file]; !ok {
			return nil
		}
		format, ok := ld.formatOf(file)
		if !ok {
			return nil
		}
		return []Layer{{Path: ld.join(dir, path.Join(sub, file)), Format: format}}
	}
	var slot []Layer
	for _, d := range ld.order {
		for _, ext := range d.Extensions() {
			file := path.Base(expand(tpl, name, ext))
			if _, ok := types[file]; ok {
				slot = append(slot, Layer{Path: ld.join(dir, path.Join(sub, file)), Format: d.Format()})
			}
		}
	}
	if len(slot) == 0 {
		return nil
	}
	ordered, err := orderSlot(slot, ld.order, ld.c.conflict)
	if err != nil {
		ld.addError(err)
		return nil
	}
	return ordered
}

// formatOf returns the searched format whose decoder claims file's extension.
func (ld *loading) formatOf(file string) (string, bool) {
	ext := strings.TrimPrefix(path.Ext(file), ".")
	for _, d := range ld.order {
		if slices.Contains(d.Extensions(), ext) {
			return d.Format(), true
		}
	}
	return "", false
}

// read reads one candidate, named by its slash path in root, whose directory entry has type typ.
func (ld *loading) read(root Root, name string, typ fs.FileMode, l Layer, follow bool) {
	c := ld.c
	r := root
	switch {
	case typ&fs.ModeSymlink != 0 && follow:
		target, err := c.fsys.EvalSymlinks(l.Path)
		if err != nil {
			ld.skip(l, LayerIgnored, WarnBrokenSymlink, fmt.Sprintf("the symlink is broken: %v", err))
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
		ld.addError(tooLarge(l, info.Size(), c.maxFileSize))
		return
	}
	data, err := readLimited(r, name, c.maxFileSize)
	if err != nil {
		ld.skip(l, LayerUnreadable, WarnUnreadable, err.Error())
		return
	}
	if int64(len(data)) > c.maxFileSize {
		ld.addError(tooLarge(l, int64(len(data)), c.maxFileSize))
		return
	}
	e := &entry{layer: l, data: data}
	if follow {
		e.info = info
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

// dedup keeps one layer for each file reached more than once, at its highest position, with the scope and trust of
// its most trusted route.
func (ld *loading) dedup() {
	var kept []*entry
	for _, e := range ld.entries {
		if e.info != nil {
			for i, prev := range kept {
				if prev.info == nil || !ld.c.fsys.SameFile(prev.info, e.info) {
					continue
				}
				if prev.layer.Trust > e.layer.Trust {
					e.layer.Scope, e.layer.Trust = prev.layer.Scope, prev.layer.Trust
				}
				kept = slices.Delete(kept, i, i+1)
				break
			}
		}
		kept = append(kept, e)
	}
	ld.entries = kept
}

// decode decodes every loaded file, and returns the layers to merge, or every problem found.
func (ld *loading) decode() ([]mergeLayer, error) {
	var errs []error
	var layers []mergeLayer
	for _, e := range ld.entries {
		if e.err != nil {
			errs = append(errs, e.err)
			continue
		}
		if e.layer.Status != LayerLoaded {
			continue
		}
		if e.tree == nil {
			doc, err := DecodeDocument(ld.c.decoder(e.layer.Format), e.layer.Path, e.data)
			if err != nil {
				var pe *ParseError
				if errors.As(err, &pe) && pe.Scope == "" {
					pe.Scope = string(e.layer.Scope)
				}
				errs = append(errs, err)
				continue
			}
			e.layer.Warnings = append(e.layer.Warnings, doc.Warnings...)
			if doc.Empty {
				e.layer.Status = LayerEmpty
				continue
			}
			e.tree = doc.Tree
		}
		layers = append(layers, mergeLayer{layer: e.layer, tree: e.tree})
	}
	return layers, errors.Join(errs...)
}

func (ld *loading) found() []Layer {
	var out []Layer
	for _, e := range ld.entries {
		if e.err == nil {
			out = append(out, e.layer)
		}
	}
	return out
}

// log sends every warning to the logger.
func (ld *loading) log() {
	logger := ld.c.logger
	if logger == nil {
		logger = slog.Default()
	}
	emit := func(w Warning) {
		logger.LogAttrs(ld.ctx, slog.LevelWarn, w.Message, slog.String("code", string(w.Code)), slog.String("path", w.Path))
	}
	for _, e := range ld.entries {
		for _, w := range e.layer.Warnings {
			emit(w)
		}
	}
	for _, w := range ld.warnings {
		emit(w)
	}
}

// addValues adds a value scope's tree, without the unset key, which a value scope can't use.
func (ld *loading) addValues(s *scopeDef, t Tree) {
	l := Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}
	if key := ld.c.merge.unsetKey; key != "" && dropKey(t, key) {
		l.Warnings = append(l.Warnings, Warning{
			Code:    WarnReservedKey,
			Message: fmt.Sprintf("the scope %q can't remove keys, so its %s keys were ignored", s.name, key),
		})
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t})
}

// dropKey removes key from every table in v, and reports whether there was one.
func dropKey(v any, key string) bool {
	found := false
	switch v := v.(type) {
	case map[string]any:
		if _, ok := v[key]; ok {
			delete(v, key)
			found = true
		}
		for _, e := range v {
			found = dropKey(e, key) || found
		}
	case []any:
		for _, e := range v {
			found = dropKey(e, key) || found
		}
	}
	return found
}

// addEnvVars adds an EnvVars scope's values.
func (ld *loading) addEnvVars(s *scopeDef, k envVarsKind) {
	h := ld.c.host
	windows := h.GOOS() == "windows"
	norm := func(name string) string {
		if windows {
			return strings.ToUpper(name)
		}
		return name
	}
	bound := map[string]bool{}
	for _, v := range ld.c.vars {
		bound[norm(v)] = true
	}

	type variable struct{ name, value string }
	var vars []variable
	for _, kv := range h.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" || len(name) < len(k.prefix) || bound[norm(name)] {
			continue
		}
		if p := name[:len(k.prefix)]; p != k.prefix && (!windows || !strings.EqualFold(p, k.prefix)) {
			continue
		}
		vars = append(vars, variable{name, value})
	}
	slices.SortFunc(vars, func(a, b variable) int { return strings.Compare(a.name, b.name) })

	t := Tree{}
	l := Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}
	warn := func(code WarningCode, format string, args ...any) {
		l.Warnings = append(l.Warnings, Warning{Code: code, Message: fmt.Sprintf(format, args...)})
	}
	for _, v := range vars {
		var key Path
		var value any = v.value
		if k.mapper != nil {
			var ok bool
			key, value, ok = k.mapper(v.name, v.value)
			if !ok {
				continue
			}
			if len(key) == 0 {
				warn(WarnEnvVarSkipped, "%s: the mapping gave an empty key, so it was skipped", v.name)
				continue
			}
			if err := checkCanonical(value, key); err != nil {
				ld.addError(NewError(CodeInternal, "MapEnv", "", fmt.Errorf("the variable %s: the function returned %w", v.name, err)))
				return
			}
		} else {
			key = strings.Split(v.name[len(k.prefix):], "__")
			if slices.Contains(key, "") {
				warn(WarnEnvVarSkipped, "%s: the name has an empty segment, so it was skipped", v.name)
				continue
			}
			for i := range key {
				key[i] = strings.ToLower(key[i])
			}
		}
		if setPath(t, key, value) {
			warn(WarnEnvVarClash, "%s clashes with an earlier variable at %s; the later name wins", v.name, key)
		}
	}
	if unset := ld.c.merge.unsetKey; unset != "" && dropKey(t, unset) {
		warn(WarnReservedKey, "the scope %q can't remove keys, so its %s keys were ignored", s.name, unset)
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t})
}

// setPath sets the value at key in t, replacing anything in the way, and reports whether it replaced something.
func setPath(t Tree, key Path, value any) bool {
	clash := false
	m := t
	for _, k := range key[:len(key)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			if _, exists := m[k]; exists {
				clash = true
			}
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	last := key[len(key)-1]
	if _, exists := m[last]; exists {
		clash = true
	}
	m[last] = value
	return clash
}
