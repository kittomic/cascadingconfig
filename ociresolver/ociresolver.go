package ociresolver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"github.com/kittomic/cascadingconfig"
)

const (
	defaultMaxLayer = 64 << 20 // WithMaxLayerSize's default (DL-216)
	maxManifest     = 4 << 20  // the distribution spec's limit for manifests
	maxRedirects    = 5

	annotationUnpack = "io.deis.oras.content.unpack" // oras's mark for a directory stored as a tar
	mediaTypeDocker  = "application/vnd.docker.distribution.manifest.v2+json"
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
	return func(r *resolver) {
		for _, h := range hosts {
			r.plain = append(r.plain, strings.ToLower(h))
		}
	}
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

// New returns a resolver for oci includes (DL-215). It resolves the reference to a manifest digest, then finds the file
// among the manifest's layers: titled files as oras push stores them, and tar layers as images store them, read as
// streams and verified against their digests. It's safe for concurrent use.
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{maxLayer: defaultMaxLayer, cache: auth.NewCache()}
	for _, o := range opts {
		o(r)
	}
	if r.maxLayer <= 0 {
		r.maxLayer = defaultMaxLayer
	}
	return r
}

type resolver struct {
	creds    func(ctx context.Context, registry string) (Credential, error)
	plain    []string
	client   *http.Client
	maxLayer int64
	cache    auth.Cache // tokens, per resolver
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return []string{"oci"} }

// Resolve implements cascadingconfig.IncludeResolver.
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	f := &fetch{r: r, ctx: ctx, ref: ref}
	return f.run()
}

// fetch is the state of one Resolve.
type fetch struct {
	r    *resolver
	ctx  context.Context
	ref  cascadingconfig.IncludeRef
	repo *remote.Repository
}

func (f *fetch) base(err error) cascadingconfig.BaseError {
	return cascadingconfig.BaseError{Op: "fetch", Path: f.ref.Raw, Err: err}
}

func (f *fetch) refErr(format string, args ...any) error {
	return &cascadingconfig.ReferenceError{BaseError: f.base(fmt.Errorf(format, args...))}
}

func (f *fetch) tooLarge(format string, args ...any) error {
	return &cascadingconfig.TooLargeError{BaseError: f.base(fmt.Errorf(format, args...))}
}

// mapErr turns an error from the registry into the one the load reports (DL-218).
func (f *fetch) mapErr(what string, err error) error {
	var resp *errcode.ErrorResponse
	switch {
	case f.ctx.Err() != nil:
		return f.ctx.Err()
	case cascadingconfig.CodeOf(err) != 0:
		return err
	case errors.Is(err, errdef.ErrNotFound), errors.As(err, &resp) && resp.StatusCode == http.StatusNotFound:
		return &cascadingconfig.NotFoundError{BaseError: f.base(fmt.Errorf("%s: %w", what, err))}
	case errors.Is(err, errdef.ErrSizeExceedsLimit):
		return f.tooLarge("%s: %w", what, err)
	}
	return &cascadingconfig.FetchError{BaseError: f.base(fmt.Errorf("%s: %w", what, err))}
}

func (f *fetch) run() (*cascadingconfig.Fetched, error) {
	r, ref := f.r, f.ref
	repo, err := remote.NewRepository(ref.Location)
	if err != nil {
		return nil, f.refErr("%w", err)
	}
	registry := strings.ToLower(repo.Reference.Registry)
	repo.PlainHTTP = slices.Contains(r.plain, registry)
	repo.MaxMetadataBytes = maxManifest
	repo.Client = &auth.Client{Client: r.httpClient(f), Cache: r.cache, Credential: r.credential}
	f.repo = repo

	version := ref.Version
	if _, digest, ok := strings.Cut(version, "@"); ok {
		version = digest
	}
	desc, err := repo.Resolve(f.ctx, version)
	if err != nil {
		return nil, f.mapErr("resolving "+ref.Version, err)
	}
	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest, mediaTypeDocker:
	case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
		return nil, f.refErr("%s is an image index; reference one of its manifests", ref.Version)
	default:
		return nil, f.refErr("%s is a %s, not an image manifest", ref.Version, desc.MediaType)
	}
	if desc.Size > maxManifest {
		return nil, f.tooLarge("the manifest is %d bytes, and the limit is %d", desc.Size, maxManifest)
	}
	rc, err := repo.Manifests().Fetch(f.ctx, desc)
	if err != nil {
		return nil, f.mapErr("fetching the manifest", err)
	}
	body, err := content.ReadAll(rc, desc)
	_ = rc.Close()
	if err != nil {
		return nil, f.mapErr("reading the manifest", err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, f.refErr("the manifest isn't valid JSON: %v", err)
	}
	data, err := f.find(m.Layers)
	if err != nil {
		return nil, err
	}
	return &cascadingconfig.Fetched{Content: data, Identity: desc.Digest.String()}, nil
}

// credential answers oras's requests for credentials from WithCredentials, or with none.
func (r *resolver) credential(ctx context.Context, hostport string) (auth.Credential, error) {
	if r.creds == nil {
		return auth.EmptyCredential, nil
	}
	c, err := r.creds(ctx, hostport)
	if err != nil {
		return auth.EmptyCredential, err
	}
	return auth.Credential{Username: c.Username, Password: c.Password, RefreshToken: c.RefreshToken, AccessToken: c.AccessToken}, nil
}

// httpClient copies the app's client and adds the redirect rules: at most 5 hops, never from https to http, and no
// allow-list checks, since what's fetched is verified by digest (DL-217).
func (r *resolver) httpClient(f *fetch) *http.Client {
	client := &http.Client{}
	if r.client != nil {
		c := *r.client
		client = &c
	}
	appCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return &cascadingconfig.FetchError{BaseError: f.base(fmt.Errorf("more than %d redirects", maxRedirects))}
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return &cascadingconfig.PolicyError{BaseError: f.base(fmt.Errorf("a redirect from https to %s", req.URL.Redacted()))}
		}
		if appCheck != nil {
			return appCheck(req, via)
		}
		return nil
	}
	return client
}

// find finds the requested file among the layers, in manifest order (DL-215).
func (f *fetch) find(layers []ocispec.Descriptor) ([]byte, error) {
	want := f.ref.Path
	for _, l := range layers {
		if title := l.Annotations[ocispec.AnnotationTitle]; title != "" && malformedName(title) {
			return nil, f.refErr("the artifact is malformed: a layer's title %q is absolute, has a .. segment or a \\", title)
		}
	}
	s := &layerState{want: want}
	var pending *ocispec.Descriptor // a titled file that matched, downloaded only if nothing later replaces it
	for i := range layers {
		l := layers[i]
		title := l.Annotations[ocispec.AnnotationTitle]
		unpack := l.Annotations[annotationUnpack] == "true"
		if title != "" && !unpack {
			if cleanName(title) == want {
				pending, s.found, s.content = &l, true, nil
			}
			continue
		}
		compression, isTar := tarType(l.MediaType)
		switch {
		case compression == "zstd":
			return nil, f.refErr("the artifact has a zstd layer, which isn't supported yet")
		case !isTar && !unpack:
			continue
		}
		before := s.found
		if err := f.applyLayer(s, l, compression); err != nil {
			return nil, err
		}
		if s.thisLayer || (before && !s.found) {
			pending = nil
		}
	}
	switch {
	case !s.found:
		return nil, &cascadingconfig.NotFoundError{BaseError: f.base(fmt.Errorf("the artifact has no %s", want))}
	case pending != nil:
		return f.download(*pending)
	}
	return s.content, nil
}

// tarType reports whether mt is a tar layer type, and its compression.
func tarType(mt string) (compression string, ok bool) {
	switch mt {
	case ocispec.MediaTypeImageLayer, "application/vnd.docker.image.rootfs.diff.tar":
		return "", true
	case ocispec.MediaTypeImageLayerGzip, "application/vnd.docker.image.rootfs.diff.tar.gzip":
		return "gzip", true
	case ocispec.MediaTypeImageLayerZstd:
		return "zstd", true
	}
	switch {
	case strings.HasSuffix(mt, "+gzip"):
		return "gzip", false
	case strings.HasSuffix(mt, "+zstd"):
		return "zstd", false
	}
	return "", false
}

// download fetches a titled file's blob, within the include size limit and the layer cap.
func (f *fetch) download(d ocispec.Descriptor) ([]byte, error) {
	switch {
	case f.ref.MaxSize > 0 && d.Size > f.ref.MaxSize:
		return nil, f.tooLarge("the file is %d bytes, and the limit is %d", d.Size, f.ref.MaxSize)
	case d.Size > f.r.maxLayer:
		return nil, f.tooLarge("the blob is %d bytes, and the layer limit is %d", d.Size, f.r.maxLayer)
	}
	rc, err := f.repo.Blobs().Fetch(f.ctx, d)
	if err != nil {
		return nil, f.mapErr("fetching "+d.Digest.String(), err)
	}
	defer func() { _ = rc.Close() }()
	data, err := content.ReadAll(rc, d)
	if err != nil {
		return nil, f.mapErr("reading "+d.Digest.String(), err)
	}
	return data, nil
}

// applyLayer streams a tar layer, verified against its digest, through s.
func (f *fetch) applyLayer(s *layerState, d ocispec.Descriptor, compression string) error {
	if d.Size > f.r.maxLayer {
		return f.tooLarge("a layer is %d bytes, and the limit is %d", d.Size, f.r.maxLayer)
	}
	rc, err := f.repo.Blobs().Fetch(f.ctx, d)
	if err != nil {
		return f.mapErr("fetching "+d.Digest.String(), err)
	}
	defer func() { _ = rc.Close() }()
	vr := content.NewVerifyReader(rc, d)
	var layer io.Reader = vr
	if compression == "gzip" {
		zr, err := gzip.NewReader(vr)
		if err != nil {
			return f.verified(vr, f.refErr("a gzip layer can't be read: %v", err))
		}
		defer func() { _ = zr.Close() }()
		layer = zr
	}
	err = s.applyTar(layer, f.r.maxLayer, f.ref.MaxSize)
	switch {
	case err == nil:
		return f.verified(vr, nil)
	case errors.Is(err, errLayerTooLarge):
		return f.tooLarge("a layer decompresses to more than %d bytes", f.r.maxLayer)
	case errors.Is(err, errFileTooLarge):
		return f.tooLarge("%s is over the limit of %d bytes", s.want, f.ref.MaxSize)
	}
	return f.verified(vr, f.refErr("the artifact is malformed: %v", err))
}

// verified drains vr and checks its digest: a mismatch is a *FetchError, since a truncated download is transient
// (DL-215); otherwise it returns err.
func (f *fetch) verified(vr *content.VerifyReader, err error) error {
	if _, cerr := io.Copy(io.Discard, vr); cerr != nil {
		return f.mapErr("reading a layer", cerr)
	}
	if verr := vr.Verify(); verr != nil {
		return &cascadingconfig.FetchError{BaseError: f.base(fmt.Errorf("a layer doesn't match its digest: %w", verr))}
	}
	return err
}

var (
	errLayerTooLarge = errors.New("the layer is over the size limit")
	errFileTooLarge  = errors.New("the file is over the size limit")
	errNotRegular    = errors.New("not a regular file")
)

// layerState is the requested file as the layers read so far leave it.
type layerState struct {
	want      string // the validated inner path
	found     bool
	content   []byte
	thisLayer bool // the file was found in the layer being read
}

// applyTar applies one uncompressed tar layer to s: an entry for want replaces it, and a whiteout over it removes it
// (DL-215). It fails on a malformed entry name anywhere in the layer, when want is matched by an entry that isn't a
// regular file, when the layer is over limit bytes, and when want's entry is over fileLimit.
func (s *layerState) applyTar(r io.Reader, limit, fileLimit int64) error {
	if limit < 0 || fileLimit < 0 {
		return errors.New("a negative limit")
	}
	s.thisLayer = false
	lr := &limitedReader{r: r, left: limit}
	tr := tar.NewReader(lr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if errors.Is(err, errLayerTooLarge) {
				return err
			}
			return fmt.Errorf("a tar layer can't be read: %w", err)
		}
		if malformedName(hdr.Name) {
			return fmt.Errorf("the entry %q is absolute, has a .. segment or a \\", hdr.Name)
		}
		name := cleanName(hdr.Name)
		dir, base := path.Dir(name), path.Base(name)
		switch {
		case name == ".":
		case base == ".wh..wh..opq":
			if s.found && !s.thisLayer && under(dir, s.want) {
				s.found, s.content = false, nil
			}
		case strings.HasPrefix(base, ".wh."):
			target := path.Join(dir, strings.TrimPrefix(base, ".wh."))
			if s.found && !s.thisLayer && (target == s.want || under(target, s.want)) {
				s.found, s.content = false, nil
			}
		case name == s.want:
			if hdr.Typeflag != tar.TypeReg {
				return fmt.Errorf("%s: %w", name, errNotRegular)
			}
			if fileLimit > 0 && hdr.Size > fileLimit {
				return errFileTooLarge
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				if errors.Is(err, errLayerTooLarge) {
					return err
				}
				return fmt.Errorf("a tar layer can't be read: %w", err)
			}
			s.found, s.content, s.thisLayer = true, data, true
		}
	}
}

// limitedReader fails with errLayerTooLarge once more than left bytes are read.
type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, errLayerTooLarge
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, errLayerTooLarge
	}
	return n, err
}

// malformedName reports whether an entry name or title is absolute, has a .. segment, or has a backslash.
func malformedName(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return true
	}
	return slices.Contains(strings.Split(name, "/"), "..")
}

// cleanName cleans an entry name, dropping a leading ./.
func cleanName(name string) string {
	return path.Clean(strings.TrimPrefix(name, "./"))
}

// under reports whether p lies below dir; everything lies below ".".
func under(dir, p string) bool {
	return dir == "." || strings.HasPrefix(p, dir+"/")
}
