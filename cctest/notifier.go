package cctest

import (
	"slices"
	"sync"

	"github.com/kittomic/cascadingconfig"
)

// Notifier is a fake cascadingconfig.Notifier whose events the test sends (DL-231). Pass its New method to
// cascadingconfig.WithNotifier. It's safe for concurrent use.
//
// Send and SendError return only once the watcher has finished with what they sent, including any reload that follows
// straight away, so a test's next Advance can't race them. Both return at once if the notifier is closed.
type Notifier struct {
	mu      sync.Mutex
	in      chan string // Send's events, forwarded to events
	inErrs  chan error  // SendError's errors, forwarded to errs
	events  chan string
	errs    chan error
	handled chan struct{}
	done    chan struct{}
	closed  bool
	watched map[string]bool
	failAdd map[string]error
}

// NewNotifier returns an open notifier.
func NewNotifier() *Notifier {
	n := &Notifier{watched: map[string]bool{}, failAdd: map[string]error{}}
	n.open()
	return n
}

func (n *Notifier) open() {
	n.in, n.inErrs = make(chan string), make(chan error)
	n.events, n.errs = make(chan string), make(chan error)
	n.handled, n.done = make(chan struct{}), make(chan struct{})
	n.closed = false
	go forward(n.in, n.events, n.done)
	go forward(n.inErrs, n.errs, n.done)
}

// forward passes values from in to out until done is closed, then closes out. Only it closes out, so a Send racing
// Close can't write to a closed channel.
func forward[V any](in <-chan V, out chan<- V, done <-chan struct{}) {
	defer close(out)
	for {
		select {
		case v := <-in:
			select {
			case out <- v:
			case <-done:
				return
			}
		case <-done:
			return
		}
	}
}

// New returns the notifier itself, as a factory for WithNotifier. A closed notifier is opened again, with no watched
// directories.
func (n *Notifier) New() (cascadingconfig.Notifier, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		n.open()
		n.watched = map[string]bool{}
	}
	return n, nil
}

// Send reports a change to path, and waits until the watcher has handled it.
func (n *Notifier) Send(path string) {
	n.mu.Lock()
	in, handled, done := n.in, n.handled, n.done
	n.mu.Unlock()
	select {
	case in <- path:
	case <-done:
		return
	}
	select {
	case <-handled:
	case <-done:
	}
}

// SendError reports err on the Errors channel, and waits until the watcher has handled it.
func (n *Notifier) SendError(err error) {
	n.mu.Lock()
	in, handled, done := n.inErrs, n.handled, n.done
	n.mu.Unlock()
	select {
	case in <- err:
	case <-done:
		return
	}
	select {
	case <-handled:
	case <-done:
	}
}

// FailAdd makes Add(dir) fail with err from now on.
func (n *Notifier) FailAdd(dir string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failAdd[dir] = err
}

// Watched returns the watched directories, sorted.
func (n *Notifier) Watched() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	dirs := make([]string, 0, len(n.watched))
	for d := range n.watched {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	return dirs
}

// Add watches dir, unless FailAdd made it fail.
func (n *Notifier) Add(dir string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.failAdd[dir]; err != nil {
		return err
	}
	n.watched[dir] = true
	return nil
}

// Remove stops watching dir.
func (n *Notifier) Remove(dir string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.watched, dir)
	return nil
}

// Events returns the channel Send writes to.
func (n *Notifier) Events() <-chan string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.events
}

// Errors returns the channel SendError writes to.
func (n *Notifier) Errors() <-chan error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.errs
}

// Close closes the Events and Errors channels. A test can call it to simulate a notifier that stops. Closing twice is
// fine.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.closed {
		n.closed = true
		close(n.done)
	}
	return nil
}

// Handled is called by the watcher when it has finished with an event or error.
func (n *Notifier) Handled() {
	n.mu.Lock()
	handled, done := n.handled, n.done
	n.mu.Unlock()
	select {
	case handled <- struct{}{}:
	case <-done:
	}
}
