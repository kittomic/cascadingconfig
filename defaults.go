package cascadingconfig

// ScopeDefaults is the scope WithDefaults defines.
const ScopeDefaults ScopeName = "defaults"

// WithDefaults defines the defaults scope from v: a struct, a pointer to one, or a map with string keys. It is
// converted once, at New, with the decoding struct tag: only non-zero fields are included, and values become canonical,
// so a time.Duration becomes "5s", a time.Time its RFC 3339 text, and an encoding.TextMarshaler its text.
//
// The scope is always the lowest and always selected, with TrustSystem; WithoutScopes(ScopeDefaults) removes it.
// Config can clear a default with null or the unset key. A second WithDefaults replaces the first. A value that can't
// be converted is an *OptionError naming the field.
func WithDefaults(v any) Option {
	_ = v // stub
	return optionFunc(func(*config) {})
}
