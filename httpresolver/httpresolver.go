package httpresolver

import (
	"context"
	"errors"
	"net/http"

	"github.com/kittomic/cascadingconfig"
)

// Option configures the resolver.
type Option func(*resolver)

// WithClient sets the HTTP client. The resolver copies it and installs its own redirect check, which runs before the
// client's own, so c isn't changed. Auth headers belong in c's transport.
func WithClient(c *http.Client) Option {
	return func(r *resolver) { r.client = c }
}

// New returns a resolver for http and https includes (DL-208).
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{}
	for _, o := range opts {
		o(r)
	}
	return r
}

type resolver struct {
	client *http.Client
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return nil } // stub

// Resolve implements cascadingconfig.IncludeResolver.
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	_, _ = ctx, ref
	return nil, errors.New("httpresolver: not implemented") // stub
}
