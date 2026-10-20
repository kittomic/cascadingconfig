package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// Pattern is one file a scope looks for.
type Pattern struct {
	// Template is a relative file path that may use {name}, {env} and {ext}, and * inside a path segment, and may
	// include subdirectories. {ext} stands for each extension of the searched formats, so one template can match
	// several files: a slot. In a file name, * matches a non-empty name with no dots; in a directory, any name that
	// doesn't start with a dot.
	Template string
	// Variant says which variant the files are. A template has {env} exactly when Variant is VariantEnv or
	// VariantEnvLocal.
	Variant Variant
	// Root says where the files' config root is.
	Root RootMode
	// Key, if set, is a dotted path, which may use {name}, to the table inside the file that holds the config.
	Key string
	// SkipHomeAndRoot keeps a Walk from searching for the pattern in ~ or /, as mise does for .config/miserc.toml.
	// Other kinds ignore it.
	SkipHomeAndRoot bool
}

// RootMode says where a pattern's files have their config root, the directory their relative paths resolve against.
type RootMode int

// Root modes.
const (
	// RootSearchDir is the directory being searched, or the scope's ConfigRoot. It is the default.
	RootSearchDir RootMode = iota
	// RootFileDir is the file's own directory.
	RootFileDir
)

// Walk is a scope kind that searches the start directory and each parent, up to the walk's stop, applying them
// root-most first. Its default trust is TrustProject.
func Walk(patterns ...Pattern) ScopeKind {
	return walkKind{patterns: slices.Clone(patterns)}
}

type walkKind struct {
	patterns []Pattern
}

func (walkKind) defaultTrust() TrustLevel { return TrustProject }
func (walkKind) readsFiles() bool         { return true }

func (k walkKind) check(c *config) {
	checkPatterns(c, "Walk", k.patterns)
}

func (k walkKind) discover(ld *loading, s *scopeDef) {
	patterns, ok := ld.overrideFilenames(k.patterns)
	if !ok {
		return
	}
	dirs, ok := ld.walkDirs(s, false)
	if !ok {
		return
	}
	ld.walking = true
	defer func() { ld.walking = false }()
	for _, dir := range dirs {
		root, ok := ld.root(dir)
		if !ok {
			continue
		}
		configRoot, ok := ld.scopeRoot(s, dir)
		if !ok {
			return
		}
		ld.search(root, dir, configRoot, patterns, s, true)
	}
}

// ExplicitFile is a scope kind that loads the files as given, with any registered decoder. A missing file is a
// *NotFoundError, an unregistered format an *UnsupportedFormatError, and a file that can't be read an *IOError. Its
// default trust is TrustProject.
func ExplicitFile(paths ...string) ScopeKind {
	return explicitKind{paths: slices.Clone(paths)}
}

type explicitKind struct {
	paths []string
}

func (explicitKind) defaultTrust() TrustLevel { return TrustProject }
func (explicitKind) readsFiles() bool         { return true }

func (k explicitKind) check(c *config) {
	if len(k.paths) == 0 {
		c.fail("ExplicitFile", "no paths")
	}
	if slices.Contains(k.paths, "") {
		c.fail("ExplicitFile", "an empty path")
	}
}

func (k explicitKind) discover(ld *loading, s *scopeDef) {
	ld.loadExplicit(k.paths, s.name, s.trust, s)
}

// FixedDir is a scope kind that searches one directory. dir is absolute, or starts with ~, {user} or {system}, and may
// use {name}. Patterns are listed lowest precedence first. Its default trust is TrustProject.
func FixedDir(dir string, patterns ...Pattern) ScopeKind {
	return fixedDirKind{dir: dir, patterns: slices.Clone(patterns)}
}

type fixedDirKind struct {
	dir      string
	patterns []Pattern
}

func (fixedDirKind) defaultTrust() TrustLevel { return TrustProject }
func (fixedDirKind) readsFiles() bool         { return true }

func (k fixedDirKind) usesPlatformDirs() bool {
	return strings.HasPrefix(k.dir, "{user}") || strings.HasPrefix(k.dir, "{system}")
}

func (k fixedDirKind) check(c *config) {
	if err := checkDir(k.dir); err != nil {
		c.fail("FixedDir", "%w", err)
	}
	checkPatterns(c, "FixedDir", k.patterns)
}

func (k fixedDirKind) discover(ld *loading, s *scopeDef) {
	dir, ok := ld.resolveDir(k.dir, s)
	if !ok {
		return
	}
	root, ok := ld.root(dir)
	if !ok {
		return
	}
	configRoot, ok := ld.scopeRoot(s, dir)
	if !ok {
		return
	}
	ld.search(root, dir, configRoot, k.patterns, s, true)
}

// checkDir checks a FixedDir directory template.
func checkDir(dir string) error {
	rest := dir
	switch {
	case dir == "":
		return errors.New("the directory is empty")
	case dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`):
		rest = dir[1:]
	case strings.HasPrefix(dir, "{user}"), strings.HasPrefix(dir, "{system}"):
		rest = dir[strings.IndexByte(dir, '}')+1:]
		if rest != "" && rest[0] != '/' && rest[0] != filepath.Separator {
			return fmt.Errorf("in %q, %s must be followed by a path separator", dir, dir[:len(dir)-len(rest)])
		}
	default:
		if !filepath.IsAbs(expand(dir, "x", "", "")) {
			return fmt.Errorf("the directory %q must be absolute, or start with ~, {user} or {system}", dir)
		}
	}
	for _, p := range placeholders(rest) {
		if p != "{name}" {
			return fmt.Errorf("unknown placeholder %s in the directory %q", p, dir)
		}
	}
	return nil
}

// checkPatterns checks a scope kind's patterns.
func checkPatterns(c *config, op string, patterns []Pattern) {
	if len(patterns) == 0 {
		c.fail(op, "no patterns")
	}
	for _, p := range patterns {
		if err := checkPattern(p); err != nil {
			c.fail(op, "%w", err)
		}
	}
}

// checkPattern checks one pattern.
func checkPattern(p Pattern) error {
	tpl := p.Template
	if tpl == "" {
		return errors.New("an empty pattern template")
	}
	if p.Variant < VariantShared || p.Variant > VariantEnvLocal {
		return fmt.Errorf("the template %q has an unknown variant %d", tpl, int(p.Variant))
	}
	if p.Root != RootSearchDir && p.Root != RootFileDir {
		return fmt.Errorf("the template %q has an unknown root mode %d", tpl, int(p.Root))
	}
	hasEnv := false
	for _, ph := range placeholders(tpl) {
		switch ph {
		case "{name}", "{ext}":
		case "{env}":
			hasEnv = true
		default:
			return fmt.Errorf("unknown placeholder %s in the template %q", ph, tpl)
		}
	}
	envVariant := p.Variant == VariantEnv || p.Variant == VariantEnvLocal
	switch {
	case envVariant && !hasEnv:
		return fmt.Errorf("the template %q is an env variant, so it needs {env}", tpl)
	case !envVariant && hasEnv:
		return fmt.Errorf("the template %q has {env}, so it must be an env or env-local variant", tpl)
	case strings.Contains(path.Dir(tpl), "{ext}"):
		return fmt.Errorf("in the template %q, {ext} can only be in the file name", tpl)
	case strings.Contains(tpl, "**"):
		return fmt.Errorf("the template %q uses **, but * only matches within one path segment", tpl)
	case !filepath.IsLocal(filepath.FromSlash(expand(tpl, "x", "x", "x"))):
		return fmt.Errorf("the template %q must be a relative path inside the directory", tpl)
	}
	if p.Key != "" {
		if _, err := parsePath(expand(p.Key, "x", "", "")); err != nil {
			return fmt.Errorf("the key %q of the template %q: %w", p.Key, tpl, err)
		}
	}
	return nil
}

// placeholders returns every {…} in s, and an unclosed { with the rest of s.
func placeholders(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return append(out, s[i:])
		}
		out = append(out, s[i:i+j+1])
		s = s[i+j+1:]
	}
}

// expand fills in {name}, {env} and {ext} in one pass, so text that a value brings in is never expanded again.
func expand(tpl, name, env, ext string) string {
	var b strings.Builder
	for i := 0; i < len(tpl); {
		switch {
		case strings.HasPrefix(tpl[i:], "{name}"):
			b.WriteString(name)
			i += len("{name}")
		case strings.HasPrefix(tpl[i:], "{env}"):
			b.WriteString(env)
			i += len("{env}")
		case strings.HasPrefix(tpl[i:], "{ext}"):
			b.WriteString(ext)
			i += len("{ext}")
		default:
			b.WriteByte(tpl[i])
			i++
		}
	}
	return b.String()
}

// EmbeddedFS is a scope kind that searches the root of fsys, such as an embed.FS compiled into the app. Its default
// trust is TrustProject.
func EmbeddedFS(fsys fs.FS, patterns ...Pattern) ScopeKind {
	return embeddedKind{fsys: fsys, patterns: slices.Clone(patterns)}
}

type embeddedKind struct {
	fsys     fs.FS
	patterns []Pattern
}

func (embeddedKind) defaultTrust() TrustLevel { return TrustProject }
func (embeddedKind) readsFiles() bool         { return true }

func (k embeddedKind) check(c *config) {
	if k.fsys == nil {
		c.fail("EmbeddedFS", "a nil fs.FS")
	}
	checkPatterns(c, "EmbeddedFS", k.patterns)
}

func (k embeddedKind) discover(ld *loading, s *scopeDef) {
	ld.search(mountRoot{fsys: k.fsys, sub: "."}, "", s.configRoot, k.patterns, s, false)
}

// ValuesFunc is a scope kind for a tree computed on every load, so a reload can re-read it. An error from f, or a
// value outside the canonical types, is a *BaseError with CodeInternal. Its default trust is TrustUser.
func ValuesFunc(f func(ctx context.Context) (Tree, error)) ScopeKind {
	return valuesFuncKind{f: f}
}

type valuesFuncKind struct {
	f func(ctx context.Context) (Tree, error)
}

func (valuesFuncKind) defaultTrust() TrustLevel { return TrustUser }
func (valuesFuncKind) readsFiles() bool         { return false }

func (k valuesFuncKind) check(c *config) {
	if k.f == nil {
		c.fail("ValuesFunc", "a nil function")
	}
}

func (k valuesFuncKind) discover(ld *loading, s *scopeDef) {
	t, err := k.f(ld.ctx)
	if err != nil {
		ld.addError(NewError(CodeInternal, "ValuesFunc", "", fmt.Errorf("the scope %q: %w", s.name, err)))
		return
	}
	if err := checkCanonical(t, nil); err != nil {
		ld.addError(NewError(CodeInternal, "ValuesFunc", "", fmt.Errorf("the scope %q: the function returned %w", s.name, err)))
		return
	}
	ld.addValues(s, tree.CopyMap(t))
}

// EnvVarsOption configures EnvVars.
type EnvVarsOption interface {
	applyEnvVarsOption(k *envVarsKind)
}

type envVarsOptionFunc func(k *envVarsKind)

func (f envVarsOptionFunc) applyEnvVarsOption(k *envVarsKind) { f(k) }

// EnvVars is a scope kind for single values from environment variables. The prefix is stripped, the name is
// lowercased, and __ separates nesting levels, so MYAPP_SERVER__PORT gives server.port. Every value is a string unless
// MapEnv maps it. Variables bound to discovery settings are never read. Its default trust is TrustUser.
func EnvVars(prefix string, opts ...EnvVarsOption) ScopeKind {
	k := envVarsKind{prefix: prefix}
	for _, o := range opts {
		if o != nil {
			o.applyEnvVarsOption(&k)
		}
	}
	return k
}

// MapEnv replaces EnvVars' mapping. f gets each variable's full name and value, and returns the key path and value, or
// false to skip the variable.
func MapEnv(f func(name, value string) (Path, any, bool)) EnvVarsOption {
	return envVarsOptionFunc(func(k *envVarsKind) {
		k.mapper, k.mapperSet = f, true
	})
}

type envVarsKind struct {
	prefix    string
	mapper    func(name, value string) (Path, any, bool)
	mapperSet bool
}

func (envVarsKind) defaultTrust() TrustLevel { return TrustUser }
func (envVarsKind) readsFiles() bool         { return false }

func (k envVarsKind) check(c *config) {
	if k.prefix == "" {
		c.fail("EnvVars", "the prefix is empty, which would read every environment variable")
	}
	if k.mapperSet && k.mapper == nil {
		c.fail("MapEnv", "a nil function")
	}
}

func (k envVarsKind) discover(ld *loading, s *scopeDef) {
	ld.addEnvVars(s, k)
}
