package cascadingconfig

import (
	"context"
	"errors"
)

// Watcher keeps a loader's config fresh: it reloads on file changes, debounced, and keeps the last good snapshot when
// a reload fails (design.md: Hot reload). It's safe for concurrent use.
type Watcher[T any] struct {
	_ [0]T
}

// Change is what a watch handler receives: after a reload that changed the merged values, after every failed reload,
// and after the first good reload that follows a failure (DL-222).
type Change[T any] struct {
	Old, New T       // decoded snapshots, shared and read-only; New is the last good config if Err != nil
	Result   *Result // the reload's result; a failed one has a nil tree
	Diff     []KeyDiff
	Err      error // non-nil for a failed reload
}

// KeyDiff is one changed key (DL-221). Only leaves are listed, and an array is one leaf.
type KeyDiff struct {
	Path                 Path
	Kind                 DiffKind
	Old, New             any    // canonical values, deep-copied; nil on the missing side
	OldOrigin, NewOrigin Origin // zero on the missing side
}

// DiffKind says how a key changed.
type DiffKind int

const (
	DiffAdded   DiffKind = iota + 1 // the key is new
	DiffRemoved                     // the key is gone
	DiffChanged                     // the key's value changed, including a table becoming a scalar or the reverse
)

func (k DiffKind) String() string { return "" } // stub

// String describes the change by its kind, path and the files its old and new values came from. It never shows
// values, because config often holds secrets.
func (d KeyDiff) String() string { return "" } // stub

// Watch loads l's config into a fresh T, then watches for changes until ctx is cancelled or the watcher is closed. A
// failed first load returns a nil watcher and the load's error. fn may be nil; otherwise it's called one change at a
// time, never concurrently.
func Watch[T any](ctx context.Context, l *Loader, fn func(Change[T]), opts ...WatchOption) (*Watcher[T], error) {
	_, _, _, _ = ctx, l, fn, opts
	return nil, errors.New("Watch: not implemented") // stub
}

// Watch is Watch for the merged tree, without decoding.
func (l *Loader) Watch(ctx context.Context, fn func(Change[Tree]), opts ...WatchOption) (*Watcher[Tree], error) {
	return Watch(ctx, l, fn, opts...)
}

// Current returns the latest good snapshot, without locking. It's shared with every caller and with the handler, so
// it must be treated as read-only (DL-220).
func (w *Watcher[T]) Current() T {
	var zero T
	return zero // stub
}

// Reload forces a reload, which also re-reads env vars and Values functions, and takes any pending events with it. It
// returns the reload's error. From any goroutine but the handler's, it also waits for the handler call the reload
// causes; from the handler's, that call follows the current one (DL-223). After Close, or once ctx is cancelled, it's a
// *BaseError with CodeCanceled.
func (w *Watcher[T]) Reload(ctx context.Context) error {
	_ = ctx
	return errors.New("Reload: not implemented") // stub
}

// Close stops watching. It waits for a running handler to return, except when called from the handler itself. It
// returns the notifier's Close error the first time, and nil after that.
func (w *Watcher[T]) Close() error {
	return nil // stub
}
