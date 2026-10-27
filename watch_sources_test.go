// These tests use Unix paths in cctest's world, which aren't absolute on Windows, so the file doesn't build there
// (DL-282).

//go:build !windows

package cascadingconfig_test

import (
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// pollInterval is the default polling interval, DL-39.
const pollInterval = 2 * time.Second

// DL-225: the notifier factory is called once per watcher, and each watcher's Close closes its own notifier.
func TestNotifierPerWatcher(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	var mu sync.Mutex
	var made []*cctest.Notifier
	factory := func() (cc.Notifier, error) {
		mu.Lock()
		defer mu.Unlock()
		n := cctest.NewNotifier()
		made = append(made, n)
		return n.New()
	}
	l := e.loader(cc.WithNotifier(factory))
	w1, err := l.Watch(t.Context(), nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	w2, err := l.Watch(t.Context(), nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w2.Close()
	if len(made) != 2 {
		t.Fatalf("the factory was called %d times for two watchers, want 2", len(made))
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	within(t, 5*time.Second, "Send to the closed watcher's notifier", func() { made[0].Send(rootCfg) })
	e.write(rootCfg, `{"a": 2}`)
	made[1].Send(rootCfg)
	e.settle()
	wantValue(t, w2.Current(), "a", int64(2))
	wantValue(t, w1.Current(), "a", int64(1))
}

// DL-225, DL-227: a factory that fails makes everything poll, with WarnWatchFailed, and Watch still succeeds.
func TestNotifierFactoryFails(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil, cc.WithNotifier(func() (cc.Notifier, error) { return nil, errors.New("no inotify instances left") }))
	if !e.logged(slog.LevelWarn, cc.WarnWatchFailed) {
		t.Errorf("no %s warning was logged", cc.WarnWatchFailed)
	}
	e.write(rootCfg, `{"a": 2}`)
	e.clock.Advance(pollInterval)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// DL-225, DL-227: a directory whose Add fails is polled, with WarnWatchFailed; the others still use events.
func TestNotifierAddFails(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`, sysFile: `{"b": 1}`})
	e.n.FailAdd(sysDir, errors.New("too many open files"))
	w := e.watch(nil)
	if !e.logged(slog.LevelWarn, cc.WarnWatchFailed) {
		t.Errorf("no %s warning was logged", cc.WarnWatchFailed)
	}
	e.wantWatched("with /etc/myapp failing", work)
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
	e.write(sysFile, `{"b": 22}`)
	e.clock.Advance(pollInterval)
	e.settle()
	wantValue(t, w.Current(), "b", int64(22))
}

// DL-225, DL-227: an error from the notifier is a warning and a reload, because events may have been lost.
func TestNotifierError(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	e.write(rootCfg, `{"a": 2}`)
	e.n.SendError(errors.New("event queue overflow"))
	if !e.logged(slog.LevelWarn, cc.WarnWatchFailed) {
		t.Errorf("no %s warning was logged", cc.WarnWatchFailed)
	}
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// DL-225, DL-227: a notifier that stops, closing its Events channel, makes the whole set poll, with a warning.
func TestNotifierStops(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	if err := e.n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	eventually(t, "a poll timer after the notifier stopped", func() bool { return e.clock.Timers() > 0 })
	if !e.logged(slog.LevelWarn, cc.WarnWatchFailed) {
		t.Errorf("no %s warning was logged", cc.WarnWatchFailed)
	}
	e.write(rootCfg, `{"a": 2}`)
	e.clock.Advance(pollInterval)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// DL-226: without a notifier, everything is polled every 2 s: edits, same-size edits, new files and removals are seen,
// and nothing is warned.
func TestPollingWithoutNotifier(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.n = nil
	w := e.watch(nil)
	e.write(rootCfg, `{"a": 10}`)
	e.clock.Advance(pollInterval - time.Millisecond)
	e.wantLoads(1, "before the first poll")
	e.clock.Advance(time.Millisecond)
	e.settle()
	wantValue(t, w.Current(), "a", int64(10))

	for _, step := range []struct {
		name string
		do   func()
		key  string
		want any
	}{
		{"a same-size edit", func() { e.write(rootCfg, `{"a": 20}`) }, "a", int64(20)},
		{"a new file", func() { e.write(sysFile, `{"b": 1}`) }, "b", int64(1)},
		{"a removal", func() { e.fs.Remove(sysFile) }, "b", nil},
	} {
		step.do()
		e.clock.Advance(pollInterval)
		e.settle()
		wantValue(t, w.Current(), step.key, step.want)
	}
	if e.logged(slog.LevelWarn, cc.WarnWatchFailed) {
		t.Errorf("polling without a notifier was warned about")
	}
}

// DL-226: polling finds nothing to reload when nothing changed.
func TestPollingIdle(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.n = nil
	e.watch(nil)
	e.clock.Advance(10 * pollInterval)
	e.wantLoads(1, "ten polls of unchanged files")
}

// DL-226: WithPolling polls everything despite a notifier, at WithPollInterval's pace.
func TestWithPolling(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch([]cc.WatchOption{cc.WithPolling(), cc.WithPollInterval(500 * time.Millisecond)})
	if got := e.n.Watched(); len(got) != 0 {
		t.Errorf("WithPolling watched %q through the notifier", got)
	}
	e.write(rootCfg, `{"a": 2}`)
	e.clock.Advance(500 * time.Millisecond)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// DL-226: WithPollInterval sets the fallback's interval too.
func TestPollIntervalFallback(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.n.FailAdd(work, errors.New("no watch"))
	w := e.watch([]cc.WatchOption{cc.WithPollInterval(300 * time.Millisecond)})
	e.write(rootCfg, `{"a": 2}`)
	e.clock.Advance(300 * time.Millisecond)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}
