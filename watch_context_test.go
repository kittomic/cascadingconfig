package cascadingconfig_test

import (
	"context"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// derivedKey is a context key, for a context derived from the handler's.
type derivedKey struct{}

// closedWithin reports whether ch is closed within d.
func closedWithin(ch <-chan struct{}, d time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

// DL-279: the handler's context is live while it runs, and cancelled once the watcher closes.
func TestWatchHandlerContext(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	got := make(chan context.Context, 1)
	w, err := e.loader().Watch(t.Context(), func(ctx context.Context, _ cc.Change[cc.Tree]) {
		if ctx.Err() != nil {
			t.Errorf("the handler's context is already done: %v", ctx.Err())
		}
		got <- ctx
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	ctx := <-got
	if ctx.Err() != nil {
		t.Errorf("the handler's context is done before Close: %v", ctx.Err())
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("the handler's context is live after Close")
	}
}

// DL-279: Close doesn't wait for a running handler; once it returns no new handler call starts, and Done closes after
// the running call returns.
func TestWatchCloseDoesntWait(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	running, release := make(chan struct{}), make(chan struct{})
	calls := 0
	w, err := e.loader().Watch(t.Context(), func(context.Context, cc.Change[cc.Tree]) {
		calls++
		close(running)
		<-release
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	go func() {
		e.save(rootCfg, `{"a": 2}`)
		e.settle() // returns once the handler does
	}()
	<-running
	within(t, 5*time.Second, "Close while the handler runs", func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if closedWithin(w.Done(), 50*time.Millisecond) {
		t.Error("Done closed while the handler was still running")
	}
	close(release)
	if !closedWithin(w.Done(), 5*time.Second) {
		t.Fatal("Done didn't close after the handler returned")
	}
	e.save(rootCfg, `{"a": 3}`)
	e.settle()
	if calls != 1 {
		t.Errorf("the handler ran %d times, want 1: none after Close", calls)
	}
}

// DL-279: Done closes once the watcher has closed, whether by Close with no handler running, or by its context being
// cancelled.
func TestWatchDone(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	if closedWithin(w.Done(), 50*time.Millisecond) {
		t.Error("Done closed before Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closedWithin(w.Done(), 5*time.Second) {
		t.Error("Done didn't close after Close")
	}

	ctx, cancel := context.WithCancel(t.Context())
	w2, err := e.loader().Watch(ctx, nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	if !closedWithin(w2.Done(), 5*time.Second) {
		t.Error("Done didn't close after the watch's context was cancelled")
	}
}

// DL-279: the handler's context marks the call as the handler's, wherever it's used: a goroutine the handler starts can
// Reload with it, and the reload's handler call follows the current one instead of waiting for it, so the handler can
// wait for that goroutine without deadlocking.
func TestWatchReloadWithHandlerContextElsewhere(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	var w *cc.Watcher[cc.Tree]
	calls := 0
	w, err := e.loader().Watch(t.Context(), func(ctx context.Context, c cc.Change[cc.Tree]) {
		e.changes.add(c)
		calls++
		if calls > 1 {
			return
		}
		e.setVals(cc.Tree{"v": int64(1)})
		done := make(chan error)
		go func() { done <- w.Reload(context.WithValue(ctx, derivedKey{}, "derived")) }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Reload with the handler's context: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Reload with the handler's context, from another goroutine, waited for the handler")
		}
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	within(t, 10*time.Second, "a handler that reloads from a goroutine", func() {
		e.save(rootCfg, `{"a": 2}`)
		e.settle()
	})
	got := e.wantChanges(2, "the save, then the reload the handler's goroutine asked for")
	if len(got) == 2 {
		wantValue(t, got[1].New, "v", int64(1))
	}
}
