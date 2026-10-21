package cctest

import (
	"time"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// AfterFunc sets a timer on c as the loader does, for this package's tests only: the method isn't public API (DL-258).
func AfterFunc(c *Clock, d time.Duration, f func()) hooks.Timer { return c.afterFunc(d, f) }
