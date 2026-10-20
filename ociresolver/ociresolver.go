package ociresolver

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/kittomic/cascadingconfig"
)

// Credential is a registry credential (DL-217).
type Credential struct {
	Username, Password string
	RefreshToken       string // an identity token, exchanged for access tokens
	AccessToken        string // a registry token, used as it is
}

// Option configures the resolver.
type Option func(*resolver)

// WithCredentials supplies credentials for each registry, by host[:port]. By default none are sent, and
// ~/.docker/config.json isn't read.
func WithCredentials(f func(ctx context.Context, registry string) (Credential, error)) Option {
	return func(r *resolver) { r.creds = f }
}

// WithPlainHTTP allows plain HTTP for the registries named, by host[:port], for tests and local registries only. Every
// other registry uses https.
func WithPlainHTTP(hosts ...string) Option {
	return func(r *resolver) { r.plain = append(r.plain, hosts...) }
}

// WithClient sets the HTTP client. The resolver copies it, so c isn't changed.
func WithClient(c *http.Client) Option {
	return func(r *resolver) { r.client = c }
}

// WithMaxLayerSize caps each blob's declared size, each download and each decompressed layer; the default is 64 MiB
// (DL-216).
func WithMaxLayerSize(n int64) Option {
	return func(r *resolver) { r.maxLayer = n }
}

// New returns a resolver for oci includes (DL-215).
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{maxLayer: 64 << 20}
	for _, o := range opts {
		o(r)
	}
	return r
}

type resolver struct {
	creds    func(ctx context.Context, registry string) (Credential, error)
	plain    []string
	client   *http.Client
	maxLayer int64
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return nil } // stub

// Resolve implements cascadingconfig.IncludeResolver.
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	_, _ = ctx, ref
	var s layerState
	_ = s.applyTar(nil, 0, 0)
	return nil, errors.New("ociresolver: not implemented") // stub
}

// layerState is the requested file as the layers read so far leave it.
type layerState struct {
	want    string // the validated inner path
	found   bool
	content []byte
}

// applyTar applies one uncompressed tar layer to s: an entry for want replaces it, and a whiteout over it removes it
// (DL-215). It fails on a malformed entry name anywhere in the layer, when want is matched by an entry that isn't a
// regular file, when the layer is over limit bytes, and when want's entry is over fileLimit.
func (s *layerState) applyTar(r io.Reader, limit, fileLimit int64) error {
	_, _, _ = s.want, s.found, s.content
	if limit < 0 || fileLimit < 0 {
		return errors.New("a negative limit")
	}
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return errors.New("not implemented") // stub
}
