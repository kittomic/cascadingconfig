package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
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

	if err := ld.discoverScopes(); err != nil {
		return nil, err
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

// discoverScopes finds each selected scope's layers, in the scopes' order, putting explicit files in their place: the
// scope that takes them, the highest of the scopes they replace, or the top.
func (ld *loading) discoverScopes() error {
	c, ctx := ld.c, ld.ctx
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
			return contextError(err)
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
	return nil
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
