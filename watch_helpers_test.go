package cascadingconfig_test

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// The watch tests watch a loader over a cctest.FS with a fake notifier and a fake clock, so no real timers run. Events
// are sent by the test, as a real notifier would send them after a change. The loader has three scopes, lowest first:
// "vals", a ValuesFunc that counts loads and returns the test's tree; "sys" over /etc/myapp (config.json); and "proj"
// over /home/me/work (myapp.json), with the include key "include".

// sysDir is the sys scope's directory; sysFile is its file.
const sysDir = "/etc/myapp"

// The default debounce, DL-41.
const (
	quiet   = 100 * time.Millisecond
	maxWait = time.Second
)

// watchEnv is one watched loader and everything the test observes about it.
type watchEnv struct {
	t     *testing.T
	fs    *cctest.FS
	n     *cctest.Notifier // nil for a loader with no notifier, which polls
	clock *cctest.Clock
	logs  *records

	loads   atomic.Int64 // calls of the vals scope: one per load
	mu      sync.Mutex
	vals    cc.Tree // what the vals scope returns
	changes calls[cc.Change[cc.Tree]]
}

// newWatchEnv starts the files in files, with /etc/hosts so /etc exists.
func newWatchEnv(t *testing.T, files map[string]string) *watchEnv {
	t.Helper()
	all := map[string]string{"/etc/hosts": ""}
	maps.Copy(all, files)
	return &watchEnv{t: t, fs: cctest.NewFS(all), n: cctest.NewNotifier(), clock: cctest.NewClock(t0), logs: &records{}}
}

// setVals sets the tree the vals scope returns from the next load on.
func (e *watchEnv) setVals(v cc.Tree) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vals = v
}

// options are the loader options every watch test uses; opts go after them.
func (e *watchEnv) options(opts ...cc.Option) []cc.Option {
	base := []cc.Option{
		cc.WithScope("vals", cc.ValuesFunc(func(context.Context) (cc.Tree, error) {
			e.loads.Add(1)
			e.mu.Lock()
			defer e.mu.Unlock()
			return maps.Clone(e.vals), nil
		})),
		cc.WithScope("sys", cc.FixedDir(sysDir, cc.Pattern{Template: "config.json"}), cc.Trust(cc.TrustSystem)),
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.json"})),
		cc.WithScopes("vals", "sys", "proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat),
		cctest.WithClock(e.clock), cc.WithLogger(slog.New(e.logs)),
	}
	if e.n != nil {
		base = append(base, cc.WithNotifier(e.n.New))
	}
	return append(base, opts...)
}

// loader builds the watched loader.
func (e *watchEnv) loader(opts ...cc.Option) *cc.Loader {
	e.t.Helper()
	return newLoaderFS(e.t, e.fs, nil, e.options(opts...)...)
}

// watch starts watching the tree, recording every change, and closes the watcher when the test ends.
func (e *watchEnv) watch(wopts []cc.WatchOption, opts ...cc.Option) *cc.Watcher[cc.Tree] {
	e.t.Helper()
	w, err := e.loader(opts...).Watch(e.t.Context(), e.changes.handle, wopts...)
	if err != nil {
		e.t.Fatalf("Watch: %v", err)
	}
	if w == nil {
		e.t.Fatalf("Watch returned a nil watcher")
	}
	e.t.Cleanup(func() { _ = w.Close() })
	return w
}

// save writes path and sends its event, as an editor's save would cause.
func (e *watchEnv) save(path, content string) {
	e.write(path, content)
	e.n.Send(path)
}

// write writes path without an event.
func (e *watchEnv) write(path, content string) { e.fs.WriteFile(path, []byte(content), 0o644) }

// eventually waits, in real time, until cond holds, for watcher work that the test can't synchronise with.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: still not true after 5s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// settle lets the quiet period pass.
func (e *watchEnv) settle() { e.clock.Advance(quiet) }

// wantLoads checks how many loads have run, the first one included.
func (e *watchEnv) wantLoads(want int64, when string) {
	e.t.Helper()
	if got := e.loads.Load(); got != want {
		e.t.Errorf("%s: %d loads, want %d", when, got, want)
	}
}

// wantChanges checks how many times the handler has been called, and returns the calls.
func (e *watchEnv) wantChanges(want int, when string) []cc.Change[cc.Tree] {
	e.t.Helper()
	got := e.changes.all()
	if len(got) != want {
		e.t.Errorf("%s: %d handler calls, want %d", when, len(got), want)
	}
	return got
}

// last returns the latest change, failing the test if there's none.
func (e *watchEnv) last() cc.Change[cc.Tree] {
	e.t.Helper()
	got := e.changes.all()
	if len(got) == 0 {
		e.t.Fatalf("the handler was never called")
	}
	return got[len(got)-1]
}

// logged reports whether a record at level with code was logged.
func (e *watchEnv) logged(level slog.Level, code cc.WarningCode) bool {
	e.logs.mu.Lock()
	defer e.logs.mu.Unlock()
	for _, r := range e.logs.got {
		if r.Level == level && (code == "" || attrs(r)["code"] == string(code)) {
			return true
		}
	}
	return false
}

// wantValue checks that tree holds want at a dotted key.
func wantValue(t *testing.T, tree cc.Tree, key string, want any) {
	t.Helper()
	var v any = tree
	for _, k := range strings.Split(key, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			t.Errorf("%s: %v isn't a table", key, v)
			return
		}
		v = m[k]
	}
	if v != want {
		t.Errorf("%s = %#v, want %#v", key, v, want)
	}
}

// within runs f and fails the test if it doesn't return within d, so a deadlock fails clearly.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s didn't return within %v", what, d)
	}
}
