package cascadingconfig

// WithScopes replaces the selected scopes with exactly names. Their precedence stays as defined, whatever order they're
// listed in. An undefined scope, a scope listed twice, or no scopes at all is an *OptionError.
func WithScopes(names ...ScopeName) Option {
	_ = names // stub
	return optionFunc(func(*config) {})
}

// WithoutScopes removes names from the selected scopes. Removing a defined scope that isn't selected does nothing; an
// undefined scope is an *OptionError.
func WithoutScopes(names ...ScopeName) Option {
	_ = names // stub
	return optionFunc(func(*config) {})
}

// WithScope defines a scope. Defining it doesn't select it. Without Before or After, it goes above every scope defined
// so far. A name already in use is an *OptionError.
func WithScope(name ScopeName, kind ScopeKind, opts ...ScopeOption) Option {
	_, _, _ = name, kind, opts // stub
	return optionFunc(func(*config) {})
}

// Before places a scope directly below scope, which must already be defined.
func Before(scope ScopeName) ScopeOption {
	_ = scope // stub
	return scopeOptionFunc(func(*scopeDef) {})
}

// After places a scope directly above scope, which must already be defined.
func After(scope ScopeName) ScopeOption {
	_ = scope // stub
	return scopeOptionFunc(func(*scopeDef) {})
}

// Trust sets a scope's trust level, overriding its kind's default.
func Trust(level TrustLevel) ScopeOption {
	_ = level // stub
	return scopeOptionFunc(func(*scopeDef) {})
}

type scopeOptionFunc func(s *scopeDef)

func (f scopeOptionFunc) applyScopeOption(s *scopeDef) { f(s) }

// Values is a scope kind for a fixed tree, such as the app's parsed flags. The tree is copied when Values is called,
// and must hold only canonical values; anything else is an *OptionError at New naming the key. Its default trust is
// TrustUser.
func Values(tree Tree) ScopeKind {
	_ = tree // stub
	return valuesKind{}
}

// valuesKind is the Values scope kind.
type valuesKind struct {
	tree Tree
}

func (k valuesKind) scopeKind() ScopeKind { return k }
