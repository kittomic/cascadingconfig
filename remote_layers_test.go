package cascadingconfig_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// DL-179, DL-203: a remote file is identified by its normalised reference, before it's fetched: a reference listed
// twice, in any spelling, is fetched once and loads at its higher position, the other route being LayerDuplicate.
func TestRemoteListedTwice(t *testing.T) {
	t.Parallel()
	const ref = "https://h.example/a.json"
	lower, higher := "https://h.example/a.json", "HTTPS://H.Example/./a.json"
	f := newFake("https").serve(ref, `{"a": 1}`)
	res := remOK(t, f, []string{lower, higher})
	if n := f.count(ref); n != 1 {
		t.Errorf("fetched %d times, want once", n)
	}
	rep := res.Describe()
	if l := layerAt(t, rep, higher); l.Status != cc.LayerLoaded {
		t.Errorf("%s = %v, want LayerLoaded", higher, l.Status)
	}
	if l := layerAt(t, rep, lower); l.Status != cc.LayerDuplicate {
		t.Errorf("%s = %v, want LayerDuplicate", lower, l.Status)
	}
	wantTreeJSON(t, res.Tree(), `{"root": 1, "a": 1}`)
}

// DL-203: different references to the same content are separate layers.
func TestRemoteSameContent(t *testing.T) {
	t.Parallel()
	f := newFake("https").serve("https://h.example/a.json", `{"a": 1}`).serve("https://mirror.example/a.json", `{"a": 1}`)
	res := remOK(t, f, []string{"https://h.example/a.json", "https://mirror.example/a.json"})
	rep := res.Describe()
	for _, p := range []string{"https://h.example/a.json", "https://mirror.example/a.json"} {
		if l := layerAt(t, rep, p); l.Status != cc.LayerLoaded {
			t.Errorf("%s = %v, want LayerLoaded", p, l.Status)
		}
	}
}

// DL-184, DL-203: a remote layer's Path is the reference as written, it takes its metadata from the root file, ranks
// below its includer, and its Remote holds the scheme, normalised reference, identity, pin and cache state.
func TestRemoteLayerMetadata(t *testing.T) {
	t.Parallel()
	const raw = "HTTPS://H.example/a.json"
	f := newFake("https").serveFetched("https://h.example/a.json", cc.Fetched{Content: []byte(`{"root": 2, "a": 2}`), Identity: "id-1"})
	res := remOK(t, f, []string{raw})
	wantTreeJSON(t, res.Tree(), `{"root": 1, "a": 2}`)
	l := layerAt(t, res.Describe(), raw)
	want := &cc.Remote{Scheme: "https", Ref: "https://h.example/a.json", Identity: "id-1"}
	if l.Status != cc.LayerLoaded || l.Scope != "proj" || l.IncludedBy != rootCfg || l.Format != "json" ||
		l.ConfigRoot != work || l.Pattern != "" || !reflect.DeepEqual(l.Remote, want) {
		t.Errorf("layer = %+v, Remote %+v\nwant loaded in proj, included by %s, json, config root %s, Remote %+v",
			l, l.Remote, rootCfg, work, want)
	}
	if l := layerAt(t, res.Describe(), rootCfg); l.Remote != nil {
		t.Errorf("the local file has Remote %+v, want nil", l.Remote)
	}
}

// DL-178, DL-184: under IncludeFlat, an include key in a remote file is a *ParseError, as in a local one.
func TestRemoteFlat(t *testing.T) {
	t.Parallel()
	const ref = "https://h.example/a.json"
	f := newFake("https").serve(ref, includeOf("", "b.json"))
	_, err := remFails(t, f, []string{ref})
	wantErr[*cc.ParseError](t, err, cc.CodeBadRequest, ref, "include")
}

// DL-204, design.md Traversal protection, step 3: under IncludeNested, a relative reference in a remote file resolves
// against the file's remote origin, relative to its directory, and is checked again; the joined reference is the
// layer's Path.
func TestRemoteRelative(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		parent, child, want string
	}{
		{"https://h.example/cfg/a.json", "b.json", "https://h.example/cfg/b.json"},
		{"https://h.example/cfg/a.json", "sub/./c.json", "https://h.example/cfg/sub/c.json"},
		{"https://h.example/cfg/a.json", "../up.json", "https://h.example/up.json"},
		{"https://h.example/cfg/a.json", "/abs.json", "https://h.example/abs.json"},
		{"git::https://h.example/r.git//dir/a.json?ref=v1", "b.json", "git::https://h.example/r.git//dir/b.json?ref=v1"},
		{"git::https://h.example/r.git//dir/a.json?ref=v1", "../top.json", "git::https://h.example/r.git//top.json?ref=v1"},
		{"oci::reg.example/acme/cfg:1//dir/a.json", "b.json", "oci::reg.example/acme/cfg:1//dir/b.json"},
	} {
		t.Run(tc.parent+" "+tc.child, func(t *testing.T) {
			f := allSchemes()
			f.fetch = nil
			// The fake serves by normalised reference, so the parent's normalised form must be its raw form here.
			f.serve(tc.parent, includeOf(`"parent": 1`, tc.child)).serve(tc.want, `{"child": 1}`)
			res := remOK(t, f, []string{tc.parent}, nested)
			wantTreeJSON(t, res.Tree(), `{"root": 1, "parent": 1, "child": 1}`)
			if tc.child == "b.json" {
				if l := layerAt(t, res.Describe(), tc.want); l.IncludedBy != tc.parent || l.Remote == nil {
					t.Errorf("layer = %+v, want included by %s, with Remote set", l, tc.parent)
				}
			}
		})
	}
}

// DL-204: a remote file can never reach the local filesystem: climbing above the repo or artifact root, ~/ and file://
// are *ReferenceErrors naming the remote file. This is an attack suite.
func TestRemoteRelativeEscapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ parent, child string }{
		{"https://h.example/cfg/a.json", "~/.ssh/config.json"},
		{"https://h.example/cfg/a.json", "file:///etc/myapp/config.json"},
		{"git::https://h.example/r.git//dir/a.json?ref=v1", "../../out.json"},
		{"git::https://h.example/r.git//dir/a.json?ref=v1", "~/x.json"},
		{"git::https://h.example/r.git//dir/a.json?ref=v1", "file:///etc/myapp/config.json"},
		{"oci::reg.example/acme/cfg:1//dir/a.json", "../../out.json"},
	} {
		t.Run(tc.parent+" "+tc.child, func(t *testing.T) {
			f := allSchemes()
			f.fetch = nil
			f.serve(tc.parent, includeOf("", tc.child))
			fsys := cctest.NewFS(map[string]string{
				rootCfg: includeOf("", tc.parent), "/etc/myapp/config.json": `{}`, "/home/me/x.json": `{}`,
			})
			_, err := incLoad(t, fsys, remOpts(f), nested)
			wantErr[*cc.ReferenceError](t, err, cc.CodeBadRequest, tc.parent)
			if n := len(f.refs()); n != 1 {
				t.Errorf("the resolver was asked %d times, want once, for the parent", n)
			}
		})
	}
}

// DL-204, design.md Per-scope policies, "Which scope judges an include": an absolute remote reference in a remote file
// is judged by the root file's policy.
func TestRemoteNestedPolicy(t *testing.T) {
	t.Parallel()
	const parent = "https://h.example/cfg/a.json"
	f := newFake("https").serve(parent, includeOf("", "https://other.example/c.json")).serve("https://other.example/c.json", `{}`)
	_, err := remFails(t, f, []string{parent}, nested,
		cc.WithScopePolicy("proj", cc.AllowResolvers("file", "https"), cc.AllowFor("https", cc.Prefix("https://h.example/"))))
	wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, parent)
	if n := f.count("https://other.example/c.json"); n != 0 {
		t.Errorf("the rejected reference was fetched %d times", n)
	}
}

// DL-179, DL-203: under IncludeNested, a remote file that reaches itself again is a cycle.
func TestRemoteCycle(t *testing.T) {
	t.Parallel()
	f := newFake("https").
		serve("https://h.example/a.json", includeOf("", "b.json")).
		serve("https://h.example/b.json", includeOf("", "a.json"))
	_, err := remFails(t, f, []string{"https://h.example/a.json"}, nested)
	wantErr[*cc.CycleError](t, err, cc.CodeLoopDetected, "https://h.example/b.json")
}

// DL-205, design.md misehardened.Preset, "Remotes off": under MiseHardenedLayout a remote reference is a 501, even with
// a resolver registered before WithoutRemoteIncludes. No layout registers a remote resolver, so MiseLayout gives a 501
// too.
func TestHardenedRemotes(t *testing.T) {
	t.Parallel()
	const ref = "https://h.example/a.json"
	files := map[string]string{rootCfg: includeOf("", ref)}
	f := newFake("https").serve(ref, `{}`)
	t.Run("MiseHardenedLayout", func(t *testing.T) {
		_, err := hardenedLoad(t, cctest.NewFS(files))
		wantErr[*cc.UnsupportedSchemeError](t, err, cc.CodeNotImplemented, rootCfg)
	})
	t.Run("MiseLayout", func(t *testing.T) {
		l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(testHost(nil)))
		if err != nil {
			t.Fatalf("NewWithPreset: %v", err)
		}
		_, err = l.Load(t.Context())
		wantErr[*cc.UnsupportedSchemeError](t, err, cc.CodeNotImplemented, rootCfg)
	})
	t.Run("registered before", func(t *testing.T) {
		remOK(t, f, []string{ref})
		_, err := remFails(t, f, []string{ref}, cc.WithoutRemoteIncludes())
		wantErr[*cc.UnsupportedSchemeError](t, err, cc.CodeNotImplemented, rootCfg)
	})
}

// DL-205: after WithoutRemoteIncludes, a resolver can be registered again, but every policy that allows its scheme must
// give it an allow-list and RequirePinned, or New fails.
func TestHardenedRemotesBackOn(t *testing.T) {
	t.Parallel()
	https := newFake("https")
	base := []cc.Option{scope, cc.WithoutRemoteIncludes(), cc.WithIncludeResolver(https)}
	allowList := cc.AllowFor("https", cc.Prefix("https://config.acme.internal/"))
	runOptionErrorCases(t, []optionErrorCase{
		{name: "engine default policy", opts: base, ops: []string{"New"}, contains: []string{"https"}},
		{name: "no allow-list", opts: append(base, cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), cc.RequirePinned())), ops: []string{"New"}},
		{name: "not pinned", opts: append(base, cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), allowList)), ops: []string{"New"}},
		{name: "scope policy", opts: append(base, cc.WithDefaultPolicy(cc.AllowResolvers("file")),
			cc.WithScopePolicy("s", cc.AllowResolvers("https"), allowList)), ops: []string{"New"}},
	})
	for _, tc := range []struct {
		name string
		opts []cc.Option
	}{
		{"scheme allow-list", []cc.Option{cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), allowList, cc.RequirePinned())}},
		{"global allow-list", []cc.Option{cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), cc.Allow(cc.Prefix("https://config.acme.internal/")), cc.RequirePinned())}},
		{"remote scheme not allowed", []cc.Option{cc.WithDefaultPolicy(cc.AllowResolvers("file"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cc.New("myapp", append(append([]cc.Option{}, base...), tc.opts...)...); err != nil {
				t.Errorf("New: %v", err)
			}
		})
	}
	t.Run("pinned reference loads", func(t *testing.T) {
		ref := "https://config.acme.internal/a.json#sha256=" + sum(`{"a": 1}`)
		g := newFake("https").serve("https://config.acme.internal/a.json", `{"a": 1}`)
		fsys := cctest.NewFS(map[string]string{rootCfg: includeOf("", ref)})
		res, err := hardenedLoad(t, fsys, cc.WithIncludeResolver(g), cc.WithoutIncludeCache(),
			cc.WithScopePolicy(cc.ScopeProject, cc.InheritDefault(), cc.AllowResolvers("file", "https"), allowList, cc.RequirePinned()))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	})
}

// DL-203, DL-205: the report shows a remote layer's Remote and each policy's pin rule, in
// Describe(), its JSON and its text.
func TestRemoteReport(t *testing.T) {
	t.Parallel()
	const raw = "https://h.example/a.json#sha256="
	ref := raw + sum(`{"a": 1}`)
	f := newFake("https").serveFetched("https://h.example/a.json", cc.Fetched{Content: []byte(`{"a": 1}`), Identity: "id-1"})
	res := remOK(t, f, []string{ref},
		cc.WithScopePolicy("proj", cc.AllowResolvers("file", "https"), cc.AllowFor("https", cc.Prefix("https://h.example/")), cc.RequirePinned()))
	rep := res.Describe()
	if p := scopeInfo(t, rep, "proj").Policy; p == nil || !p.RequirePinned {
		t.Errorf("proj policy = %+v, want RequirePinned", p)
	}
	l := layerAt(t, rep, ref)
	js, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"remote":{`, `"scheme":"https"`, `"ref":"https://h.example/a.json"`, `"identity":"id-1"`, `"pinned":true`} {
		if !strings.Contains(string(js), key) {
			t.Errorf("layer JSON %s doesn't contain %s", js, key)
		}
	}
	pjs, err := json.Marshal(scopeInfo(t, rep, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pjs), `"require_pinned":true`) {
		t.Errorf("scope JSON %s doesn't contain require_pinned", pjs)
	}
	text := rep.String()
	for _, want := range []string{"remote https://h.example/a.json @ id-1", `allow for https: Prefix("https://h.example/"); pinned`} {
		if !strings.Contains(text, want) {
			t.Errorf("report:\n%s\nwant it to contain %q", text, want)
		}
	}
}
