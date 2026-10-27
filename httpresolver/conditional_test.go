package httpresolver_test

import (
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/httpresolver"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// validated serves /a.json with a validator header, answering a matching conditional request with 304, and records
// the conditional headers of every request.
type validated struct {
	mu            sync.Mutex
	body          string
	etag, lastMod string
	inm, ims      []string // If-None-Match and If-Modified-Since, per request
}

func (v *validated) set(body, etag, lastMod string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.body, v.etag, v.lastMod = body, etag, lastMod
}

func (v *validated) serve(w http.ResponseWriter, r *http.Request) {
	v.mu.Lock()
	defer v.mu.Unlock()
	inm, ims := r.Header.Get("If-None-Match"), r.Header.Get("If-Modified-Since")
	v.inm, v.ims = append(v.inm, inm), append(v.ims, ims)
	if v.etag != "" {
		w.Header().Set("ETag", v.etag)
	}
	if v.lastMod != "" {
		w.Header().Set("Last-Modified", v.lastMod)
	}
	if (inm != "" && inm == v.etag) || (inm == "" && ims != "" && ims == v.lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(v.body))
}

func (v *validated) headers() (inm, ims []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.inm...), append([]string(nil), v.ims...)
}

// cached loads ref with an include cache with a 1 hour TTL, on clock.
func cached(t *testing.T, dir string, clock *cctest.Clock, ref string) *cc.Result {
	t.Helper()
	return loadOK(t, httpresolver.New(), []string{ref}, cc.WithIncludeCache(dir, time.Hour), cctest.WithClock(clock))
}

// DL-229: the ETag is sent back as If-None-Match once the TTL has passed; a 304 uses the cached copy, shown as
// revalidated; a changed file is fetched.
func TestETag(t *testing.T) {
	t.Parallel()
	v := &validated{body: `{"v": 1}`, etag: `"v1"`}
	s := newServer(t, false, v.serve)
	ref, dir, clock := s.URL+"/a.json", filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)

	cached(t, dir, clock, ref)
	clock.Advance(2 * time.Hour)
	res := cached(t, dir, clock, ref)
	if got := layerAt(t, res, ref).Remote.Cache; got != "revalidated" {
		t.Errorf("after a 304: Cache = %q, want revalidated", got)
	}
	if got := res.Tree()["v"]; got != int64(1) {
		t.Errorf("v = %v, want 1", got)
	}
	v.set(`{"v": 2}`, `"v2"`, "")
	clock.Advance(2 * time.Hour)
	res = cached(t, dir, clock, ref)
	if got := res.Tree()["v"]; got != int64(2) {
		t.Errorf("after a change: v = %v, want 2", got)
	}
	inm, _ := v.headers()
	if want := []string{"", `"v1"`, `"v1"`}; len(inm) != 3 || inm[0] != want[0] || inm[1] != want[1] || inm[2] != want[2] {
		t.Errorf("If-None-Match = %q, want %q", inm, want)
	}
}

// DL-229: a weak ETag is sent back as it came.
func TestWeakETag(t *testing.T) {
	t.Parallel()
	v := &validated{body: `{"v": 1}`, etag: `W/"w1"`}
	s := newServer(t, false, v.serve)
	ref, dir, clock := s.URL+"/a.json", filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	cached(t, dir, clock, ref)
	clock.Advance(2 * time.Hour)
	if got := layerAt(t, cached(t, dir, clock, ref), ref).Remote.Cache; got != "revalidated" {
		t.Errorf("Cache = %q, want revalidated", got)
	}
	if inm, _ := v.headers(); len(inm) != 2 || inm[1] != `W/"w1"` {
		t.Errorf("If-None-Match = %q, want W/\"w1\" the second time", inm)
	}
}

// DL-229: without an ETag, Last-Modified is sent back as If-Modified-Since.
func TestLastModified(t *testing.T) {
	t.Parallel()
	const lm = "Wed, 21 Oct 2026 07:28:00 GMT"
	v := &validated{body: `{"v": 1}`, lastMod: lm}
	s := newServer(t, false, v.serve)
	ref, dir, clock := s.URL+"/a.json", filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	cached(t, dir, clock, ref)
	clock.Advance(2 * time.Hour)
	if got := layerAt(t, cached(t, dir, clock, ref), ref).Remote.Cache; got != "revalidated" {
		t.Errorf("Cache = %q, want revalidated", got)
	}
	if inm, ims := v.headers(); len(ims) != 2 || ims[1] != lm || inm[1] != "" {
		t.Errorf("If-None-Match = %q, If-Modified-Since = %q; want only If-Modified-Since %q", inm, ims, lm)
	}
}

// DL-229: with both headers, the ETag is used.
func TestETagPreferred(t *testing.T) {
	t.Parallel()
	v := &validated{body: `{"v": 1}`, etag: `"e"`, lastMod: "Wed, 21 Oct 2026 07:28:00 GMT"}
	s := newServer(t, false, v.serve)
	ref, dir, clock := s.URL+"/a.json", filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	cached(t, dir, clock, ref)
	clock.Advance(2 * time.Hour)
	cached(t, dir, clock, ref)
	if inm, ims := v.headers(); len(inm) != 2 || inm[1] != `"e"` || ims[1] != "" {
		t.Errorf("If-None-Match = %q, If-Modified-Since = %q; want only If-None-Match \"e\"", inm, ims)
	}
}
