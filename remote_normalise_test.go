package cascadingconfig_test

import (
	"errors"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// allSchemes is a fake for every built-in remote scheme, plus custom URL-form and getter-form schemes, answering
// every reference with `{}`, whose SHA-256 is emptyHash.
func allSchemes() *fakeResolver {
	f := newFake("http", "https", "git", "oci", "s3", "hg")
	f.fetch = anyFile(`{}`, "application/json")
	return f
}

var emptyHash = sum(`{}`)

// DL-197, DL-200, design.md Allow-lists, "The normalised reference string": each form is normalised before the resolver
// sees it, and IncludeRef's fields are split out as documented.
func TestNormalisedReferences(t *testing.T) {
	digest := "sha256:" + sha64
	for _, tc := range []struct {
		raw, ref, location, path, version string
	}{
		// URL form
		{raw: "HTTPS://Config.Example.COM./a/./b/../c.json", ref: "https://config.example.com/a/c.json", location: "https://config.example.com/a/c.json"},
		{raw: "https://h.example:443/x.json", ref: "https://h.example/x.json", location: "https://h.example/x.json"},
		{raw: "http://h.example:80/x.json", ref: "http://h.example/x.json", location: "http://h.example/x.json"},
		{raw: "https://h.example:8443/x.json", ref: "https://h.example:8443/x.json", location: "https://h.example:8443/x.json"},
		{raw: "https://h.example", ref: "https://h.example/", location: "https://h.example/"},
		{raw: "https://h.example/%7Euser/a%2Db.json", ref: "https://h.example/~user/a-b.json", location: "https://h.example/~user/a-b.json"},
		{raw: "https://h.example/a%3ab.json", ref: "https://h.example/a%3Ab.json", location: "https://h.example/a%3Ab.json"},
		{raw: "https://h.example/c.json?v=1&b=2", ref: "https://h.example/c.json?v=1&b=2", location: "https://h.example/c.json?v=1&b=2"},
		{raw: "https://xn--bcher-kva.example/a.json", ref: "https://xn--bcher-kva.example/a.json", location: "https://xn--bcher-kva.example/a.json"},
		{raw: "https://h.example/a.json?b=1#sha256=" + emptyHash, ref: "https://h.example/a.json?b=1", location: "https://h.example/a.json?b=1", version: "sha256:" + emptyHash},
		{raw: "s3://Bucket.Example/a.json", ref: "s3://bucket.example/a.json", location: "s3://bucket.example/a.json"},
		// getter form
		{raw: "git::https://GitHub.com/acme/./config.git//myapp/./base.json?ref=v1.4.0", ref: "git::https://github.com/acme/config.git//myapp/base.json?ref=v1.4.0", location: "https://github.com/acme/config.git", path: "myapp/base.json", version: "v1.4.0"},
		{raw: "git::https://github.com/acme/config.git//a.json", ref: "git::https://github.com/acme/config.git//a.json", location: "https://github.com/acme/config.git", path: "a.json"},
		{raw: "git::ssh://git@GitHub.com:22/acme/config.git//a.json?ref=main", ref: "git::ssh://git@github.com/acme/config.git//a.json?ref=main", location: "ssh://git@github.com/acme/config.git", path: "a.json", version: "main"},
		{raw: "git::file:///srv/repos/config//dir/a.json?ref=" + sha40, ref: "git::file:///srv/repos/config//dir/a.json?ref=" + sha40, location: "file:///srv/repos/config", path: "dir/a.json", version: sha40},
		{raw: "hg::https://h.example/repo//a.json?ref=tip", ref: "hg::https://h.example/repo//a.json?ref=tip", location: "https://h.example/repo", path: "a.json", version: "tip"},
		// image form
		{raw: "oci::GHCR.io/acme/myapp-config:1.4.0", ref: "oci::ghcr.io/acme/myapp-config:1.4.0//myapp.toml", location: "ghcr.io/acme/myapp-config", path: "myapp.toml", version: "1.4.0"},
		{raw: "oci::ghcr.io/acme/cfg", ref: "oci::ghcr.io/acme/cfg:latest//myapp.toml", location: "ghcr.io/acme/cfg", path: "myapp.toml", version: "latest"},
		{raw: "oci::ghcr.io/acme/cfg@" + digest + "//conf/a.json", ref: "oci::ghcr.io/acme/cfg@" + digest + "//conf/a.json", location: "ghcr.io/acme/cfg", path: "conf/a.json", version: digest},
		{raw: "oci::ghcr.io/acme/cfg:1.0@" + digest + "//a.json", ref: "oci::ghcr.io/acme/cfg:1.0@" + digest + "//a.json", location: "ghcr.io/acme/cfg", path: "a.json", version: "1.0@" + digest},
		{raw: "oci::localhost:5000/acme/cfg:1//./a.json", ref: "oci::localhost:5000/acme/cfg:1//a.json", location: "localhost:5000/acme/cfg", path: "a.json", version: "1"},
		{raw: "oci::localhost/cfg:1//a.json", ref: "oci::localhost/cfg:1//a.json", location: "localhost/cfg", path: "a.json", version: "1"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			f := allSchemes()
			remOK(t, f, []string{tc.raw})
			calls := f.calls()
			if len(calls) != 1 {
				t.Fatalf("the resolver was asked %d times, want once", len(calls))
			}
			got := calls[0]
			if got.Raw != tc.raw || got.Ref != tc.ref || got.Location != tc.location || got.Path != tc.path ||
				got.Version != tc.version {
				t.Errorf("IncludeRef = Raw %q, Ref %q, Location %q, Path %q, Version %q\nwant Raw %q, Ref %q, Location %q, Path %q, Version %q",
					got.Raw, got.Ref, got.Location, got.Path, got.Version, tc.raw, tc.ref, tc.location, tc.path, tc.version)
			}
			if got.From.Path != rootCfg || got.From.Scope != "proj" {
				t.Errorf("From = %s in %s, want %s in proj", got.From.Path, got.From.Scope, rootCfg)
			}
		})
	}
}

// DL-197, DL-200, design.md Traversal protection: references that could pass an allow-list in one form and be fetched
// in another, or that don't follow their form, are *ReferenceErrors naming the including file, and never reach a
// resolver. This is an attack suite.
func TestRejectedReferences(t *testing.T) {
	for _, raw := range []string{
		// userinfo
		"https://user@h.example/x.json",
		"https://user:pw@h.example/x.json",
		"https://config.acme.internal@evil.example/x.json",
		"git::https://user@github.com/acme/config.git//a.json",
		"git::ssh://git:secret@github.com/acme/config.git//a.json",
		// encoded separators and dots, backslashes and control characters
		"https://h.example/a/%2e%2e/x.json",
		"https://h.example/a/%2E%2E/x.json",
		"https://h.example/a%2fb.json",
		"https://h.example/a%2Fb.json",
		"https://h.example/a%5cb.json",
		"https://h.example/a%5Cb.json",
		`https://h.example/a\b.json`,
		"https://h.example/a\x01b.json",
		"https://h.example/a\x00b.json",
		// hosts and ports
		"https://exämple.com/x.json",
		"https:///x.json",
		"https://h.example:99999/x.json",
		"https://h.example:port/x.json",
		// fragments
		"https://h.example/x.json#top",
		"https://h.example/x.json#sha256=abc",
		// getter form
		"http::https://h.example/x.json",
		"git::https://github.com/acme/config.git?ref=v1",
		"git::https://github.com/acme/config.git//a.json?ref=v1&sshkey=abc",
		"git::https://github.com/acme/config.git//a.json?depth=1",
		"git::https://github.com/acme/config.git//a.json?ref=",
		"git::https://github.com/acme/config.git//a.json?ref=-x",
		"git::https://github.com/acme/config.git//a.json?ref=a..b",
		"git::https://github.com/acme/config.git//a.json?ref=a@{1}",
		"git::https://github.com/acme/config.git//a.json?ref=a b",
		"git::https://github.com/acme/config.git//a.json?ref=a~1",
		"git::https://github.com/acme/config.git//a.json?ref=a^",
		"git::https://github.com/acme/config.git//a.json?ref=a:b",
		"git::https://github.com/acme/config.git//a.json?ref=a*",
		"git::https://github.com/acme/config.git//a.json?ref=a[b",
		`git::https://github.com/acme/config.git//a.json?ref=a\b`,
		"git::git@github.com:acme/config.git//a.json",
		"git::ext::sh -c touch% /tmp/pwned//a.json",
		"git::/srv/repos/config//a.json",
		// inner paths
		"git::https://github.com/acme/config.git//../a.json",
		"git::https://github.com/acme/config.git//dir/../../a.json",
		`git::https://github.com/acme/config.git//dir\a.json`,
		"git::https://github.com/acme/config.git//dir/a\x01.json",
		"oci::ghcr.io/acme/cfg:1//../a.json",
		// image form
		"oci::acme/cfg:1",
		"oci::alpine",
		"oci::ghcr.io/Acme/cfg:1",
		"oci::ghcr.io/acme/cfg:-bad",
		"oci::ghcr.io/acme/cfg@sha512:" + sha64 + sha64,
		"oci::ghcr.io/acme/cfg@sha256:abc",
	} {
		t.Run(strings.ToValidUTF8(raw, "?"), func(t *testing.T) {
			f := allSchemes()
			_, err := remFails(t, f, []string{raw})
			wantErr[*cc.ReferenceError](t, err, cc.CodeBadRequest, rootCfg)
			var re *cc.ReferenceError
			if errors.As(err, &re) && re.Op != "include" {
				t.Errorf("Op = %q, want include", re.Op)
			}
			if n := len(f.refs()); n != 0 {
				t.Errorf("the resolver was asked %d times, want 0", n)
			}
		})
	}
}

// DL-56, DL-176, DL-202, design.md Allow-lists, "Matchers": Glob's * stays within a segment and ** crosses them;
// Regexp matches the whole normalised string; Prefix sees the query; Extension and FileName see only the file path.
func TestRemoteMatchers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		m     cc.Matcher
		raw   string
		allow bool
	}{
		{"Glob * in one segment", cc.Glob("https://h.example/acme/*"), "https://h.example/acme/a.json", true},
		{"Glob * doesn't cross /", cc.Glob("https://h.example/acme/*"), "https://h.example/acme/team/a.json", false},
		{"Glob ** crosses /", cc.Glob("https://h.example/acme/**"), "https://h.example/acme/team/a.json", true},
		{"Glob on git", cc.Glob("git::https://github.com/acme/*"), "git::https://github.com/acme/config.git//a.json?ref=v1", false},
		{"Glob ** on git", cc.Glob("git::https://github.com/acme/**"), "git::https://github.com/acme/config.git//a.json?ref=v1", true},
		{"Glob for one owner's repos", cc.Glob("git::https://github.com/acme/*//**"), "git::https://github.com/acme/config.git//a.json?ref=v1", true},
		{"Glob for one owner's repos, nested", cc.Glob("git::https://github.com/acme/*//**"), "git::https://github.com/acme/team/config.git//a.json?ref=v1", false},
		{"Glob sees the normalised form", cc.Glob("https://h.example/acme/*"), "HTTPS://H.EXAMPLE:443/x/../acme/a.json", true},
		{"Regexp whole string", cc.Regexp(`https://h\.example/.*`), "https://h.example/a.json", true},
		{"Regexp isn't a search", cc.Regexp(`h\.example`), "https://h.example/a.json", false},
		{"Prefix sees the query", cc.Prefix("https://h.example/a.json?v="), "https://h.example/a.json?v=2", true},
		{"Prefix without a slash", cc.Prefix("https://h.example"), "https://h.example.evil.example/a.json", true},
		{"Extension ignores the query", cc.Extension("json"), "https://h.example/a.json?x=y.toml", true},
		{"Extension ignores the query, no match", cc.Extension("toml"), "https://h.example/a.json?x=y.toml", false},
		{"Extension on an inner path", cc.Extension("json"), "git::https://h.example/r.toml//a.json?ref=v1", true},
		{"FileName on an inner path", cc.FileName("a.json"), "git::https://h.example/r.git//dir/a.json?ref=v1", true},
		{"FileName ignores the ref", cc.FileName("v1"), "git::https://h.example/r.git//dir/a.json?ref=v1", false},
		{"FileName on oci", cc.FileName("myapp.toml"), "oci::ghcr.io/acme/cfg:1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := allSchemes()
			_, err := remLoad(t, f, []string{tc.raw},
				cc.WithDefaultPolicy(cc.AllowResolvers("file", "https", "git", "oci"), cc.Allow(tc.m)))
			switch {
			case tc.allow && err != nil:
				t.Errorf("Load: %v, want the reference allowed", err)
			case !tc.allow:
				wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg)
				if n := len(f.refs()); n != 0 {
					t.Errorf("the resolver was asked %d times, want 0", n)
				}
			}
		})
	}
}

// DL-55, DL-201, design.md Remote references, "Pinned references": the loader decides pins from the form, and
// RequirePinned rejects anything else before fetching.
func TestRequirePinned(t *testing.T) {
	pinned := cc.WithDefaultPolicy(cc.AllowResolvers("file", "https", "git", "oci", "s3", "hg"), cc.RequirePinned())
	for _, tc := range []struct {
		raw    string
		pinned bool
	}{
		{"git::https://h.example/r.git//a.json?ref=" + sha40, true},
		{"git::https://h.example/r.git//a.json?ref=" + sha64, true},
		{"git::https://h.example/r.git//a.json?ref=" + sha40[:12], false},
		{"git::https://h.example/r.git//a.json?ref=v1.4.0", false},
		{"git::https://h.example/r.git//a.json", false},
		{"oci::ghcr.io/acme/cfg@sha256:" + sha64, true},
		{"oci::ghcr.io/acme/cfg:1.0@sha256:" + sha64, true},
		{"oci::ghcr.io/acme/cfg:1.0", false},
		{"https://h.example/a.json#sha256=" + emptyHash, true},
		{"https://h.example/a.json", false},
		{"s3://bucket.example/a.json#sha256=" + emptyHash, true},
		{"s3://bucket.example/a.json?versionId=7", false},
		{"hg::https://h.example/repo//a.json?ref=" + sha40, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			f := allSchemes()
			res, err := remLoad(t, f, []string{tc.raw}, pinned)
			if !tc.pinned {
				wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg, "pinned")
				if n := len(f.refs()); n != 0 {
					t.Errorf("the resolver was asked %d times, want 0", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if l := layerAt(t, res.Describe(), tc.raw); l.Remote == nil || !l.Remote.Pinned {
				t.Errorf("Remote = %+v, want pinned", l.Remote)
			}
		})
	}
}

// DL-55, DL-201: the loader checks an integrity fragment against the content itself, with or without RequirePinned, and
// never passes the fragment to the resolver.
func TestIntegrityFragment(t *testing.T) {
	good := "https://h.example/a.json#sha256=" + emptyHash
	t.Run("match", func(t *testing.T) {
		f := allSchemes()
		res := remOK(t, f, []string{good})
		got := f.calls()[0]
		if strings.Contains(got.Location, "#") || strings.Contains(got.Ref, "#") {
			t.Errorf("the resolver saw Location %q and Ref %q, want no fragment", got.Location, got.Ref)
		}
		if l := layerAt(t, res.Describe(), good); l.Remote == nil || !l.Remote.Pinned || l.Remote.Ref != "https://h.example/a.json" {
			t.Errorf("Remote = %+v, want pinned, with the fragment dropped from Ref", l.Remote)
		}
	})
	for _, tc := range []struct{ name, raw string }{
		{"mismatch", "https://h.example/a.json#sha256=" + sum("other content")},
		{"mismatch on a custom scheme", "s3://bucket.example/a.json#sha256=" + sum("other content")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := remFails(t, allSchemes(), []string{tc.raw})
			wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg)
		})
	}
}
