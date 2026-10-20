package cascadingconfig_test

import (
	"context"
	"io/fs"
	"net/url"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// checkNormalised fails the test if ref breaks a rule every normalised reference keeps (DL-200, design.md Traversal
// protection, step 1).
func checkNormalised(t *testing.T, ref cc.IncludeRef) {
	t.Helper()
	s := ref.Ref
	lower := strings.ToLower(s)
	for _, bad := range []string{"%2e", "%2f", "%5c", `\`, "#"} {
		if strings.Contains(lower, bad) {
			t.Errorf("Ref %q contains %q", s, bad)
		}
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			t.Errorf("Ref %q contains the control character %U", s, r)
		}
	}
	if ref.Path != "" && !fs.ValidPath(ref.Path) {
		t.Errorf("Path %q of %q isn't a valid inner path", ref.Path, s)
	}
	if !strings.Contains(ref.Location, "://") {
		return
	}
	u, err := url.Parse(ref.Location)
	if err != nil {
		t.Errorf("Location %q of %q doesn't parse: %v", ref.Location, s, err)
		return
	}
	if u.User != nil {
		if _, hasPw := u.User.Password(); u.Scheme != "ssh" || hasPw {
			t.Errorf("Location %q of %q has userinfo", ref.Location, s)
		}
	}
	for _, r := range u.Host {
		if r > 0x7e || (r >= 'A' && r <= 'Z') {
			t.Errorf("Location %q of %q has a host that isn't lowercase ASCII", ref.Location, s)
		}
	}
	for seg := range strings.SplitSeq(u.EscapedPath(), "/") {
		if seg == "." || seg == ".." {
			t.Errorf("Location %q of %q has a dot segment", ref.Location, s)
		}
	}
}

// normalisedOnce loads rootCfg including raw, with every scheme registered, and returns the reference the resolver
// was asked for, if any.
func normalisedOnce(t *testing.T, raw string) (cc.IncludeRef, bool) {
	t.Helper()
	f := allSchemes()
	fsys := cctest.NewFS(map[string]string{rootCfg: includeOf("", raw)})
	l, err := cc.New("myapp", cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), incScopes, remOpts(f))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _ = l.Load(t.Context())
	calls := f.calls()
	if len(calls) == 0 {
		return cc.IncludeRef{}, false
	}
	return calls[0], true
}

// FuzzNormaliseReference checks that any reference reaching a resolver is normalised: it keeps every rule of
// checkNormalised, and normalising it again changes nothing (DL-197, DL-200).
func FuzzNormaliseReference(f *testing.F) {
	for _, s := range []string{
		"HTTPS://Config.Example.COM./a/./b/../c.json",
		"https://h.example:443/x.json?v=1",
		"https://h.example/%7Euser/a%2Db.json",
		"https://h.example/a.json#sha256=" + emptyHash,
		"https://user@h.example/x.json",
		"https://h.example/a/%2e%2e/x.json",
		"git::https://GitHub.com/acme/./config.git//myapp/./base.json?ref=v1.4.0",
		"git::ssh://git@GitHub.com:22/acme/config.git//a.json?ref=main",
		"git::https://github.com/acme/config.git//dir/../../a.json",
		"oci::GHCR.io/acme/myapp-config:1.4.0",
		"oci::ghcr.io/acme/cfg:1.0@sha256:" + sha64 + "//conf/a.json",
		"hg::https://h.example/repo//a.json?ref=tip",
		"s3://Bucket.Example/a.json",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if raw == "" {
			return
		}
		ref, ok := normalisedOnce(t, raw)
		if !ok {
			return
		}
		checkNormalised(t, ref)
		again, ok := normalisedOnce(t, ref.Ref)
		if !ok {
			t.Fatalf("%q normalised to %q, which no resolver is asked for", raw, ref.Ref)
		}
		if again.Ref != ref.Ref || again.Location != ref.Location || again.Path != ref.Path || again.Version != ref.Version {
			t.Errorf("%q normalised to %+v, and again to %+v", raw, ref, again)
		}
	})
}

// FuzzIncludeRemote checks that whatever a remote file includes, under IncludeNested, never reaches the local
// filesystem, and every reference that reaches a resolver is normalised (DL-204).
func FuzzIncludeRemote(f *testing.F) {
	parents := []string{
		"https://h.example/dir/a.json",
		"git::https://h.example/r.git//dir/a.json?ref=v1",
		"oci::reg.example/acme/cfg:1//dir/a.json",
	}
	for _, s := range []string{
		`{"include": "b.json"}`,
		`{"include": ["../x.json", "~/secret.json", "file:///etc/secret.json", "/etc/secret.json"]}`,
		`{"include": ["../../../../etc/secret.json", "..\\..\\etc\\secret.json", "%2e%2e/secret.json"]}`,
		`{"include": ["https://user@h.example/x.json", "git::ext::sh//a.json", "sub/./c.json"]}`,
	} {
		for i := range parents {
			f.Add(uint8(i), s)
		}
	}
	f.Fuzz(func(t *testing.T, which uint8, body string) {
		parent := parents[int(which)%len(parents)]
		res := allSchemes()
		res.fetch = func(_ context.Context, ref cc.IncludeRef) (*cc.Fetched, error) {
			if ref.Ref == parent {
				return &cc.Fetched{Content: []byte(body), MediaType: "application/json"}, nil
			}
			return &cc.Fetched{Content: []byte(`{"remote": 1}`), MediaType: "application/json"}, nil
		}
		fsys := cctest.NewFS(map[string]string{
			rootCfg:                includeOf("", parent),
			"/etc/secret.json":     `{"secret": 1}`,
			"/home/me/secret.json": `{"secret": 1}`,
			work + "/x.json":       `{"secret": 1}`,
		})
		l, err := cc.New("myapp", cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), incScopes, nested, remOpts(res))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		r, err := l.Load(t.Context())
		if err == nil {
			if _, leaked := r.Tree()["secret"]; leaked {
				t.Errorf("a remote file's include read a local file: %v", r.Tree())
			}
		}
		calls := res.calls()
		if len(calls) == 0 || calls[0].Ref != parent {
			t.Fatalf("the parent %s was never fetched", parent)
		}
		for _, ref := range calls {
			checkNormalised(t, ref)
			if ref.Scheme == "file" {
				t.Errorf("a resolver was asked for the local reference %q", ref.Raw)
			}
		}
	})
}
