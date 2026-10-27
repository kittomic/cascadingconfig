package cascadingconfig_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// The permission tests use MiseHardenedLayout, whose default policy checks files with StrictModes (DL-189, DL-191),
// on a host whose current user is uid 1000.

const myUID = 1000

func hardenedHost() cctest.Host {
	h := testHost(nil)
	h.UID = myUID
	return h
}

// hardenedLoad loads fsys with MiseHardenedLayout on hardenedHost.
func hardenedLoad(t *testing.T, fsys *cctest.FS, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	l, err := cc.NewWithPreset("myapp", cc.MiseHardenedLayout,
		append([]cc.Option{cc.WithFileSystem(fsys), cctest.WithHost(hardenedHost())}, opts...)...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	return l.Load(t.Context())
}

// DL-20, DL-191, design.md misehardened.Preset, "File permissions": a searched file writable by group or other, or in
// a directory that is, or owned by someone other than the current user or root, is ignored with a warning.
func TestStrictModesIgnores(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*cctest.FS)
		ignore bool
		reason string
	}{
		{"defaults", func(*cctest.FS) {}, false, ""},
		{"group-writable file", func(f *cctest.FS) { f.Chmod(rootCfg, 0o664) }, true, "group"},
		{"other-writable file", func(f *cctest.FS) { f.Chmod(rootCfg, 0o646) }, true, "other"},
		{"group-writable directory", func(f *cctest.FS) { f.Chmod(work, 0o775) }, true, "group"},
		{"read-only for group and other", func(f *cctest.FS) { f.Chmod(rootCfg, 0o444) }, false, ""},
		{"owned by me", func(f *cctest.FS) { f.Chown(rootCfg, myUID) }, false, ""},
		{"owned by root", func(f *cctest.FS) { f.Chown(rootCfg, 0) }, false, ""},
		{"owned by someone else", func(f *cctest.FS) { f.Chown(rootCfg, 1001) }, true, "owned"},
		{"directory owned by someone else", func(f *cctest.FS) { f.Chown(work, 1001) }, true, "owned"},
		{"owner unknown", func(f *cctest.FS) { f.Chmod(rootCfg, 0o644) }, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := cctest.NewFS(map[string]string{rootCfg: `{"a": 1}`})
			tc.change(fsys)
			res, err := hardenedLoad(t, fsys)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			l := layerAt(t, res.Describe(), rootCfg)
			if !tc.ignore {
				if l.Status != cc.LayerLoaded {
					t.Errorf("layer = %+v, want it loaded", l)
				}
				return
			}
			if l.Status != cc.LayerIgnored || !hasWarningCode(l.Warnings, cc.WarnFilePermissions) ||
				!strings.Contains(l.Reason, tc.reason) {
				t.Errorf("layer = %+v, want LayerIgnored with a file-permissions warning whose reason says %q", l, tc.reason)
			}
			wantTreeJSON(t, res.Tree(), `{}`)
		})
	}
}

// DL-191: a directory with the sticky bit, such as /tmp, may be writable by everyone; without it, it may not.
func TestStrictModesStickyDirectory(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/tmp/x/myapp.json": `{"a": 1}`, "/tmp/y/myapp.json": `{"b": 2}`})
	fsys.Chmod("/tmp/x", fs.ModeSticky|0o777)
	fsys.Chmod("/tmp/y", 0o777)
	res, err := hardenedLoad(t, fsys,
		cc.WithScope("x", cc.FixedDir("/tmp/x", cc.Pattern{Template: "myapp.json"})),
		cc.WithScope("y", cc.FixedDir("/tmp/y", cc.Pattern{Template: "myapp.json"})),
		cc.WithScopes(cc.ScopeProject, "x", "y"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
}

// DL-191: StrictModes allows everything on Windows, where ACLs aren't checked yet; the same file is ignored on Linux.
func TestStrictModesWindows(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/myapp.json": `{"a": 1}`})
	fsys.Chmod("/srv/myapp.json", 0o666)
	fsys.Chown("/srv/myapp.json", 1001)
	for goos, want := range map[string]string{"windows": `{"a": 1}`, "linux": `{}`} {
		h := hardenedHost()
		h.GOOS = goos
		res, err := newLoaderHost(t, fsys, h,
			cc.WithScope("srv", cc.FixedDir("/srv", cc.Pattern{Template: "myapp.json"})), cc.WithScopes("srv"),
			cc.WithDefaultPolicy(cc.FilePermissions(cc.StrictModes)),
		).Load(t.Context())
		if err != nil {
			t.Fatalf("%s: Load: %v", goos, err)
		}
		wantTreeJSON(t, res.Tree(), want)
	}
}

// DL-191: StrictModes on its own, for apps that call it from their own check.
func TestStrictModesFunction(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/a/f.json": `{}`, "/b/f.json": `{}`})
	fsys.Chmod("/b/f.json", 0o620)
	info := func(path string) fs.FileInfo {
		r, err := fsys.OpenRoot("/")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		fi, err := r.Stat(strings.TrimPrefix(path, "/"))
		if err != nil {
			t.Fatal(err)
		}
		return fi
	}
	if p, _ := cc.StrictModes(cc.FileCheck{Path: "/a/f.json", Info: info("/a/f.json"), UID: myUID, GOOS: "linux"}); p != cc.PermissionAllow {
		t.Errorf("StrictModes(0644) = %v, want PermissionAllow", p)
	}
	p, reason := cc.StrictModes(cc.FileCheck{Path: "/b/f.json", Info: info("/b/f.json"), UID: myUID, GOOS: "linux"})
	if p != cc.PermissionIgnore || !strings.Contains(reason, "group") {
		t.Errorf("StrictModes(0620) = %v, %q; want PermissionIgnore saying group", p, reason)
	}
	if p, _ := cc.StrictModes(cc.FileCheck{Path: "/b/f.json", Info: info("/b/f.json"), UID: myUID, GOOS: "windows"}); p != cc.PermissionAllow {
		t.Errorf("StrictModes on Windows = %v, want PermissionAllow", p)
	}
}

// DL-191, design.md Filesystem hook, "Permission policy": the check gets every file read and its containing directory;
// for a symlink, the target and the target's directory.
func TestFilePermissionsChecks(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{
		rootCfg:             `{"include": "base.json"}`,
		work + "/base.json": `{}`,
		"/srv/real.json":    `{}`,
	})
	fsys.Symlink("/srv/real.json", "/etc/myapp/config.json")
	var seen calls[cc.FileCheck]
	_, err := incLoad(t, fsys, cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.FilePermissions(func(c cc.FileCheck) (cc.Permission, string) {
		seen.add(c)
		return cc.PermissionAllow, ""
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	type check struct {
		path  string
		dir   bool
		scope cc.ScopeName
	}
	var got []check
	for _, c := range seen.all() {
		if c.UID != 0 || c.GOOS != "linux" || c.Info == nil {
			t.Errorf("check = %+v, want the host's UID (0), GOOS linux and file info", c)
		}
		if c.Info != nil && c.Info.IsDir() != c.Dir {
			t.Errorf("check for %s: Dir = %v, but the info says IsDir %v", c.Path, c.Dir, c.Info.IsDir())
		}
		got = append(got, check{c.Path, c.Dir, c.Scope})
	}
	for _, want := range []check{
		{"/srv/real.json", false, "sys"},
		{"/srv", true, "sys"},
		{rootCfg, false, "proj"},
		{work, true, "proj"},
		{work + "/base.json", false, "proj"},
	} {
		if !slices.Contains(got, want) {
			t.Errorf("no check for %+v in %+v", want, got)
		}
	}
	for _, c := range got {
		if c.path == "/etc/myapp/config.json" || c.path == "/home/me" || c.path == "/etc" {
			t.Errorf("unexpected check for %+v: a symlink's own path, or a higher ancestor", c)
		}
	}
}

// DL-191: PermissionError fails the load with a *PolicyError naming the file.
func TestFilePermissionsError(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	_, err := incLoad(t, cctest.NewFS(map[string]string{rootCfg: `{}`}),
		cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.FilePermissions(func(c cc.FileCheck) (cc.Permission, string) {
			if c.Path == rootCfg {
				return cc.PermissionError, "not allowed here"
			}
			return cc.PermissionAllow, ""
		})))
	wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, rootCfg, "not allowed here")
}

// DL-181: an included or explicit file that a permission check would ignore is a *PolicyError, because a file named
// directly is never silently skipped.
func TestFilePermissionsNamedFiles(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	ignoreBad := cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.FilePermissions(func(c cc.FileCheck) (cc.Permission, string) {
		if strings.HasSuffix(c.Path, "bad.json") {
			return cc.PermissionIgnore, "writable by group"
		}
		return cc.PermissionAllow, ""
	}))
	t.Run("included", func(t *testing.T) {
		_, err := incLoad(t, cctest.NewFS(map[string]string{rootCfg: `{"include": "bad.json"}`, work + "/bad.json": `{}`}), ignoreBad)
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, work+"/bad.json", "writable by group")
	})
	t.Run("explicit", func(t *testing.T) {
		_, err := incLoad(t, cctest.NewFS(map[string]string{"/srv/bad.json": `{}`}), ignoreBad,
			cc.WithConfigFiles("/srv/bad.json"))
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, "/srv/bad.json", "writable by group")
	})
	t.Run("searched", func(t *testing.T) {
		res, err := incLoad(t, cctest.NewFS(map[string]string{"/srv/bad.json": `{}`}), ignoreBad,
			cc.WithScope("srv", cc.FixedDir("/srv", cc.Pattern{Template: "bad.json"})), cc.WithScopes("srv"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if l := layerAt(t, res.Describe(), "/srv/bad.json"); l.Status != cc.LayerIgnored {
			t.Errorf("layer = %+v, want LayerIgnored", l)
		}
	})
}

// DL-190, DL-191: bootstrap files get their scope's permission check, and are skipped with a warning when they fail,
// so a group-writable project bootstrap file can't choose the env.
func TestFilePermissionsBootstrap(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	rc := work + "/.myapprc.json"
	fsys := cctest.NewFS(map[string]string{
		rc:                       `{"env": ["dev"]}`,
		work + "/myapp.dev.json": `{"dev": true}`,
	})
	fsys.Chmod(rc, 0o664)
	res, err := hardenedLoad(t, fsys)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{}`)
	rep := res.Describe()
	var boot *cc.Layer
	for i := range rep.Bootstrap {
		if rep.Bootstrap[i].Path == rc {
			boot = &rep.Bootstrap[i]
		}
	}
	if boot == nil || boot.Status != cc.LayerIgnored || !hasWarningCode(boot.Warnings, cc.WarnFilePermissions) {
		t.Errorf("bootstrap layer = %+v, want LayerIgnored with a file-permissions warning", boot)
	}
	if len(rep.Envs.Selected) != 0 {
		t.Errorf("envs = %v, want none", rep.Envs.Selected)
	}
}

// DL-191, F4: cctest.FS reports an owner through OwnerInfo only after Chown.
func TestCctestOwners(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/a.json": `{}`, "/b.json": `{}`})
	fsys.Chown("/b.json", 42)
	r, err := fsys.OpenRoot("/")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, tc := range []struct {
		name string
		uid  int
		ok   bool
	}{{"a.json", 0, false}, {"b.json", 42, true}} {
		fi, err := r.Stat(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		oi, isOwner := fi.(cc.OwnerInfo)
		if !isOwner {
			t.Fatalf("%s: file info %T doesn't implement OwnerInfo", tc.name, fi)
		}
		if uid, ok := oi.Owner(); uid != tc.uid || ok != tc.ok {
			t.Errorf("%s: Owner() = %d, %v; want %d, %v", tc.name, uid, ok, tc.uid, tc.ok)
		}
	}
}

func hasWarningCode(ws []cc.Warning, code cc.WarningCode) bool {
	return slices.ContainsFunc(ws, func(w cc.Warning) bool { return w.Code == code })
}
