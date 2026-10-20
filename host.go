package cascadingconfig

import (
	"errors"
	"maps"
	"os"
	"runtime"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// host is everything the loader reads about the machine it runs on. The loader never reads these from the process
// directly, so tests can fake them with cctest.Host.
type host interface {
	GOOS() string
	GOARCH() string
	LookupEnv(key string) (string, bool)
	Environ() []string
	Home() (string, error) // an error when the host has no home directory
	Cwd() (string, error)
	UID() int
}

var errNoHome = errors.New("the host has no home directory")

// osHost is the real host.
type osHost struct{}

func (osHost) GOOS() string                        { return runtime.GOOS }
func (osHost) GOARCH() string                      { return runtime.GOARCH }
func (osHost) LookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
func (osHost) Environ() []string                   { return os.Environ() }
func (osHost) Home() (string, error)               { return os.UserHomeDir() }
func (osHost) Cwd() (string, error)                { return os.Getwd() }
func (osHost) UID() int                            { return os.Getuid() }

// withHost replaces the loader's host.
func withHost(h host) Option {
	return optionFunc(func(c *config) {
		if h == nil {
			c.fail("WithHost", "a nil host")
			return
		}
		c.host = h
	})
}

// fakeHost is a host built from a hooks.Host. An empty Home means there is no home directory.
type fakeHost struct{ h hooks.Host }

func (f fakeHost) GOOS() string   { return f.h.GOOS }
func (f fakeHost) GOARCH() string { return f.h.GOARCH }

func (f fakeHost) LookupEnv(key string) (string, bool) {
	v, ok := f.h.Env[key]
	return v, ok
}

func (f fakeHost) Environ() []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(f.h.Env)) {
		out = append(out, k+"="+f.h.Env[k])
	}
	return out
}

func (f fakeHost) Home() (string, error) {
	if f.h.Home == "" {
		return "", errNoHome
	}
	return f.h.Home, nil
}

func (f fakeHost) Cwd() (string, error) {
	if f.h.Cwd == "" {
		return "", errors.New("the host has no current directory")
	}
	return f.h.Cwd, nil
}

func (f fakeHost) UID() int { return f.h.UID }

func init() {
	hooks.WithHost = func(h hooks.Host) any { return withHost(fakeHost{h}) }
}
