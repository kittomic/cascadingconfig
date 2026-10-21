package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// watchOptionFunc is a WatchOption that runs a function on the watch settings.
type watchOptionFunc func(s *watchSettings)

func (watchOptionFunc) watchOption() {}

// watchSettings are a watcher's options.
type watchSettings struct {
	quiet        time.Duration // WithDebounce
	maxWait      time.Duration // WithDebounce
	pollInterval time.Duration // WithPollInterval
	polling      bool          // WithPolling
	remote       []remoteRule  // WithRemotePoll, in the order given
	anyEvent     bool          // WithReloadOnAnyEvent
}

// The watch defaults (DL-39, DL-41).
const (
	defaultQuiet        = 100 * time.Millisecond
	defaultMaxWait      = time.Second
	defaultPollInterval = 2 * time.Second
)

// newWatchSettings applies opts to the defaults, and checks them: every mistake is an *OptionError, joined.
func newWatchSettings(opts []WatchOption) (watchSettings, error) {
	s := watchSettings{quiet: defaultQuiet, maxWait: defaultMaxWait, pollInterval: defaultPollInterval}
	var errs []error
	fail := func(op, format string, args ...any) {
		errs = append(errs, &OptionError{BaseError: BaseError{Op: op, Err: fmt.Errorf(format, args...)}})
	}
	for _, o := range opts {
		f, ok := o.(watchOptionFunc)
		if !ok {
			fail("Watch", "a nil WatchOption")
			continue
		}
		f(&s)
	}
	switch {
	case s.quiet < 0:
		fail("WithDebounce", "the quiet period must not be negative, not %v", s.quiet)
	case s.maxWait < s.quiet:
		fail("WithDebounce", "maxWait (%v) must be at least the quiet period (%v)", s.maxWait, s.quiet)
	}
	if s.pollInterval <= 0 {
		fail("WithPollInterval", "the interval must be positive, not %v", s.pollInterval)
	}
	for _, r := range s.remote {
		if r.every < 0 {
			fail("WithRemotePoll", "the interval must not be negative, not %v", r.every)
		}
		for _, m := range r.matchers {
			if m == nil {
				fail("WithRemotePoll", "a nil matcher")
			}
		}
	}
	return s, errors.Join(errs...)
}

// remoteInterval returns how often to poll ref: the most recent rule that matches it, or 0 for never (DL-230).
func (s watchSettings) remoteInterval(ref IncludeRef, name string) time.Duration {
	for i := len(s.remote) - 1; i >= 0; i-- {
		r := s.remote[i]
		if len(r.matchers) == 0 || allowed(r.matchers, ref, name) {
			return r.every
		}
	}
	return 0
}

// remoteRule is one WithRemotePoll.
type remoteRule struct {
	every    time.Duration
	matchers []Matcher
}

// WithDebounce sets how events are coalesced (DL-228): each event restarts the quiet period, and a burst reloads no
// later than maxWait after its first event. The default is 100 ms and 1 s. A quiet of 0 reloads on every event. A
// negative quiet, or a maxWait below quiet, is an *OptionError from Watch.
func WithDebounce(quiet, maxWait time.Duration) WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.quiet, s.maxWait = quiet, maxWait })
}

// WithPollInterval sets the interval for all polling: directories whose watch failed, everything when there's no
// notifier, and everything with WithPolling (DL-226). The default is 2 s. An interval that isn't positive is an
// *OptionError from Watch.
func WithPollInterval(d time.Duration) WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.pollInterval = d })
}

// WithPolling polls every watched directory, without using the notifier.
func WithPolling() WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.polling = true })
}

// WithRemotePoll re-checks unpinned remote includes in the background every d (DL-230): those the matchers match, or
// all of them with no matchers. The matchers see the normalised reference, as in allow-lists. When rules overlap, the
// most recently given wins. A d of 0 means never; a negative d is an *OptionError from Watch. Remote polling is off
// unless a rule turns it on.
func WithRemotePoll(d time.Duration, m ...Matcher) WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.remote = append(s.remote, remoteRule{d, slices.Clone(m)}) })
}

// WithReloadOnAnyEvent calls the handler after every reload, not only when the merged values change.
func WithReloadOnAnyEvent() WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.anyEvent = true })
}
