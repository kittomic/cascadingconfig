package cascadingconfig

import (
	"time"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// realClock is the loader's default clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) hooks.Timer { return time.AfterFunc(d, f) }

// withClock makes the loader use clock, and turns off remote-poll jitter; cctest.WithClock reaches it through the
// hooks.
func withClock(clock hooks.Clock) Option {
	return optionFunc(func(c *config) {
		if clock == nil {
			c.fail("WithClock", "a nil clock")
			return
		}
		c.clock, c.noJitter = clock, true
	})
}

// now returns the loader's time.
func (c *config) now() time.Time { return c.clock.Now() }

func init() {
	hooks.WithClock = func(c hooks.Clock) any { return withClock(c) }
}
