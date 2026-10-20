package cascadingconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// bootFound writes the bootstrap files a load read, one per line: scope, path and status, then any warning codes.
func bootFound(res *Result) string {
	var b strings.Builder
	for _, l := range res.bootstrap {
		fields := []string{string(l.Scope), l.Path, statusNames[l.Status]}
		for _, w := range l.Warnings {
			fields = append(fields, string(w.Code))
		}
		b.WriteString(strings.Join(fields, " ") + "\n")
	}
	return b.String()
}

// bootWarnings returns the codes of every warning on the bootstrap files.
func bootWarnings(res *Result) []WarningCode {
	var out []WarningCode
	for _, l := range res.bootstrap {
		for _, w := range l.Warnings {
			out = append(out, w.Code)
		}
	}
	return out
}

func hasCode(codes []WarningCode, code WarningCode) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// miseFails loads MiseLayout over files and returns the error it must fail with.
func miseFails(t *testing.T, files map[string]string, change func(*hooks.Host), opts ...Option) error {
	t.Helper()
	h := testHost()
	if change != nil {
		change(&h)
	}
	l, err := NewWithPreset("myapp", MiseLayout,
		append([]Option{WithFileSystem(memFS{memfs.New(files)}), withHost(fakeHost{h}), WithDecoder(tomlAsJSON)}, opts...)...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	_, err = l.Load(t.Context())
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	return err
}

// DL-131, DL-138, config-locations.md 3.7: MiseLayout's bootstrap files, lowest precedence first, from inside ~
// (where .config/myapprc.toml isn't read) and from outside it (where it isn't read in / either).
func TestGoldenBootstrapFiles(t *testing.T) {
	files := map[string]string{}
	for _, p := range []string{
		"/etc/myapp/myapprc.toml", "/home/me/.config/myapp/myapprc.toml", "/home/me/.config/myapp/myapprc.local.toml",
		"/home/me/.config/myapprc.toml", "/home/me/.myapprc.toml", "/home/me/work/.config/myapprc.toml",
		"/home/me/work/.myapprc.toml", app + "/.myapprc.local.toml",
		"/.config/myapprc.toml", "/.myapprc.toml", "/srv/.myapprc.toml", "/srv/app/.config/myapprc.toml",
	} {
		files[p] = `{}`
	}
	inside := miseLoad(t, memfs.New(files), inDir(app, nil))
	golden.Check(t, "discovery/bootstrap-inside-home.golden", []byte(bootFound(inside)))
	outside := miseLoad(t, memfs.New(files), inDir("/srv/app", nil))
	golden.Check(t, "discovery/bootstrap-outside-home.golden", []byte(bootFound(outside)))
}

// DL-133, DL-134: env from bootstrap files selects variants; a higher file replaces a lower one's list; {NAME}_ENV and
// WithEnvs beat them; bootstrap keys never reach the config.
func TestBootstrapEnv(t *testing.T) {
	files := fileSet(app+"/myapp.dev.toml", app+"/myapp.ci.toml", app+"/myapp.qa.toml")
	files["/home/me/.config/myapp/myapprc.toml"] = `{"env": ["ci"]}`
	files[app+"/.myapprc.toml"] = `{"env": ["dev"]}`
	tests := []struct {
		name   string
		change func(*hooks.Host)
		opts   []Option
		want   string
	}{
		{"the nearest file wins", nil, nil, app + "/myapp.dev.toml"},
		{"MYAPP_ENV beats it", setEnv("MYAPP_ENV", "qa"), nil, app + "/myapp.qa.toml"},
		{"WithEnvs beats it", nil, []Option{WithEnvs("ci")}, app + "/myapp.ci.toml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := miseLoad(t, memfs.New(files), inDir(app, tc.change), tc.opts...)
			wantPaths(t, res, tc.want)
			if _, ok := res.Tree()["env"]; ok {
				t.Errorf("a bootstrap key reached the config")
			}
		})
	}
}

// srv is a project directory outside ~, so the walk never reaches the user's own files.
const srv = "/srv/app"

// formatFiles has a TOML and a JSON config file in the system, user and project scopes.
func formatFiles() map[string]string {
	return fileSet(
		"/etc/myapp/config.json", "/etc/myapp/config.toml",
		"/home/me/.config/myapp/config.json", "/home/me/.config/myapp/config.toml",
		srv+"/myapp.json", srv+"/myapp.toml",
	)
}

// DL-135, design.md Bootstrap file, "Scope limits": each scope takes formats from the highest-precedence bootstrap
// file at least as trusted as itself; the env var beats them all, and they beat the app's option.
func TestBootstrapFormatsByTrust(t *testing.T) {
	sys := []string{"/etc/myapp/config.json", "/etc/myapp/config.toml"}
	usr := []string{"/home/me/.config/myapp/config.json", "/home/me/.config/myapp/config.toml"}
	prj := []string{srv + "/myapp.json", srv + "/myapp.toml"}
	only := func(paths []string, ext string) []string {
		var out []string
		for _, p := range paths {
			if strings.HasSuffix(p, ext) {
				out = append(out, p)
			}
		}
		return out
	}
	cat := func(lists ...[]string) []string {
		var out []string
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	rc := map[string]string{
		"system":  "/etc/myapp/myapprc.toml",
		"user":    "/home/me/.config/myapp/myapprc.toml",
		"project": srv + "/.myapprc.toml",
	}
	tests := []struct {
		name   string
		rcs    map[string]string
		change func(*hooks.Host)
		opts   []Option
		want   []string
	}{
		{"project only", map[string]string{"project": "json"}, nil, nil, cat(sys, usr, only(prj, ".json"))},
		{"user only", map[string]string{"user": "json"}, nil, nil, cat(sys, only(usr, ".json"), only(prj, ".json"))},
		{"user and project", map[string]string{"user": "json", "project": "toml"}, nil, nil, cat(sys, only(usr, ".json"), only(prj, ".toml"))},
		{"system only", map[string]string{"system": "json"}, nil, nil, cat(only(sys, ".json"), only(usr, ".json"), only(prj, ".json"))},
		{
			"the env var wins", map[string]string{"project": "json"}, setEnv("MYAPP_CONFIG_FORMATS", "toml"), nil,
			cat(only(sys, ".toml"), only(usr, ".toml"), only(prj, ".toml")),
		},
		{
			"bootstrap beats the option", map[string]string{"project": "json"}, nil, []Option{WithFormats("toml")},
			cat(only(sys, ".toml"), only(usr, ".toml"), only(prj, ".json")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := formatFiles()
			for scope, format := range tc.rcs {
				files[rc[scope]] = `{"formats": ["` + format + `"]}`
			}
			res := miseLoad(t, memfs.New(files), inDir(srv, tc.change), tc.opts...)
			wantPaths(t, res, tc.want...)
		})
	}
}

// DL-136, design.md File format precedence: {NAME}_CONFIG_FORMATS takes WithFormats' syntax, applies to the bootstrap
// search too, and an unknown format is an *OptionError at Load named after the variable.
func TestConfigFormatsVariable(t *testing.T) {
	files := fileSet(app+"/myapp.json", app+"/myapp.toml")
	files[app+"/.myapprc.toml"] = `{"env": ["toml-rc"]}`
	files[app+"/.myapprc.json"] = `{"env": ["json-rc"]}`
	res := miseLoad(t, memfs.New(files), inDir(app, setEnv("MYAPP_CONFIG_FORMATS", " .json ")))
	wantPaths(t, res, app+"/myapp.json")
	if bootFound(res) != "project "+app+"/.myapprc.json loaded\n" {
		t.Errorf("bootstrap files:\n%s\nwant only the JSON one", bootFound(res))
	}

	for _, value := range []string{"xml", "yaml", "toml,toml"} {
		err := miseFails(t, files, inDir(app, setEnv("MYAPP_CONFIG_FORMATS", value)))
		var oe *OptionError
		if !errors.As(err, &oe) || oe.Op != "MYAPP_CONFIG_FORMATS" {
			t.Errorf("MYAPP_CONFIG_FORMATS=%s: error = %v, want an *OptionError named after the variable", value, err)
		}
	}
}

// design.md File format precedence, "In the bootstrap file": a bootstrap file's formats don't change how bootstrap
// files are found; formats = [] and an unknown format are errors naming the file.
func TestBootstrapFormatsRules(t *testing.T) {
	files := fileSet(app + "/myapp.toml")
	files[app+"/.myapprc.json"] = `{"formats": ["json"]}`
	files[app+"/.myapprc.local.toml"] = `{"env": ["dev"]}`
	res := miseLoad(t, memfs.New(files), inDir(app, nil))
	if !strings.Contains(bootFound(res), ".myapprc.local.toml loaded") {
		t.Errorf("bootstrap files:\n%s\nwant the TOML one read despite formats = [json]", bootFound(res))
	}

	for _, value := range []string{`[]`, `["xml"]`} {
		bad := fileSet(app + "/myapp.toml")
		bad[app+"/.myapprc.toml"] = `{"formats": ` + value + `}`
		err := miseFails(t, bad, inDir(app, nil))
		if oe, ok := asErr[*OptionError](err); !ok || oe.Path != app+"/.myapprc.toml" {
			t.Errorf("formats = %s: error = %v, want an *OptionError naming the file", value, err)
		}
	}
}

// DL-26, DL-68, DL-135: ignored_config_paths resolve against their file, apply to scopes at most as trusted as the
// file, and an entry that would have matched a more trusted scope's file is dropped with a warning.
func TestBootstrapIgnoredPaths(t *testing.T) {
	files := fileSet("/etc/myapp/config.toml", "/home/me/.config/myapp/config.toml", app+"/myapp.toml", app+"/myapp/config.toml")
	files[app+"/.myapprc.toml"] = `{"ignored_config_paths": ["/etc/myapp", "myapp"]}`
	files["/home/me/.config/myapp/myapprc.toml"] = `{"ignored_config_paths": ["` + app + `/myapp.toml"]}`
	res := miseLoad(t, memfs.New(files), inDir(app, nil))
	wantFound(t, res,
		"system /etc/myapp/config.toml loaded",
		"user /home/me/.config/myapp/config.toml loaded",
		"project "+app+"/myapp/config.toml ignored",
		"project "+app+"/myapp.toml ignored",
	)
	if !hasCode(bootWarnings(res), WarnBootstrapKeyDropped) {
		t.Errorf("bootstrap warnings = %v, want WarnBootstrapKeyDropped for /etc/myapp", bootWarnings(res))
	}
}

// DL-133: ceiling_paths from a bootstrap file limit the main walk, not the bootstrap walk; override_config_filenames
// replace the walk's shared names, and the env var beats them.
func TestBootstrapWalkKeys(t *testing.T) {
	files := fileSet("/home/me/work/myapp.toml", app+"/myapp.toml", app+"/myapp.x.toml", app+"/custom.toml", app+"/env.toml")
	files["/home/me/.myapprc.toml"] = `{"env": ["x"]}`
	files[app+"/.myapprc.toml"] = `{"ceiling_paths": [".."], "override_config_filenames": ["custom.{ext}"]}`
	res := miseLoad(t, memfs.New(files), inDir(app, nil))
	wantPaths(t, res, app+"/custom.toml", app+"/myapp.x.toml")

	res = miseLoad(t, memfs.New(files), inDir(app, setEnv("MYAPP_OVERRIDE_CONFIG_FILENAMES", "env.toml")))
	wantPaths(t, res, app+"/env.toml", app+"/myapp.x.toml")
}

// DL-132, DL-78: unknown keys, include and _unset warn; WithoutBootstrapKey and WithBootstrapLimit drop keys with a
// warning; MiseHardenedLayout limits project files to env and formats.
func TestBootstrapKeyLimits(t *testing.T) {
	rc := app + "/.myapprc.toml"
	content := `{"env": ["dev"], "formats": ["toml"], "ceiling_paths": ["/x"], "include": ["a.toml"], "_unset": ["b"], "nope": 1}`
	files := fileSet(app+"/myapp.dev.toml", app+"/myapp.json")
	files[rc] = content

	res := miseLoad(t, memfs.New(files), inDir(app, nil))
	codes := bootWarnings(res)
	if n := countCode(codes, WarnBootstrapUnknownKey); n != 3 {
		t.Errorf("unknown-key warnings = %d, want 3 (include, _unset, nope): %v", n, codes)
	}
	wantPaths(t, res, app+"/myapp.dev.toml")

	res = miseLoad(t, memfs.New(files), inDir(app, nil), WithoutBootstrapKey(BootstrapFormats))
	wantPaths(t, res, app+"/myapp.json", app+"/myapp.dev.toml")
	if !hasCode(bootWarnings(res), WarnBootstrapUnknownKey) {
		t.Errorf("a disabled key didn't warn: %v", bootWarnings(res))
	}

	res = miseLoad(t, memfs.New(files), inDir(app, nil), WithBootstrapLimit(ScopeProject, BootstrapEnv))
	wantPaths(t, res, app+"/myapp.json", app+"/myapp.dev.toml")
	if !hasCode(bootWarnings(res), WarnBootstrapKeyDropped) {
		t.Errorf("a key outside the limit didn't warn: %v", bootWarnings(res))
	}

	h := testHost()
	h.Cwd = app
	l, err := NewWithPreset("myapp", MiseHardenedLayout, WithFileSystem(memFS{memfs.New(files)}), withHost(fakeHost{h}), WithDecoder(tomlAsJSON))
	if err != nil {
		t.Fatal(err)
	}
	res, err = l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n := countCode(bootWarnings(res), WarnBootstrapKeyDropped); n != 1 {
		t.Errorf("hardened: dropped-key warnings = %d, want 1 (ceiling_paths): %v", n, bootWarnings(res))
	}
	wantPaths(t, res, app+"/myapp.dev.toml")
}

func countCode(codes []WarningCode, code WarningCode) int {
	n := 0
	for _, c := range codes {
		if c == code {
			n++
		}
	}
	return n
}

// DL-78, DL-133: a value of the wrong type, or an unsafe environment name, is an *OptionError at Load naming the file.
func TestBootstrapWrongTypes(t *testing.T) {
	for _, content := range []string{
		`{"env": "dev"}`, `{"env": [1]}`, `{"env": ["../x"]}`, `{"formats": "toml"}`, `{"ceiling_paths": "/x"}`,
		`{"ignored_config_paths": [true]}`, `{"override_config_filenames": {"a": 1}}`,
	} {
		t.Run(content, func(t *testing.T) {
			files := fileSet(app + "/myapp.toml")
			files[app+"/.myapprc.toml"] = content
			err := miseFails(t, files, inDir(app, nil))
			if oe, ok := asErr[*OptionError](err); !ok || oe.Path != app+"/.myapprc.toml" {
				t.Errorf("error = %v, want an *OptionError naming the file", err)
			}
		})
	}
}

// DL-134: WithBootstrapTransform sees each bootstrap file's layer and a copy of its tree, can rewrite it, and runs in
// registration order; its error is a *TransformError naming the file.
func TestBootstrapTransform(t *testing.T) {
	files := fileSet(app+"/myapp.dev.toml", app+"/myapp.prod.toml")
	files[app+"/.myapprc.toml"] = `{"env": ["prod"]}`
	var seen []string
	allowDev := func(_ context.Context, l Layer, t Tree) (Tree, error) {
		seen = append(seen, "first "+l.Path)
		t["env"] = []any{"dev"}
		return t, nil
	}
	second := func(_ context.Context, l Layer, t Tree) (Tree, error) {
		seen = append(seen, "second "+string(l.Scope))
		return t, nil
	}
	res := miseLoad(t, memfs.New(files), inDir(app, nil), WithBootstrapTransform(allowDev), WithBootstrapTransform(second))
	wantPaths(t, res, app+"/myapp.dev.toml")
	if want := []string{"first " + app + "/.myapprc.toml", "second project"}; strings.Join(seen, ";") != strings.Join(want, ";") {
		t.Errorf("transforms ran as %q, want %q", seen, want)
	}

	boom := errors.New("not allowed")
	err := miseFails(t, files, inDir(app, nil), WithBootstrapTransform(func(context.Context, Layer, Tree) (Tree, error) { return nil, boom }))
	te, ok := asErr[*TransformError](err)
	if !ok || te.Path != app+"/.myapprc.toml" || !errors.Is(err, boom) {
		t.Errorf("error = %v, want a *TransformError for the file wrapping the function's error", err)
	}
	wantCode(t, err, CodeUnprocessable)
}

// design.md Choosing scopes: an unselected scope's bootstrap files aren't read; a scope replaced by explicit files
// still has its bootstrap files read.
func TestBootstrapScopeSelection(t *testing.T) {
	files := fileSet(app+"/myapp.toml", app+"/myapp.dev.toml", "/x/explicit.toml", "/home/me/.config/myapp/config.dev.toml")
	files["/home/me/.config/myapp/myapprc.toml"] = `{"env": ["dev"]}`
	res := miseLoad(t, memfs.New(files), inDir(app, nil), WithScopes(ScopeProject))
	wantPaths(t, res, app+"/myapp.toml")

	files = fileSet("/x/explicit.toml", "/home/me/.config/myapp/config.dev.toml")
	files[app+"/.myapprc.toml"] = `{"env": ["dev"]}`
	res = miseLoad(t, memfs.New(files), inDir(app, nil), WithConfigFiles("/x/explicit.toml"))
	wantPaths(t, res, "/home/me/.config/myapp/config.dev.toml", "/x/explicit.toml")
}
