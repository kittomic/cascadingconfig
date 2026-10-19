package cascadingconfig

import "github.com/kittomic/cascadingconfig/internal/hooks"

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

// withHost replaces the loader's host.
func withHost(h host) Option {
	_ = h // stub
	return optionFunc(func(*config) {})
}

// fakeHost is a host built from a hooks.Host.
type fakeHost struct{ h hooks.Host }

func (f fakeHost) GOOS() string                    { return f.h.GOOS }
func (f fakeHost) GOARCH() string                  { return f.h.GOARCH }
func (f fakeHost) LookupEnv(string) (string, bool) { return "", false }
func (f fakeHost) Environ() []string               { return nil }
func (f fakeHost) Home() (string, error)           { return f.h.Home, nil }
func (f fakeHost) Cwd() (string, error)            { return f.h.Cwd, nil }
func (f fakeHost) UID() int                        { return f.h.UID }

func init() {
	hooks.WithHost = func(h hooks.Host) any { return withHost(fakeHost{h}) }
}
