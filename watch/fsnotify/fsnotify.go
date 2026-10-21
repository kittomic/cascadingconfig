package fsnotify

import (
	"errors"

	"github.com/kittomic/cascadingconfig"
)

// New returns a notifier backed by github.com/fsnotify/fsnotify, for cascadingconfig.WithNotifier(fsnotify.New)
// (DL-232). It drops Chmod-only events, reports lost events (fsnotify.ErrEventOverflow) on Errors, and its Close can be
// called twice.
func New() (cascadingconfig.Notifier, error) {
	return nil, errors.New("fsnotify: not implemented") // stub
}
