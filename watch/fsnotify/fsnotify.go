package fsnotify

import (
	"sync"

	"github.com/fsnotify/fsnotify"

	"github.com/kittomic/cascadingconfig"
)

// New returns a notifier backed by github.com/fsnotify/fsnotify, for cascadingconfig.WithNotifier(fsnotify.New)
// (DL-232). It drops Chmod-only events, reports lost events (fsnotify.ErrEventOverflow) on Errors, and its Close can be
// called twice.
func New() (cascadingconfig.Notifier, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	n := &notifier{w: w, events: make(chan string), errs: make(chan error), done: make(chan struct{})}
	go n.forward()
	return n, nil
}

type notifier struct {
	w      *fsnotify.Watcher
	events chan string
	errs   chan error
	done   chan struct{}
	once   sync.Once
}

// forward passes fsnotify's events and errors on until the watcher closes, then closes Events and Errors.
func (n *notifier) forward() {
	defer close(n.errs)
	defer close(n.events)
	evs, errs := n.w.Events, n.w.Errors
	for evs != nil || errs != nil {
		select {
		case e, ok := <-evs:
			if !ok {
				evs = nil
				continue
			}
			if e.Op&^fsnotify.Chmod == 0 {
				continue // Spotlight, anti-virus and backup tools make Chmod noise
			}
			select {
			case n.events <- e.Name:
			case <-n.done:
				return
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			select {
			case n.errs <- err:
			case <-n.done:
				return
			}
		case <-n.done:
			return
		}
	}
}

func (n *notifier) Add(dir string) error    { return n.w.Add(dir) }
func (n *notifier) Remove(dir string) error { return n.w.Remove(dir) }
func (n *notifier) Events() <-chan string   { return n.events }
func (n *notifier) Errors() <-chan error    { return n.errs }

// Close stops watching. A second call returns nil.
func (n *notifier) Close() error {
	err := error(nil)
	n.once.Do(func() {
		close(n.done)
		err = n.w.Close()
	})
	return err
}
