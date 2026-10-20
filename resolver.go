package cascadingconfig

import (
	"context"
	"time"
)

// IncludeResolver fetches remote includes for the schemes it handles (DL-196). The built-in file resolver is part of
// the loader instead (DL-182). Resolve gets a reference that is already normalised and allowed by the including file's
// policy, and must honour ctx for cancellation and timeouts. A loader can call it from several goroutines at once.
type IncludeResolver interface {
	// Schemes returns the schemes this resolver handles, such as "http" and "https".
	Schemes() []string
	// Resolve fetches ref.
	Resolve(ctx context.Context, ref IncludeRef) (*Fetched, error)
}

// Fetched is what a resolver fetched (DL-198). The loader picks the format from the reference's path first, and from
// MediaType only when the path gives none.
type Fetched struct {
	Content   []byte
	MediaType string // the media type the transport reports, if any, such as http's Content-Type
	Identity  string // the canonical identity, such as a commit id or digest; empty means sha256:<hex> of Content
}

// Remote describes a remote include in the report (DL-203).
type Remote struct {
	Scheme   string `json:"scheme"`
	Ref      string `json:"ref"` // the normalised reference
	Identity string `json:"identity,omitempty"`
	Pinned   bool   `json:"pinned"`
	Cache    string `json:"cache,omitempty"` // "" when fetched, "cached", or "stale"
}

// WithIncludeResolver registers r for each of its schemes; the last registration of a scheme wins (DL-196). A nil
// resolver, no schemes, an invalid scheme, or the scheme "file", which can't be replaced, is an *OptionError.
func WithIncludeResolver(r IncludeResolver) Option {
	return optionFunc(func(*config) { _ = r }) // stub
}

// WithIncludeTimeout bounds each remote fetch; the default is 30 seconds (DL-199). Running out is a 504 *FetchError.
// A duration that isn't positive is an *OptionError.
func WithIncludeTimeout(d time.Duration) Option {
	return optionFunc(func(*config) { _ = d }) // stub
}

// WithMaxIncludeSize limits the size of remote content; the default is 1 MiB (DL-198). Content over the limit is a
// *TooLargeError. A limit that isn't positive is an *OptionError.
func WithMaxIncludeSize(n int64) Option {
	return optionFunc(func(*config) { _ = n }) // stub
}

// WithoutRemoteIncludes removes every remote resolver registered before it, and makes the loader hardened: a policy
// that allows a remote scheme must give it an allow-list and RequirePinned(), or New returns an *OptionError (DL-205).
func WithoutRemoteIncludes() Option {
	return optionFunc(func(*config) {}) // stub
}

// WithIncludeCache sets the include cache's directory and TTL (DL-206, DL-207). An empty dir keeps the default,
// {user cache dir}/{name}/includes; a TTL of 0 fetches every time, keeping the cache only as a fallback. A relative
// dir or a negative TTL is an *OptionError.
func WithIncludeCache(dir string, ttl time.Duration) Option {
	return optionFunc(func(*config) { _, _ = dir, ttl }) // stub
}

// WithoutIncludeCache turns the include cache off, and with it the stale fallback.
func WithoutIncludeCache() Option {
	return optionFunc(func(*config) {}) // stub
}
