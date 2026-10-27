package httpresolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kittomic/cascadingconfig"
)

// maxRedirects is how many redirects a fetch follows (DL-208).
const maxRedirects = 5

// Option configures the resolver. It's sealed: only this package's With functions make one.
type Option interface{ apply(r *resolver) }

type optionFunc func(r *resolver)

func (f optionFunc) apply(r *resolver) { f(r) }

// WithClient sets the HTTP client. The resolver copies it and installs its own redirect check, which runs before the
// client's own, so c isn't changed. Auth headers belong in c's transport.
func WithClient(c *http.Client) Option {
	return optionFunc(func(r *resolver) { r.client = c })
}

// New returns a resolver for http and https includes. The loader normalises and checks every reference before calling
// it, strips and verifies integrity fragments, and bounds each fetch with its include timeout.
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{}
	for _, o := range opts {
		o.apply(r)
	}
	return r
}

type resolver struct {
	client *http.Client
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return []string{"http", "https"} }

// Resolve implements cascadingconfig.IncludeResolver. Only a 200 is content: a 404 or 410 is a *NotFoundError, and
// any other status a *FetchError. Content over ref.MaxSize is a *TooLargeError.
//
// The validator is the ETag, or else Last-Modified, sent back as If-None-Match or If-Modified-Since; a 304 in answer
// is ErrNotModified (DL-229).
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	base := func(err error) cascadingconfig.BaseError {
		return cascadingconfig.BaseError{Op: "fetch", Path: ref.Raw, Err: err}
	}
	client := &http.Client{}
	if r.client != nil {
		c := *r.client
		client = &c
	}
	appCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return &cascadingconfig.FetchError{BaseError: base(fmt.Errorf("more than %d redirects", maxRedirects))}
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return &cascadingconfig.PolicyError{BaseError: base(fmt.Errorf("a redirect from https to %s", req.URL.Redacted()))}
		}
		target := *req.URL
		target.Fragment, target.RawFragment = "", ""
		if ref.Check != nil {
			if err := ref.Check(target.String()); err != nil {
				return err
			}
		}
		if appCheck != nil {
			return appCheck(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.Location, nil)
	if err != nil {
		return nil, err
	}
	conditional := true
	if v, ok := strings.CutPrefix(ref.Validator, etagPrefix); ok {
		req.Header.Set("If-None-Match", v)
	} else if v, ok := strings.CutPrefix(ref.Validator, lastModifiedPrefix); ok {
		req.Header.Set("If-Modified-Since", v)
	} else {
		conditional = false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotModified && conditional:
		return nil, cascadingconfig.ErrNotModified
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return nil, &cascadingconfig.NotFoundError{BaseError: base(fmt.Errorf("the server answered %s", resp.Status))}
	case resp.StatusCode != http.StatusOK:
		return nil, &cascadingconfig.FetchError{BaseError: base(fmt.Errorf("the server answered %s", resp.Status))}
	case ref.MaxSize > 0 && resp.ContentLength > ref.MaxSize:
		return nil, &cascadingconfig.TooLargeError{BaseError: base(fmt.Errorf(
			"the server announced %d bytes, and the limit is %d", resp.ContentLength, ref.MaxSize))}
	}
	body := io.Reader(resp.Body)
	if ref.MaxSize > 0 {
		body = io.LimitReader(resp.Body, ref.MaxSize+1)
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if ref.MaxSize > 0 && int64(len(content)) > ref.MaxSize {
		return nil, &cascadingconfig.TooLargeError{BaseError: base(errors.New("the body is over the size limit"))}
	}
	sum := sha256.Sum256(content)
	return &cascadingconfig.Fetched{
		Content: content, MediaType: resp.Header.Get("Content-Type"), Identity: "sha256:" + hex.EncodeToString(sum[:]),
		Validator: validator(resp.Header),
	}, nil
}

// The validator's prefixes say which header it came from.
const (
	etagPrefix         = "etag:"
	lastModifiedPrefix = "last-modified:"
)

// validator is the response's ETag, weak or strong, or else its Last-Modified, or none.
func validator(h http.Header) string {
	if v := h.Get("ETag"); v != "" {
		return etagPrefix + v
	}
	if v := h.Get("Last-Modified"); v != "" {
		return lastModifiedPrefix + v
	}
	return ""
}
