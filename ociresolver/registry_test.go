package ociresolver_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// The tests load /home/me/work/myapp.json, a project file whose include key names oci references to an in-memory
// registry: a hand-written subset of the distribution API (DL-218).

const rootCfg = "/home/me/work/myapp.json"

const (
	mtManifest    = "application/vnd.oci.image.manifest.v1+json"
	mtIndex       = "application/vnd.oci.image.index.v1+json"
	mtDocker      = "application/vnd.docker.distribution.manifest.v2+json"
	mtEmpty       = "application/vnd.oci.empty.v1+json"
	mtTar         = "application/vnd.oci.image.layer.v1.tar"
	mtTarGzip     = mtTar + "+gzip"
	mtTarZstd     = mtTar + "+zstd"
	mtDockerLayer = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	titleKey      = "org.opencontainers.image.title"
	unpackKey     = "io.deis.oras.content.unpack"
)

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// registry is an in-memory registry. With user set, every /v2/ request needs a bearer token from /token, which asks
// for those basic credentials; with anonymous set instead, /token hands out tokens to anyone.
type registry struct {
	*httptest.Server
	cdn *httptest.Server // serves blobs when blobs redirect there

	mu         sync.Mutex
	blobs      map[string][]byte
	manifests  map[string][2]string // "repo@ref" → media type, body
	requests   []string             // "METHOD path" on the registry, "CDN METHOD path" on the CDN
	fail       bool                 // every registry request is a 500
	user, pass string
	anonymous  bool
	tokenCreds []string // the basic credentials /token received
	redirect   string   // when set, blob requests redirect to redirect + path
	corrupt    map[string]bool
}

func newRegistry(t *testing.T, tls bool) *registry {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	r := &registry{blobs: map[string][]byte{}, manifests: map[string][2]string{}, corrupt: map[string]bool{}}
	if tls {
		r.Server = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	} else {
		r.Server = httptest.NewServer(http.HandlerFunc(r.serve))
	}
	r.cdn = httptest.NewServer(http.HandlerFunc(r.serveCDN))
	t.Cleanup(r.Close)
	t.Cleanup(r.cdn.Close)
	return r
}

// host is the registry's host:port, as a reference names it.
func (r *registry) host() string {
	return strings.TrimPrefix(strings.TrimPrefix(r.URL, "http://"), "https://")
}

func (r *registry) record(prefix string, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, prefix+req.Method+" "+req.URL.Path)
}

// cdnRequests returns the requests the CDN got.
func (r *registry) cdnRequests() []string {
	var out []string
	for _, req := range r.requested() {
		if strings.HasPrefix(req, "CDN ") {
			out = append(out, req)
		}
	}
	return out
}

func (r *registry) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

func (r *registry) serve(w http.ResponseWriter, req *http.Request) {
	r.record("", req)
	r.mu.Lock()
	fail := r.fail
	r.mu.Unlock()
	if fail {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if req.URL.Path == "/token" {
		r.serveToken(w, req)
		return
	}
	if !strings.HasPrefix(req.URL.Path, "/v2/") {
		http.NotFound(w, req)
		return
	}
	r.mu.Lock()
	needAuth := r.user != "" || r.anonymous
	r.mu.Unlock()
	if needAuth && req.Header.Get("Authorization") != "Bearer tok-ok" {
		challenge := fmt.Sprintf(`Bearer realm=%q,service="test"`, r.URL+"/token")
		if repo, _, ok := splitPath(req.URL.Path); ok {
			challenge += fmt.Sprintf(`,scope="repository:%s:pull"`, repo)
		}
		w.Header().Set("WWW-Authenticate", challenge)
		http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)
		return
	}
	if req.URL.Path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	repo, rest, ok := splitPath(req.URL.Path)
	switch {
	case ok && strings.HasPrefix(rest, "manifests/"):
		r.serveManifest(w, req, repo, strings.TrimPrefix(rest, "manifests/"))
	case ok && strings.HasPrefix(rest, "blobs/"):
		r.mu.Lock()
		redirect := r.redirect
		r.mu.Unlock()
		if redirect != "" {
			http.Redirect(w, req, redirect+req.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		r.serveBlob(w, req, strings.TrimPrefix(rest, "blobs/"))
	default:
		http.NotFound(w, req)
	}
}

func (r *registry) serveCDN(w http.ResponseWriter, req *http.Request) {
	r.record("CDN ", req)
	if _, rest, ok := splitPath(req.URL.Path); ok && strings.HasPrefix(rest, "blobs/") {
		r.serveBlob(w, req, strings.TrimPrefix(rest, "blobs/"))
		return
	}
	http.NotFound(w, req)
}

// splitPath splits /v2/<repo>/<manifests|blobs>/<ref> into the repo and the rest.
func splitPath(p string) (repo, rest string, ok bool) {
	p = strings.TrimPrefix(p, "/v2/")
	for _, kind := range []string{"/manifests/", "/blobs/"} {
		if i := strings.LastIndex(p, kind); i > 0 {
			return p[:i], p[i+1:], true
		}
	}
	return "", "", false
}

func (r *registry) serveToken(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	user, pass, anonymous := r.user, r.pass, r.anonymous
	if u, p, ok := req.BasicAuth(); ok {
		r.tokenCreds = append(r.tokenCreds, u+":"+p)
	}
	r.mu.Unlock()
	if !anonymous {
		if u, p, ok := req.BasicAuth(); !ok || u != user || p != pass {
			http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"token":"tok-ok","access_token":"tok-ok"}`))
}

func (r *registry) serveManifest(w http.ResponseWriter, req *http.Request, repo, ref string) {
	r.mu.Lock()
	m, ok := r.manifests[repo+"@"+ref]
	r.mu.Unlock()
	if !ok {
		http.Error(w, `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", m[0])
	w.Header().Set("Docker-Content-Digest", digestOf([]byte(m[1])))
	w.Header().Set("Content-Length", fmt.Sprint(len(m[1])))
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(m[1]))
}

func (r *registry) serveBlob(w http.ResponseWriter, req *http.Request, digest string) {
	r.mu.Lock()
	b, ok := r.blobs[digest]
	corrupt := r.corrupt[digest]
	r.mu.Unlock()
	if !ok {
		http.Error(w, `{"errors":[{"code":"BLOB_UNKNOWN"}]}`, http.StatusNotFound)
		return
	}
	if corrupt {
		b = append([]byte("X"), b[1:]...)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	w.Header().Set("Docker-Content-Digest", digest)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}

// layer is a layer to push.
type layer struct {
	mediaType   string
	data        []byte
	size        int64 // the declared size; 0 means len(data)
	annotations map[string]string
}

// fileLayer is a file as oras push stores it: the raw content, with oras's default media type and a title.
func fileLayer(title, content string) layer {
	return layer{mediaType: mtTar, data: []byte(content), annotations: map[string]string{titleKey: title}}
}

// tarEntry is one entry of a tar layer.
type tarEntry struct {
	name string
	typ  byte
	body string
	link string
}

func file(name, body string) tarEntry { return tarEntry{name: name, typ: tar.TypeReg, body: body} }

func tarBytes(entries ...tarEntry) []byte {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body)), Linkname: e.link}
		if e.typ != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			panic(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				panic(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func gzipBytes(b []byte) []byte {
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(b); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// tarLayer is an untitled, image-style tar layer.
func tarLayer(gz bool, entries ...tarEntry) layer {
	if gz {
		return layer{mediaType: mtTarGzip, data: gzipBytes(tarBytes(entries...))}
	}
	return layer{mediaType: mtTar, data: tarBytes(entries...)}
}

// dirLayer is a directory as oras push stores it: a gzipped tar whose entries start with the directory's name, titled
// with that name and marked for unpacking.
func dirLayer(dir string, entries ...tarEntry) layer {
	return layer{mediaType: mtTarGzip, data: gzipBytes(tarBytes(entries...)),
		annotations: map[string]string{titleKey: dir, unpackKey: "true"}}
}

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func (r *registry) addBlob(b []byte) string {
	d := digestOf(b)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blobs[d] = b
	return d
}

// push stores an OCI image manifest with layers under repo:tag, and returns the manifest's digest.
func (r *registry) push(repo, tag string, layers ...layer) string {
	return r.pushAs(repo, tag, mtManifest, mtEmpty, layers...)
}

func (r *registry) pushAs(repo, tag, mediaType, configType string, layers ...layer) string {
	config := []byte("{}")
	m := map[string]any{
		"schemaVersion": 2, "mediaType": mediaType,
		"config": descriptor{MediaType: configType, Digest: r.addBlob(config), Size: int64(len(config))},
	}
	var ds []descriptor
	for _, l := range layers {
		size := l.size
		if size == 0 {
			size = int64(len(l.data))
		}
		ds = append(ds, descriptor{MediaType: l.mediaType, Digest: r.addBlob(l.data), Size: size, Annotations: l.annotations})
	}
	m["layers"] = ds
	body, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return r.pushRaw(repo, tag, mediaType, body)
}

// pushRaw stores a manifest body as it is, under repo:tag and repo@digest.
func (r *registry) pushRaw(repo, tag, mediaType string, body []byte) string {
	d := digestOf(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	if tag != "" {
		r.manifests[repo+"@"+tag] = [2]string{mediaType, string(body)}
	}
	r.manifests[repo+"@"+d] = [2]string{mediaType, string(body)}
	return d
}

// ociRef is the include reference for path in repo at version (":tag" or "@digest") on the registry.
func (r *registry) ociRef(repo, version, path string) string {
	return "oci::" + r.host() + "/" + repo + version + "//" + path
}

// load loads rootCfg including refs, with res registered and no include cache.
func load(t *testing.T, res cc.IncludeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	b, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"root": 1, "include": ` + string(b) + `}`})
	base := []cc.Option{
		cc.WithFileSystem(fsys), cc.WithDecoder(ccjson.Decoder()),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}),
		cc.WithScope("proj", cc.FixedDir("/home/me/work", cc.Pattern{Template: "myapp.json"})), cc.WithScopes("proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat), cc.WithIncludeResolver(res), cc.WithoutIncludeCache(),
	}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l.Load(t.Context())
}

func loadOK(t *testing.T, res cc.IncludeResolver, refs []string, opts ...cc.Option) *cc.Result {
	t.Helper()
	got, err := load(t, res, refs, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got
}

func wantCode(t *testing.T, err error, code cc.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("Load succeeded, want code %d", code)
	}
	if got := cc.CodeOf(err); got != code {
		t.Errorf("code = %d, want %d: %v", got, code, err)
	}
}

func wantValue(t *testing.T, res *cc.Result, key string, want any) {
	t.Helper()
	if got := res.Tree()[key]; got != want {
		t.Errorf("%s = %v (%T), want %v", key, got, got, want)
	}
}
