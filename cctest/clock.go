package cctest

import (
	"sort"
	"sync"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// Clock is a fake clock for TTLs, debounce and polling. It only moves when the test moves it. It's safe for concurrent
// use.
//
// Timers fire only inside Advance and Set, synchronously and in time order, with the clock showing each timer's time
// while it fires. The watcher's timers return only once the watcher has handled them, including any reload and handler
// call, so when Advance returns, everything that fell due has happened (DL-231).
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*timer
	seq    int
	firing sync.Mutex // one Advance or Set fires timers at a time
}

// NewClock returns a clock that starts at t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d, firing every timer that falls due. Advance(0) fires the timers already due.
func (c *Clock) Advance(d time.Duration) {
	c.firing.Lock()
	defer c.firing.Unlock()
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	c.fireUntil(target)
}

// Set moves the clock to t. Moving forward fires every timer that falls due; moving back fires none.
func (c *Clock) Set(t time.Time) {
	c.firing.Lock()
	defer c.firing.Unlock()
	c.mu.Lock()
	if t.Before(c.now) {
		c.now = t
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.fireUntil(t)
}

// fireUntil fires the due timers one at a time, earliest first, then leaves the clock at target. A timer that fires
// can start another, which fires too if it falls due by target.
func (c *Clock) fireUntil(target time.Time) {
	for {
		c.mu.Lock()
		sort.SliceStable(c.timers, func(i, j int) bool {
			a, b := c.timers[i], c.timers[j]
			if !a.when.Equal(b.when) {
				return a.when.Before(b.when)
			}
			return a.seq < b.seq
		})
		if len(c.timers) == 0 || c.timers[0].when.After(target) {
			c.now = target
			c.mu.Unlock()
			return
		}
		t := c.timers[0]
		c.timers = c.timers[1:]
		if t.when.After(c.now) {
			c.now = t.when
		}
		c.mu.Unlock()
		t.f()
	}
}

// AfterFunc calls f once the clock reaches d from now, inside the Advance or Set that gets it there. A d of 0 or less
// is due at once, and fires on the next Advance, Advance(0) included. It implements the loader's internal clock.
func (c *Clock) AfterFunc(d time.Duration, f func()) hooks.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	t := &timer{c: c, when: c.now.Add(max(d, 0)), seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	return t
}

// Timers returns how many timers are waiting to fire.
func (c *Clock) Timers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type timer struct {
	c    *Clock
	when time.Time
	seq  int
	f    func()
}

// Stop removes the timer, and reports whether it hadn't fired yet.
func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	for i, o := range t.c.timers {
		if o == t {
			t.c.timers = append(t.c.timers[:i], t.c.timers[i+1:]...)
			return true
		}
	}
	return false
}

// WithClock returns an option that makes a loader read the time and set its timers from c. It also turns off
// remote-poll jitter, so polls happen exactly on time.
func WithClock(c *Clock) cascadingconfig.Option {
	return hooks.WithClock(c).(cascadingconfig.Option)
}
