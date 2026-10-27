// These tests use Unix paths in cctest's world, which aren't absolute on Windows, so the file doesn't build there
// (DL-282).

//go:build !windows

package cascadingconfig_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// fuzzFS is a project at /home/me/work with files inside it, files outside it, and symlinks that lead out, the same
// escapes as the confinement attack suite. Every file outside the project sets "out".
func fuzzFS(root string) *cctest.FS {
	fsys := cctest.NewFS(map[string]string{
		rootCfg:                 root,
		work + "/a.json":        `{"in": 1}`,
		work + "/sub/b.json":    `{"in": 2}`,
		work + "/sub/c.toml":    `{"in": 3}`,
		"/home/me/out.json":     `{"out": 1}`,
		"/home/me/workx/a.json": `{"out": 2}`,
		"/srv/secret.json":      `{"out": 3}`,
		"/etc/myapp/x.json":     `{"out": 4}`,
	})
	fsys.Symlink("/srv/secret.json", work+"/link.json")
	fsys.Symlink("/srv", work+"/dir")
	fsys.Symlink("../../../../srv", work+"/sub/up")
	fsys.Symlink(work+"/a.json", "/srv/in.json")
	fsys.Symlink("loop.json", work+"/loop.json")
	return fsys
}

// expectedCodes are the codes a load with includes may fail with: bad references and parse errors, policy rejections,
// missing files, files over the limit, formats without a decoder, unreadable files, remote schemes and cycles.
var expectedCodes = []cc.Code{
	cc.CodeBadRequest, cc.CodeForbidden, cc.CodeNotFound, cc.CodeTooLarge, cc.CodeUnsupportedMediaType,
	cc.CodeInternal, cc.CodeNotImplemented, cc.CodeLoopDetected,
}

// checkFuzzLoad loads fsys with nested, confined includes, and checks that nothing outside the project loaded and that
// every error is a typed one.
func checkFuzzLoad(t *testing.T, fsys *cctest.FS, what string) {
	t.Helper()
	res, err := newLoaderFS(t, fsys, nil, incScopes, nested,
		cc.WithDecoder(jsonNamed{"toml", []string{"toml"}}),
		cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("{config_root}")),
	).Load(t.Context())
	if res == nil {
		t.Fatalf("%s: Load returned a nil Result", what)
	}
	if err != nil {
		for _, e := range cc.Errors(err) {
			if code := cc.CodeOf(e); !slices.Contains(expectedCodes, code) {
				t.Fatalf("%s: error %v has code %d, want one of %v", what, e, code, expectedCodes)
			}
		}
		return
	}
	if _, ok := res.Tree()["out"]; ok {
		t.Fatalf("%s: a file outside the project loaded: %v", what, res.Tree())
	}
	for _, l := range res.Describe().Layers {
		if l.IncludedBy == "" || l.Status != cc.LayerLoaded {
			continue
		}
		target, err := fsys.EvalSymlinks(l.Path)
		if err != nil {
			t.Fatalf("%s: the included layer %s loaded, but doesn't resolve: %v", what, l.Path, err)
		}
		if rel, err := filepath.Rel(work, target); err != nil || !filepath.IsLocal(rel) {
			t.Fatalf("%s: the included layer %s resolves to %s, outside %s", what, l.Path, target, work)
		}
	}
}

// FuzzIncludeConfinement checks, for any reference, that a confined include never loads a file outside its roots,
// whatever dot segments, symlinks, URL forms or prefixes it uses, and that every failure is a typed error (DL-180,
// DL-183, DL-185).
func FuzzIncludeConfinement(f *testing.F) {
	for _, ref := range []string{
		"a.json", "sub/b.json", "./sub/c.toml", "../out.json", "sub/../../out.json", "/etc/myapp/x.json",
		"file:///srv/secret.json", "file://localhost/home/me/work/a.json", "file://host/a.json", "~/out.json",
		"/home/me/workx/a.json", "../workx/a.json", "link.json", "dir/secret.json", "sub/up/secret.json",
		"/srv/in.json", "loop.json", "myapp.json", "git::https://example.com/x.git//a.json", "https://example.com/a.json",
		`C:\a.json`, "", ".", "..", "/", "sub", "a.json\x00", "%2e%2e/out.json", "file:///home/me/work/../out.json",
	} {
		f.Add(ref)
	}
	f.Fuzz(func(t *testing.T, ref string) {
		r, err := json.Marshal(ref)
		if err != nil {
			return
		}
		checkFuzzLoad(t, fuzzFS(`{"include": `+string(r)+`}`), "reference "+string(r))
	})
}

// FuzzIncludeFile checks, for any including file, that reading its include key never panics, never loads a file
// outside the project, and only fails with typed errors (DL-184).
func FuzzIncludeFile(f *testing.F) {
	for _, root := range []string{
		`{"include": "a.json"}`, `{"include": ["a.json", "sub/b.json"]}`, `{"include": []}`, `{"include": [1]}`,
		`{"include": {"a": 1}}`, `{"include": null}`, `{"include": ["", "a.json"]}`, `{"include": "../out.json"}`,
		`{"include": "myapp.json"}`, `{"a": {"include": "x"}, "include": "sub/b.json"}`, `{"include": true}`, `[]`, `{`,
	} {
		f.Add(root)
	}
	f.Fuzz(func(t *testing.T, root string) {
		checkFuzzLoad(t, fuzzFS(root), "file "+root)
	})
}
