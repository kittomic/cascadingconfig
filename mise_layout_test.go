package cascadingconfig

import (
	"errors"
	"path"
	"reflect"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

const app = "/home/me/work/app"

func under(dir string, names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = path.Join(dir, n)
	}
	return out
}

// DL-124: MiseLayout defines system, user, project and override, lowest first, and selects them all.
func TestMiseLayoutScopes(t *testing.T) {
	t.Parallel()
	c := cfgOf(t, "myapp", MiseLayout)
	want := []ScopeName{ScopeSystem, ScopeUser, ScopeProject, ScopeOverride}
	if got := c.scopeOrder(); !reflect.DeepEqual(got, want) {
		t.Errorf("scopes = %q, want %q", got, want)
	}
	for name, trust := range map[ScopeName]TrustLevel{
		ScopeSystem: TrustSystem, ScopeUser: TrustUser, ScopeProject: TrustProject, ScopeOverride: TrustUser,
	} {
		if got, _ := c.scopeTrust(name); got != trust {
			t.Errorf("trust of %s = %v, want %v", name, got, trust)
		}
	}
}

// DL-129, design.md Environments: the worked example, on Linux x64 with MYAPP_ENV=dev.
func TestGoldenWorkedExample(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(under(app,
		"myapp.toml", "myapp.unix.toml", "myapp.linux.toml", "myapp.linux-x64.toml", "myapp.dev.toml",
		"myapp.local.toml", "myapp.unix.local.toml", "myapp.linux.local.toml", "myapp.linux-x64.local.toml",
		"myapp.dev.local.toml",
	)...))
	res := miseLoad(t, fsys, inDir(app, setEnv("MYAPP_ENV", "dev")))
	golden.Check(t, "discovery/worked-example.golden", []byte(detail(res)))
	wantTree(t, res, `{"last": "`+app+`/myapp.dev.local.toml"}`)
}

// DL-124, DL-129, config-locations.md 3.2: one project directory with a file for every pattern in layers A–D, for env
// dev. The files are created in alphabetical order, so the golden order comes only from the layout.
func TestGoldenFullProjectDirectory(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(under(app,
		".config/myapp/conf.d/a.dev.local.toml", ".config/myapp/conf.d/a.dev.toml", ".config/myapp/conf.d/a.toml",
		".config/myapp/conf.d/f/myapp.dev.local.toml", ".config/myapp/conf.d/f/myapp.dev.toml",
		".config/myapp/conf.d/f/myapp.local.toml", ".config/myapp/conf.d/f/myapp.toml",
		".config/myapp/config.dev.local.toml", ".config/myapp/config.dev.toml", ".config/myapp/config.local.toml",
		".config/myapp/config.toml", ".config/myapp/myapp.local.toml", ".config/myapp/myapp.toml",
		".config/myapp.dev.local.toml", ".config/myapp.dev.toml", ".config/myapp.local.toml", ".config/myapp.toml",
		".myapp/conf.d/a.dev.local.toml", ".myapp/conf.d/a.dev.toml", ".myapp/conf.d/a.toml",
		".myapp/conf.d/f/myapp.dev.local.toml", ".myapp/conf.d/f/myapp.dev.toml", ".myapp/conf.d/f/myapp.local.toml",
		".myapp/conf.d/f/myapp.toml", ".myapp/config.dev.local.toml", ".myapp/config.dev.toml", ".myapp/config.local.toml",
		".myapp/config.toml", ".myapp.dev.local.toml", ".myapp.dev.toml", ".myapp.local.toml", ".myapp.toml",
		"myapp/conf.d/a.dev.local.toml", "myapp/conf.d/a.dev.toml", "myapp/conf.d/a.toml",
		"myapp/conf.d/f/myapp.dev.local.toml", "myapp/conf.d/f/myapp.dev.toml", "myapp/conf.d/f/myapp.local.toml",
		"myapp/conf.d/f/myapp.toml", "myapp/config.dev.local.toml", "myapp/config.dev.toml", "myapp/config.local.toml",
		"myapp/config.toml", "myapp.dev.local.toml", "myapp.dev.toml", "myapp.local.toml", "myapp.toml",
	)...))
	res := miseLoad(t, fsys, inDir(app, nil), WithEnvs("dev"))
	golden.Check(t, "discovery/full-project-directory.golden", []byte(detail(res)))
}

// DL-124, DL-129, config-locations.md 3.4–3.6: the system, user and override scopes, from outside ~ so the walk
// finds none of them.
func TestGoldenUserAndOverrideScopes(t *testing.T) {
	t.Parallel()
	user := "/home/me/.config/myapp"
	files := under(user,
		"conf.d/a.dev.local.toml", "conf.d/a.dev.toml", "conf.d/a.toml", "conf.d/f/myapp.dev.local.toml",
		"conf.d/f/myapp.dev.toml", "conf.d/f/myapp.local.toml", "conf.d/f/myapp.toml", "config.dev.local.toml",
		"config.dev.override.toml", "config.dev.toml", "config.local.toml", "config.override.toml", "config.toml",
		"myapp.dev.local.toml", "myapp.dev.toml", "myapp.local.toml", "myapp.toml",
	)
	files = append(files, "/etc/myapp/config.dev.toml", "/etc/myapp/config.toml")
	res := miseLoad(t, memfs.New(fileSet(files...)), inDir("/srv/app", nil), WithEnvs("dev"))
	golden.Check(t, "discovery/user-and-override.golden", []byte(detail(res)))
}

// DL-61, DL-110: when the start dir is under ~, the user's config.toml is also a project file of ~. It loads once, at
// the project position, with the user scope and trust.
func TestUserFileFoundByTheWalk(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/home/me/.config/myapp/config.toml", app+"/myapp.toml"))
	res := miseLoad(t, fsys, inDir(app, nil))
	if len(res.found) != 2 {
		t.Fatalf("found:\n%s\nwant the user file once, then the project file", detail(res))
	}
	l := res.found[0]
	if l.Path != "/home/me/.config/myapp/config.toml" || l.Scope != ScopeUser || l.Trust != TrustUser {
		t.Errorf("first layer = %s, scope %s, trust %v; want the user file in the user scope", l.Path, l.Scope, l.Trust)
	}
}

// DL-128: when MYAPP_CONFIG_DIR moves the user directory, the walk drops files under the default one.
func TestMovedUserDirectory(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/home/me/.config/myapp/config.toml", "/custom/config.toml"))
	res := miseLoad(t, fsys, inDir(app, setEnv("MYAPP_CONFIG_DIR", "/custom")))
	wantPaths(t, res, "/custom/config.toml")
}

// DL-125: MYAPP_GLOBAL_CONFIG_FILE makes one file the whole user scope and turns override off; MYAPP_SYSTEM_CONFIG_FILE
// makes one file the whole system scope. A missing one is a 404.
func TestSingleFileBindings(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(
		"/etc/myapp/config.toml", "/home/me/.config/myapp/config.toml", "/home/me/.config/myapp/config.override.toml",
		"/opt/global.toml", "/opt/system.toml",
	))
	res := miseLoad(t, fsys, inDir("/srv/app", setEnv("MYAPP_GLOBAL_CONFIG_FILE", "/opt/global.toml", "MYAPP_SYSTEM_CONFIG_FILE", "/opt/system.toml")))
	golden.Check(t, "discovery/single-file-bindings.golden", []byte(detail(res)))

	l, err := NewWithPreset("myapp", MiseLayout, WithFileSystem(memFS{fsys}), WithDecoder(tomlAsJSON),
		hostWith(map[string]string{"MYAPP_GLOBAL_CONFIG_FILE": "/opt/missing.toml"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Load(t.Context())
	wantCode(t, err, CodeNotFound)
}

// DL-125, config-locations.md 3.2: MYAPP_OVERRIDE_CONFIG_FILENAMES replaces the walk's shared names and removes its
// local files; env and env-local files still load.
func TestOverrideConfigFilenames(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(under(app,
		"myapp.toml", "custom.toml", "custom.json", "other.toml", "myapp.dev.toml", "myapp.local.toml", "myapp.dev.local.toml",
	)...))
	res := miseLoad(t, fsys, inDir(app, setEnv("MYAPP_OVERRIDE_CONFIG_FILENAMES", "custom.{ext}, other.toml")), WithEnvs("dev"))
	wantPaths(t, res, under(app, "custom.json", "custom.toml", "other.toml", "myapp.dev.toml", "myapp.dev.local.toml")...)
}

// DL-127: ignored paths, from WithIgnoredPaths and MYAPP_IGNORED_CONFIG_PATHS together, match a file or every file
// below a directory, after resolving symlinks. Matches are LayerIgnored, without a warning.
func TestIgnoredPaths(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(under(app, "myapp.toml", ".myapp.toml", "myapp/config.toml", ".config/myapp.toml")...))
	fsys.Symlink(app+"/.config", "/home/me/linked")
	res := miseLoad(t, fsys, inDir(app, setEnv("MYAPP_IGNORED_CONFIG_PATHS", app+"/myapp")),
		WithIgnoredPaths(app+"/.myapp.toml", "/home/me/linked"))
	wantFound(t, res,
		"project "+app+"/.config/myapp.toml ignored",
		"project "+app+"/myapp/config.toml ignored",
		"project "+app+"/myapp.toml loaded",
		"project "+app+"/.myapp.toml ignored",
	)
	for _, l := range res.found {
		if l.Status == LayerIgnored && (l.Reason == "" || len(l.Warnings) != 0) {
			t.Errorf("%s: reason %q, warnings %v; want a reason and no warning", l.Path, l.Reason, l.Warnings)
		}
	}
}

// DL-126, design.md Explicit config files: WithConfigFiles replaces only the project scope, at its position, and the
// files load in order with any registered decoder, even one the format order leaves out, and without variants.
func TestExplicitConfigFiles(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(
		"/etc/myapp/config.json", "/home/me/.config/myapp/config.override.json", app+"/myapp.json",
		"/x/a.toml", "/x/a.local.toml", "/x/b.json",
	))
	res := miseLoad(t, fsys, inDir(app, nil), WithFormats("json"), WithConfigFiles("/x/a.toml", "/x/b.json"))
	wantPaths(t, res, "/etc/myapp/config.json", "/x/a.toml", "/x/b.json", "/home/me/.config/myapp/config.override.json")
	if len(res.found) != 4 {
		t.FailNow()
	}
	for _, l := range res.found[1:3] {
		if l.Scope != ScopeProject || l.Trust != TrustProject {
			t.Errorf("%s: scope %s, trust %v; want project and TrustProject", l.Path, l.Scope, l.Trust)
		}
	}
}

// DL-126: MYAPP_CONFIG_FILE is a path list, beaten by WithConfigFiles; relative paths resolve against the current
// directory; the files load even when project isn't selected; and they're never ignored.
func TestConfigFileBinding(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/x/a.toml", "/x/b.toml", app+"/rel.toml", app+"/myapp.toml"))
	res := miseLoad(t, fsys, inDir(app, setEnv("MYAPP_CONFIG_FILE", "/x/a.toml:rel.toml")), WithIgnoredPaths("/x"))
	wantPaths(t, res, "/x/a.toml", app+"/rel.toml")

	res = miseLoad(t, fsys, inDir(app, setEnv("MYAPP_CONFIG_FILE", "/x/a.toml")), WithConfigFiles("/x/b.toml"))
	wantPaths(t, res, "/x/b.toml")

	res = miseLoad(t, fsys, inDir(app, nil), WithConfigFiles("/x/a.toml"), WithScopes(ScopeUser))
	wantPaths(t, res, "/x/a.toml")
}

// DL-126: an explicit file that's missing is a 404, one with no registered decoder a 415, and a directory an
// *IOError.
func TestExplicitFileErrors(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/x/a.ini"))
	fsys.Mkdir("/x/dir.toml", 0o755)
	tests := []struct {
		file string
		code Code
	}{
		{"/x/missing.toml", CodeNotFound},
		{"/x/a.ini", CodeUnsupportedMediaType},
		{"/x/dir.toml", CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			l, err := NewWithPreset("myapp", MiseLayout, WithFileSystem(memFS{fsys}), WithDecoder(tomlAsJSON),
				withHost(fakeHost{testHost()}), WithConfigFiles(tc.file))
			if err != nil {
				t.Fatal(err)
			}
			_, err = l.Load(t.Context())
			wantCode(t, err, tc.code)
			if tc.code == CodeInternal {
				if _, ok := asErr[*IOError](err); !ok {
					t.Errorf("error = %v, want an *IOError", err)
				}
			}
		})
	}
}

// DL-126: the ExplicitFile kind loads files as given, in order, with the scope's trust.
func TestExplicitFileKind(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/x/a.json", "/x/b.json"))
	res := mustLoad(t, fsys, WithScope("files", ExplicitFile("/x/b.json", "/x/a.json"), Trust(TrustSystem)), WithScopes("files"))
	wantPaths(t, res, "/x/b.json", "/x/a.json")
	if len(res.found) == 0 {
		t.FailNow()
	}
	if res.found[0].Trust != TrustSystem {
		t.Errorf("trust = %v, want TrustSystem", res.found[0].Trust)
	}
	err := loadFails(t, fsys, WithScope("files", ExplicitFile("/x/missing.json")), WithScopes("files"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want a *NotFoundError", err)
	}
}

// DL-125: DisabledBy skips a scope while its binding is set.
func TestDisabledBy(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/d/config.json"))
	res := mustLoad(t, fsys, hostWith(map[string]string{"MYAPP_OFF": "1"}, nil), WithBinding("OFF", ""),
		WithScope("s", FixedDir("/d", Pattern{Template: "config.json"}), DisabledBy("OFF")), WithScopes("s"))
	wantPaths(t, res)
}
