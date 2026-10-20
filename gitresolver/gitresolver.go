package gitresolver

import (
	"context"
	"errors"
	"net/http"

	"github.com/kittomic/cascadingconfig"
)

// protocolVersion is the git protocol version every fetch forces. Only the internal test of the fallback for servers
// that refuse a commit id changes it, to 0, since protocol v2 servers serve any reachable commit.
var protocolVersion = "2"

// Option configures the resolver.
type Option func(*resolver)

// WithProtocols sets the transports git may use, such as "https", "ssh", "http" or "file". The default is https and
// ssh. A repo URL with any other scheme is a *PolicyError, before git runs (DL-212).
func WithProtocols(protocols ...string) Option {
	return func(r *resolver) { r.protocols = append([]string(nil), protocols...) }
}

// WithCredentialHelper sets git's credential.helper. By default there is none, and git never prompts.
func WithCredentialHelper(helper string) Option {
	return func(r *resolver) { r.helper = helper }
}

// WithGit sets the git program. By default it's found on PATH.
func WithGit(path string) Option {
	return func(r *resolver) { r.git = path }
}

// WithClient sets the HTTP client that follows an http or https repo URL's redirects before git runs (DL-213). The
// resolver copies it, so c isn't changed.
func WithClient(c *http.Client) Option {
	return func(r *resolver) { r.client = c }
}

// New returns a resolver for git includes (DL-211).
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{protocols: []string{"https", "ssh"}}
	for _, o := range opts {
		o(r)
	}
	return r
}

type resolver struct {
	protocols []string
	helper    string
	git       string
	client    *http.Client
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return nil } // stub

// Resolve implements cascadingconfig.IncludeResolver.
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	_, _, _, _, _ = ctx, ref, commands, parseLsTree, protocolVersion
	return nil, errors.New("gitresolver: not implemented") // stub
}

// commands returns the argument lists, after any -c settings, of the git commands that fetch path at ref from repo:
// fetch, rev-parse, ls-tree, cat-file -s and cat-file blob. commit and blob stand for the ids earlier commands print.
func commands(repo, ref, path, commit, blob string) [][]string {
	_, _, _, _, _ = repo, ref, path, commit, blob
	return nil // stub
}

// parseLsTree parses one line of git ls-tree output, "<mode> SP <type> SP <oid> TAB <path>", and reports whether it
// was well formed.
func parseLsTree(line string) (mode, kind, oid, path string, ok bool) {
	_ = line
	return "", "", "", "", false // stub
}
