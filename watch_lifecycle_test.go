package cascadingconfig_test

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-220: a failed first load returns a nil watcher and the load's error.
func TestWatchFirstLoadFails(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": `})
	w, err := e.loader().Watch(t.Context(), nil)
	if w != nil {
		_ = w.Close()
		t.Errorf("Watch returned a watcher")
	}
	wantErr[*cc.ParseError](t, err, cc.CodeBadRequest, rootCfg)
	typed, err := cc.Watch[watchCfg](t.Context(), e.loader(), nil)
	if typed != nil || err == nil {
		t.Errorf("Watch[T] = %v, %v; want nil and an error", typed, err)
	}
}

// DL-220: a nil handler is allowed, and Current still follows the files.
func TestWatchNilHandler(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w, err := e.loader().Watch(t.Context(), nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// DL-77, DL-220: cancelling ctx stops watching: the notifier is closed, Reload is a 499, Current keeps the last
// snapshot, and Close still works.
func TestWatchCancel(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	ctx, cancel := context.WithCancel(t.Context())
	w, err := e.loader().Watch(ctx, e.changes.handle)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	err = w.Reload(t.Context())
	wantErr[*cc.BaseError](t, err, cc.CodeCanceled, "")
	within(t, 5*time.Second, "Send after cancelling", func() { e.save(rootCfg, `{"a": 2}`) })
	e.settle()
	e.wantLoads(1, "after cancelling")
	wantValue(t, w.Current(), "a", int64(1))
	within(t, 5*time.Second, "Close", func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

// DL-77, DL-220: Close is idempotent; after it, Reload is a 499 and Current keeps the last snapshot.
func TestWatchCloseTwice(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	if err := w.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	wantErr[*cc.BaseError](t, w.Reload(t.Context()), cc.CodeCanceled, "")
	wantValue(t, w.Current(), "a", int64(1))
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	e.wantLoads(1, "after Close")
}

// DL-77, DL-223: Close from inside the handler returns without waiting for the handler, so it doesn't deadlock.
func TestWatchCloseFromHandler(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	var w *cc.Watcher[cc.Tree]
	closed := make(chan error, 1)
	w, err := e.loader().Watch(t.Context(), func(context.Context, cc.Change[cc.Tree]) { closed <- w.Close() })
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	within(t, 5*time.Second, "a reload whose handler calls Close", func() {
		e.save(rootCfg, `{"a": 2}`)
		e.settle()
	})
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	default:
		t.Fatalf("the handler wasn't called")
	}
	wantErr[*cc.BaseError](t, w.Reload(t.Context()), cc.CodeCanceled, "")
}

// DL-223, DL-279: Reload from inside the handler, with the handler's context, runs the reload and returns its error;
// the change it causes is delivered after the current call returns.
func TestWatchReloadFromHandler(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	var w *cc.Watcher[cc.Tree]
	var calls atomic.Int64
	var reloadErr error
	w, err := e.loader().Watch(t.Context(), func(ctx context.Context, c cc.Change[cc.Tree]) {
		e.changes.add(c)
		if calls.Add(1) == 1 {
			e.setVals(cc.Tree{"v": int64(1)})
			reloadErr = w.Reload(ctx)
			if got := len(e.changes.all()); got != 1 {
				t.Errorf("inside the handler, Reload delivered its change before the handler returned")
			}
		}
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	within(t, 5*time.Second, "a reload whose handler calls Reload", func() {
		e.save(rootCfg, `{"a": 2}`)
		e.settle()
	})
	if reloadErr != nil {
		t.Errorf("Reload from the handler: %v", reloadErr)
	}
	within(t, 5*time.Second, "an outside Reload", func() {
		if err := w.Reload(t.Context()); err != nil {
			t.Errorf("Reload: %v", err)
		}
	})
	got := e.wantChanges(2, "the save, then the handler's Reload")
	if len(got) == 2 {
		wantValue(t, got[1].New, "v", int64(1))
	}
}

// DL-223: Reload from outside the handler waits for the reload and the handler call it causes; it also takes pending
// events with it, so the debounce timer doesn't reload again.
func TestWatchReloadWaits(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	e.save(rootCfg, `{"a": 2}`)
	if err := w.Reload(t.Context()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	e.wantChanges(1, "when Reload returns")
	e.wantLoads(2, "when Reload returns")
	e.clock.Advance(10 * maxWait)
	e.wantLoads(2, "after the debounce would have fired")
}

// DL-77, DL-223, design.md Hot reload: a manual Reload re-reads Values functions, which can't be watched.
func TestWatchReloadValues(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{}`})
	w := e.watch(nil)
	e.setVals(cc.Tree{"flag": true})
	e.clock.Advance(time.Hour)
	e.wantLoads(1, "a changed Values function, with no reload")
	if err := w.Reload(t.Context()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	wantValue(t, w.Current(), "flag", true)
}

// DL-77: a panicking handler is recovered and logged at error level, and watching continues.
func TestWatchHandlerPanic(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	var calls atomic.Int64
	w, err := e.loader().Watch(t.Context(), func(context.Context, cc.Change[cc.Tree]) {
		if calls.Add(1) == 1 {
			panic("boom")
		}
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	if !e.logged(slog.LevelError, "") {
		t.Errorf("the panic wasn't logged at error level")
	}
	e.save(rootCfg, `{"a": 3}`)
	e.settle()
	if got := calls.Load(); got != 2 {
		t.Errorf("handler called %d times, want 2", got)
	}
	wantValue(t, w.Current(), "a", int64(3))
}

// DL-40, DL-223: handler calls never overlap, even with Reloads from several goroutines while events arrive.
func TestWatchSerialHandler(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 0}`})
	var active, most, calls atomic.Int64
	w, err := e.loader().Watch(t.Context(), func(context.Context, cc.Change[cc.Tree]) {
		n := active.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		calls.Add(1)
		time.Sleep(time.Millisecond)
		active.Add(-1)
	}, cc.WithDebounce(0, 0))
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			e.setVals(cc.Tree{"g": int64(i)})
			if err := w.Reload(t.Context()); err != nil {
				t.Errorf("Reload: %v", err)
			}
		})
	}
	for i := range 8 {
		e.save(rootCfg, `{"a": `+string(rune('1'+i))+`}`)
	}
	wg.Wait()
	if most.Load() != 1 {
		t.Errorf("up to %d handler calls ran at once, want 1", most.Load())
	}
	if calls.Load() == 0 {
		t.Errorf("the handler was never called")
	}
}
