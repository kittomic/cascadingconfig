package cascadingconfig

// Require is the level of a RequireConfig requirement: RequireAny, or a combination of the flags below.
type Require uint

// RequireAny counts any loaded layer, of any kind. It's the most forgiving level.
const RequireAny Require = 0

// Flags that narrow what counts. Combine them with |.
const (
	RequireFile     Require = 1 << iota // only layers read from files: discovery, explicit files and includes
	RequireNonEmpty                     // empty layers don't count
)

// RequireConfig makes a load fail with a *NotFoundError unless config is found at level. With no scopes, one
// qualifying layer anywhere is enough; with scopes, each of them must have one. Each RequireConfig adds a requirement,
// and all of them must hold.
//
// A value layer with no keys never counts. Explicit files count for the scope they replace, or for every scope under
// WithConfigFilesOnly. An unknown flag, or a scope that isn't defined or selected, is an *OptionError.
func RequireConfig(level Require, scopes ...ScopeName) Option {
	_, _ = level, scopes // stub
	return optionFunc(func(*config) {})
}
