package cascadingconfig

// Option configures a loader. Options are sealed: only this package creates them, so the set can grow without
// breaking anyone. Other modules provide values that options take, as in WithDecoder(toml.Decoder()).
type Option interface {
	applyOption(c *config)
}

// ScopeKind is how a scope finds its sources, passed to WithScope.
type ScopeKind interface {
	scopeKind() ScopeKind
}

// ScopeOption sets where a scope goes and how far it is trusted, passed to WithScope.
type ScopeOption interface {
	applyScopeOption(s *scopeDef)
}

// PolicyOption is a per-scope policy setting, passed to WithDefaultPolicy and WithScopePolicy. Its options arrive in M7.
type PolicyOption interface {
	policyOption()
}

// Matcher matches include references in allow-lists. Its constructors arrive in M7.
type Matcher interface {
	matcher()
}

// WatchOption configures watching, passed to Watch. Its options arrive in M9.
type WatchOption interface {
	watchOption()
}

// Options bundles several options into one, so an app can share a group of them.
func Options(opts ...Option) Option {
	_ = opts // stub
	return optionFunc(func(*config) {})
}

// optionFunc is an Option that runs a function on the configuration.
type optionFunc func(c *config)

func (f optionFunc) applyOption(c *config) { f(c) }

// config is a loader's configuration, built by running its options in order onto a fresh value.
type config struct {
	name    string   // the app name: {name}
	envName string   // {NAME}: the app name in upper case, with every other character replaced by _
	presets []string // the preset's name and its bases, nearest first; empty without a preset

	scopes []*scopeDef // defined, lowest precedence first

	conflict    FormatConflict
	merge       mergeConfig
	envs        []string // WithEnvs; nil when not set
	configFiles []string // WithConfigFiles; nil when not set
}

// scopeDef is one defined scope.
type scopeDef struct {
	name  ScopeName
	kind  ScopeKind
	trust TrustLevel
}

// scopeOrder returns the selected scopes, lowest precedence first.
func (c *config) scopeOrder() []ScopeName {
	return nil
}

// scopeTrust returns a defined scope's trust level.
func (c *config) scopeTrust(name ScopeName) (TrustLevel, bool) {
	for _, s := range c.scopes {
		if s.name == name {
			return s.trust, true
		}
	}
	return 0, false
}

// scopeKind returns a defined scope's kind.
func (c *config) scopeKind(name ScopeName) ScopeKind {
	for _, s := range c.scopes {
		if s.name == name {
			return s.kind
		}
	}
	return nil
}

// formatOrder returns the formats that are searched, highest precedence first: the app's WithFormats, or else the
// layout's WithDefaultFormats without the formats that have no decoder, or else every decoder in registration order.
func (c *config) formatOrder() []string {
	return nil
}

// decoder returns the registered decoder for a format, or nil.
func (c *config) decoder(format string) Decoder {
	_ = format // stub
	return nil
}

// bindingNames returns each declared binding's variable name.
func (c *config) bindingNames() map[Binding]string {
	return nil
}
