package cascadingconfig_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// validating serves one reference's content with a validator, and answers a matching IncludeRef.Validator with
// notModified, by default ErrNotModified itself. It records the validator of every request.
type validating struct {
	mu          sync.Mutex
	content     string
	validator   string
	notModified error
	seen        []string
}

func (v *validating) set(content, validator string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.content, v.validator = content, validator
}

func (v *validating) fetch(_ context.Context, ref cc.IncludeRef) (*cc.Fetched, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seen = append(v.seen, ref.Validator)
	if ref.Validator != "" && ref.Validator == v.validator {
		if v.notModified != nil {
			return nil, v.notModified
		}
		return nil, cc.ErrNotModified
	}
	return &cc.Fetched{Content: []byte(v.content), Validator: v.validator}, nil
}

// validators returns the IncludeRef.Validator of every request, in order.
func (v *validating) validators() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.seen...)
}

func newValidating(content, validator string) (*validating, *fakeResolver) {
	v := &validating{content: content, validator: validator}
	f := newFake("https")
	f.fetch = v.fetch
	return v, f
}

const valRef = "https://h.example/a.json"

// DL-229: after the TTL, the loader passes the stored validator; ErrNotModified uses the cached copy, shown as
// "revalidated", and restarts the TTL; a changed source is fetched.
func TestRevalidate(t *testing.T) {
	t.Parallel()
	c := newCacheEnv(t)
	v, f := newValidating(`{"v": 1}`, `"e1"`)
	if got := cacheState(t, c.ok(t, f, []string{valRef}), valRef); got != "" {
		t.Errorf("first load: Cache = %q, want fetched", got)
	}
	c.clock.Advance(61 * time.Minute)
	res := c.ok(t, f, []string{valRef})
	if got := cacheState(t, res, valRef); got != "revalidated" {
		t.Errorf("after the TTL: Cache = %q, want revalidated", got)
	}
	wantTreeJSON(t, res.Tree(), `{"v": 1}`)
	c.clock.Advance(59 * time.Minute)
	if got := cacheState(t, c.ok(t, f, []string{valRef}), valRef); got != "cached" {
		t.Errorf("within the restarted TTL: Cache = %q, want cached", got)
	}
	v.set(`{"v": 2}`, `"e2"`)
	c.clock.Advance(2 * time.Minute)
	res = c.ok(t, f, []string{valRef})
	if got := cacheState(t, res, valRef); got != "" {
		t.Errorf("after a change: Cache = %q, want fetched", got)
	}
	wantTreeJSON(t, res.Tree(), `{"v": 2}`)
	if got, want := v.validators(), []string{"", `"e1"`, `"e1"`}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("validators sent = %q, want %q", got, want)
	}
}

// DL-229: a wrapped ErrNotModified counts too.
func TestRevalidateWrapped(t *testing.T) {
	t.Parallel()
	c := newCacheEnv(t)
	v, f := newValidating(`{"v": 1}`, "etag:abc")
	v.notModified = fmt.Errorf("server said 304: %w", cc.ErrNotModified)
	c.ok(t, f, []string{valRef})
	c.clock.Advance(2 * time.Hour)
	if got := cacheState(t, c.ok(t, f, []string{valRef}), valRef); got != "revalidated" {
		t.Errorf("Cache = %q, want revalidated", got)
	}
}

// DL-229: the loader keeps a validator only if it's printable ASCII and at most 1 KiB, so a hostile server can't
// inject headers through it.
func TestValidatorRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, validator string
		kept            bool
	}{
		{"weak etag", `W/"abc"`, true},
		{"with a space", "last-modified:Wed, 21 Oct 2026 07:28:00 GMT", true},
		{"1 KiB", strings.Repeat("a", 1024), true},
		{"over 1 KiB", strings.Repeat("a", 1025), false},
		{"CRLF", "\"x\"\r\nX-Injected: 1", false},
		{"NUL", "a\x00b", false},
		{"DEL", "a\x7fb", false},
		{"non-ASCII", "é", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCacheEnv(t)
			v, f := newValidating(`{"v": 1}`, tc.validator)
			c.ok(t, f, []string{valRef})
			c.clock.Advance(2 * time.Hour)
			c.ok(t, f, []string{valRef})
			got := v.validators()
			want := ""
			if tc.kept {
				want = tc.validator
			}
			if len(got) != 2 || got[1] != want {
				t.Errorf("second request's validator = %q, want %q", got[len(got)-1], want)
			}
		})
	}
}

// DL-229: without the cache the loader holds no copy, so it never sends a validator; with it, the same loads do.
func TestValidatorWithoutCache(t *testing.T) {
	t.Parallel()
	v, f := newValidating(`{"v": 1}`, `"e1"`)
	remOK(t, f, []string{valRef})
	remOK(t, f, []string{valRef})
	if got := v.validators(); len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Errorf("without the cache: validators sent = %q, want none", got)
	}
	c := newCacheEnv(t)
	c.ttl = 0
	v, f = newValidating(`{"v": 1}`, `"e1"`)
	c.ok(t, f, []string{valRef})
	c.ok(t, f, []string{valRef})
	if got := v.validators(); len(got) != 2 || got[1] != `"e1"` {
		t.Errorf("with the cache and a TTL of 0: validators sent = %q, want \"e1\" the second time", got)
	}
}

// DL-207, DL-229: a *FetchError while revalidating falls back to the stale copy.
func TestRevalidateFetchError(t *testing.T) {
	t.Parallel()
	c := newCacheEnv(t)
	_, f := newValidating(`{"v": 1}`, `"e1"`)
	c.ok(t, f, []string{valRef})
	c.clock.Advance(2 * time.Hour)
	var sent string
	f.fetch = func(_ context.Context, ref cc.IncludeRef) (*cc.Fetched, error) {
		sent = ref.Validator
		return nil, &cc.FetchError{BaseError: cc.BaseError{Op: "fetch", Path: valRef}}
	}
	res := c.ok(t, f, []string{valRef})
	if sent != `"e1"` {
		t.Errorf("the failed request's validator = %q, want \"e1\"", sent)
	}
	if got := cacheState(t, res, valRef); got != "stale" {
		t.Errorf("Cache = %q, want stale", got)
	}
	wantTreeJSON(t, res.Tree(), `{"v": 1}`)
}
