package cascadingconfig_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

const gitRef = "git::https://g.example/r.git//b.json?ref=main"

// remoteWatch watches rootCfg including refs, with the resolvers, an include cache with a 1 hour TTL, and wopts.
func remoteWatch(t *testing.T, refs []string, wopts []cc.WatchOption, opts ...cc.Option) (*watchEnv, *cc.Watcher[cc.Tree]) {
	t.Helper()
	e := newWatchEnv(t, map[string]string{rootCfg: includeOf(`"a": 1`, refs...)})
	base := []cc.Option{cc.WithIncludeCache(filepath.Join(t.TempDir(), "includes"), time.Hour)}
	return e, e.watch(wopts, append(base, opts...)...)
}

// polls returns how many requests v got after the first load's.
func polls(v *validating) int { return len(v.validators()) - 1 }

// DL-229, DL-230: a remote poll revalidates, ignoring the cache TTL; an unchanged source doesn't reload, and a changed
// one is stored and reloads, from the cache.
func TestRemotePoll(t *testing.T) {
	v, f := newValidating(`{"r": 1}`, `"e1"`)
	e, w := remoteWatch(t, []string{valRef}, []cc.WatchOption{cc.WithRemotePoll(10 * time.Minute)}, cc.WithIncludeResolver(f))
	e.clock.Advance(10*time.Minute - time.Second)
	if polls(v) != 0 {
		t.Errorf("%d polls before the interval, want 0", polls(v))
	}
	e.clock.Advance(time.Second)
	e.settle()
	if got := v.validators(); len(got) != 2 || got[1] != `"e1"` {
		t.Errorf("validators sent = %q, want a poll with \"e1\"", got)
	}
	e.wantLoads(1, "an unchanged poll")

	v.set(`{"r": 2}`, `"e2"`)
	e.clock.Advance(10 * time.Minute)
	e.settle()
	wantValue(t, w.Current(), "r", int64(2))
	e.wantLoads(2, "a changed poll")
	if polls(v) != 2 {
		t.Errorf("%d requests after the first load, want 2: the reload reads the cache", polls(v))
	}
}

// DL-230: rules match the normalised reference; when they overlap, the most recently given wins; 0 never polls; and
// remote polling is off without a rule.
func TestRemotePollRules(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		rules                []cc.WatchOption
		httpsPolls, gitPolls int
	}{
		{"none", nil, 0, 0},
		{"every remote", []cc.WatchOption{cc.WithRemotePoll(time.Minute)}, 60, 60},
		{"git slower", []cc.WatchOption{cc.WithRemotePoll(time.Minute), cc.WithRemotePoll(time.Hour, cc.Prefix("git::"))}, 60, 1},
		{"the later rule wins", []cc.WatchOption{cc.WithRemotePoll(time.Hour, cc.Prefix("git::")), cc.WithRemotePoll(time.Minute)}, 60, 60},
		{"git never", []cc.WatchOption{cc.WithRemotePoll(time.Minute), cc.WithRemotePoll(0, cc.Prefix("git::"))}, 60, 0},
		{"by glob", []cc.WatchOption{cc.WithRemotePoll(30*time.Minute, cc.Glob("https://h.example/**"))}, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hv, hf := newValidating(`{"h": 1}`, "eh")
			gv := &validating{content: `{"g": 1}`, validator: "eg"}
			gf := newFake("git")
			gf.fetch = gv.fetch
			e, _ := remoteWatch(t, []string{valRef, gitRef}, tc.rules, cc.WithIncludeResolver(hf), cc.WithIncludeResolver(gf))
			for range 60 {
				e.clock.Advance(time.Minute)
			}
			if polls(hv) != tc.httpsPolls || polls(gv) != tc.gitPolls {
				t.Errorf("in an hour: %d https and %d git polls, want %d and %d", polls(hv), polls(gv), tc.httpsPolls, tc.gitPolls)
			}
		})
	}
}

// DL-230: a failed poll doubles that reference's interval, up to 8 times, and a success resets it.
func TestRemotePollBackoff(t *testing.T) {
	var mu sync.Mutex
	var at []time.Duration
	failing := true
	var e *watchEnv
	f := newFake("https")
	f.fetch = func(context.Context, cc.IncludeRef) (*cc.Fetched, error) {
		mu.Lock()
		defer mu.Unlock()
		if e != nil {
			at = append(at, e.clock.Now().Sub(t0))
		}
		if e != nil && failing {
			return nil, &cc.FetchError{BaseError: cc.BaseError{Op: "fetch", Path: valRef}}
		}
		return &cc.Fetched{Content: []byte(`{"r": 1}`), Validator: "e"}, nil
	}
	env, _ := remoteWatch(t, []string{valRef}, []cc.WatchOption{cc.WithRemotePoll(time.Minute)}, cc.WithIncludeResolver(f))
	mu.Lock()
	e = env
	mu.Unlock()
	for range 23 {
		env.clock.Advance(time.Minute)
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	for range 10 {
		env.clock.Advance(time.Minute)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []time.Duration{1, 3, 7, 15, 23, 31, 32, 33}
	for i := range want {
		want[i] *= time.Minute
	}
	if !slices.Equal(at, want) {
		t.Errorf("polls at %v, want %v", at, want)
	}
}

// DL-230: without the cache there's nowhere to keep validators, so each poll is a full reload.
func TestRemotePollWithoutCache(t *testing.T) {
	_, f := newValidating(`{"r": 1}`, `"e1"`)
	e, _ := remoteWatch(t, []string{valRef}, []cc.WatchOption{cc.WithRemotePoll(time.Minute)},
		cc.WithIncludeResolver(f), cc.WithoutIncludeCache())
	e.clock.Advance(time.Minute)
	e.settle()
	e.wantLoads(2, "a poll without the cache")
	e.wantChanges(0, "an unchanged reload")
}

// DL-230: pinned references never change, so they're never polled; an unpinned one beside it is.
func TestRemotePollPinned(t *testing.T) {
	ref := "git::https://g.example/r.git//b.json?ref=" + sha40
	gv := &validating{content: `{"g": 1}`, validator: sha40}
	gf := newFake("git")
	gf.fetch = gv.fetch
	hv, hf := newValidating(`{"h": 1}`, "eh")
	e, _ := remoteWatch(t, []string{ref, valRef}, []cc.WatchOption{cc.WithRemotePoll(time.Minute)},
		cc.WithIncludeResolver(gf), cc.WithIncludeResolver(hf))
	for range 60 {
		e.clock.Advance(time.Minute)
	}
	if polls(gv) != 0 || polls(hv) != 60 {
		t.Errorf("in an hour: the pinned reference was polled %d times and the unpinned %d, want 0 and 60", polls(gv), polls(hv))
	}
}
