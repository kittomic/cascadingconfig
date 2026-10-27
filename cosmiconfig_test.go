// These tests use Unix paths in cctest's world, which aren't absolute on Windows, so the file doesn't build there
// (DL-282).

//go:build !windows

package cascadingconfig

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// cosmiLoader builds a loader from CosmiconfigLayout over files, with YAML registered as JSON, on the test host
// changed by change.
func cosmiLoader(t *testing.T, files map[string]string, change func(h *hooks.Host), opts ...Option) *Loader {
	t.Helper()
	h := testHost()
	if change != nil {
		change(&h)
	}
	l, err := NewWithPreset("myapp", CosmiconfigLayout,
		append([]Option{WithFileSystem(memFS{memfs.New(files)}), withHost(fakeHost{h}), WithDecoder(yamlAsJSON)}, opts...)...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	return l
}

func cosmiLoad(t *testing.T, files map[string]string, change func(h *hooks.Host), opts ...Option) *Result {
	t.Helper()
	res, err := cosmiLoader(t, files, change, opts...).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// loaded returns the paths of the layers that loaded.
func loaded(res *Result) []string {
	var out []string
	for _, l := range res.found {
		if l.Status == LayerLoaded {
			out = append(out, l.Path)
		}
	}
	return out
}

// DL-146, DL-147, config-locations.md 2.1: one directory with every search place. The full order shows in the layer
// list, and only .myapprc loads. JS files and package.json are never candidates.
func TestGoldenCosmiconfigSearchPlaces(t *testing.T) {
	t.Parallel()
	files := fileSet(under(srv,
		".myapprc", ".myapprc.json", ".myapprc.yaml", ".myapprc.yml", ".myapprc.js",
		".config/myapprc", ".config/myapprc.json", ".config/myapprc.yaml", ".config/myapprc.yml",
		"myapp.config.js",
	)...)
	files[srv+"/package.json"] = `{"myapp": {"from": "package.json"}}`
	res := cosmiLoad(t, files, inDir(srv, nil))
	golden.Check(t, "discovery/cosmiconfig-search-places.golden", []byte(detail(res)))
	wantTree(t, res, `{"last": "`+srv+`/.myapprc"}`)
}

// strategyFiles has an .myapprc.json in every directory the strategy tests can reach, a root marker in
// /home/me/work, and a user config.
var strategyFiles = func() map[string]string {
	files := fileSet(
		"/.myapprc.json", "/home/.myapprc.json", "/home/me/.myapprc.json", "/home/me/work/.myapprc.json",
		app+"/.myapprc.json", "/home/me/.config/myapp/config.json",
	)
	files["/home/me/work/.git/HEAD"] = "ref: refs/heads/main"
	return files
}()

// DL-42, DL-140, DL-147, design.md Search strategies: the directories each strategy searches, with the stop dir and
// the root marker inclusive. The layer lists show every file found; first-match resolution loads the nearest.
func TestGoldenCosmiconfigStrategies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts []Option
	}{
		{"SearchNone (the default)", nil},
		{"SearchProject with a marker", []Option{WithSearchStrategy(SearchProject), WithRootMarker(".git")}},
		{"SearchProject without a marker in reach", []Option{WithSearchStrategy(SearchProject), WithRootMarker("go.mod")}},
		{"SearchGlobal", []Option{WithSearchStrategy(SearchGlobal)}},
		{"SearchGlobal with a stop dir", []Option{WithSearchStrategy(SearchGlobal), WithStopDir("/home/me/work")}},
		{"SearchGlobal with a stop dir that isn't an ancestor", []Option{WithSearchStrategy(SearchGlobal), WithStopDir("/elsewhere")}},
	}
	var b strings.Builder
	for _, tc := range cases {
		res := cosmiLoad(t, strategyFiles, inDir(app, nil), tc.opts...)
		b.WriteString("# " + tc.name + "\n" + detail(res))
	}
	golden.Check(t, "discovery/cosmiconfig-strategies.golden", []byte(b.String()))
}

// DL-139, design.md Search strategies: with first-match resolution, SearchGlobal reaches the user directory only when
// nothing nearer matched.
func TestSearchGlobalReachesUserLast(t *testing.T) {
	t.Parallel()
	files := fileSet("/home/me/.config/myapp/config.json", "/home/me/work/.myapprc.json")
	res := cosmiLoad(t, files, inDir(app, nil), WithSearchStrategy(SearchGlobal))
	wantPathsList(t, loaded(res), "/home/me/work/.myapprc.json")

	files = fileSet("/home/me/.config/myapp/config.json")
	res = cosmiLoad(t, files, inDir(app, nil), WithSearchStrategy(SearchGlobal))
	wantPathsList(t, loaded(res), "/home/me/.config/myapp/config.json")
}

func wantPathsList(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("paths = %q, want %q", got, want)
	}
}

// DL-139: first-match resolution loads only the highest-precedence non-empty file across every scope. Empty files are
// skipped; the others are LayerIgnored, without a warning; a broken file below the match isn't decoded, so it doesn't
// fail the load, but one above it does.
func TestFirstMatch(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/low/config.json":  `{"low": true`,
		"/mid/config.json":  `{"mid": true}`,
		"/high/config.json": `  `,
	}
	scopes := []Option{
		WithResolution(FirstMatch),
		fixed("low", "/low", "config.json"), fixed("mid", "/mid", "config.json"), fixed("high", "/high", "config.json"),
		WithScopes("low", "mid", "high"),
	}
	res := mustLoad(t, memfs.New(files), scopes...)
	wantTree(t, res, `{"mid": true}`)
	wantFound(t, res, "low /low/config.json ignored", "mid /mid/config.json loaded", "high /high/config.json empty")
	if l := layerFor(t, res, "/low/config.json"); l.Reason == "" || len(l.Warnings) != 0 {
		t.Errorf("the ignored layer: reason %q, warnings %v; want a reason and no warning", l.Reason, l.Warnings)
	}

	files["/high/config.json"] = `{"high": `
	err := loadFails(t, memfs.New(files), scopes...)
	if pe, ok := asErr[*ParseError](err); !ok || pe.Path != "/high/config.json" {
		t.Errorf("error = %v, want the high file's *ParseError", err)
	}
}

// DL-142: the meta file in the current directory: its {name} key wins outright when non-empty, an empty table counts
// as empty, and its cosmiconfig key's searchPlaces and mergeSearchPlaces change the walk. It is read from the
// current directory, not the start directory.
func TestCosmiconfigMeta(t *testing.T) {
	t.Parallel()
	base := fileSet(srv+"/.myapprc.json", srv+"/myapp.settings.json", srv+"/sub/.myapprc.json")
	with := func(meta string) map[string]string {
		files := map[string]string{srv + "/.config/config.json": meta}
		for k, v := range base {
			files[k] = v
		}
		return files
	}

	res := cosmiLoad(t, with(`{"myapp": {"from": "meta"}}`), inDir(srv, nil))
	wantTree(t, res, `{"from": "meta"}`)

	res = cosmiLoad(t, with(`{"myapp": {}}`), inDir(srv, nil))
	wantPathsList(t, loaded(res), srv+"/.myapprc.json")

	res = cosmiLoad(t, with(`{"cosmiconfig": {"searchPlaces": ["{name}.settings.json"]}}`), inDir(srv, nil))
	wantPathsList(t, loaded(res), srv+"/myapp.settings.json")

	res = cosmiLoad(t, with(`{"cosmiconfig": {"searchPlaces": ["nothing.json"], "mergeSearchPlaces": false}}`), inDir(srv, nil))
	wantPathsList(t, loaded(res))

	res = cosmiLoad(t, with(`{"cosmiconfig": {"loaders": {}, "searchStrategy": "global"}}`), inDir(srv, nil))
	if n := countCode(bootWarnings(res), WarnBootstrapUnknownKey); n != 2 {
		t.Errorf("unknown-key warnings = %d, want 2 (loaders, searchStrategy): %v", n, bootWarnings(res))
	}

	res = cosmiLoad(t, with(`{"myapp": {"from": "meta"}}`), inDir(srv, nil), WithStartDir("sub"))
	wantTree(t, res, `{"from": "meta"}`)
	files := fileSet(srv + "/sub/.myapprc.json")
	files[srv+"/sub/.config/config.json"] = `{"myapp": {"from": "sub meta"}}`
	res = cosmiLoad(t, files, inDir(srv, nil), WithStartDir("sub"))
	wantPathsList(t, loaded(res), srv+"/sub/.myapprc.json")
}

// DL-144: with CosmiconfigLayout, explicit files replace every scope; nothing is searched.
func TestCosmiconfigExplicitFiles(t *testing.T) {
	t.Parallel()
	files := fileSet(srv+"/.myapprc.json", "/x/explicit.json")
	files[srv+"/.config/config.json"] = `{"myapp": {"from": "meta"}}`
	res := cosmiLoad(t, files, inDir(srv, nil), WithConfigFiles("/x/explicit.json"))
	wantPaths(t, res, "/x/explicit.json")
}

// DL-141, design.md Choosing a file's format: WithFileFormat rules beat the decoders' extensions, the latest rule
// first; a rule whose formats have no decoder doesn't apply; .myapprc is YAML when YAML is registered, otherwise JSON.
func TestFileFormatRules(t *testing.T) {
	t.Parallel()
	files := fileSet("/d/app.conf", "/d/app.json", "/d/.myapprc", "/d/deep/nested/app.settings", "/d/noext")
	tests := []struct {
		name     string
		template string
		opts     []Option
		format   string
	}{
		{"an extension rule", "app.conf", []Option{WithFileFormat(Extension("conf", ".cfg"), "yaml")}, "yaml"},
		{"a rule beats an extension", "app.json", []Option{WithFileFormat(Extension(".json"), "yaml")}, "yaml"},
		{
			"the latest rule first", "app.conf",
			[]Option{WithFileFormat(Extension("conf"), "yaml"), WithFileFormat(FileName("app.*"), "json")}, "json",
		},
		{"a rule without a decoder doesn't apply", "app.json", []Option{WithFileFormat(Extension("json"), "toml")}, "json"},
		{"the first registered format", ".myapprc", []Option{WithFileFormat(FileName(".{name}rc"), "toml", "yaml", "json")}, "yaml"},
		{"Glob with **", "deep/nested/app.settings", []Option{WithFileFormat(Glob("/d/**/*.settings"), "json")}, "json"},
		{"Regexp", "noext", []Option{WithFileFormat(Regexp(`/d/no[a-z]+`), "json")}, "json"},
		{"MatchFunc", "noext", []Option{WithFileFormat(MatchFunc(func(p string) bool { return filepath.Base(p) == "noext" }), "yaml")}, "yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mustLoad(t, memfs.New(files), append(tc.opts, WithDecoder(yamlAsJSON),
				WithScope("s", FixedDir("/d", Pattern{Template: tc.template})), WithScopes("s"))...)
			if len(res.found) != 1 || res.found[0].Format != tc.format {
				t.Errorf("found:\n%s\nwant one %s layer", detail(res), tc.format)
			}
		})
	}

	res := mustLoad(t, memfs.New(files), WithFileFormat(FileName(".{name}rc"), "yaml", "json"),
		WithScope("s", FixedDir("/d", Pattern{Template: ".{name}rc"})), WithScopes("s"))
	if len(res.found) != 1 || res.found[0].Format != "json" {
		t.Errorf("without YAML: found:\n%s\nwant .myapprc as JSON", detail(res))
	}
}

// DL-141: Glob's * stays within one segment; Regexp must match the whole path; a name no rule or extension matches
// isn't a candidate; and a matched file only loads when its format is in the scope's searched order.
func TestFileFormatMatching(t *testing.T) {
	t.Parallel()
	files := fileSet("/d/a/b.settings", "/d/noext")
	res := mustLoad(t, memfs.New(files), WithFileFormat(Glob("/d/*.settings"), "json"), WithFileFormat(Regexp(`no`), "json"),
		WithScope("s", FixedDir("/d", Pattern{Template: "a/b.settings"}, Pattern{Template: "noext"})), WithScopes("s"))
	wantFound(t, res)

	res = mustLoad(t, memfs.New(files), WithDecoder(yamlAsJSON), WithFormats("json"), WithFileFormat(FileName("noext"), "yaml"),
		WithScope("s", FixedDir("/d", Pattern{Template: "noext"})), WithScopes("s"))
	wantFound(t, res)
}

// DL-141: explicit files get their format from the rules too, so a file without an extension can be named.
func TestExplicitFileFormatRules(t *testing.T) {
	t.Parallel()
	files := fileSet("/home/me/.myapprc")
	res := cosmiLoad(t, files, inDir(srv, nil), WithConfigFiles("~/.myapprc"))
	wantPaths(t, res, "/home/me/.myapprc")
	if res.found[0].Format != "yaml" {
		t.Errorf("format = %q, want yaml", res.found[0].Format)
	}
}

// DL-143: EnvPathsDirs' user directory on each OS; it has no system directory.
func TestEnvPathsDirs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"linux", nil, filepath.Join("/home/me", ".config", "myapp")},
		{"linux", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, filepath.Join("/xdg", "myapp")},
		{"darwin", nil, filepath.Join("/home/me", "Library", "Preferences", "myapp")},
		{"darwin", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, filepath.Join("/home/me", "Library", "Preferences", "myapp")},
		{"windows", map[string]string{"APPDATA": `C:\Users\me\AppData\Roaming`}, filepath.Join(`C:\Users\me\AppData\Roaming`, "myapp", "Config")},
		{"windows", nil, filepath.Join("/home/me", "AppData", "Roaming", "myapp", "Config")},
	}
	for _, tc := range tests {
		h := testHost()
		h.GOOS = tc.goos
		for k, v := range tc.env {
			h.Env[k] = v
		}
		got, err := EnvPathsDirs.userDir(fakeHost{h}, "myapp")
		if err != nil || got != tc.want {
			t.Errorf("%s %v: userDir = %q, %v; want %q", tc.goos, tc.env, got, err, tc.want)
		}
	}

	_, err := New("myapp", WithPlatformDirs(EnvPathsDirs), WithDecoder(builtinJSONDecoder),
		WithScope("s", FixedDir("{system}", Pattern{Template: "config.json"})), WithScopes("s"))
	var oe *OptionError
	if !errors.As(err, &oe) {
		t.Errorf("{system} with EnvPathsDirs: error = %v, want an *OptionError", err)
	}
}

// DL-44: a package.json with a {name} key loads nothing in CosmiconfigLayout.
func TestCosmiconfigIgnoresPackageJSON(t *testing.T) {
	t.Parallel()
	files := map[string]string{srv + "/package.json": `{"myapp": {"a": 1}}`}
	res := cosmiLoad(t, files, inDir(srv, nil))
	wantFound(t, res)
}
