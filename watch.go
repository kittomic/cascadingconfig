package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// Watcher keeps a loader's config fresh: it reloads on file changes, debounced, and keeps the last good snapshot when
// a reload fails (design.md: Hot reload). It's safe for concurrent use.
type Watcher[T any] struct {
	l   *Loader
	c   *config
	ctx context.Context
	fn  func(context.Context, Change[T])
	s   watchSettings

	cur atomic.Pointer[snapshot[T]]

	// hctx is the base of every handler call's context, cancelled when the watcher closes (DL-279).
	hctx    context.Context
	hcancel context.CancelFunc

	// run is held for a reload and the handler calls it causes, so handler calls never overlap (DL-223).
	run sync.Mutex
	// inner orders a reload that the running handler call asks for, through its context, with the call's end, so two
	// reloads never run at once; it guards queue (DL-279).
	inner sync.Mutex
	queue []Change[T] // changes waiting for the handler

	mu       sync.Mutex
	closed   bool
	done     chan struct{} // closed by Close: stops the watcher's goroutines
	running  bool          // a handler call is running
	finished chan struct{} // Done: closed once closed and no handler call is running
	ended    bool          // finished is closed
	notifier Notifier
	pollAll  bool               // every directory polls: no notifier, a failed one, or WithPolling
	plan     *watchPlan         // the current watch set
	watched  map[string]bool    // directories added to the notifier
	polled   map[string]dirScan // directories that poll, with their last scan
	warned   map[string]bool    // directories already warned about
	pollT    hooks.Timer
	pending  bool // events are waiting for a reload
	gen, seq int  // the burst and the latest event in it, so a stale timer does nothing
	quietT   hooks.Timer
	maxT     hooks.Timer
	failed   bool // the last reload failed
	remotes  map[string]*remoteState
}

// snapshot is one good load: the decoded value and its result.
type snapshot[T any] struct {
	value T
	res   *Result
}

// Change is what a watch handler receives: after a reload that changed the merged values, after every failed reload,
// and after the first good reload that follows a failure (DL-222).
type Change[T any] struct {
	Old, New T       // decoded snapshots, shared and read-only; New is the last good config if Err != nil
	Result   *Result // the reload's result; a failed one has a nil tree
	Diff     []KeyDiff
	Err      error // non-nil for a failed reload
}

// KeyDiff is one leaf of either tree that differs: a scalar, an array or an empty table (DL-234). A Diff is sorted by
// path, segment by segment, so entries nest when a value changes shape: {"b":{"b":1}} → {"b":0} is "added b" and
// "removed b.b".
type KeyDiff struct {
	Path                 Path
	Kind                 DiffKind
	Old, New             any    // canonical values, deep-copied; nil on the missing side
	OldOrigin, NewOrigin Origin // zero on the missing side
}

// DiffKind says how a leaf changed.
type DiffKind int

const (
	DiffAdded   DiffKind = iota + 1 // the leaf is new
	DiffRemoved                     // the leaf is gone
	DiffChanged                     // the leaf's value changed
)

// Watch loads l's config into a fresh T, then watches for changes until ctx is cancelled or the watcher is closed. A
// failed first load returns a nil watcher and the load's error. fn may be nil; otherwise it's called one change at a
// time, never concurrently.
func Watch[T any](ctx context.Context, l *Loader, fn func(context.Context, Change[T]), opts ...WatchOption) (*Watcher[T], error) {
	s, err := newWatchSettings(opts)
	if err != nil {
		return nil, err
	}
	rec := newWatchRecord()
	res, err := l.load(ctx, rec)
	if err != nil {
		return nil, err
	}
	v, err := decodeFresh[T](res)
	if err != nil {
		return nil, err
	}
	w := &Watcher[T]{
		l: l, c: l.cfg, ctx: ctx, fn: fn, s: s, done: make(chan struct{}), finished: make(chan struct{}),
		watched: map[string]bool{}, polled: map[string]dirScan{}, warned: map[string]bool{}, remotes: map[string]*remoteState{},
	}
	w.hctx, w.hcancel = context.WithCancel(ctx) //nolint:gosec // Close calls hcancel, and a cancelled ctx calls Close
	w.cur.Store(&snapshot[T]{value: v, res: res})
	switch {
	case s.polling:
		w.pollAll = true
		w.logger().LogAttrs(ctx, slog.LevelDebug, "watching by polling, as WithPolling asks", slog.String("op", "polling"))
	case w.c.newNotifier == nil:
		w.pollAll = true
		w.logger().LogAttrs(ctx, slog.LevelDebug, "watching by polling, as no notifier is registered",
			slog.String("op", "polling"))
	default:
		n, err := w.c.newNotifier()
		if err == nil && n == nil {
			err = errors.New("the notifier factory returned nil")
		}
		if err != nil {
			w.pollAll = true
			w.warn(WarnWatchFailed, fmt.Sprintf("the notifier can't be started, so every directory is polled: %v", err))
		} else {
			w.notifier = n
		}
	}
	w.mu.Lock()
	w.applyPlan(planWatch(w.c.fsys, rec))
	w.syncRemotes(rec)
	w.mu.Unlock()
	if w.notifier != nil {
		go w.events(w.notifier)
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = w.Close()
		case <-w.done:
		}
	}()
	return w, nil
}

// Watch is Watch for the merged tree, without decoding.
func (l *Loader) Watch(ctx context.Context, fn func(context.Context, Change[Tree]), opts ...WatchOption) (*Watcher[Tree], error) {
	return Watch(ctx, l, fn, opts...)
}

// decodeFresh decodes res into a fresh T, or copies its tree for a Tree (DL-72).
func decodeFresh[T any](res *Result) (T, error) {
	var v T
	if p, ok := any(&v).(*Tree); ok {
		*p = res.Tree()
		return v, nil
	}
	if err := res.Decode(&v); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// Current returns the latest good snapshot, without locking. It's shared with every caller and with the handler, so
// it must be treated as read-only (DL-220).
func (w *Watcher[T]) Current() T {
	return w.cur.Load().value
}

// Reload forces a reload, which also re-reads env vars and Values functions, and takes any pending events with it. It
// returns the reload's error. Called with the context a running handler call received, or one derived from it, the
// handler call the reload causes follows the current one; called with any other context, Reload also waits for that
// call, so a handler that reloads must pass its own context (DL-279). After Close, or once ctx is cancelled, it's a
// *BaseError with CodeCanceled.
func (w *Watcher[T]) Reload(ctx context.Context) error {
	if err := w.stopped(); err != nil {
		return err
	}
	if call, ok := ctx.Value(handlerKey{}).(*handlerCall); ok && call.w == any(w) {
		w.inner.Lock()
		if call.active {
			defer w.inner.Unlock()
			w.mu.Lock()
			w.clearPending()
			w.mu.Unlock()
			ch, notify, err := w.reload(ctx)
			if notify && w.fn != nil {
				w.queue = append(w.queue, ch)
			}
			return err
		}
		w.inner.Unlock() // the call has ended, so this is an outside Reload
	}
	w.run.Lock()
	defer w.run.Unlock()
	if err := w.stopped(); err != nil {
		return err
	}
	w.mu.Lock()
	w.clearPending()
	w.mu.Unlock()
	ch, notify, err := w.reload(ctx)
	if notify {
		w.deliver(ch)
	}
	return err
}

// Close stops watching. It doesn't wait for a running handler call: once it returns, no new call starts, and Done
// closes when the running one returns, so Close can be called from the handler (DL-279). It cancels the handlers'
// context, and returns the notifier's Close error the first time, and nil after that.
func (w *Watcher[T]) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.done)
	w.endIfIdle()
	w.clearPending()
	if w.pollT != nil {
		w.pollT.Stop()
		w.pollT = nil
	}
	for ref, st := range w.remotes {
		if st.timer != nil {
			st.timer.Stop()
		}
		delete(w.remotes, ref)
	}
	n := w.notifier
	w.mu.Unlock()
	w.hcancel()
	if n != nil {
		return n.Close()
	}
	return nil
}

// Done returns a channel that's closed once the watcher has closed, by Close or its context, and its last handler
// call has returned (DL-279).
func (w *Watcher[T]) Done() <-chan struct{} { return w.finished }

// endIfIdle closes finished once the watcher is closed and no handler call is running. The caller holds mu.
func (w *Watcher[T]) endIfIdle() {
	if w.closed && !w.running && !w.ended {
		w.ended = true
		close(w.finished)
	}
}

// stopped returns the error for a watcher that's closed or whose ctx is done.
func (w *Watcher[T]) stopped() error {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed || w.ctx.Err() != nil {
		return NewError(CodeCanceled, "watch", "", errors.New("the watcher is closed"))
	}
	return nil
}

// reload loads again, decodes, re-plans the watch set and returns the change, and whether the handler should see it
// (DL-222). The caller holds run.
func (w *Watcher[T]) reload(ctx context.Context) (Change[T], bool, error) {
	w.debug("reload", "reloading")
	rec := newWatchRecord()
	res, err := w.l.load(ctx, rec)
	var v T
	if err == nil {
		v, err = decodeFresh[T](res)
	}
	old := w.cur.Load()
	if err != nil {
		w.mu.Lock()
		w.applyPlan(planWatch(w.c.fsys, rec).union(w.plan))
		w.failed = true
		w.mu.Unlock()
		if w.ctx.Err() != nil || ctx.Err() != nil {
			return Change[T]{}, false, err
		}
		w.warn(WarnReloadFailed, fmt.Sprintf("a reload failed, so the last good config is kept: %v", err))
		return Change[T]{Old: old.value, New: old.value, Result: res, Err: err}, true, err
	}
	w.mu.Lock()
	recovered := w.failed
	w.failed = false
	w.applyPlan(planWatch(w.c.fsys, rec))
	w.syncRemotes(rec)
	w.mu.Unlock()
	changed := !equalValues(old.res.tree, res.tree)
	w.cur.Store(&snapshot[T]{value: v, res: res})
	ch := Change[T]{Old: old.value, New: v, Result: res}
	if changed {
		ch.Diff = diffResults(old.res, res)
	}
	w.debug("reloaded", "reloaded", slog.Int("changes", len(ch.Diff)))
	return ch, changed || recovered || w.s.anyEvent, nil
}

// deliver calls the handler with ch, then with any changes that reloads the calls asked for queued. The caller holds
// run.
func (w *Watcher[T]) deliver(ch Change[T]) {
	if w.fn == nil {
		return
	}
	w.inner.Lock()
	w.queue = append(w.queue, ch)
	w.inner.Unlock()
	for {
		w.inner.Lock()
		if len(w.queue) == 0 {
			w.inner.Unlock()
			return
		}
		next := w.queue[0]
		w.queue = w.queue[1:]
		w.inner.Unlock()

		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			w.inner.Lock()
			w.queue = nil
			w.inner.Unlock()
			return
		}
		w.running = true
		w.mu.Unlock()
		w.call(next)
		w.mu.Lock()
		w.running = false
		w.endIfIdle()
		w.mu.Unlock()
	}
}

// handlerCall marks a context as a handler call's (DL-279). Reload recognises it while the call runs.
type handlerCall struct {
	w      any
	active bool // guarded by the watcher's inner
}

type handlerKey struct{}

// call runs the handler once, with a context marked as this call's, recovering a panic, which is logged at error level
// (DL-77).
func (w *Watcher[T]) call(ch Change[T]) {
	marker := &handlerCall{w: w, active: true}
	defer func() {
		w.inner.Lock()
		marker.active = false
		w.inner.Unlock()
	}()
	defer func() {
		if r := recover(); r != nil {
			w.logger().LogAttrs(w.ctx, slog.LevelError, fmt.Sprintf("the watch handler panicked: %v", r))
		}
	}()
	w.fn(context.WithValue(w.hctx, handlerKey{}, marker), ch)
}

// events reads the notifier until it stops or the watcher closes.
func (w *Watcher[T]) events(n Notifier) {
	evs, errs := n.Events(), n.Errors()
	ack := func() {
		if h, ok := n.(hooks.Handled); ok {
			h.Handled()
		}
	}
	for evs != nil {
		select {
		case p, ok := <-evs:
			if !ok {
				evs = nil
				w.notifierStopped()
				continue
			}
			w.event(p)
			ack()
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			w.notifierError(err)
			ack()
		case <-w.done:
			return
		}
	}
}

// event handles a notifier event for p.
func (w *Watcher[T]) event(p string) {
	if w.ctx.Err() != nil {
		return
	}
	w.debug("event", "file event", slog.String("path", p))
	w.mu.Lock()
	if w.closed || w.plan == nil || !w.plan.relevant(w.c.fsys, p) {
		w.mu.Unlock()
		return
	}
	now := w.markPending()
	w.mu.Unlock()
	if now {
		w.fire(-1, -1)
	}
}

// notifierError warns about a notifier error and reloads, because events may have been lost (DL-225).
func (w *Watcher[T]) notifierError(err error) {
	w.warn(WarnWatchFailed, fmt.Sprintf("the notifier failed, so the config is reloaded in case events were lost: %v", err))
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	now := w.markPending()
	w.mu.Unlock()
	if now {
		w.fire(-1, -1)
	}
}

// notifierStopped makes every directory poll, because the notifier closed its Events channel (DL-225).
func (w *Watcher[T]) notifierStopped() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.warn(WarnWatchFailed, "the notifier stopped, so every directory is polled")
	w.pollAll = true
	w.watched = map[string]bool{}
	w.applyPlan(w.plan)
}

// markPending records an event: it starts or extends the burst's timers, or says to reload now when the quiet period
// is 0 (DL-228). The caller holds mu.
func (w *Watcher[T]) markPending() bool {
	if w.s.quiet == 0 {
		w.pending = true
		return true
	}
	if !w.pending {
		w.pending = true
		w.gen++
		gen := w.gen
		w.maxT = w.c.clock.AfterFunc(w.s.maxWait, func() { w.fire(gen, -1) })
	}
	if w.quietT != nil {
		w.quietT.Stop()
	}
	w.seq++
	gen, seq := w.gen, w.seq
	w.quietT = w.c.clock.AfterFunc(w.s.quiet, func() { w.fire(gen, seq) })
	return false
}

// clearPending forgets pending events and stops their timers. The caller holds mu.
func (w *Watcher[T]) clearPending() {
	w.pending = false
	w.gen++
	for _, t := range []hooks.Timer{w.quietT, w.maxT} {
		if t != nil {
			t.Stop()
		}
	}
	w.quietT, w.maxT = nil, nil
}

// fire reloads for pending events, unless the timer that called it is stale: gen names its burst, and seq the event
// whose quiet period ended, or -1.
func (w *Watcher[T]) fire(gen, seq int) {
	w.run.Lock()
	defer w.run.Unlock()
	w.mu.Lock()
	stale := (gen >= 0 && gen != w.gen) || (seq >= 0 && seq != w.seq)
	if w.closed || !w.pending || stale || w.ctx.Err() != nil {
		w.mu.Unlock()
		return
	}
	w.clearPending()
	w.mu.Unlock()
	ch, notify, _ := w.reload(w.ctx)
	if notify {
		w.deliver(ch)
	}
}

// applyPlan makes pl the watch set: directories are added to or removed from the notifier, or polled (DL-225,
// DL-226). Every polled directory is scanned again, as the reload that made pl saw it. The caller holds mu.
func (w *Watcher[T]) applyPlan(pl *watchPlan) {
	if pl == nil {
		return
	}
	w.plan = pl
	for dir := range w.watched {
		if _, ok := pl.dirs[dir]; !ok || w.pollAll {
			if w.notifier != nil && !w.pollAll {
				_ = w.notifier.Remove(dir)
			}
			delete(w.watched, dir)
		}
	}
	polled := map[string]dirScan{}
	for dir := range pl.dirs {
		if w.watched[dir] {
			continue
		}
		if !w.pollAll && w.notifier != nil {
			err := w.notifier.Add(dir)
			if err == nil {
				w.watched[dir] = true
				continue
			}
			if !w.warned[dir] {
				w.warned[dir] = true
				w.warn(WarnWatchFailed, fmt.Sprintf("%s can't be watched, so it's polled: %v", dir, err))
			}
		}
		polled[dir] = pl.scan(w.c.fsys, dir, w.c.maxFileSize)
	}
	w.polled = polled
	w.debug("watch-set", "watch set planned", slog.Int("dirs", len(pl.dirs)))
	switch {
	case len(w.polled) == 0 && w.pollT != nil:
		w.pollT.Stop()
		w.pollT = nil
	case len(w.polled) > 0 && w.pollT == nil:
		w.armPoll()
	}
}

// armPoll sets the next poll. The caller holds mu.
func (w *Watcher[T]) armPoll() {
	w.pollT = w.c.clock.AfterFunc(w.s.pollInterval, w.poll)
}

// poll scans every polled directory, and records an event if any changed (DL-226).
func (w *Watcher[T]) poll() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	changed := false
	for dir, prev := range w.polled {
		now := w.plan.scan(w.c.fsys, dir, w.c.maxFileSize)
		if !prev.same(w.c.fsys, now) {
			changed = true
		}
		w.polled[dir] = now
	}
	now := false
	if changed {
		now = w.markPending()
	}
	w.pollT = nil
	if len(w.polled) > 0 {
		w.armPoll()
	}
	w.mu.Unlock()
	if now {
		w.fire(-1, -1)
	}
}

func (w *Watcher[T]) logger() *slog.Logger { return w.c.log() }

// debug logs a watcher trace at Debug, with its op (DL-263).
func (w *Watcher[T]) debug(op, msg string, attrs ...slog.Attr) {
	w.logger().LogAttrs(w.ctx, slog.LevelDebug, msg, append([]slog.Attr{slog.String("op", op)}, attrs...)...)
}

// warn logs a watcher warning, with its code as an attribute, since the watcher has no report (DL-227).
func (w *Watcher[T]) warn(code WarningCode, msg string) {
	w.logger().LogAttrs(w.ctx, levelOf(code), msg, slog.String("code", string(code)))
}
