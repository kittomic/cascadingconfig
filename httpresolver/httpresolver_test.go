package httpresolver_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
	"github.com/kittomic/cascadingconfig/httpresolver"
)

// The tests load /home/me/work/myapp.json, a project file whose include key names references on test servers.

const rootCfg = "/home/me/work/myapp.json"

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// load loads rootCfg including refs, with r registered and no include cache.
func load(t *testing.T, r cc.IncludeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	b, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"root": 1, "include": ` + string(b) + `}`})
	base := []cc.Option{
		cc.WithFileSystem(fsys), cc.WithDecoder(ccjson.Decoder()),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}),
		cc.WithScope("proj", cc.FixedDir("/home/me/work", cc.Pattern{Template: "myapp.json"})), cc.WithScopes("proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat), cc.WithIncludeResolver(r), cc.WithoutIncludeCache(),
	}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l.Load(t.Context())
}

func loadOK(t *testing.T, r cc.IncludeResolver, refs []string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := load(t, r, refs, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
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

func layerAt(t *testing.T, res *cc.Result, path string) cc.Layer {
	t.Helper()
	for _, l := range res.Describe().Layers {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no layer %s", path)
	return cc.Layer{}
}

// server serves files by path, each with a content type, and records the request URIs it saw.
type server struct {
	*httptest.Server
	mu   sync.Mutex
	uris []string
}

func newServer(t *testing.T, tls bool, h http.HandlerFunc) *server {
	s := &server{}
	wrapped := func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.uris = append(s.uris, r.RequestURI)
		s.mu.Unlock()
		h(w, r)
	}
	if tls {
		s.Server = httptest.NewTLSServer(http.HandlerFunc(wrapped))
	} else {
		s.Server = httptest.NewServer(http.HandlerFunc(wrapped))
	}
	t.Cleanup(s.Close)
	return s
}

func (s *server) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.uris)
}

// files is a handler serving path → content as application/json.
func files(m map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, ok := m[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// redirects is a handler where /r/N redirects to /r/N-1, /r/0 serves `{"a": 1}`, and /to?u=URL redirects to URL.
func redirects(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/to" {
		http.Redirect(w, r, r.URL.Query().Get("u"), http.StatusFound)
		return
	}
	n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
	switch {
	case err != nil:
		http.NotFound(w, r)
	case n == 0:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a": 1}`))
	default:
		http.Redirect(w, r, fmt.Sprintf("/r/%d", n-1), http.StatusFound)
	}
}

// DL-208: the resolver handles http and https.
func TestSchemes(t *testing.T) {
	got := slices.Sorted(slices.Values(httpresolver.New().Schemes()))
	if !slices.Equal(got, []string{"http", "https"}) {
		t.Errorf("Schemes() = %q, want http and https", got)
	}
}

// DL-198, DL-208: the body is the content, Content-Type is the media type, and the identity is the body's SHA-256.
func TestFetch(t *testing.T) {
	srv := newServer(t, false, files(map[string]string{"/a.json": `{"a": 1}`, "/config": `{"c": 1}`}))
	res := loadOK(t, httpresolver.New(), []string{srv.URL + "/a.json", srv.URL + "/config"})
	got, want := res.Tree(), cc.Tree{"root": int64(1), "a": int64(1), "c": int64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	l := layerAt(t, res, srv.URL+"/config")
	if l.Format != "json" || l.Remote == nil || l.Remote.Identity != "sha256:"+sum(`{"c": 1}`) {
		t.Errorf("layer = %+v, Remote %+v; want json, with the body's SHA-256 as identity", l, l.Remote)
	}
}

// DL-208: WithClient supplies the client, so a TLS test server's client reaches it, and the default client doesn't.
func TestTLS(t *testing.T) {
	srv := newServer(t, true, files(map[string]string{"/a.json": `{"a": 1}`}))
	loadOK(t, httpresolver.New(httpresolver.WithClient(srv.Client())), []string{srv.URL + "/a.json"})
	_, err := load(t, httpresolver.New(), []string{srv.URL + "/a.json"})
	wantCode(t, err, cc.CodeBadGateway)
}

// DL-55, DL-201: an integrity fragment is never sent to the server.
func TestFragmentNotSent(t *testing.T) {
	srv := newServer(t, false, files(map[string]string{"/a.json": `{"a": 1}`}))
	loadOK(t, httpresolver.New(), []string{srv.URL + "/a.json?v=2#sha256=" + sum(`{"a": 1}`)})
	if got := srv.requested(); !slices.Equal(got, []string{"/a.json?v=2"}) {
		t.Errorf("requests = %q, want just /a.json?v=2", got)
	}
}

// DL-208: at most 5 redirects are followed.
func TestRedirectLimit(t *testing.T) {
	srv := newServer(t, false, redirects)
	loadOK(t, httpresolver.New(), []string{srv.URL + "/r/5"})
	_, err := load(t, httpresolver.New(), []string{srv.URL + "/r/6"})
	wantCode(t, err, cc.CodeBadGateway)
}

// DL-208: a redirect from https to http is a *PolicyError, and the plain-http target is never requested.
func TestRedirectDowngrade(t *testing.T) {
	plain := newServer(t, false, files(map[string]string{"/a.json": `{"a": 1}`}))
	secure := newServer(t, true, redirects)
	_, err := load(t, httpresolver.New(httpresolver.WithClient(secure.Client())),
		[]string{secure.URL + "/to?u=" + plain.URL + "/a.json"})
	wantCode(t, err, cc.CodeForbidden)
	if got := plain.requested(); len(got) != 0 {
		t.Errorf("the http server got %q", got)
	}
}

// DL-198, DL-208, design.md Allow-lists, "Redirects": every redirect target is normalised and matched against the
// including file's scope's allow-lists; one that fails is never requested.
func TestRedirectAllowList(t *testing.T) {
	srv := newServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start/ok":
			http.Redirect(w, r, "/ok/a.json", http.StatusFound)
		case "/start/bad":
			http.Redirect(w, r, "/bad/a.json", http.StatusFound)
		case "/start/userinfo":
			http.Redirect(w, r, strings.Replace(srvURL(r), "http://", "http://user@", 1)+"/ok/a.json", http.StatusFound)
		default:
			files(map[string]string{"/ok/a.json": `{"a": 1}`, "/bad/a.json": `{"bad": 1}`})(w, r)
		}
	})
	policy := cc.WithScopePolicy("proj", cc.AllowResolvers("file", "http"),
		cc.AllowFor("http", cc.Prefix(srv.URL+"/start/"), cc.Prefix(srv.URL+"/ok/")))
	loadOK(t, httpresolver.New(), []string{srv.URL + "/start/ok"}, policy)
	_, err := load(t, httpresolver.New(), []string{srv.URL + "/start/bad"}, policy)
	wantCode(t, err, cc.CodeForbidden)
	_, err = load(t, httpresolver.New(), []string{srv.URL + "/start/userinfo"}, policy)
	wantCode(t, err, cc.CodeBadRequest)
	for _, uri := range srv.requested() {
		if strings.HasPrefix(uri, "/bad/") {
			t.Errorf("the rejected target %s was requested", uri)
		}
	}
}

func srvURL(r *http.Request) string { return "http://" + r.Host }

// DL-208: only a 200 is content; a 404 or 410 is a *NotFoundError, and any other status a 502.
func TestStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   cc.Code
	}{
		{http.StatusNotFound, cc.CodeNotFound},
		{http.StatusGone, cc.CodeNotFound},
		{http.StatusForbidden, cc.CodeBadGateway},
		{http.StatusUnauthorized, cc.CodeBadGateway},
		{http.StatusInternalServerError, cc.CodeBadGateway},
		{http.StatusNoContent, cc.CodeBadGateway},
		{http.StatusNotModified, cc.CodeBadGateway},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := newServer(t, false, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"a": 1}`))
			})
			_, err := load(t, httpresolver.New(), []string{srv.URL + "/a.json"})
			wantCode(t, err, tc.code)
		})
	}
}

// DL-198, DL-208: a Content-Length over the limit is a 413 before the body is read, and a body over the limit without
// one is a 413 too.
func TestSizeLimit(t *testing.T) {
	t.Run("Content-Length", func(t *testing.T) {
		srv := newServer(t, false, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(2<<20))
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		start := time.Now()
		_, err := load(t, httpresolver.New(), []string{srv.URL + "/a.json"}, cc.WithIncludeTimeout(10*time.Second))
		wantCode(t, err, cc.CodeTooLarge)
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("the load took %v, want the 413 before the body is read", d)
		}
	})
	t.Run("chunked body", func(t *testing.T) {
		srv := newServer(t, false, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"a": "`))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("x", 100) + `"}`))
		})
		_, err := load(t, httpresolver.New(), []string{srv.URL + "/a.json"}, cc.WithMaxIncludeSize(100))
		wantCode(t, err, cc.CodeTooLarge)
	})
}

// DL-199: the per-fetch timeout reaches the request.
func TestTimeout(t *testing.T) {
	srv := newServer(t, false, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	_, err := load(t, httpresolver.New(), []string{srv.URL + "/a.json"}, cc.WithIncludeTimeout(100*time.Millisecond))
	wantCode(t, err, cc.CodeGatewayTimeout)
}

// countingTransport counts the requests it carries.
type countingTransport struct {
	mu sync.Mutex
	n  int
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return http.DefaultTransport.RoundTrip(r)
}

// DL-208: WithClient's client carries the requests, its own redirect check runs after the resolver's, and the client
// isn't changed.
func TestWithClient(t *testing.T) {
	srv := newServer(t, false, redirects)
	rt := &countingTransport{}
	var appChecks int
	check := func(*http.Request, []*http.Request) error {
		appChecks++
		return nil
	}
	client := &http.Client{Transport: rt, CheckRedirect: check, Timeout: time.Minute}
	before := *client
	loadOK(t, httpresolver.New(httpresolver.WithClient(client)), []string{srv.URL + "/r/2"})
	if rt.n != 3 || appChecks != 2 {
		t.Errorf("%d requests and %d app redirect checks, want 3 and 2", rt.n, appChecks)
	}
	if client.Transport != before.Transport || client.Timeout != before.Timeout ||
		reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(before.CheckRedirect).Pointer() {
		t.Errorf("the app's client was changed")
	}
}
