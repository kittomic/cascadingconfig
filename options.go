package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// Option configures a loader. Options are sealed: only this package creates them, so the set can grow without
// breaking anyone. Other modules provide values that options take, as in WithDecoder(toml.Decoder()).
type Option interface {
	applyOption(c *config)
}

// ScopeKind is how a scope finds its sources, passed to WithScope.
type ScopeKind interface {
	defaultTrust() TrustLevel
	// check reports the kind's own mistakes when WithScope uses it.
	check(c *config)
	// readsFiles reports whether the kind needs a decoder.
	readsFiles() bool
	// discover adds the scope's layers to the load, lowest precedence first.
	discover(ld *loading, s *scopeDef)
}

// ScopeOption sets where a scope goes and how far it is trusted, passed to WithScope.
type ScopeOption interface {
	applyScopeOption(s *scopeSpec)
}

// PolicyOption is a per-scope policy setting, passed to WithDefaultPolicy and WithScopePolicy. Its options arrive in M7.
type PolicyOption interface {
	policyOption()
}

// Matcher matches file paths, for WithFileFormat, and include references, in allow-lists.
type Matcher interface {
	matcher()
	// check reports the matcher's own mistakes, and whether it is usable.
	check(c *config) bool
	// matches reports whether the matcher accepts slashPath, for app name.
	matches(slashPath, name string) bool
}

// WatchOption configures watching, passed to Watch. Its options arrive in M9.
type WatchOption interface {
	watchOption()
}

// Options bundles several options into one, so an app can share a group of them. They run in place, in order.
func Options(opts ...Option) Option {
	opts = slices.Clone(opts)
	return optionFunc(func(c *config) {
		for _, o := range opts {
			c.apply("Options", o)
		}
	})
}

// optionFunc is an Option that runs a function on the configuration.
type optionFunc func(c *config)

func (f optionFunc) applyOption(c *config) { f(c) }

// config is a loader's configuration, built by running its options in order onto a fresh value, then finished by
// finish, which checks what can only be checked once every option has run.
type config struct {
	name    string   // the app name: {name}
	envName string   // {NAME}: the app name in upper case, with every other character replaced by _
	presets []string // the preset's name and its bases, nearest first; empty without a preset

	scopes   []*scopeDef // defined, lowest precedence first
	selected map[ScopeName]bool

	decoders       []Decoder // in registration order; at most one per format
	formats        []string  // WithFormats, as written; nil when not set
	defaultFormats []string  // WithDefaultFormats, as written; nil when not set
	order          []string  // the formats searched, highest precedence first; set by finish
	conflict       FormatConflict

	merge mergeConfig // its rules are built by finish
	rules []mergeRule

	envs        []string // WithEnvs; nil when not set
	configFiles []string // WithConfigFiles; nil when not set

	bindings []bindingDef       // declared, in declaration order
	vars     map[Binding]string // each declared binding's variable; set by finish

	host        host
	fsys        FileSystem
	maxFileSize int64
	logger      *slog.Logger // nil means slog.Default(), looked up at each load
	dirs        PlatformDirs // nil when not set

	startDir string // WithStartDir; empty means the current directory
	walkStop WalkStop
	ceilings []string // WithCeilingPaths
	ignored  []string // WithIgnoredPaths

	fileFormats []fileFormatRule // WithFileFormat, in registration order

	resolution      Resolution
	stopDir         string   // WithStopDir; empty means ~
	markers         []string // WithRootMarker
	configFilesOnly bool     // WithConfigFilesOnly

	bootstrapKeys       map[BootstrapKey]bool
	bootstrapLimits     map[ScopeName][]BootstrapKey
	bootstrapTransforms []func(ctx context.Context, l Layer, t Tree) (Tree, error)

	errs []error
}

// scopeDef is one defined scope.
type scopeDef struct {
	name    ScopeName
	kind    ScopeKind
	trust   TrustLevel
	invalid bool // its kind reported a mistake, so later checks leave it alone

	configRoot     string  // ConfigRoot; empty means the searched directory
	replacedByFile Binding // ReplacedByFile
	disabledBy     Binding // DisabledBy
	explicitFiles  bool    // ReplacedByConfigFiles
	bootstrap      []Pattern
}

// newConfig returns the engine defaults for app name.
func newConfig(name string) *config {
	c := &config{
		name: name, envName: envName(name), selected: map[ScopeName]bool{}, merge: defaultMergeConfig(),
		host: osHost{}, fsys: osFileSystem{}, maxFileSize: defaultMaxFileSize,
	}
	if err := checkName(name); err != nil {
		c.fail("New", "invalid app name %q: %w", name, err)
	}
	return c
}

// apply runs one option; op names where it came from, for a nil option.
func (c *config) apply(op string, o Option) {
	if o == nil {
		c.fail(op, "a nil option")
		return
	}
	o.applyOption(c)
}

// fail records an *OptionError for option op.
func (c *config) fail(op, format string, args ...any) {
	c.errs = append(c.errs, &OptionError{BaseError: BaseError{Op: op, Err: fmt.Errorf(format, args...)}})
}

// finish checks what needs every option to have run: the format order, the merge rules, the bindings, and that some
// scope is selected.
func (c *config) finish() error {
	c.finishFormats()
	if len(c.rules) > 0 {
		rs, err := newRuleSet(c.rules)
		if err != nil {
			c.errs = append(c.errs, Errors(err)...)
		}
		c.merge.rules = rs
	}
	c.finishBindings()
	c.finishBootstrap()
	if c.stopDir != "" && c.walkStop != StopAtHome {
		c.fail("WithStopDir", "a stop dir only applies to StopAtHome walks, as cosmiconfig allows stopDir only with the global strategy")
	}
	if c.walkStop == StopAtMarker && len(c.markers) == 0 {
		c.fail("WithRootMarker", "a StopAtMarker walk needs at least one root marker")
	}
	if len(c.scopeOrder()) == 0 {
		c.fail("New", "no scopes are selected, so nothing would be searched")
	}
	for _, s := range c.scopes {
		if fd, ok := s.kind.(fixedDirKind); ok && !s.invalid && fd.usesPlatformDirs() {
			switch {
			case c.dirs == nil:
				c.fail("FixedDir", "the scope %q uses %s, which needs WithPlatformDirs", s.name, fd.dir)
				s.invalid = true
			case strings.HasPrefix(fd.dir, "{system}") && !c.dirs.hasSystem():
				c.fail("FixedDir", "the scope %q uses {system}, but its platform directories have no system directory", s.name)
				s.invalid = true
			}
		}
		if c.selected[s.name] && !s.invalid && s.kind.readsFiles() && len(c.order) == 0 {
			c.fail("New", "the scope %q reads files, but no format has a registered decoder", s.name)
		}
	}
	return errors.Join(c.errs...)
}

// scopeOrder returns the selected scopes, lowest precedence first.
func (c *config) scopeOrder() []ScopeName {
	var out []ScopeName
	for _, s := range c.scopes {
		if c.selected[s.name] {
			out = append(out, s.name)
		}
	}
	return out
}

// scope returns a defined scope, or nil.
func (c *config) scope(name ScopeName) *scopeDef {
	for _, s := range c.scopes {
		if s.name == name {
			return s
		}
	}
	return nil
}

// scopeTrust returns a defined scope's trust level.
func (c *config) scopeTrust(name ScopeName) (TrustLevel, bool) {
	if s := c.scope(name); s != nil {
		return s.trust, true
	}
	return 0, false
}

// scopeKind returns a defined scope's kind.
func (c *config) scopeKind(name ScopeName) ScopeKind {
	if s := c.scope(name); s != nil {
		return s.kind
	}
	return nil
}

// formatOrder returns the formats that are searched, highest precedence first: the app's WithFormats, or else the
// layout's WithDefaultFormats without the formats that have no decoder, or else every decoder in registration order.
func (c *config) formatOrder() []string {
	return slices.Clone(c.order)
}

// decoder returns the registered decoder for a format, or nil.
func (c *config) decoder(format string) Decoder {
	for _, d := range c.decoders {
		if d.Format() == format {
			return d
		}
	}
	return nil
}

// bindingNames returns each declared binding's variable name.
func (c *config) bindingNames() map[Binding]string {
	out := make(map[Binding]string, len(c.vars))
	for b, v := range c.vars {
		out[b] = v
	}
	return out
}
