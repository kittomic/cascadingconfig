package cascadingconfig

import (
	"time"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// withClock makes the loader read the time from now; cctest.WithClock reaches it through the hooks.
func withClock(now func() time.Time) Option {
	return optionFunc(func(*config) { _ = now }) // stub
}

func init() {
	hooks.WithClock = func(now func() time.Time) any { return withClock(now) }
}
