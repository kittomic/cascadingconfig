package cascadingconfig

// WithFileFormat decodes the files m accepts with the first listed format that has a registered decoder. Rules are
// checked before the decoders' extensions, the most recently registered first.
func WithFileFormat(m Matcher, formats ...string) Option {
	_, _ = m, formats // stub
	return optionFunc(func(*config) {})
}

// Extension matches files with any of the extensions, given with or without the dot.
func Extension(exts ...string) Matcher {
	_ = exts // stub
	return stubMatcher{}
}

// FileName matches a file's base name against a glob, in which * matches any run of characters. It may use {name}.
func FileName(glob string) Matcher {
	_ = glob // stub
	return stubMatcher{}
}

// Glob matches a file's whole slash path: * matches within one path segment, and ** across segments.
func Glob(pattern string) Matcher {
	_ = pattern // stub
	return stubMatcher{}
}

// Regexp matches a file's whole slash path against a regular expression, which must match all of it.
func Regexp(expr string) Matcher {
	_ = expr // stub
	return stubMatcher{}
}

// MatchFunc matches the paths f accepts.
func MatchFunc(f func(path string) bool) Matcher {
	_ = f // stub
	return stubMatcher{}
}

type stubMatcher struct{}

func (stubMatcher) matcher() {}
