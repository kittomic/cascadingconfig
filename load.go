package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Load discovers, reads, decodes and merges every selected scope, and returns the result. Every problem found is
// returned, joined.
func (l *Loader) Load(ctx context.Context) (*Result, error) {
	c := l.cfg
	ld := &loading{ctx: ctx, c: c, roots: map[string]Root{}, dirs: map[string][]fs.DirEntry{}}
	defer ld.closeRoots()
	if err := ld.formatOrder(); err != nil {
		return nil, err
	}
	if err := ld.readBootstrap(); err != nil {
		ld.log()
		return nil, err
	}
	if err := ld.selectEnvs(); err != nil {
		return nil, err
	}
	ld.ignoredPaths()

	explicit := c.configFiles
	if explicit == nil {
		if v := ld.binding(BindConfigFile); v != "" {
			explicit = ld.pathList(v)
		}
	}
	placed := false
	for _, s := range c.scopes {
		if err := ctx.Err(); err != nil {
			return nil, contextError(err)
		}
		ld.order = ld.orderFor(s)
		switch {
		case s.explicitFiles && explicit != nil:
			ld.loadExplicit(explicit, s.name, TrustProject, s)
			placed = true
		case !c.selected[s.name]:
		case s.disabledBy != "" && ld.binding(s.disabledBy) != "":
		case s.replacedByFile != "" && ld.binding(s.replacedByFile) != "":
			ld.loadExplicit([]string{ld.binding(s.replacedByFile)}, s.name, s.trust, s)
		default:
			s.kind.discover(ld, s)
		}
	}
	if explicit != nil && !placed {
		ld.loadExplicit(explicit, "", TrustProject, nil)
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
	for _, e := range ld.boot {
		res.bootstrap = append(res.bootstrap, e.layer)
	}
	return res, nil
}

// formatOrder works out the order bootstrap files are found with, and every scope's order unless a bootstrap file
// changes it: {NAME}_CONFIG_FORMATS, or else the app's or the preset's order.
func (ld *loading) formatOrder() error {
	c := ld.c
	formats := c.order
	if v := ld.binding(BindConfigFormats); v != "" {
		parsed, errs := c.parseFormats(strings.Split(v, ","), true)
		if len(errs) > 0 {
			return &OptionError{BaseError: BaseError{Op: c.vars[BindConfigFormats], Err: errors.Join(errs...)}}
		}
		formats, ld.formatsFromEnv = parsed, true
	}
	ld.baseOrder = ld.decoders(formats)
	return nil
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
	dirs     map[string][]fs.DirEntry // directory listings, by root and path

	envs      []string      // the environments, lowest precedence first
	envSource string        // where the selected environments came from, for errors
	ignored   []ignoredPath // ignored paths, with symlinks resolved
	dropUnder string        // the default user directory, when {NAME}_CONFIG_DIR moved it
	walk      []string      // the main walk's directories, once worked out
	walked    bool
	walking   bool // a Walk is searching, so SkipHomeAndRoot applies

	baseOrder      []Decoder // {NAME}_CONFIG_FORMATS, or the app's or the preset's order
	formatsFromEnv bool
	boot           []*entry // the bootstrap files, lowest precedence first
	settings       bootSettings
}

// entry is one layer the load considered, or a problem that fails it.
type entry struct {
	layer Layer
	data  []byte      // a file's contents
	tree  Tree        // a value scope's tree
	info  fs.FileInfo // a file's identity in the loader's FileSystem, for dedup; nil for EmbeddedFS
	key   Path        // the table inside the file that holds the config, if the pattern has a Key
	err   error

	dropped bool // the pattern's key wasn't in the file, so it counts as not found
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

// expansion is one pattern for one environment, or for none.
type expansion struct {
	p   Pattern
	env string
}

// expansions orders a scope's patterns for one directory: shared, then env once per environment, then local, then
// env-local, keeping the listed order within each group.
func (ld *loading) expansions(patterns []Pattern) []expansion {
	var out []expansion
	group := func(v Variant, env string) {
		for _, p := range patterns {
			if p.Variant == v {
				out = append(out, expansion{p, env})
			}
		}
	}
	group(VariantShared, "")
	for _, env := range ld.envs {
		group(VariantEnv, env)
	}
	group(VariantLocal, "")
	for _, env := range ld.envs {
		group(VariantEnvLocal, env)
	}
	return out
}

// search finds each pattern's files in root, in pattern order. dir is root's path, or empty for an fs.FS, whose layers
// are named by their slash path. configRoot is the root of RootSearchDir patterns. follow says whether symlinks can be
// followed through the loader's FileSystem.
func (ld *loading) search(root Root, dir, configRoot string, patterns []Pattern, s *scopeDef, follow bool) {
	skip := ld.walking && ld.isHomeOrRoot(dir)
	for _, x := range ld.expansions(patterns) {
		if skip && x.p.SkipHomeAndRoot {
			continue
		}
		segs := strings.Split(x.p.Template, "/")
		subs := []string{"."}
		for _, seg := range segs[:len(segs)-1] {
			subs = ld.matchDirs(root, dir, subs, expand(seg, ld.c.name, x.env, ""))
		}
		last := segs[len(segs)-1]
		for _, sub := range subs {
			entries := ld.readDir(root, dir, sub)
			if entries == nil {
				continue
			}
			for _, l := range ld.candidates(last, x.env, entries, dir, sub) {
				l.Scope, l.Trust = s.name, s.trust
				l.Variant, l.Env, l.Pattern = x.p.Variant, x.env, x.p.Template
				l.ConfigRoot = configRoot
				if x.p.Root == RootFileDir {
					l.ConfigRoot = filepath.Dir(l.Path)
				}
				if ld.dropUnder != "" && within(l.Path, ld.dropUnder) {
					continue
				}
				if l.Status != LayerLoaded {
					ld.entries = append(ld.entries, &entry{layer: l})
					continue
				}
				name := path.Base(filepath.ToSlash(l.Path))
				var key Path
				if x.p.Key != "" {
					key, _ = parsePath(expand(x.p.Key, ld.c.name, "", ""))
				}
				ld.read(root, path.Join(sub, name), typeOf(entries, name), l, follow, key)
			}
		}
	}
}

// isHomeOrRoot reports whether dir is ~ or a filesystem root.
func (ld *loading) isHomeOrRoot(dir string) bool {
	if filepath.Dir(dir) == dir {
		return true
	}
	home, err := ld.c.host.Home()
	return err == nil && filepath.Clean(home) == dir
}

func typeOf(entries []fs.DirEntry, name string) fs.FileMode {
	for _, e := range entries {
		if e.Name() == name {
			return e.Type()
		}
	}
	return 0
}

// readDir lists sub, a slash path in root, once per load. A missing directory gives nil; any other failure is a
// warning.
func (ld *loading) readDir(root Root, dir, sub string) []fs.DirEntry {
	key := dir + "\x00" + sub
	if entries, ok := ld.dirs[key]; ok {
		return entries
	}
	entries, err := root.ReadDir(sub)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			where := ld.join(dir, sub)
			ld.warnings = append(ld.warnings, Warning{
				Code: WarnUnreadable, Path: where, Message: fmt.Sprintf("%s can't be searched: %v", where, err),
			})
		}
		entries = nil
	}
	if entries == nil {
		entries = []fs.DirEntry{}
	}
	ld.dirs[key] = entries
	if len(entries) == 0 {
		return nil
	}
	return entries
}

// matchDirs extends each of subs by one directory segment, which may hold a * glob.
func (ld *loading) matchDirs(root Root, dir string, subs []string, seg string) []string {
	if !strings.Contains(seg, "*") {
		out := make([]string, len(subs))
		for i, sub := range subs {
			out[i] = path.Join(sub, seg)
		}
		return out
	}
	re := globPattern(seg, false)
	var out []string
	for _, sub := range subs {
		for _, e := range ld.readDir(root, dir, sub) {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && re.MatchString(e.Name()) {
				out = append(out, path.Join(sub, e.Name()))
			}
		}
	}
	return out
}

// globPattern turns a path segment with * into a regexp. In a file name, * matches a non-empty name with no dots; in a
// directory, any non-empty name.
func globPattern(seg string, file bool) *regexp.Regexp {
	star := ".+"
	if file {
		star = "[^.]+"
	}
	parts := strings.Split(seg, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, "("+star+")") + "$")
}

func (ld *loading) join(dir, slashPath string) string {
	if dir == "" {
		return slashPath
	}
	return filepath.Join(dir, filepath.FromSlash(slashPath))
}

// candidates returns the files in one directory that match a file-name template, lowest precedence first. With
// {ext}, the files with the same stem form a slot ordered by orderSlot; slots apply in alphabetical order.
func (ld *loading) candidates(last, env string, entries []fs.DirEntry, dir, sub string) []Layer {
	name := ld.c.name
	type match struct {
		file, format string
	}
	slots := map[string][]match{}
	add := func(e, format string) {
		if env != "" && len(e) > 255 {
			ld.envNameError(env, fmt.Errorf("the file name %q is longer than 255 bytes", e))
			return
		}
		if !strings.Contains(e, "*") {
			if hasEntry(entries, e) {
				slots[""] = append(slots[""], match{e, format})
			}
			return
		}
		re := globPattern(e, true)
		for _, en := range entries {
			if m := re.FindStringSubmatch(en.Name()); m != nil {
				stem := strings.Join(m[1:], "\x00")
				slots[stem] = append(slots[stem], match{en.Name(), format})
			}
		}
	}
	if !slices.Contains(placeholders(last), "{ext}") {
		e := expand(last, name, env, "")
		if strings.Contains(e, "*") {
			re := globPattern(e, true)
			for _, en := range entries {
				if re.MatchString(en.Name()) {
					if format, ok := ld.formatOf(en.Name()); ok {
						slots[en.Name()] = append(slots[en.Name()], match{en.Name(), format})
					}
				}
			}
		} else if format, ok := ld.formatOf(e); ok {
			add(e, format)
		}
	} else {
		for _, d := range ld.order {
			for _, ext := range d.Extensions() {
				add(expand(last, name, env, ext), d.Format())
			}
		}
	}

	var out []Layer
	for _, stem := range slices.Sorted(maps.Keys(slots)) {
		var slot []Layer
		for _, m := range slots[stem] {
			slot = append(slot, Layer{Path: ld.join(dir, path.Join(sub, m.file)), Format: m.format})
		}
		ordered, err := orderSlot(slot, ld.order, ld.c.conflict)
		if err != nil {
			ld.addError(err)
			continue
		}
		out = append(out, ordered...)
	}
	return out
}

func hasEntry(entries []fs.DirEntry, name string) bool {
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name })
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

// within reports whether p is dir or below it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// envNameError reports an environment name that gives an invalid file name, once per name.
func (ld *loading) envNameError(env string, err error) {
	op := ld.envSource
	if op == "" {
		op = "load"
	}
	for _, e := range ld.entries {
		var oe *OptionError
		if errors.As(e.err, &oe) && oe.Op == op && strings.Contains(oe.Error(), fmt.Sprintf("%q", env)) {
			return
		}
	}
	ld.addError(&OptionError{BaseError: BaseError{Op: op, Err: fmt.Errorf("the environment %q: %w", env, err)}})
}

// binding returns the value of a declared binding's variable, or "" when it isn't declared or set.
func (ld *loading) binding(b Binding) string {
	v, ok := ld.c.vars[b]
	if !ok {
		return ""
	}
	val, _ := ld.c.host.LookupEnv(v)
	return val
}

// pathList splits a path-list value with the host's separator, dropping empty entries.
func (ld *loading) pathList(v string) []string {
	sep := ":"
	if ld.c.host.GOOS() == "windows" {
		sep = ";"
	}
	return dropEmpty(strings.Split(v, sep))
}

// selectEnvs works out the environments: platform ones first, then the selected ones, from WithEnvs or {NAME}_ENV.
func (ld *loading) selectEnvs() error {
	c := ld.c
	selected := c.envs
	ld.envSource = "WithEnvs"
	if selected == nil {
		ld.envSource = c.vars[BindEnv]
		for _, e := range strings.Split(ld.binding(BindEnv), ",") {
			if e = strings.TrimSpace(e); e != "" {
				if err := checkName(e); err != nil {
					return &OptionError{BaseError: BaseError{Op: ld.envSource, Err: fmt.Errorf("invalid environment name %q: %w", e, err)}}
				}
				selected = append(selected, e)
			}
		}
	}
	if selected == nil && ld.settings.env != nil {
		selected, ld.envSource = ld.settings.env, "bootstrap"
	}
	var envs []string
	for i, e := range selected {
		if !slices.Contains(selected[i+1:], e) {
			envs = append(envs, e)
		}
	}
	for _, p := range platformEnvs(c.host.GOOS(), c.host.GOARCH()) {
		if !slices.Contains(envs, p) {
			ld.envs = append(ld.envs, p)
		}
	}
	ld.envs = append(ld.envs, envs...)
	return nil
}

// ignoredPaths resolves WithIgnoredPaths and {NAME}_IGNORED_CONFIG_PATHS.
func (ld *loading) ignoredPaths() {
	paths := slices.Clone(ld.c.ignored)
	if v := ld.binding(BindIgnoredConfigPaths); v != "" {
		paths = append(paths, ld.pathList(v)...)
	}
	for _, p := range paths {
		abs, ok := ld.hostPath(p, nil)
		if !ok {
			continue
		}
		ld.ignored = append(ld.ignored, ignoredPath{path: ld.realPath(abs), trust: TrustSystem})
	}
	for _, p := range ld.settings.ignored {
		p.path = ld.realPath(p.path)
		ld.ignored = append(ld.ignored, p)
	}
}

// realPath resolves p's symlinks, if it exists.
func (ld *loading) realPath(p string) string {
	if resolved, err := ld.c.fsys.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// isIgnored reports whether a file in a scope with trust, whose resolved path is resolved, is under an ignored path.
// An entry from a less trusted bootstrap file is dropped for it, with a warning on that file.
func (ld *loading) isIgnored(resolved string, trust TrustLevel) (string, bool) {
	for i := range ld.ignored {
		p := &ld.ignored[i]
		if !within(resolved, p.path) {
			continue
		}
		if p.trust >= trust {
			return p.path, true
		}
		if !p.drop && p.from != nil {
			p.drop = true
			p.from.layer.Warnings = append(p.from.layer.Warnings, Warning{
				Code: WarnBootstrapKeyDropped, Path: p.from.layer.Path,
				Message: fmt.Sprintf("%s: the ignored path %s points into a more trusted scope, so it was dropped for %s",
					p.from.layer.Path, p.path, resolved),
			})
		}
	}
	return "", false
}

// scopeRoot returns the config root for a scope's RootSearchDir patterns in dir.
func (ld *loading) scopeRoot(s *scopeDef, dir string) (string, bool) {
	if s.configRoot == "" {
		return dir, true
	}
	return ld.hostPath(s.configRoot, s)
}

// overrideFilenames applies {NAME}_OVERRIDE_CONFIG_FILENAMES to a walk's patterns: the listed names replace its
// shared patterns, and its local patterns are removed.
func (ld *loading) overrideFilenames(patterns []Pattern) ([]Pattern, bool) {
	names := strings.Split(ld.binding(BindOverrideConfigFilenames), ",")
	if ld.binding(BindOverrideConfigFilenames) == "" {
		if ld.settings.override == nil {
			return patterns, true
		}
		names = ld.settings.override
	}
	var out []Pattern
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		p := Pattern{Template: n}
		if err := checkPattern(p); err != nil {
			ld.addError(&OptionError{BaseError: BaseError{Op: ld.c.vars[BindOverrideConfigFilenames], Err: err}})
			return nil, false
		}
		out = append(out, p)
	}
	for _, p := range patterns {
		if p.Variant == VariantEnv || p.Variant == VariantEnvLocal {
			out = append(out, p)
		}
	}
	return out, true
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
	home, homeErr := h.Home()
	var dirs []string
	for d := filepath.Clean(start); !ceilings[d]; {
		dirs = append(dirs, d)
		if c.walkStop == StopAtHome && homeErr == nil && d == filepath.Clean(home) {
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

// loadExplicit loads files as given, at the current position: no variants, any registered decoder, and errors for
// files that are missing, unreadable or of an unknown format. Explicit files are never ignored.
func (ld *loading) loadExplicit(paths []string, scope ScopeName, trust TrustLevel, s *scopeDef) {
	c := ld.c
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
		base := BaseError{Op: "load", Path: abs, Scope: string(scope)}
		resolved, err := c.fsys.EvalSymlinks(abs)
		if err != nil {
			base.Err = err
			if errors.Is(err, fs.ErrNotExist) {
				ld.addError(&NotFoundError{BaseError: base})
			} else {
				ld.addError(&IOError{BaseError: base})
			}
			continue
		}
		format, ok := c.formatFor(strings.TrimPrefix(filepath.Ext(abs), "."))
		if !ok {
			base.Err = fmt.Errorf("no registered decoder handles the extension %q", filepath.Ext(abs))
			ld.addError(&UnsupportedFormatError{BaseError: base})
			continue
		}
		l.Format = format
		root, err := c.fsys.OpenRoot(filepath.Dir(resolved))
		if err != nil {
			base.Err = err
			ld.addError(&IOError{BaseError: base})
			continue
		}
		info, err := root.Stat(filepath.Base(resolved))
		switch {
		case err != nil:
			base.Err = err
		case !info.Mode().IsRegular():
			base.Err = fmt.Errorf("it is %s, not a regular file", describeMode(info.Mode()))
		case info.Size() > c.maxFileSize:
			_ = root.Close()
			ld.addError(tooLarge(l, info.Size(), c.maxFileSize))
			continue
		}
		if base.Err != nil {
			_ = root.Close()
			ld.addError(&IOError{BaseError: base})
			continue
		}
		data, err := readLimited(root, filepath.Base(resolved), c.maxFileSize)
		_ = root.Close()
		if err != nil {
			base.Err = err
			ld.addError(&IOError{BaseError: base})
			continue
		}
		if int64(len(data)) > c.maxFileSize {
			ld.addError(tooLarge(l, int64(len(data)), c.maxFileSize))
			continue
		}
		ld.entries = append(ld.entries, &entry{layer: l, data: data, info: info})
	}
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
	e := &entry{layer: l, data: data, key: key}
	if follow {
		e.info = info
	}
	ld.entries = append(ld.entries, e)
}

// ignore records l as ignored, without a warning, if resolved is under an ignored path.
func (ld *loading) ignore(l Layer, resolved string) bool {
	p, ok := ld.isIgnored(resolved, l.Trust)
	if !ok {
		return false
	}
	l.Status = LayerIgnored
	l.Reason = fmt.Sprintf("it is under the ignored path %s", p)
	ld.entries = append(ld.entries, &entry{layer: l})
	return true
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
			if e.key != nil {
				t, ok, err := subtree(doc.Tree, e.key)
				if err != nil {
					errs = append(errs, &ParseError{
						BaseError: BaseError{Op: "decode", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: err},
						Format:    e.layer.Format,
					})
					continue
				}
				if !ok {
					e.dropped = true
					continue
				}
				e.tree = t
			}
		}
		layers = append(layers, mergeLayer{layer: e.layer, tree: e.tree})
	}
	return layers, errors.Join(errs...)
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

func (ld *loading) found() []Layer {
	var out []Layer
	for _, e := range ld.entries {
		if e.err == nil && !e.dropped {
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
	for _, e := range ld.boot {
		for _, w := range e.layer.Warnings {
			emit(w)
		}
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
