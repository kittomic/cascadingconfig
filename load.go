package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Load discovers, reads, decodes and merges every selected scope, and returns the result. Every problem found is
// returned, joined.
//
// A failed load still returns a non-nil *Result, whose report shows what was found and what broke, and whose Tree is
// nil.
func (l *Loader) Load(ctx context.Context) (*Result, error) {
	return l.load(ctx, nil)
}

// load is Load, recording what a watcher needs in rec when it isn't nil.
func (l *Loader) load(ctx context.Context, rec *watchRecord) (*Result, error) {
	c := l.cfg
	ld := &loading{
		ctx: ctx, c: c, roots: map[string]Root{}, dirs: map[string][]fs.DirEntry{}, skipped: map[ScopeName]string{},
		rec: rec,
	}
	ld.settings.mergeSearchPlaces = true
	defer ld.closeRoots()
	start := c.now()
	res, err := ld.run()
	ld.log()
	if err != nil {
		res = &Result{err: err}
	}
	res.cfg, res.report = c, ld.report()
	ld.trace(res.report, c.now().Sub(start))
	return res, err
}

// run runs the load's stages: settings, then discovery and reading, then merging, then the requirements. A stage
// reports every problem it finds, and a failed stage stops the load.
func (ld *loading) run() (*Result, error) {
	c, ctx := ld.c, ld.ctx
	if err := ld.formatOrder(); err != nil {
		return nil, err
	}
	if err := ld.readBootstrap(); err != nil {
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
	// Under WithConfigFilesOnly, explicit files replace every scope that searches for files, at the place of the
	// highest of them; value scopes keep their places (DL-247).
	filesOnly, last := explicit != nil && c.configFilesOnly, -1
	if filesOnly {
		for i, s := range c.scopes {
			if s.kind.readsFiles() {
				last = i
			}
		}
	}
	for i, s := range c.scopes {
		if err := ctx.Err(); err != nil {
			return nil, contextError(err)
		}
		ld.order = ld.orderFor(s)
		if filesOnly && s.kind.readsFiles() {
			if c.selected[s.name] {
				ld.skipped[s.name] = "replaced by explicit files"
			}
			if i == last {
				ld.loadExplicit(explicit, "", TrustProject, nil)
				placed = true
			}
			continue
		}
		switch {
		case s.explicitFiles && explicit != nil:
			ld.skipped[s.name] = "replaced by explicit files"
			ld.loadExplicit(explicit, s.name, TrustProject, s)
			placed = true
		case !c.selected[s.name]:
		case s.disabledBy != "" && ld.binding(s.disabledBy) != "":
			ld.skipped[s.name] = "disabled by " + c.vars[s.disabledBy]
		case s.replacedByFile != "" && ld.binding(s.replacedByFile) != "":
			ld.skipped[s.name] = "replaced by the file " + c.vars[s.replacedByFile] + " names"
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
	layers, err := ld.process()
	if err != nil {
		return nil, err
	}
	res, err := merge(c.merge, layers)
	if err != nil {
		return nil, err
	}
	if len(c.resultTransforms) > 0 {
		if res.tree, err = ld.transformResult(res.tree); err != nil {
			return nil, err
		}
	}
	if err := ld.checkRequirements(); err != nil {
		return nil, err
	}
	res.found = ld.found()
	res.warnings = ld.warnings
	for _, e := range ld.boot {
		res.bootstrap = append(res.bootstrap, e.layer)
	}
	res.envVars = make([]map[string]string, len(layers))
	for i, l := range layers {
		res.envVars[i] = l.vars
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
	booting        bool     // bootstrap files are being searched, so no probes are recorded
	settings       bootSettings

	cache       *os.Root // the include cache, once opened
	cacheOpened bool
	cacheWarned bool

	selectedEnvs []string             // the selected environments, lowest precedence first, for the report
	platformEnvs []string             // the platform environments in use, lowest precedence first
	skipped      map[ScopeName]string // why a selected scope wasn't searched
	probes       []Probe              // with WithProbes

	rec *watchRecord // what a watched load records; nil otherwise
}

// entry is one layer the load considered, or a problem that fails it.
type entry struct {
	layer Layer
	data  []byte      // a file's contents
	tree  Tree        // a value scope's tree
	info  fs.FileInfo // a file's identity in the loader's FileSystem, for dedup; nil for EmbeddedFS
	real  string      // a file's path with symlinks resolved, for permission and trust checks
	// dirInfo is the containing directory of the file real names, for permission checks.
	dirInfo fs.FileInfo
	key     Path // the table inside the file that holds the config, if the pattern has a Key
	err     error
	vars    map[string]string // an EnvVars layer's variable for each key path

	dropped  bool // the pattern's key wasn't in the file, so it counts as not found
	file     bool // read from the loader's FileSystem, not an fs.FS
	value    bool // a value scope's tree, not a file
	explicit bool // an explicit config file
	included bool // an included file, already checked against its root file's policy

	remote *remoteRef // a remote include's reference
}

func (ld *loading) addError(err error) {
	ld.entries = append(ld.entries, &entry{err: err})
}

// skip records a layer that won't load, with a warning.
func (ld *loading) skip(l Layer, status LayerStatus, code WarningCode, reason string) {
	l.Status, l.Reason = status, reason
	l.warn(code, "%s: %s", l.Path, reason)
	ld.entries = append(ld.entries, &entry{layer: l})
}

// failLayer records a layer that breaks the load, with a reason built from err's fields, never its message, which
// could quote the file.
func (ld *loading) failLayer(l Layer, err error) {
	l.Status, l.Reason = LayerFailed, failReason(err)
	ld.entries = append(ld.entries, &entry{layer: l, err: err})
}

// failReason describes err for a failed layer's Reason.
func failReason(err error) string {
	var pe *ParseError
	switch {
	case errors.As(err, &pe):
		if pe.Line > 0 {
			return fmt.Sprintf("parse error at line %d, column %d", pe.Line, pe.Column)
		}
		return "parse error"
	case errors.Is(err, ErrTooLarge):
		return "the file is over the size limit"
	case errors.Is(err, ErrNotFound):
		return "the file doesn't exist"
	case errors.Is(err, ErrUnsupportedFormat):
		return "no decoder for its format"
	case errors.Is(err, ErrIO):
		return "the file can't be read"
	case errors.Is(err, ErrTransform):
		return "a transform failed"
	case errors.Is(err, ErrIncludeCycle):
		return "an include cycle"
	case errors.Is(err, ErrUnsupportedScheme):
		return "no resolver for its scheme"
	case errors.Is(err, ErrInvalidReference):
		return "an invalid reference"
	case errors.Is(err, ErrPolicy):
		return "rejected by a policy"
	default:
		return fmt.Sprintf("failed with code %d", CodeOf(err))
	}
}

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
	ld.watchSearch(dir, patterns)
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
		found := false
		for _, sub := range subs {
			entries := ld.readDir(root, dir, sub)
			if entries == nil {
				continue
			}
			candidates := ld.candidates(last, x.env, entries, dir, sub)
			found = found || len(candidates) > 0
			for _, l := range candidates {
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
		if !found && ld.c.probes && !ld.booting {
			ld.probes = append(ld.probes, Probe{Scope: s.name, Dir: dir, Pattern: strings.ReplaceAll(x.p.Template, "{env}", x.env)})
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

// readDir lists sub, a slash path in root, once per load. A missing directory gives nil; any other failure is a
// warning.
func (ld *loading) readDir(root Root, dir, sub string) []fs.DirEntry {
	key := dir + "\x00" + sub
	if entries, ok := ld.dirs[key]; ok {
		return entries
	}
	entries, err := root.ReadDir(sub)
	if err != nil {
		where := ld.join(dir, sub)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case underNonDirIn(root, sub):
			ld.notADir(where)
		default:
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
				if f, ok := ld.searchedFormat(dir, sub, e, format); ok {
					slots[""] = append(slots[""], match{e, f})
				}
			}
			return
		}
		re := globPattern(e, true)
		for _, en := range entries {
			if m := re.FindStringSubmatch(en.Name()); m != nil {
				if f, ok := ld.searchedFormat(dir, sub, en.Name(), format); ok {
					stem := strings.Join(m[1:], "\x00")
					slots[stem] = append(slots[stem], match{en.Name(), f})
				}
			}
		}
	}
	if !slices.Contains(placeholders(last), "{ext}") {
		e := expand(last, name, env, "")
		if strings.Contains(e, "*") {
			re := globPattern(e, true)
			for _, en := range entries {
				if re.MatchString(en.Name()) {
					if format, ok := ld.searchedFormat(dir, sub, en.Name(), ""); ok {
						slots[en.Name()] = append(slots[en.Name()], match{en.Name(), format})
					}
				}
			}
		} else {
			add(e, "")
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

// searchedFormat returns the format of a candidate named file in sub of dir, from the file-format rules or else
// fallback (the decoder whose extension {ext} stood for) or the file's extension. It must be one the scope searches.
func (ld *loading) searchedFormat(dir, sub, file, fallback string) (string, bool) {
	p := filepath.ToSlash(ld.join(dir, path.Join(sub, file)))
	format, ok := ld.c.fileFormat(p)
	if fallback != "" && !ld.c.ruleMatches(p) {
		format, ok = fallback, true
	}
	if !ok {
		return "", false
	}
	for _, d := range ld.order {
		if d.Format() == format {
			return format, true
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
		selected, ld.envSource = ld.settings.env, ld.settings.envFrom
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
			ld.platformEnvs = append(ld.platformEnvs, p)
		}
	}
	ld.envs = append(ld.envs, envs...)
	ld.selectedEnvs = envs
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
			p.from.layer.warn(WarnBootstrapKeyDropped, "%s: the ignored path %s points into a more trusted scope, so it was dropped for %s",
				p.from.layer.Path, p.path, resolved)
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

// searchPlaces applies a bootstrap file's searchPlaces to a walk's patterns: added at the highest precedence, or,
// with mergeSearchPlaces false, replacing them.
func (ld *loading) searchPlaces(patterns []Pattern) []Pattern {
	if ld.settings.searchPlaces == nil {
		return patterns
	}
	var places []Pattern
	for _, n := range ld.settings.searchPlaces {
		places = append(places, Pattern{Template: n})
	}
	if !ld.settings.mergeSearchPlaces {
		return places
	}
	return append(slices.Clone(patterns), places...)
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

// dedup loads each file reached more than once at its highest position, with the scope and trust of its most trusted
// route. Each lower route stays in the report as LayerDuplicate.
func (ld *loading) dedup() {
	for i, e := range ld.entries {
		if e.info == nil {
			continue
		}
		for _, prev := range ld.entries[:i] {
			if prev.info == nil || !ld.c.fsys.SameFile(prev.info, e.info) {
				continue
			}
			via := e.layer.Scope
			if prev.layer.Trust > e.layer.Trust {
				e.layer.Scope, e.layer.Trust = prev.layer.Scope, prev.layer.Trust
			}
			prev.layer.Status = LayerDuplicate
			prev.layer.Reason = fmt.Sprintf("loaded at a higher position through the %s scope", via)
			prev.info = nil
			break
		}
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

func (ld *loading) found() []Layer {
	var out []Layer
	for _, e := range ld.entries {
		if e.err == nil && !e.dropped && e.layer.Status != LayerDuplicate {
			out = append(out, e.layer)
		}
	}
	return out
}

// log sends every warning to the logger.
func (ld *loading) log() {
	logger := ld.c.log()
	emit := func(w Warning) {
		logger.LogAttrs(ld.ctx, levelOf(w.Code), w.Message, slog.String("code", string(w.Code)), slog.String("path", w.Path))
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
	ld.addValuesLayer(Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}, t)
}

// addValuesLayer adds a value layer l with tree t, without the unset key. A tree with no keys is LayerEmpty (DL-264).
func (ld *loading) addValuesLayer(l Layer, t Tree) {
	s := l.Scope
	if key := ld.c.merge.unsetKey; key != "" && dropKey(t, key) {
		l.warn(WarnReservedKey, "the scope %q can't remove keys, so its %s keys were ignored", s, key)
	}
	if len(t) == 0 {
		l.Status = LayerEmpty
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t, value: true})
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
	type setBy struct {
		key  Path
		name string
	}
	var sets []setBy // the variable behind each key, without those a later variable replaced
	l := Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}
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
				l.warn(WarnEnvVarSkipped, "%s: the mapping gave an empty key, so it was skipped", v.name)
				continue
			}
			if err := checkCanonical(value, key); err != nil {
				ld.addError(NewError(CodeInternal, "MapEnv", "", fmt.Errorf("the variable %s: the function returned %w", v.name, err)))
				return
			}
		} else {
			key = strings.Split(v.name[len(k.prefix):], "__")
			if slices.Contains(key, "") {
				l.warn(WarnEnvVarSkipped, "%s: the name has an empty segment, so it was skipped", v.name)
				continue
			}
			for i := range key {
				key[i] = strings.ToLower(key[i])
			}
		}
		if setPath(t, key, value) {
			l.warn(WarnEnvVarClash, "%s clashes with an earlier variable at %s; the later name wins", v.name, key)
		}
		sets = slices.DeleteFunc(sets, func(b setBy) bool { return hasPrefix(b.key, key) || hasPrefix(key, b.key) })
		sets = append(sets, setBy{slices.Clone(key), v.name})
	}
	if unset := ld.c.merge.unsetKey; unset != "" && dropKey(t, unset) {
		l.warn(WarnReservedKey, "the scope %q can't remove keys, so its %s keys were ignored", s.name, unset)
	}
	by := make(map[string]string, len(sets))
	for _, b := range sets {
		by[b.key.String()] = b.name
	}
	if len(t) == 0 {
		l.Status = LayerEmpty // no variable supplied a key (DL-264)
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t, value: true, vars: by})
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
