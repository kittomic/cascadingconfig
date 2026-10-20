package ociresolver_test

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/ociresolver"
)

// plain is a resolver that may use the registry over plain HTTP.
func plain(r *registry, opts ...ociresolver.Option) cc.IncludeResolver {
	return ociresolver.New(append([]ociresolver.Option{ociresolver.WithPlainHTTP(r.host(), r.cdn.Listener.Addr().String())}, opts...)...)
}

// DL-215: the resolver handles oci.
func TestSchemes(t *testing.T) {
	if got := ociresolver.New().Schemes(); !slices.Equal(got, []string{"oci"}) {
		t.Errorf("Schemes() = %q, want oci", got)
	}
}

// DL-215, design.md oci, "Manifests": a tag, a digest, and a tag with a digest each resolve to the manifest, whose
// digest is the identity.
func TestManifests(t *testing.T) {
	r := newRegistry(t, false)
	d := r.push("acme/cfg", "1.0", fileLayer("a.json", `{"v": 1}`))
	for _, version := range []string{":1.0", "@" + d, ":1.0@" + d} {
		t.Run(version, func(t *testing.T) {
			ref := r.ociRef("acme/cfg", version, "a.json")
			res := loadOK(t, plain(r), []string{ref})
			wantValue(t, res, "v", int64(1))
			for _, l := range res.Describe().Layers {
				if l.Path == ref && (l.Remote == nil || l.Remote.Identity != d) {
					t.Errorf("Remote = %+v, want identity %s", l.Remote, d)
				}
			}
		})
	}
}

// DL-215: a Docker v2 schema 2 manifest with a Docker layer loads; an image index is a *ReferenceError.
func TestManifestTypes(t *testing.T) {
	r := newRegistry(t, false)
	r.pushAs("acme/docker", "1", mtDocker, "application/vnd.docker.container.image.v1+json",
		layer{mediaType: mtDockerLayer, data: gzipBytes(tarBytes(file("a.json", `{"v": 1}`)))})
	wantValue(t, loadOK(t, plain(r), []string{r.ociRef("acme/docker", ":1", "a.json")}), "v", int64(1))

	m := r.push("acme/cfg", "", fileLayer("a.json", `{}`))
	index := `{"schemaVersion":2,"mediaType":"` + mtIndex + `","manifests":[{"mediaType":"` + mtManifest +
		`","digest":"` + m + `","size":1}]}`
	r.pushRaw("acme/cfg", "multi", mtIndex, []byte(index))
	_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":multi", "a.json")})
	wantCode(t, err, cc.CodeBadRequest)
}

// DL-60, DL-215, design.md oci, "Two artifact forms": a titled layer is a file whatever its media type, unless oras's
// unpack annotation marks a directory tar; an untitled tar layer, plain or gzip, is read as entries; an untitled
// non-tar layer is ignored.
func TestArtifactForms(t *testing.T) {
	r := newRegistry(t, false)
	r.push("acme/files", "1",
		fileLayer("a.json", `{"a": 1}`),
		layer{mediaType: "application/json", data: []byte(`{"b": 1}`), annotations: map[string]string{titleKey: "b.json"}})
	r.push("acme/dir", "1", dirLayer("conf", file("conf/a.json", `{"conf": 1}`)))
	r.push("acme/image", "1", tarLayer(false, file("a.json", `{"plain": 1}`)), tarLayer(true, file("etc/b.json", `{"gz": 1}`)))
	r.push("acme/ignored", "1", layer{mediaType: "application/json", data: []byte(`{"x": 1}`)})
	for _, tc := range []struct {
		repo, path, key string
	}{
		{"acme/files", "a.json", "a"},
		{"acme/files", "b.json", "b"},
		{"acme/dir", "conf/a.json", "conf"},
		{"acme/image", "a.json", "plain"},
		{"acme/image", "etc/b.json", "gz"},
	} {
		t.Run(tc.repo+"/"+tc.path, func(t *testing.T) {
			wantValue(t, loadOK(t, plain(r), []string{r.ociRef(tc.repo, ":1", tc.path)}), tc.key, int64(1))
		})
	}
	t.Run("untitled non-tar layer", func(t *testing.T) {
		_, err := load(t, plain(r), []string{r.ociRef("acme/ignored", ":1", "x.json")})
		wantCode(t, err, cc.CodeNotFound)
	})
}

// DL-60, DL-215: layers are taken in manifest order, and the last match wins.
func TestLastLayerWins(t *testing.T) {
	r := newRegistry(t, false)
	r.push("acme/cfg", "1",
		tarLayer(false, file("a.json", `{"v": 1}`)),
		fileLayer("a.json", `{"v": 2}`),
		tarLayer(true, file("a.json", `{"v": 3}`), file("b.json", `{"v": 1}`)),
		fileLayer("b.json", `{"v": 2}`))
	wantValue(t, loadOK(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")}), "v", int64(3))
	wantValue(t, loadOK(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "b.json")}), "v", int64(2))
}

// DL-215, design.md oci, "Duplicates": whiteouts work as in an image.
func TestWhiteouts(t *testing.T) {
	r := newRegistry(t, false)
	base := tarLayer(false, file("a.json", `{"v": 1}`), file("dir/b.json", `{"v": 1}`), file("dir/sub/c.json", `{"v": 1}`))
	r.push("acme/file", "1", base, tarLayer(false, file(".wh.a.json", "")))
	r.push("acme/dir", "1", base, tarLayer(false, file(".wh.dir", "")))
	r.push("acme/opaque", "1", base, tarLayer(false, file("dir/.wh..wh..opq", ""), file("dir/new.json", `{"v": 2}`)))
	r.push("acme/again", "1", base, tarLayer(false, file(".wh.a.json", "")), tarLayer(false, file("a.json", `{"v": 3}`)))
	for _, tc := range []struct {
		repo, path string
		v          int64 // 0: want a 404
	}{
		{"acme/file", "a.json", 0},
		{"acme/file", "dir/b.json", 1},
		{"acme/dir", "dir/b.json", 0},
		{"acme/dir", "dir/sub/c.json", 0},
		{"acme/dir", "a.json", 1},
		{"acme/opaque", "dir/b.json", 0},
		{"acme/opaque", "dir/sub/c.json", 0},
		{"acme/opaque", "dir/new.json", 2},
		{"acme/opaque", "a.json", 1},
		{"acme/again", "a.json", 3},
	} {
		t.Run(tc.repo+"/"+tc.path, func(t *testing.T) {
			res, err := load(t, plain(r), []string{r.ociRef(tc.repo, ":1", tc.path)})
			if tc.v == 0 {
				wantCode(t, err, cc.CodeNotFound)
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantValue(t, res, "v", tc.v)
		})
	}
}

// DL-215, design.md oci, "Entry matching": an entry or title with an absolute name, a .. segment or a \ fails the whole
// fetch, even when it isn't the requested file; a leading ./ is fine. This is an attack suite.
func TestMalformedEntries(t *testing.T) {
	good := file("a.json", `{"v": 1}`)
	for _, tc := range []struct {
		name string
		l    layer
	}{
		{"absolute entry", tarLayer(false, good, file("/etc/x.json", `{}`))},
		{"dot-dot entry", tarLayer(false, good, file("../x.json", `{}`))},
		{"inner dot-dot entry", tarLayer(true, good, file("dir/../../x.json", `{}`))},
		{"backslash entry", tarLayer(false, good, file(`dir\x.json`, `{}`))},
		{"absolute link entry", tarLayer(false, good, tarEntry{name: "/x", typ: tar.TypeSymlink, link: "a.json"})},
		{"dot-dot title", fileLayer("../x.json", `{}`)},
		{"absolute title", fileLayer("/x.json", `{}`)},
		{"dot-dot in a directory layer", dirLayer("conf", good, file("conf/../../x.json", `{}`))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry(t, false)
			layers := []layer{tc.l}
			if tc.l.annotations[titleKey] != "" && tc.l.annotations[unpackKey] == "" {
				layers = append(layers, tarLayer(false, good))
			}
			r.push("acme/cfg", "1", layers...)
			_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
			wantCode(t, err, cc.CodeBadRequest)
		})
	}
	t.Run("leading ./", func(t *testing.T) {
		r := newRegistry(t, false)
		r.push("acme/cfg", "1", tarLayer(false, file("./a.json", `{"v": 1}`)))
		wantValue(t, loadOK(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")}), "v", int64(1))
	})
}

// DL-215, design.md oci, "Regular files only": a symlink, a hard link, a directory or a FIFO is a *ReferenceError;
// zstd layers are a *ReferenceError for now.
func TestNonRegular(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    tarEntry
	}{
		{"symlink", tarEntry{name: "a.json", typ: tar.TypeSymlink, link: "b.json"}},
		{"hard link", tarEntry{name: "a.json", typ: tar.TypeLink, link: "b.json"}},
		{"directory", tarEntry{name: "a.json", typ: tar.TypeDir}},
		{"fifo", tarEntry{name: "a.json", typ: tar.TypeFifo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry(t, false)
			r.push("acme/cfg", "1", tarLayer(false, file("b.json", `{}`), tc.e))
			_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
			wantCode(t, err, cc.CodeBadRequest)
		})
	}
	t.Run("zstd", func(t *testing.T) {
		r := newRegistry(t, false)
		r.push("acme/cfg", "1", layer{mediaType: mtTarZstd, data: []byte("zstd bytes")})
		_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeBadRequest)
	})
}

// DL-215, design.md oci, "Digests": a blob or manifest whose bytes don't match its digest fails, as a *FetchError.
func TestDigests(t *testing.T) {
	t.Run("blob", func(t *testing.T) {
		r := newRegistry(t, false)
		r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
		r.corrupt[digestOf([]byte(`{"v": 1}`))] = true
		_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeBadGateway)
	})
	t.Run("manifest", func(t *testing.T) {
		r := newRegistry(t, false)
		d := r.push("acme/cfg", "", fileLayer("a.json", `{"v": 1}`))
		other := r.push("acme/other", "", fileLayer("a.json", `{"v": 2}`))
		r.mu.Lock()
		r.manifests["acme/cfg@"+d] = r.manifests["acme/other@"+other]
		r.mu.Unlock()
		_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", "@"+d, "a.json")})
		wantCode(t, err, cc.CodeBadGateway)
	})
}

// DL-216, design.md oci, "Caps": a blob declared over WithMaxLayerSize is never downloaded; a layer that decompresses
// past it, a file over the include size limit, and a manifest over 4 MiB are *TooLargeErrors.
func TestCaps(t *testing.T) {
	t.Run("declared size", func(t *testing.T) {
		r := newRegistry(t, false)
		l := fileLayer("a.json", `{"v": 1}`)
		l.size = 1 << 20
		r.push("acme/cfg", "1", l)
		_, err := load(t, plain(r, ociresolver.WithMaxLayerSize(64<<10)), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeTooLarge)
		for _, req := range r.requested() {
			if strings.Contains(req, digestOf(l.data)) {
				t.Errorf("the oversized blob was requested: %s", req)
			}
		}
	})
	t.Run("decompressed layer", func(t *testing.T) {
		r := newRegistry(t, false)
		r.push("acme/cfg", "1", tarLayer(true, file("big.bin", strings.Repeat("\x00", 1<<20)), file("a.json", `{"v": 1}`)))
		_, err := load(t, plain(r, ociresolver.WithMaxLayerSize(64<<10)), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeTooLarge)
	})
	t.Run("file over the include limit", func(t *testing.T) {
		r := newRegistry(t, false)
		r.push("acme/cfg", "1", tarLayer(false, file("a.json", `{"v": "`+strings.Repeat("x", 100)+`"}`)))
		_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")}, cc.WithMaxIncludeSize(50))
		wantCode(t, err, cc.CodeTooLarge)
	})
	t.Run("manifest over 4 MiB", func(t *testing.T) {
		r := newRegistry(t, false)
		l := fileLayer("a.json", `{"v": 1}`)
		l.annotations["padding"] = strings.Repeat("x", 4<<20)
		r.push("acme/cfg", "1", l)
		_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeTooLarge)
	})
}

// DL-217, design.md oci, "Options" and "Auth": credentials come only from WithCredentials, by registry; a registry
// that hands out anonymous tokens needs none; an error from the credentials function is a *FetchError.
func TestCredentials(t *testing.T) {
	r := newRegistry(t, false)
	r.user, r.pass = "u", "p"
	r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
	ref := r.ociRef("acme/cfg", ":1", "a.json")
	_, err := load(t, plain(r), []string{ref})
	wantCode(t, err, cc.CodeBadGateway)
	var asked []string
	creds := ociresolver.WithCredentials(func(_ context.Context, registry string) (ociresolver.Credential, error) {
		asked = append(asked, registry)
		return ociresolver.Credential{Username: "u", Password: "p"}, nil
	})
	wantValue(t, loadOK(t, plain(r, creds), []string{ref}), "v", int64(1))
	if len(asked) == 0 || asked[0] != r.host() {
		t.Errorf("the credentials function was asked for %q, want %s", asked, r.host())
	}
	failing := ociresolver.WithCredentials(func(context.Context, string) (ociresolver.Credential, error) {
		return ociresolver.Credential{}, errors.New("keychain locked")
	})
	_, err = load(t, plain(r, failing), []string{ref})
	wantCode(t, err, cc.CodeBadGateway)

	anon := newRegistry(t, false)
	anon.anonymous = true
	anon.push("acme/cfg", "1", fileLayer("a.json", `{"v": 2}`))
	wantValue(t, loadOK(t, plain(anon), []string{anon.ociRef("acme/cfg", ":1", "a.json")}), "v", int64(2))
	if len(anon.tokenCreds) != 0 {
		t.Errorf("the anonymous token service got credentials %q", anon.tokenCreds)
	}
}

// DL-217: ~/.docker/config.json is never read. Not parallel, because it sets HOME and DOCKER_CONFIG.
func TestNoDockerConfig(t *testing.T) {
	r := newRegistry(t, false)
	r.user, r.pass = "u", "p"
	r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
	home := t.TempDir()
	cfg := `{"auths": {"` + r.host() + `": {"auth": "dTpw"}}}` // u:p
	for _, dir := range []string{filepath.Join(home, ".docker"), filepath.Join(home, "dc")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, "dc"))
	_, err := load(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")})
	wantCode(t, err, cc.CodeBadGateway)
}

// DL-217, design.md oci, "Options": plain HTTP only for the registries WithPlainHTTP names; a TLS registry works
// through WithClient.
func TestPlainHTTP(t *testing.T) {
	r := newRegistry(t, false)
	r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
	_, err := load(t, ociresolver.New(), []string{r.ociRef("acme/cfg", ":1", "a.json")})
	wantCode(t, err, cc.CodeBadGateway)
	_, err = load(t, ociresolver.New(ociresolver.WithPlainHTTP("localhost:1")), []string{r.ociRef("acme/cfg", ":1", "a.json")})
	wantCode(t, err, cc.CodeBadGateway)
	secure := newRegistry(t, true)
	secure.push("acme/cfg", "1", fileLayer("a.json", `{"v": 2}`))
	res := loadOK(t, ociresolver.New(ociresolver.WithClient(secure.Client())), []string{secure.ociRef("acme/cfg", ":1", "a.json")})
	wantValue(t, res, "v", int64(2))
}

// DL-217, design.md oci, "Redirects": blob downloads redirected to another host are followed without allow-list
// checks, but never from https to http; the app's client isn't changed.
func TestRedirects(t *testing.T) {
	t.Run("to a CDN", func(t *testing.T) {
		r := newRegistry(t, false)
		r.redirect = r.cdn.URL
		r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
		allowRegistry := cc.WithDefaultPolicy(cc.AllowResolvers("file", "oci"), cc.AllowFor("oci", cc.Prefix("oci::"+r.host()+"/")))
		wantValue(t, loadOK(t, plain(r), []string{r.ociRef("acme/cfg", ":1", "a.json")}, allowRegistry), "v", int64(1))
		if len(r.cdnRequests()) == 0 {
			t.Errorf("no blob request reached the CDN: %q", r.requested())
		}
	})
	t.Run("https to http", func(t *testing.T) {
		r := newRegistry(t, true)
		r.redirect = r.cdn.URL
		r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
		_, err := load(t, ociresolver.New(ociresolver.WithClient(r.Client())), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		wantCode(t, err, cc.CodeForbidden)
		if got := r.cdnRequests(); len(got) != 0 {
			t.Errorf("the http CDN got %q", got)
		}
	})
	t.Run("client unchanged", func(t *testing.T) {
		r := newRegistry(t, true)
		r.push("acme/cfg", "1", fileLayer("a.json", `{"v": 1}`))
		client := r.Client()
		before := *client
		loadOK(t, ociresolver.New(ociresolver.WithClient(client)), []string{r.ociRef("acme/cfg", ":1", "a.json")})
		if client.CheckRedirect != nil || client.Transport != before.Transport || client.Timeout != before.Timeout {
			t.Errorf("the app's client was changed")
		}
	})
}

// DL-218: a missing repository, tag, digest or path is a *NotFoundError.
func TestNotFound(t *testing.T) {
	r := newRegistry(t, false)
	r.push("acme/cfg", "1", fileLayer("a.json", `{}`))
	for _, ref := range []string{
		r.ociRef("acme/nope", ":1", "a.json"),
		r.ociRef("acme/cfg", ":2", "a.json"),
		r.ociRef("acme/cfg", "@sha256:"+strings.Repeat("0", 64), "a.json"),
		r.ociRef("acme/cfg", ":1", "b.json"),
	} {
		t.Run(ref, func(t *testing.T) {
			_, err := load(t, plain(r), []string{ref})
			wantCode(t, err, cc.CodeNotFound)
		})
	}
}

// DL-218: a server error is a *FetchError.
func TestServerError(t *testing.T) {
	srv := newRegistry(t, false)
	srv.fail = true
	_, err := load(t, plain(srv), []string{srv.ociRef("acme/cfg", ":1", "a.json")})
	wantCode(t, err, cc.CodeBadGateway)
}
