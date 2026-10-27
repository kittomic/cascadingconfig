package cascadingconfig_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// cacheEnv is a cache dir and a clock that several loads share, as several runs of an app would.
type cacheEnv struct {
	dir   string
	clock *cctest.Clock
	ttl   time.Duration
}

func newCacheEnv(t *testing.T) *cacheEnv {
	return &cacheEnv{dir: filepath.Join(t.TempDir(), "includes"), clock: cctest.NewClock(t0), ttl: time.Hour}
}

// load loads rootCfg including refs, with the cache and clock, and opts after them.
func (c *cacheEnv) load(t *testing.T, f *fakeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	fsys := cctest.NewFS(map[string]string{rootCfg: includeOf("", refs...)})
	base := []cc.Option{
		incScopes, cc.WithIncludeResolver(f), cc.WithDecoder(mediaDec{}),
		cc.WithIncludeCache(c.dir, c.ttl), cctest.WithClock(c.clock),
	}
	return newLoaderFS(t, fsys, nil, append(base, opts...)...).Load(t.Context())
}

func (c *cacheEnv) ok(t *testing.T, f *fakeResolver, refs []string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := c.load(t, f, refs, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// cacheState returns the Remote.Cache of the layer at path.
func cacheState(t *testing.T, res *cc.Result, path string) string {
	t.Helper()
	l := layerAt(t, res.Describe(), path)
	if l.Remote == nil {
		t.Fatalf("layer %s has no Remote", path)
	}
	return string(l.Remote.Cache)
}

// DL-19, DL-207: an unpinned reference is served from the cache until the TTL, by the loader's clock, has passed.
func TestCacheTTL(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	c := newCacheEnv(t)
	f := newFake("https").serve(ref, `{"v": 1}`)
	if got := cacheState(t, c.ok(t, f, []string{ref}), ref); got != "" {
		t.Errorf("first load: Cache = %q, want fetched", got)
	}
	f.serve(ref, `{"v": 2}`)
	c.clock.Advance(59 * time.Minute)
	res := c.ok(t, f, []string{ref})
	if got := cacheState(t, res, ref); got != "cached" || f.count(ref) != 1 {
		t.Errorf("within the TTL: Cache = %q after %d fetches, want cached after 1", got, f.count(ref))
	}
	wantTreeJSON(t, res.Tree(), `{"v": 1}`)
	c.clock.Advance(2 * time.Minute)
	res = c.ok(t, f, []string{ref})
	if got := cacheState(t, res, ref); got != "" || f.count(ref) != 2 {
		t.Errorf("after the TTL: Cache = %q after %d fetches, want fetched after 2", got, f.count(ref))
	}
	wantTreeJSON(t, res.Tree(), `{"v": 2}`)
}

// DL-19, DL-207: a pinned reference is never fetched again.
func TestCachePinned(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	ref := "https://h.example/a.json#sha256=" + sum(`{"v": 1}`)
	c := newCacheEnv(t)
	f := newFake("https").serve("https://h.example/a.json", `{"v": 1}`)
	c.ok(t, f, []string{ref})
	c.clock.Advance(10000 * time.Hour)
	if got := cacheState(t, c.ok(t, f, []string{ref}), ref); got != "cached" || f.count("https://h.example/a.json") != 1 {
		t.Errorf("Cache = %q after %d fetches, want cached after 1", got, f.count("https://h.example/a.json"))
	}
}

// DL-207: a TTL of 0 always fetches, and keeps the cache as a fallback.
func TestCacheZeroTTL(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	c := newCacheEnv(t)
	c.ttl = 0
	f := newFake("https").serve(ref, `{"v": 1}`)
	c.ok(t, f, []string{ref})
	c.ok(t, f, []string{ref})
	if n := f.count(ref); n != 2 {
		t.Errorf("fetched %d times, want 2", n)
	}
	f.fail(ref, errors.New("connection refused"))
	if got := cacheState(t, c.ok(t, f, []string{ref}), ref); got != "stale" {
		t.Errorf("Cache = %q, want stale", got)
	}
}

// DL-19, DL-207: only a *FetchError falls back to a cached copy, with WarnStaleInclude; a 404, a policy rejection or a
// bad reference fails the load, and so does a *FetchError with nothing cached.
func TestCacheStale(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	for _, tc := range []struct {
		name  string
		err   error
		stale bool
	}{
		{"connection refused", errors.New("connection refused"), true},
		{"timeout", timeoutErr{}, true},
		{"fetch error", &cc.FetchError{BaseError: cc.BaseError{Op: "fetch", Path: ref}}, true},
		{"not found", &cc.NotFoundError{BaseError: cc.BaseError{Op: "fetch", Path: ref}}, false},
		{"policy", &cc.PolicyError{BaseError: cc.BaseError{Op: "fetch", Path: ref}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCacheEnv(t)
			f := newFake("https").serve(ref, `{"v": 1}`)
			c.ok(t, f, []string{ref})
			c.clock.Advance(2 * time.Hour)
			f.fail(ref, tc.err)
			res, err := c.load(t, f, []string{ref})
			if !tc.stale {
				if err == nil {
					t.Fatalf("Load succeeded from the cache, want the %s error", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantTreeJSON(t, res.Tree(), `{"v": 1}`)
			l := layerAt(t, res.Describe(), ref)
			if l.Remote == nil || l.Remote.Cache != "stale" || !hasWarningCode(l.Warnings, cc.WarnStaleInclude) {
				t.Errorf("layer = %+v, Remote %+v; want stale, with WarnStaleInclude", l, l.Remote)
			}
		})
	}
	t.Run("nothing cached", func(t *testing.T) {
		c := newCacheEnv(t)
		_, err := c.load(t, newFake("https").fail(ref, errors.New("connection refused")), []string{ref})
		wantErr[*cc.FetchError](t, err, cc.CodeBadGateway, rootCfg)
	})
}

// DL-58, DL-207: WithoutIncludeCache fetches every time, with no stale fallback.
func TestWithoutIncludeCache(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	f := newFake("https").serve(ref, `{"v": 1}`)
	remOK(t, f, []string{ref})
	remOK(t, f, []string{ref})
	if n := f.count(ref); n != 2 {
		t.Errorf("fetched %d times, want 2", n)
	}
	f.fail(ref, errors.New("connection refused"))
	_, err := remFails(t, f, []string{ref})
	wantErr[*cc.FetchError](t, err, cc.CodeBadGateway, rootCfg)
}

// DL-58, DL-206, design.md Traversal protection, step 4: no cache file or directory name contains any part of a
// reference, and the cache's files are private to the user.
func TestCacheNames(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	refs := []string{"https://cachehost.example/zebra/quokka.json", "git::https://cachehost.example/wombat.git//okapi/tapir.json?ref=narwhal"}
	c := newCacheEnv(t)
	f := newFake("https", "git").serve(refs[0], `{"a": 1}`).serve(refs[1], `{"b": 1}`)
	c.ok(t, f, refs)
	var files int
	err := filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, part := range []string{"cachehost", "example", "zebra", "quokka", "wombat", "okapi", "tapir", "narwhal"} {
			if strings.Contains(strings.ToLower(d.Name()), part) {
				t.Errorf("cache entry %s contains %q", p, part)
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			if want := map[bool]fs.FileMode{true: 0o700, false: 0o600}[d.IsDir()]; info.Mode().Perm() != want {
				t.Errorf("%s has mode %v, want %v", p, info.Mode().Perm(), want)
			}
		}
		if d.Type().IsRegular() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Error("the cache holds no files")
	}
}

// DL-206: content is stored under its own SHA-256, so two files with the same identity, such as two files in one
// commit, don't collide.
func TestCacheSameIdentity(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	a := "git::https://h.example/r.git//a.json?ref=main"
	b := "git::https://h.example/r.git//b.json?ref=main"
	c := newCacheEnv(t)
	f := newFake("git").
		serveFetched(a, cc.Fetched{Content: []byte(`{"a": 1}`), Identity: sha40}).
		serveFetched(b, cc.Fetched{Content: []byte(`{"b": 1}`), Identity: sha40})
	c.ok(t, f, []string{a, b})
	res := c.ok(t, f, []string{a, b})
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 1}`)
	if cacheState(t, res, a) != "cached" || cacheState(t, res, b) != "cached" {
		t.Errorf("want both served from the cache")
	}
}

// DL-207: cached content is rechecked on use, so a loader with a smaller size limit or a narrower allow-list rejects
// what an earlier load cached.
func TestCacheRechecked(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	t.Run("size", func(t *testing.T) {
		c := newCacheEnv(t)
		f := newFake("https").serve(ref, `{"a": "1234567"}`)
		c.ok(t, f, []string{ref})
		_, err := c.load(t, f, []string{ref}, cc.WithMaxIncludeSize(8))
		wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, ref)
	})
	t.Run("allow-list", func(t *testing.T) {
		c := newCacheEnv(t)
		f := newFake("https").serve(ref, `{}`)
		c.ok(t, f, []string{ref})
		_, err := c.load(t, f, []string{ref},
			cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), cc.AllowFor("https", cc.Prefix("https://other.example/"))))
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg)
	})
	t.Run("pinning", func(t *testing.T) {
		c := newCacheEnv(t)
		f := newFake("https").serve(ref, `{}`)
		c.ok(t, f, []string{ref})
		_, err := c.load(t, f, []string{ref}, cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), cc.RequirePinned()))
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg)
	})
}

// DL-206: cached content that doesn't match its recorded SHA-256 counts as missing, so the reference is fetched again.
func TestCacheCorrupt(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	const content = `{"v": "original"}`
	c := newCacheEnv(t)
	f := newFake("https").serve(ref, content)
	c.ok(t, f, []string{ref})
	var changed int
	err := filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		if err == nil && bytes.Equal(data, []byte(content)) {
			changed++
			return os.WriteFile(p, []byte(`{"v": "tampered"}`), 0o600)
		}
		return err
	})
	if err != nil || changed == 0 {
		t.Fatalf("no cached copy of the content to change (%d changed, %v)", changed, err)
	}
	res := c.ok(t, f, []string{ref})
	wantTreeJSON(t, res.Tree(), content)
	if n := f.count(ref); n != 2 {
		t.Errorf("fetched %d times, want 2", n)
	}
}

// DL-206: a cache that can't be used is skipped with WarnIncludeCache, and the load fetches.
func TestCacheUnusable(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	c := newCacheEnv(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.dir = filepath.Join(blocker, "includes")
	f := newFake("https").serve(ref, `{"v": 1}`)
	res := c.ok(t, f, []string{ref})
	wantTreeJSON(t, res.Tree(), `{"v": 1}`)
	if !hasWarning(res.Describe(), cc.WarnIncludeCache) {
		t.Errorf("no %s warning in the report", cc.WarnIncludeCache)
	}
}

// DL-206, design.md Caching, "User cache dir": the default dir is {user cache dir}/{name}/includes, from the host's
// XDG_CACHE_HOME on Linux.
func TestCacheDefaultDir(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	cacheHome := t.TempDir()
	fsys := cctest.NewFS(map[string]string{rootCfg: includeOf("", ref)})
	f := newFake("https").serve(ref, `{}`)
	_, err := newLoaderFS(t, fsys, map[string]string{"XDG_CACHE_HOME": cacheHome},
		incScopes, cc.WithIncludeResolver(f), cctest.WithClock(cctest.NewClock(t0))).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cacheHome, "myapp", "includes"))
	if err != nil || len(entries) == 0 {
		t.Errorf("the default cache dir holds %d entries (%v), want the cached include", len(entries), err)
	}
}
