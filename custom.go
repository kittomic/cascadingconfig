package cascadingconfig

import (
	"context"
	"log/slog"
)

// Scope is the escape hatch for custom discovery (design.md: Custom scopes). Discover returns the scope's sources,
// lowest precedence first. It can be called from several goroutines at once.
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

func (k customKind) check(*config) { _ = k.s } // stub

func (k customKind) discover(*loading, *scopeDef) { _ = k.s } // stub
