package cascadingconfig

import "time"

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

// remoteRule is one WithRemotePoll.
type remoteRule struct {
	every    time.Duration
	matchers []Matcher
}

// WithDebounce sets how events are coalesced (DL-228): each event restarts the quiet period, and a burst reloads no
// later than maxWait after its first event. The default is 100 ms and 1 s. A quiet of 0 reloads on every event. A
// negative quiet, or a maxWait below quiet, is an *OptionError from Watch.
func WithDebounce(quiet, maxWait time.Duration) WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.quiet, s.maxWait = quiet, maxWait }) // stub: no checks
}

// WithPollInterval sets the interval for all polling: directories whose watch failed, everything when there's no
// notifier, and everything with WithPolling (DL-226). The default is 2 s. An interval that isn't positive is an
// *OptionError from Watch.
func WithPollInterval(d time.Duration) WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.pollInterval = d }) // stub: no checks
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
	return watchOptionFunc(func(s *watchSettings) { s.remote = append(s.remote, remoteRule{d, m}) }) // stub: no checks
}

// WithReloadOnAnyEvent calls the handler after every reload, not only when the merged values change.
func WithReloadOnAnyEvent() WatchOption {
	return watchOptionFunc(func(s *watchSettings) { s.anyEvent = true })
}
