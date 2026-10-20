package cctest

import (
	"sync"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// Clock is a fake clock for TTLs, debounce and polling. It only moves when the test moves it. It's safe for concurrent
// use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock that starts at t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// WithClock returns an option that makes a loader read the time from c.
func WithClock(c *Clock) cascadingconfig.Option {
	return hooks.WithClock(c.Now).(cascadingconfig.Option)
}
