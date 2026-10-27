package ociresolver_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// blobRequests counts the blob downloads in requests.
func blobRequests(requests []string) int {
	n := 0
	for _, r := range requests {
		if strings.HasPrefix(r, "GET ") && strings.Contains(r, "/blobs/") {
			n++
		}
	}
	return n
}

// DL-229: the validator is the manifest digest. Once the TTL has passed, an unchanged tag is revalidated without
// downloading any blob; a moved tag is fetched.
func TestRevalidateTag(t *testing.T) {
	t.Parallel()
	r := newRegistry(t, false)
	first := r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
	ref := r.ociRef("acme/cfg", ":1", "a.json")
	dir, clock := filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	opts := []cc.Option{cc.WithIncludeCache(dir, time.Hour), cctest.WithClock(clock)}

	loadOK(t, plain(r), []string{ref}, opts...)
	before := len(r.requested())
	clock.Advance(2 * time.Hour)
	res := loadOK(t, plain(r), []string{ref}, opts...)
	for _, l := range res.Describe().Layers {
		if l.Path == ref && (l.Remote == nil || l.Remote.Cache != "revalidated" || l.Remote.Identity != first) {
			t.Errorf("an unchanged tag: Remote = %+v, want revalidated at %s", l.Remote, first)
		}
	}
	if n := blobRequests(r.requested()[before:]); n != 0 {
		t.Errorf("an unchanged tag downloaded %d blobs, want 0", n)
	}

	second := r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 2}`))
	clock.Advance(2 * time.Hour)
	res = loadOK(t, plain(r), []string{ref}, opts...)
	wantValue(t, res, "v", int64(2))
	for _, l := range res.Describe().Layers {
		if l.Path == ref && (l.Remote == nil || l.Remote.Identity != second) {
			t.Errorf("a moved tag: Remote = %+v, want identity %s", l.Remote, second)
		}
	}
}
