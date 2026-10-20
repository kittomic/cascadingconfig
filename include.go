package cascadingconfig

// IncludeNesting says whether an included file can include others (DL-178).
type IncludeNesting int

const (
	// IncludeFlat forbids nesting: an include key in an included file is a *ParseError, as in mise.
	IncludeFlat IncludeNesting = iota
	// IncludeNested lets included files include, with cycle detection.
	IncludeNested
)

// WithIncludeKey sets the include key, read at the top level of each file that can include (DL-178, DL-187). An empty
// key, or one equal to the unset key, is an *OptionError.
func WithIncludeKey(key string, n IncludeNesting) Option {
	_, _ = key, n // stub
	return optionFunc(func(*config) {})
}

// WithoutIncludeKey turns includes off, so the key a layout set is ordinary data again.
func WithoutIncludeKey() Option {
	return optionFunc(func(*config) {}) // stub
}

// WithoutIncludeResolver removes the resolver for scheme. Removing the built-in "file" resolver while an include key is
// set is an *OptionError (DL-177); a policy whose AllowResolvers leaves out "file" forbids local includes instead.
func WithoutIncludeResolver(scheme string) Option {
	_ = scheme // stub
	return optionFunc(func(*config) {})
}

// IncludeRef is an include reference, as a resolver and a MatchRef matcher see it.
type IncludeRef struct {
	Raw      string // as written in the config file
	Scheme   string // "file" until M8
	Location string // the normalised reference: for "file", the absolute slash path after symlinks
	Path     string // path inside a repo or artifact, if any
	Version  string // git ref, tag or digest, if any
	From     Layer  // the including file
}
