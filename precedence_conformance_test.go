package cascadingconfig_test

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// The precedence conformance tests (DL-245) check each rule of docs/precedence.md by its ID, with expected orders
// written by hand from the rules.

// precMise loads files with MiseLayout on Linux x64, home /home/me, start dir /home/me/work/app, with JSON's decoder
// standing in for TOML and YAML, the host's env vars, and opts.
func precMise(t *testing.T, files map[string]string, env map[string]string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := precMiseLoad(t, cctest.NewFS(files), env, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

func precMiseLoad(t *testing.T, fsys *cctest.FS, env map[string]string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	h := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work/app", Env: env}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout,
		append([]cc.Option{cc.WithFileSystem(fsys), cctest.WithHost(h), fzDecoders(), cc.WithLogger(slog.New(&records{}))}, opts...)...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	return l.Load(t.Context())
}

// filesOf gives every path the content {"top": path}.
func filesOf(paths ...string) map[string]string {
	m := map[string]string{}
	for _, p := range paths {
		m[p] = `{"top": "` + p + `"}`
	}
	return m
}

// wantOrder checks the report's layers, lowest first, as "path status" or "path status scope" lines.
func wantOrder(t *testing.T, res *cc.Result, withScope bool, want ...string) {
	t.Helper()
	var got []string
	for _, l := range res.Describe().Layers {
		line := l.Path + " " + l.Status.String()
		if withScope {
			line += " " + string(l.Scope)
		}
		got = append(got, line)
	}
	if !slices.Equal(got, want) {
		t.Errorf("layers:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// loaded is "path loaded" for each path.
func loaded(paths ...string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p + " loaded"
	}
	return out
}

// DL-244: the tables in docs/precedence.md are exactly the oracle's.
func TestPrecedenceDocTables(t *testing.T) {
	data, err := os.ReadFile("docs/precedence.md")
	if err != nil {
		t.Fatal(err)
	}
	heading := regexp.MustCompile("^#### `(\\w+)` scope `(\\w+)`")
	row := regexp.MustCompile("^\\| *\\d+ *\\| *`([^`]+)` *\\| *([a-z-]+) *\\| *([a-z]+) *\\|$")
	doc := map[string][]oPattern{}
	key := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			key = m[1] + " scope " + m[2]
			continue
		}
		if strings.HasPrefix(line, "#") {
			key = ""
		}
		if m := row.FindStringSubmatch(line); m != nil && key != "" {
			v, ok := oVariantNames[m[2]]
			if !ok || (m[3] != "file" && m[3] != "search") {
				t.Fatalf("%s: a row with variant %q and root %q", key, m[2], m[3])
			}
			doc[key] = append(doc[key], oPattern{tpl: m[1], variant: v, fileRoot: m[3] == "file"})
		}
	}
	for k, want := range oLayoutTables {
		if !slices.Equal(doc[k], want) {
			t.Errorf("%s: the doc's table has %d rows that differ from the oracle's %d", k, len(doc[k]), len(want))
		}
	}
	for k := range doc {
		if _, ok := oLayoutTables[k]; !ok {
			t.Errorf("the doc has a table %q the oracle doesn't", k)
		}
	}
}

// PR-1: scopes rank as defined, with After and Before; the defaults are lowest; a scope disabled by its binding takes
// no part; a scope replaced by its binding's file holds exactly that file, at its place.
func TestPR1Scopes(t *testing.T) {
	// the user directory is outside ~, so the walk doesn't reach it too (PR-11)
	xdg := map[string]string{"XDG_CONFIG_HOME": "/xdg"}
	files := filesOf("/etc/myapp/config.json", "/xdg/myapp/config.json", "/home/me/work/app/myapp.json",
		"/xdg/myapp/config.override.json", "/srv/global.json")
	res := precMise(t, files, xdg,
		cc.WithDefaults(map[string]any{"d": "x"}),
		cc.WithScope("a", cc.Values(cc.Tree{"a": "1"}), cc.After(cc.ScopeUser)),
		cc.WithScope("b", cc.Values(cc.Tree{"b": "1"}), cc.Before(cc.ScopeSystem)),
		cc.WithScopes(cc.ScopeSystem, cc.ScopeUser, cc.ScopeProject, cc.ScopeOverride, "a", "b"))
	var scopes []string
	for _, l := range res.Describe().Layers {
		scopes = append(scopes, string(l.Scope))
	}
	if want := []string{"defaults", "b", "system", "user", "a", "project", "override"}; !slices.Equal(scopes, want) {
		t.Errorf("scopes in order %q, want %q", scopes, want)
	}
	xdg["MYAPP_GLOBAL_CONFIG_FILE"] = "/srv/global.json"
	res = precMise(t, files, xdg)
	wantOrder(t, res, true,
		"/etc/myapp/config.json loaded system", "/srv/global.json loaded user", "/home/me/work/app/myapp.json loaded project")
}

// PR-2: the walk applies root-most first and stops at ~, inclusive; ceilings are exclusive; a moved user directory
// drops the walk's candidates under the default one.
func TestPR2Walk(t *testing.T) {
	files := filesOf("/home/myapp.json", "/home/me/myapp.json", "/home/me/work/myapp.json", "/home/me/work/app/myapp.json")
	wantOrder(t, precMise(t, files, nil), false, loaded("/home/me/myapp.json", "/home/me/work/myapp.json", "/home/me/work/app/myapp.json")...)
	wantOrder(t, precMise(t, files, nil, cc.WithCeilingPaths("/home/me/work")), false, loaded("/home/me/work/app/myapp.json")...)
	wantOrder(t, precMise(t, files, nil, cc.WithWalkStop(cc.StopAtRoot)), false,
		loaded("/home/myapp.json", "/home/me/myapp.json", "/home/me/work/myapp.json", "/home/me/work/app/myapp.json")...)

	moved := filesOf("/home/me/.config/myapp/config.json", "/srv/conf/config.json", "/home/me/myapp.json")
	wantOrder(t, precMise(t, moved, map[string]string{"MYAPP_CONFIG_DIR": "/srv/conf"}), false,
		loaded("/srv/conf/config.json", "/home/me/myapp.json")...)
}

// PR-3, PR-4: precedence.md example 1, on Linux x64 with MYAPP_ENV=dev.
func TestPrecedenceExample1(t *testing.T) {
	d := "/home/me/work/app/"
	names := []string{"myapp.json", "myapp.unix.json", "myapp.linux.json", "myapp.linux-x64.json", "myapp.dev.json",
		"myapp.local.json", "myapp.unix.local.json", "myapp.linux.local.json", "myapp.linux-x64.local.json", "myapp.dev.local.json"}
	var paths []string
	for _, n := range names {
		paths = append(paths, d+n)
	}
	shuffled := slices.Clone(paths)
	slices.Reverse(shuffled)
	res := precMise(t, filesOf(shuffled...), map[string]string{"MYAPP_ENV": "dev"})
	wantOrder(t, res, false, loaded(paths...)...)
}

// PR-4: a name selected twice keeps its later place; a platform environment that's selected leaves the platform
// list.
func TestPR4Environments(t *testing.T) {
	d := "/home/me/work/app/"
	files := filesOf(d+"myapp.unix.json", d+"myapp.linux.json", d+"myapp.dev.json", d+"myapp.prod.json")
	wantOrder(t, precMise(t, files, nil, cc.WithEnvs("dev", "prod", "dev")), false,
		loaded(d+"myapp.unix.json", d+"myapp.linux.json", d+"myapp.prod.json", d+"myapp.dev.json")...)
	wantOrder(t, precMise(t, files, nil, cc.WithEnvs("linux", "dev")), false,
		loaded(d+"myapp.unix.json", d+"myapp.linux.json", d+"myapp.dev.json")...)
	wantOrder(t, precMise(t, files, nil, cc.WithEnvs("dev", "linux")), false,
		loaded(d+"myapp.unix.json", d+"myapp.dev.json", d+"myapp.linux.json")...)
}

// PR-5: within a group, patterns rank as listed, even when the groups are interleaved.
func TestPR5PatternOrder(t *testing.T) {
	fsys := cctest.NewFS(filesOf("/c/l1.json", "/c/s1.json", "/c/l2.json", "/c/s2.json"))
	l := newLoaderFS(t, fsys, nil, cc.WithScope("s", cc.FixedDir("/c",
		cc.Pattern{Template: "l2.{ext}", Variant: cc.VariantLocal},
		cc.Pattern{Template: "s2.{ext}"},
		cc.Pattern{Template: "l1.{ext}", Variant: cc.VariantLocal},
		cc.Pattern{Template: "s1.{ext}"},
	)), cc.WithScopes("s"))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantOrder(t, res, false, loaded("/c/s2.json", "/c/s1.json", "/c/l2.json", "/c/l1.json")...)
}

// PR-6, PR-7: precedence.md example 3.
func TestPrecedenceExample3(t *testing.T) {
	c := "/srv/app/.myapp/conf.d/"
	files := filesOf(c+"b.yaml", c+"a.json", c+"a.toml", c+"a.yml", c+"a.yaml", c+"x/myapp.toml", c+"w/myapp.json")
	fsys := cctest.NewFS(files)
	h := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/srv/app"}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(fsys), cctest.WithHost(h), fzDecoders(), cc.WithCeilingPaths("/srv"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantOrder(t, res, false, loaded(c+"a.json", c+"a.yml", c+"a.yaml", c+"a.toml", c+"b.yaml", c+"w/myapp.json", c+"x/myapp.toml")...)
}

// PR-6: directory globs skip hidden directories; file globs don't take dotted stems; matching is case-sensitive.
func TestPR6Globs(t *testing.T) {
	c := "/home/me/work/app/.myapp/conf.d/"
	files := filesOf(c+".h/myapp.json", c+"a.b.json", c+"A.JSON", c+"a.json", "/home/me/work/app/MyApp.json")
	wantOrder(t, precMise(t, files, nil), false, loaded(c+"a.json")...)
}

// PR-7: {NAME}_CONFIG_FORMATS sets every scope's order; ConflictFirst loads only a slot's top file; ConflictFail
// fails.
func TestPR7Formats(t *testing.T) {
	d := "/home/me/work/app/"
	files := filesOf(d+"myapp.json", d+"myapp.toml", d+"myapp.yaml")
	wantOrder(t, precMise(t, files, map[string]string{"MYAPP_CONFIG_FORMATS": "json,toml"}), false,
		loaded(d+"myapp.toml", d+"myapp.json")...)
	wantOrder(t, precMise(t, files, nil, cc.WithFormatConflict(cc.ConflictFirst)), false,
		d+"myapp.json ignored", d+"myapp.yaml ignored", d+"myapp.toml loaded")
	if _, err := precMiseLoad(t, cctest.NewFS(files), nil, cc.WithFormatConflict(cc.ConflictFail)); cc.CodeOf(err) != cc.CodeConflict {
		t.Errorf("ConflictFail: %v, want a 409", err)
	}
}

// PR-7: a bootstrap file's formats apply to scopes at most as trusted as it is.
func TestPR7FormatsByTrust(t *testing.T) {
	files := filesOf("/xdg/myapp/config.json", "/xdg/myapp/config.toml",
		"/home/me/work/app/myapp.json", "/home/me/work/app/myapp.toml")
	files["/home/me/work/app/.myapprc.json"] = `{"formats": ["json"]}`
	wantOrder(t, precMise(t, files, map[string]string{"XDG_CONFIG_HOME": "/xdg"}), false, loaded("/xdg/myapp/config.json",
		"/xdg/myapp/config.toml", "/home/me/work/app/myapp.json")...)
}

// PR-8: candidates that can't be used keep their places, with their statuses.
func TestPR8Statuses(t *testing.T) {
	d := "/home/me/work/app/"
	fsys := cctest.NewFS(filesOf(d+".myapp/config.json", d+".config/myapp.json", d+"myapp.local.json"))
	fsys.WriteFile(d+"myapp.json", nil, 0o644)
	fsys.Symlink(d+"missing.json", d+".myapp.json")
	fsys.Mkdir(d+"myapp/config.json", 0o755)
	res, err := precMiseLoad(t, fsys, nil, cc.WithIgnoredPaths(d+".config/myapp.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantOrder(t, res, false,
		d+".config/myapp.json ignored", d+".myapp/config.json loaded", d+"myapp/config.json ignored",
		d+"myapp.json empty", d+".myapp.json ignored", d+"myapp.local.json loaded")
}

// PR-9: explicit files take the project scope's place, in the order given, with no siblings; with no marked scope,
// they rank above every scope.
func TestPR9ExplicitFiles(t *testing.T) {
	files := filesOf("/etc/myapp/config.json", "/home/me/work/app/myapp.json", "/home/me/.config/myapp/config.override.json",
		"/x/b.json", "/x/a.json", "/x/a.local.json")
	wantOrder(t, precMise(t, files, nil, cc.WithConfigFiles("/x/b.json", "/x/a.json")), true,
		"/etc/myapp/config.json loaded system", "/x/b.json loaded project", "/x/a.json loaded project",
		"/home/me/.config/myapp/config.override.json loaded override")

	fsys := cctest.NewFS(filesOf("/c/s.json", "/x/a.json"))
	l := newLoaderFS(t, fsys, nil, cc.WithScope("s", cc.FixedDir("/c", cc.Pattern{Template: "s.{ext}"})), cc.WithScopes("s"),
		cc.WithConfigFiles("/x/a.json"))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantOrder(t, res, true, "/c/s.json loaded s", "/x/a.json loaded ")
}

// PR-9, DL-247: under WithConfigFilesOnly, explicit files replace the scopes that search for files, and value scopes
// still take their places, so flags beat the file and defaults sit below it.
func TestPR9ConfigFilesOnlyKeepsValues(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{"/x/a.json": `{"a": "file", "f": "file", "d": "file"}`, "/home/me/work/app/.myapprc": `{"p": 1}`})
	l, err := cc.NewWithPreset("myapp", cc.CosmiconfigLayout, cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)),
		cc.WithDefaults(map[string]any{"d": "default", "only": "default"}), cc.WithScope("flags", cc.Values(cc.Tree{"f": "flag"})),
		cc.WithScopes(cc.ScopeProject, "meta", "flags"), cc.WithConfigFiles("/x/a.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": "file", "f": "flag", "d": "file", "only": "default"}`)
}

// PR-10: an included file ranks directly below its includer, and later entries rank above earlier ones.
func TestPR10Includes(t *testing.T) {
	d := "/home/me/work/app/"
	files := filesOf(d+"a.json", d+"b.json", d+"myapp.local.json")
	files[d+"myapp.json"] = `{"include": ["a.json", "b.json"]}`
	wantOrder(t, precMise(t, files, nil), false, loaded(d+"a.json", d+"b.json", d+"myapp.json", d+"myapp.local.json")...)
}

// PR-11: precedence.md example 2: a file reached twice ranks at its higher place, with its most trusted route's scope.
func TestPrecedenceExample2(t *testing.T) {
	files := filesOf("/etc/myapp/config.toml", "/home/me/.config/myapp/config.toml", "/home/me/myapp.toml",
		"/home/me/work/myapp.toml", "/home/me/work/app/myapp.toml", "/home/me/work/app/.myapp.toml",
		"/home/me/.config/myapp/config.override.toml")
	fsys := cctest.NewFS(files)
	h := cctest.Host{GOOS: "darwin", GOARCH: "arm64", Home: "/home/me", Cwd: "/home/me/work/app"}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(fsys), cctest.WithHost(h), fzDecoders())
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantOrder(t, res, true,
		"/etc/myapp/config.toml loaded system",
		"/home/me/.config/myapp/config.toml duplicate user",
		"/home/me/.config/myapp/config.toml loaded user",
		"/home/me/myapp.toml loaded project",
		"/home/me/work/myapp.toml loaded project",
		"/home/me/work/app/myapp.toml loaded project",
		"/home/me/work/app/.myapp.toml loaded project",
		"/home/me/.config/myapp/config.override.toml loaded override")
}

// PR-12: value layers sit at their scopes' places, and a custom scope's Tree sources in the order returned.
func TestPR12ValueLayers(t *testing.T) {
	s := sources(cc.Source{Tree: cc.Tree{"v": "t1"}}, cc.Source{Tree: cc.Tree{"v": "t2"}})
	res := precMise(t, filesOf("/home/me/work/app/myapp.json"), nil,
		cc.WithScope("cust", cc.Custom(s), cc.Before(cc.ScopeProject)),
		cc.WithScope("flags", cc.Values(cc.Tree{"v": "flag"})),
		cc.WithScopes(cc.ScopeProject, "cust", "flags"))
	var got []string
	for _, l := range res.Describe().Layers {
		got = append(got, string(l.Scope)+":"+l.Path)
	}
	if want := []string{"cust:", "cust:", "project:/home/me/work/app/myapp.json", "flags:"}; !slices.Equal(got, want) {
		t.Errorf("layers %q, want %q", got, want)
	}
	wantValue(t, res.Tree(), "v", "flag")
}

// PR-13: first-match chooses the highest non-empty file; files below it are ignored undecoded; a broken file above it
// fails the load; value layers still merge.
func TestPR13FirstMatch(t *testing.T) {
	d := work + "/"
	fsys := cctest.NewFS(map[string]string{d + ".myapprc.json": `{"a": 1}`, d + ".config/myapprc.json": `{"broken": `})
	fsys.WriteFile(d+".myapprc", nil, 0o644)
	l, err := cc.NewWithPreset("myapp", cc.CosmiconfigLayout, cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)),
		cc.WithScope("flags", cc.Values(cc.Tree{"f": "flag"})), cc.WithScopes(cc.ScopeProject, "meta", "flags"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1, "f": "flag"}`)
	wantOrder(t, res, false, d+".config/myapprc.json ignored", d+".myapprc.json loaded", d+".myapprc empty", " loaded")

	fsys.WriteFile(d+".myapprc", []byte("a: ["), 0o644)
	if _, err := l.Load(context.Background()); err == nil {
		t.Errorf("a broken file above the chosen one didn't fail the load")
	}
}

// PR-14: a higher bootstrap file's env replaces a lower one's.
func TestPR14Bootstrap(t *testing.T) {
	d := "/home/me/work/app/"
	files := filesOf(d+"myapp.dev.json", d+"myapp.prod.json")
	files["/home/me/.config/myapp/myapprc.json"] = `{"env": ["dev"]}`
	files[d+".myapprc.json"] = `{"env": ["prod"]}`
	wantOrder(t, precMise(t, files, nil), false, loaded(d+"myapp.prod.json")...)
}
