package cascadingconfig

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// IncludeResolver fetches remote includes for the schemes it handles. The built-in file resolver is part of the loader
// instead. Resolve gets a reference that is already normalised and allowed by the including file's policy, and must
// honour ctx for cancellation and timeouts. A loader can call it from several goroutines at once.
type IncludeResolver interface {
	// Schemes returns the schemes this resolver handles, such as "http" and "https".
	Schemes() []string
	// Resolve fetches ref.
	Resolve(ctx context.Context, ref IncludeRef) (*Fetched, error)
}

// Fetched is what a resolver fetched. The loader picks the format from the reference's path first, and from MediaType
// only when the path gives none.
type Fetched struct {
	Content   []byte
	MediaType string // the media type the transport reports, if any, such as http's Content-Type
	Identity  string // the canonical identity, such as a commit id or digest; empty means sha256:<hex> of Content
	// Validator is an opaque value the resolver wants back in IncludeRef.Validator next time, such as an ETag. The loader
	// keeps it only if it's printable ASCII and at most 1 KiB.
	Validator string
}

// ErrNotModified is returned by a resolver, wrapped or not, when the source still matches IncludeRef.Validator, so the
// loader uses its copy.
var ErrNotModified = errors.New("cascadingconfig: not modified")

// Remote describes a remote include in the report.
type Remote struct {
	Scheme   string     `json:"scheme"`
	Ref      string     `json:"ref"` // the normalised reference
	Identity string     `json:"identity,omitempty"`
	Pinned   bool       `json:"pinned"`
	Cache    CacheState `json:"cache,omitempty"`
}

// CacheState says where a remote include's content came from.
type CacheState string

// Cache states.
const (
	CacheFetched     CacheState = ""            // fetched from its source
	CacheCached      CacheState = "cached"      // from the include cache, within its TTL or pinned
	CacheRevalidated CacheState = "revalidated" // from the cache, after the source said it hadn't changed
	CacheStale       CacheState = "stale"       // from the cache, because fetching failed
)

// The defaults for remote includes (DL-198, DL-199, DL-207, DL-210).
const (
	defaultIncludeTimeout  = 30 * time.Second
	defaultMaxIncludeSize  = 1 << 20
	defaultMaxIncludeDepth = 16
	defaultMaxIncludes     = 1000
	defaultCacheTTL        = time.Hour
)

// WithIncludeResolver registers r for each of its schemes; the last registration of a scheme wins. A nil resolver, no
// schemes, an invalid scheme, or the scheme "file", which can't be replaced, is an *OptionError.
func WithIncludeResolver(r IncludeResolver) Option {
	return optionFunc(func(c *config) {
		if r == nil {
			c.fail("WithIncludeResolver", "a nil resolver")
			return
		}
		schemes := r.Schemes()
		if len(schemes) == 0 {
			c.fail("WithIncludeResolver", "the resolver handles no schemes")
			return
		}
		for _, s := range schemes {
			switch {
			case strings.EqualFold(s, "file"):
				c.fail("WithIncludeResolver", `the built-in "file" resolver can't be replaced`)
				return
			case len(s) < 2 || !isScheme(s):
				c.fail("WithIncludeResolver", "invalid scheme %q: a letter, then letters, digits, +, - or ., two characters or more", s)
				return
			}
		}
		if c.resolvers == nil {
			c.resolvers = map[string]IncludeResolver{}
		}
		for _, s := range schemes {
			c.resolvers[strings.ToLower(s)] = r
		}
	})
}

// WithIncludeTimeout bounds each remote fetch; the default is 30 seconds. Running out is a 504 *FetchError. A duration
// that isn't positive is an *OptionError.
func WithIncludeTimeout(d time.Duration) Option {
	return optionFunc(func(c *config) {
		if d <= 0 {
			c.fail("WithIncludeTimeout", "the timeout must be positive, not %v", d)
			return
		}
		c.includeTimeout = d
	})
}

// WithMaxIncludeSize limits the size of remote content; the default is 1 MiB. Content over the limit is a
// *TooLargeError. A limit that isn't positive is an *OptionError.
func WithMaxIncludeSize(n int64) Option {
	return optionFunc(func(c *config) {
		if n <= 0 {
			c.fail("WithMaxIncludeSize", "the limit must be positive, not %d", n)
			return
		}
		c.maxIncludeSize = n
	})
}

// WithoutRemoteIncludes removes every remote resolver registered before it, and makes the loader hardened: a policy
// that allows a remote scheme must give it an allow-list and RequirePinned(), or New returns an *OptionError.
func WithoutRemoteIncludes() Option {
	return optionFunc(func(c *config) {
		c.resolvers = nil
		c.hardened = true
	})
}

// WithIncludeCache sets the include cache's directory and TTL. An empty dir keeps the default, {user cache
// dir}/{name}/includes; a TTL of 0 fetches every time, keeping the cache only as a fallback. A relative dir or a
// negative TTL is an *OptionError.
func WithIncludeCache(dir string, ttl time.Duration) Option {
	return optionFunc(func(c *config) {
		switch {
		case dir != "" && !filepath.IsAbs(dir):
			c.fail("WithIncludeCache", "the cache dir %q must be absolute", dir)
		case ttl < 0:
			c.fail("WithIncludeCache", "the TTL must not be negative, not %v", ttl)
		default:
			c.cacheDir, c.cacheTTL, c.noCache = dir, ttl, false
		}
	})
}

// WithoutIncludeCache turns the include cache off, and with it the stale fallback.
func WithoutIncludeCache() Option {
	return optionFunc(func(c *config) { c.noCache = true })
}

// finishRemote checks a hardened loader's policies: each that allows a remote scheme must give it an allow-list and
// require pinned references (DL-205).
func (c *config) finishRemote() {
	if !c.hardened || len(c.resolvers) == 0 {
		return
	}
	check := func(name string, p *policy) {
		for _, s := range slices.Sorted(maps.Keys(c.resolvers)) {
			if !p.usable(c, s) {
				continue
			}
			var missing []string
			if len(p.allow) == 0 && len(p.allowFor[s]) == 0 {
				missing = append(missing, "an allow-list")
			}
			if !p.pinned {
				missing = append(missing, "RequirePinned")
			}
			if len(missing) > 0 {
				c.fail("New", "the %s policy allows the remote scheme %q without %s, which WithoutRemoteIncludes requires",
					name, s, strings.Join(missing, " or "))
			}
		}
	}
	check("default", c.policyDefault)
	for _, name := range slices.Sorted(maps.Keys(c.policies)) {
		check(string(name), c.policies[name])
	}
}
