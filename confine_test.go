package cascadingconfig_test

import (
	"encoding/json"
	"errors"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// The confinement tests are an attack suite (implementation-plan.md, security behaviour): every way out of the
// confinement roots is a 403 naming the including file, never a skip.

// confined is a default policy that allows local includes under the including file's config root only. proj's config
// root is /home/me/work.
var confined = cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("{config_root}"))

// confineFS is a project with files and links inside and outside /home/me/work.
func confineFS(ref string) *cctest.FS {
	r, _ := json.Marshal(ref)
	fsys := cctest.NewFS(map[string]string{
		rootCfg:                 `{"include": ` + string(r) + `}`,
		work + "/sub/a.json":    `{"in": 1}`,
		work + "/real.json":     `{"in": 1}`,
		"/home/me/outside.json": `{"out": 1}`,
		"/home/me/workx/a.json": `{"out": 1}`,
		"/srv/secret.json":      `{"out": 1}`,
		"/etc/myapp/x.json":     `{"out": 1}`,
	})
	fsys.Symlink("/srv/secret.json", work+"/link.json")
	fsys.Symlink("../../../../srv", work+"/sub/up")
	fsys.Symlink("/srv", work+"/dir")
	fsys.Symlink(work+"/real.json", "/srv/in.json")
	return fsys
}

// DL-180: references that resolve outside the roots, directly, through dot segments, through symlinks to files or
// directories, or into a sibling with the same prefix, are *PolicyErrors.
func TestConfineEscapes(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	for _, ref := range []string{
		"../outside.json",
		"sub/../../outside.json",
		"/etc/myapp/x.json",
		"file:///srv/secret.json",
		"~/outside.json",
		"/home/me/workx/a.json",
		"../workx/a.json",
		"link.json",
		"dir/secret.json",
		"sub/up/secret.json",
	} {
		t.Run(ref, func(t *testing.T) {
			res, err := incLoad(t, confineFS(ref), confined)
			if err == nil {
				t.Fatalf("Load succeeded, want a *PolicyError")
			}
			if !errors.Is(err, cc.ErrPolicy) {
				t.Fatalf("error = %v, want a *PolicyError", err)
			}
			wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg, ref)
			if res != nil && res.Tree() != nil {
				t.Errorf("a failed load has a tree: %v", res.Tree())
			}
		})
	}
}

// DL-180: references inside the roots load, including through a symlink outside the roots whose target is inside,
// because the target is what's checked.
func TestConfineAllows(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	for _, ref := range []string{"sub/a.json", work + "/sub/a.json", "./sub/a.json", "/srv/in.json"} {
		t.Run(ref, func(t *testing.T) {
			res, err := incLoad(t, confineFS(ref), confined)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantTreeJSON(t, res.Tree(), `{"in": 1}`)
		})
	}
}

// DL-180, design.md Local files, "Confine": roots are resolved through symlinks too; a root that doesn't exist matches
// nothing.
func TestConfineRoots(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	t.Run("a root through a symlink", func(t *testing.T) {
		fsys := confineFS("sub/a.json")
		fsys.Symlink(work, "/srv/link")
		res, err := incLoad(t, fsys, cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/srv/link")))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"in": 1}`)
	})
	t.Run("a missing root", func(t *testing.T) {
		_, err := incLoad(t, confineFS("sub/a.json"), cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/nowhere")))
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg, "sub/a.json")
	})
	t.Run("a missing root among others", func(t *testing.T) {
		res, err := incLoad(t, confineFS("sub/a.json"),
			cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/nowhere", "{config_root}")))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"in": 1}`)
	})
	t.Run("~", func(t *testing.T) {
		res, err := incLoad(t, confineFS("../outside.json"), cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("~")))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"out": 1}`)
	})
	t.Run("{user}", func(t *testing.T) {
		fsys := confineFS("/home/me/.config/myapp/shared.json")
		fsys.WriteFile("/home/me/.config/myapp/shared.json", []byte(`{"in": 1}`), 0o644)
		opts := cc.Options(cc.WithPlatformDirs(cc.XDGDirs), cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("{user}")))
		res, err := incLoad(t, fsys, opts)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"in": 1}`)
		_, err = incLoad(t, confineFS("../outside.json"), opts)
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg, "../outside.json")
	})
}

// DL-180: confinement applies to includes only. Searched and explicit files outside the roots load.
func TestConfineOnlyIncludes(t *testing.T) {
	t.Parallel()
	files := map[string]string{sysFile: `{"a": 1}`, "/srv/x.json": `{"b": 2}`}
	res, err := newLoader(t, files, nil, incScopes,
		cc.WithScope("x", cc.ExplicitFile("/srv/x.json")), cc.WithScopes("sys", "proj", "x"),
		cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/nowhere")),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2}`)
}

// DL-20, DL-189: MiseHardenedLayout confines local includes to the config root, {user} and {system}.
func TestHardenedConfinement(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	userCfg := "/home/me/.config/myapp/config.json"
	systemCfg := "/etc/myapp/config.json"
	for _, tc := range []struct {
		name, from, ref string
		ok              bool
	}{
		{"project, inside", rootCfg, "conf/a.json", true},
		{"project, outside", rootCfg, "../../../etc/passwd.json", false},
		{"project, into the user dir", rootCfg, "/home/me/.config/myapp/a.json", true},
		{"user, under ~", userCfg, "~/dotfiles/a.json", true},
		{"system, inside", systemCfg, "shared/a.json", true},
		{"system, into a project", systemCfg, work + "/conf/a.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref, _ := json.Marshal(tc.ref)
			files := map[string]string{
				tc.from:                         `{"include": ` + string(ref) + `}`,
				work + "/conf/a.json":           `{"a": 1}`,
				"/etc/passwd.json":              `{"a": 1}`,
				"/home/me/.config/myapp/a.json": `{"a": 1}`,
				"/home/me/dotfiles/a.json":      `{"a": 1}`,
				"/etc/myapp/shared/a.json":      `{"a": 1}`,
			}
			l, err := cc.NewWithPreset("myapp", cc.MiseHardenedLayout, cc.WithFileSystem(cctest.NewFS(files)),
				cctest.WithHost(testHost(nil)))
			if err != nil {
				t.Fatalf("NewWithPreset: %v", err)
			}
			res, err := l.Load(t.Context())
			if !tc.ok {
				wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, tc.from, tc.ref)
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantTreeJSON(t, res.Tree(), `{"a": 1}`)
		})
	}
}
