package cascadingconfig

// WithMergeRule sets the strategy for a path, in either path form; * matches one key. A rule for a path that already
// has one replaces it. Ambiguous rules are an *OptionError.
func WithMergeRule[P PathSpec](path P, strategy MergeStrategy) Option {
	_, _ = path, strategy // stub
	return optionFunc(func(*config) {})
}

// WithMergeFunc sets a custom merge for a path, in either path form.
func WithMergeFunc[P PathSpec](path P, f MergeFunc) Option {
	_, _ = path, f // stub
	return optionFunc(func(*config) {})
}

// WithArrayMerge sets how arrays combine where no rule applies: Replace (the default), Append or Prepend.
func WithArrayMerge(strategy MergeStrategy) Option {
	_ = strategy // stub
	return optionFunc(func(*config) {})
}

// WithUnsetKey renames the reserved unset key, which is _unset by default. It shares one setting with WithoutUnset.
func WithUnsetKey(key string) Option {
	_ = key // stub
	return optionFunc(func(*config) {})
}

// WithoutUnset turns unsetting off, so the unset key is plain data.
func WithoutUnset() Option {
	return optionFunc(func(*config) {})
}
