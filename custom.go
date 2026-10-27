package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// Scope discovers a custom scope's sources, for config no built-in scope kind can find. It supplies only discovery: the
// scope's name, placement, trust and selection come from WithScope, with Custom(s) as its kind. Discover returns the
// sources, lowest precedence first, and can be called from several goroutines at once.
type Scope interface {
	Discover(ctx context.Context, d Discovery) ([]Source, error)
}

// Watchable is optionally implemented by a Scope that can report what to watch (DL-239). The directories WatchDirs
// returns are watched with no name filter, so any event in them reloads; that's how new sources appear. It's called
// after Discover on every watched load and reload, never by a plain Load.
type Watchable interface {
	WatchDirs(ctx context.Context, d Discovery) ([]string, error)
}

// Discovery is what the loader passes to a Scope (DL-237). Everything comes through the loader's host, so cctest's
// fakes reach custom scopes too.
type Discovery struct {
	Name         string                          // the app name
	Scope        ScopeName                       // the scope being discovered
	StartDir     string                          // WithStartDir, or the current directory
	Home         string                          // "" if the host has none
	Envs         []string                        // the selected environments, lowest precedence first
	PlatformEnvs []string                        // the platform environments in use, lowest precedence first
	Formats      []string                        // the scope's searched formats, highest precedence first
	FS           FileSystem                      // the loader's filesystem
	LookupEnv    func(key string) (string, bool) // the host's env vars
	Logger       *slog.Logger
}

// Source is one candidate a Scope found (DL-238): exactly one of Path and Tree.
type Source struct {
	// Path is an absolute path, read through the FileSystem like a searched file, so symlinks, ignored paths, dedup,
	// permission checks, the trust store and includes apply. A missing one is a *NotFoundError.
	Path string
	// Tree is an already-decoded tree, loaded like a Values scope's.
	Tree       Tree
	Variant    Variant
	Env        string
	Format     string // empty: from Path's extension; otherwise a registered format
	ConfigRoot string // empty: the file's own directory
}

// Custom is a scope kind for an app's own discovery (DL-236). Its default trust is TrustProject, and it needs a
// registered decoder. A nil s is an *OptionError.
func Custom(s Scope) ScopeKind {
	return customKind{s: s}
}

type customKind struct {
	s Scope
}

func (customKind) defaultTrust() TrustLevel { return TrustProject }
func (customKind) readsFiles() bool         { return true }

func (k customKind) check(c *config) {
	if k.s == nil {
		c.fail("Custom", "a nil Scope")
	}
}

// discover asks the scope for its sources, checks them all, then loads them in order: Path sources like files named
// directly, and Tree sources like a Values scope (DL-238). On a watched load, it also records the scope's WatchDirs
// (DL-239).
func (k customKind) discover(ld *loading, s *scopeDef) {
	c := ld.c
	d := ld.discovery(s)
	srcs, err := k.s.Discover(ld.ctx, d)
	if err != nil {
		if CodeOf(err) == 0 {
			err = NewError(CodeInternal, "Custom", "", fmt.Errorf("the scope %q: %w", s.name, err))
		}
		ld.addError(err)
		return
	}
	for i, src := range srcs {
		if err := c.checkSource(src); err != nil {
			ld.addError(NewError(CodeInternal, "Custom", "", fmt.Errorf("the scope %q returned a broken source (number %d): %w", s.name, i+1, err)))
			return
		}
	}
	for _, src := range srcs {
		l := Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded, Variant: src.Variant, Env: src.Env}
		if src.Tree != nil {
			ld.addValuesLayer(l, tree.CopyMap(src.Tree))
			continue
		}
		l.Path = filepath.Clean(src.Path)
		l.ConfigRoot = filepath.Dir(l.Path)
		if src.ConfigRoot != "" {
			l.ConfigRoot = filepath.Clean(src.ConfigRoot)
		}
		ld.readNamed(l, src.Format, false)
	}
	if w, ok := k.s.(Watchable); ok && ld.rec != nil {
		ld.watchDirs(w, d, s)
	}
}

// discovery is what a custom scope sees, all through the loader's host (DL-237).
func (ld *loading) discovery(s *scopeDef) Discovery {
	c, h := ld.c, ld.c.host
	d := Discovery{
		Name: c.name, Scope: s.name, Envs: slices.Clone(ld.selectedEnvs), PlatformEnvs: slices.Clone(ld.platformEnvs),
		FS: c.fsys, LookupEnv: h.LookupEnv, Logger: c.log(),
	}
	if home, err := h.Home(); err == nil {
		d.Home = home
	}
	if cwd, err := h.Cwd(); err == nil {
		d.StartDir = cwd
	}
	if c.startDir != "" {
		if dir, ok := ld.hostPath(c.startDir, s); ok {
			d.StartDir = dir
		}
	}
	for _, dec := range ld.order {
		d.Formats = append(d.Formats, dec.Format())
	}
	return d
}

// checkSource says what's wrong with a source, if anything: it needs exactly one of an absolute Path and a Tree, a
// registered Format, and canonical values.
func (c *config) checkSource(src Source) error {
	switch {
	case src.Path != "" && src.Tree != nil:
		return errors.New("it has both a Path and a Tree")
	case src.Path == "" && src.Tree == nil:
		return errors.New("it has neither a Path nor a Tree")
	case src.Tree != nil:
		if err := checkCanonical(src.Tree, nil); err != nil {
			return fmt.Errorf("its tree has %w", err)
		}
		return nil
	case !filepath.IsAbs(src.Path):
		return fmt.Errorf("its Path %q isn't absolute", src.Path)
	case src.Format != "" && !slices.ContainsFunc(c.decoders, func(d Decoder) bool { return d.Format() == src.Format }):
		return fmt.Errorf("its Format %q has no registered decoder", src.Format)
	}
	return nil
}

// watchDirs records the directories a Watchable scope asks to be watched, with no name filter. A relative directory,
// or an error, is a WarnWatchFailed warning (DL-239).
func (ld *loading) watchDirs(w Watchable, d Discovery, s *scopeDef) {
	warn := func(msg string) {
		ld.warnings = append(ld.warnings, Warning{Code: WarnWatchFailed, Message: fmt.Sprintf("the scope %q: %s", s.name, msg)})
	}
	dirs, err := w.WatchDirs(ld.ctx, d)
	if err != nil {
		warn(fmt.Sprintf("WatchDirs failed, so only its files are watched: %v", err))
		return
	}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			warn(fmt.Sprintf("WatchDirs returned %q, which isn't absolute, so it isn't watched", dir))
			continue
		}
		ld.rec.targets = append(ld.rec.targets, watchTarget{base: filepath.Clean(dir), segs: []nameMatcher{{any: true}}})
	}
}
