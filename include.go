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
	return optionFunc(func(c *config) {
		switch {
		case key == "":
			c.fail("WithIncludeKey", "an empty key; use WithoutIncludeKey to turn includes off")
			return
		case n != IncludeFlat && n != IncludeNested:
			c.fail("WithIncludeKey", "unknown nesting %d", n)
			return
		}
		c.includeKey, c.nesting = key, n
	})
}

// WithoutIncludeKey turns includes off, so the key a layout set is ordinary data again.
func WithoutIncludeKey() Option {
	return optionFunc(func(c *config) { c.includeKey = "" })
}

// WithoutIncludeResolver removes the resolver for scheme. Removing the built-in "file" resolver while an include key is
// set is an *OptionError (DL-177); a policy whose AllowResolvers leaves out "file" forbids local includes instead.
func WithoutIncludeResolver(scheme string) Option {
	return optionFunc(func(c *config) {
		switch scheme {
		case "":
			c.fail("WithoutIncludeResolver", "an empty scheme")
		case "file":
			c.noFileResolver = true
		}
	})
}

// finishIncludes checks the include key against the unset key and the file resolver.
func (c *config) finishIncludes() {
	switch {
	case c.includeKey == "":
	case c.includeKey == c.merge.unsetKey:
		c.fail("WithIncludeKey", "the key %q is also the unset key", c.includeKey)
	case c.noFileResolver:
		c.fail("New", "the include key %q is set, but the file resolver was removed, so every local include would fail",
			c.includeKey)
	}
}

// hasResolver reports whether a resolver is registered for scheme. Until M8, only the built-in file resolver exists.
func (c *config) hasResolver(scheme string) bool {
	return scheme == "file" && !c.noFileResolver
}

// resolverSchemes returns the registered resolvers' schemes, sorted.
func (c *config) resolverSchemes() []string {
	if c.noFileResolver {
		return nil
	}
	return []string{"file"}
}

// IncludeRef is an include reference, as a resolver and a MatchRef matcher see it (DL-198).
type IncludeRef struct {
	Raw      string // as written in the config file
	Scheme   string // "file", "https", "git", "oci", …
	Ref      string // the normalised reference string that matchers see
	Location string // the URL without its fragment, the repo URL, or registry/repository; for "file", the path
	Path     string // path inside a repo or artifact, if any
	Version  string // git ref; oci tag, digest or tag@digest; sha256:<hex> from an integrity fragment
	From     Layer  // the including file
	MaxSize  int64  // the size limit the loader enforces on the content

	// Check normalises another reference, such as a redirect target, and runs the scheme and allow-list checks this
	// reference passed, under the same policy. It returns a *ReferenceError or a *PolicyError.
	Check func(ref string) error
}
