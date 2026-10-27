package cascadingconfig_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// DL-158, api.md Describe: a mise project's report, as text and JSON. It has a defaults scope, a bootstrap file that
// selects an environment, a user file the walk reaches too, an empty env file, an EnvVars scope and an unknown key.
// Secret values in the fixtures never appear.
func TestReportGolden(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":              `{"log": {"level": "info"}}`,
		"/home/me/.config/myapp/myapprc.json": `{"env": ["dev"]}`,
		"/home/me/.config/myapp/config.json":  `{"password": "SECRET-user"}`,
		"/home/me/work/myapp.json":            `{"server": {"port": 8080, "token": "SECRET-project"}}`,
		"/home/me/work/myapp.dev.json":        "  \n",
	})
	env := map[string]string{"MYAPP_IGNORED_CONFIG_PATHS": "/SECRET-ignored", "MYAPP_SERVER__HOST": "SECRET-env"}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(fsys), cctest.WithHost(testHost(env)),
		cc.WithDefaults(map[string]any{"log": map[string]any{"format": "text"}}),
		cc.WithScope("env", cc.EnvVars("MYAPP_"), cc.After(cc.ScopeOverride)),
		cc.WithScopes(cc.ScopeSystem, cc.ScopeUser, cc.ScopeProject, cc.ScopeOverride, "env"),
	)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var v struct {
		Log    logConfig `koanf:"log"`
		Server struct {
			Port  int    `koanf:"port"`
			Token string `koanf:"token"`
			Host  string `koanf:"host"`
		} `koanf:"server"`
	}
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rep := res.Describe()

	text := rep.String()
	cctest.Golden(t, "report/mise-project.golden", []byte(text))
	noSecret(t, "the report text", text, "SECRET")

	js, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	cctest.Golden(t, "report/mise-project.json.golden", append(js, '\n'))
	noSecret(t, "the report JSON", string(js), "SECRET")
}

// DL-152: Origin.EnvVar names the variable behind a key from EnvVars, through MapEnv too; other keys have none.
func TestOriginEnvVar(t *testing.T) {
	t.Parallel()
	env := map[string]string{"MYAPP_SERVER__PORT": "1", "MYAPP_X": "2"}
	mapped := cc.MapEnv(func(name, value string) (cc.Path, any, bool) {
		return cc.Path{"mapped", strings.ToLower(name)}, value, true
	})
	l := newLoader(t, map[string]string{projFile: `{"file": 1}`}, env, twoScopes,
		cc.WithScope("env", cc.EnvVars("MYAPP_")), cc.WithScope("mapped", cc.EnvVars("MYAPP_", mapped)),
		cc.WithScopes("proj", "env", "mapped"))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for key, want := range map[string]string{"server.port": "MYAPP_SERVER__PORT", "x": "MYAPP_X", "mapped.myapp_x": "MYAPP_X", "file": ""} {
		o, ok := cc.OriginOf(res, key)
		if !ok || o.EnvVar != want {
			t.Errorf("OriginOf(%s).EnvVar = %q, %v; want %q", key, o.EnvVar, ok, want)
		}
	}
}

// DL-156, DL-75: a failed load returns a result whose report lists each broken file as LayerFailed, with a reason
// built from the error's fields, and a nil tree.
func TestFailedLoadReport(t *testing.T) {
	t.Parallel()
	res, err := newLoader(t, map[string]string{
		sysFile:  `{"a": "SECRET-1",`,
		projFile: `{"b": SECRET-2}`,
	}, nil, twoScopes).Load(t.Context())
	if err == nil {
		t.Fatalf("Load succeeded, want parse errors")
	}
	if errs := cc.Errors(err); len(errs) != 2 || !errors.Is(errs[0], cc.ErrParse) || !errors.Is(errs[1], cc.ErrParse) {
		t.Errorf("errors = %v, want two parse errors", errs)
	}
	if res == nil {
		t.Fatalf("Load returned a nil Result")
	}
	if tree := res.Tree(); tree != nil {
		t.Errorf("Tree = %v, want nil", tree)
	}
	rep := res.Describe()
	for _, path := range []string{sysFile, projFile} {
		l := layerAt(t, rep, path)
		if l.Status != cc.LayerFailed || !strings.HasPrefix(l.Reason, "parse error") {
			t.Errorf("%s: status %v, reason %q; want LayerFailed and a parse error", path, l.Status, l.Reason)
		}
		noSecret(t, "the reason", l.Reason, "SECRET")
	}
	js, _ := json.Marshal(rep)
	noSecret(t, "the report text", rep.String(), "SECRET")
	noSecret(t, "the report JSON", string(js), "SECRET")
}

// DL-155: a failed stage stops the load. With a broken file, the merge error the other files would cause and the unmet
// requirement aren't reported.
func TestLoadStages(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		sysFile:                  `{`,
		projFile:                 `{"a": 1}`,
		"/home/me/work/top.json": `{"a": [2]}`,
	}
	res, err := newLoader(t, files, nil, twoScopes,
		cc.WithScope("top", cc.FixedDir("/home/me/work", cc.Pattern{Template: "top.json"})),
		cc.WithScopes("sys", "proj", "top"),
		cc.WithMergeRule("a", cc.Append),
		cc.RequireConfig(cc.RequireFile, "sys"),
	).Load(t.Context())
	errs := cc.Errors(err)
	if len(errs) != 1 || !errors.Is(errs[0], cc.ErrParse) {
		t.Errorf("errors = %v, want only the parse error", errs)
	}
	if res == nil {
		t.Fatalf("Load returned a nil Result")
	}

	delete(files, sysFile)
	_, err = newLoader(t, files, nil, twoScopes,
		cc.WithScope("top", cc.FixedDir("/home/me/work", cc.Pattern{Template: "top.json"})),
		cc.WithScopes("sys", "proj", "top"),
		cc.WithMergeRule("a", cc.Append),
		cc.RequireConfig(cc.RequireFile, "sys"),
	).Load(t.Context())
	if errs := cc.Errors(err); len(errs) != 1 || !errors.Is(errs[0], cc.ErrMerge) {
		t.Errorf("errors = %v, want only the merge error", errs)
	}
}

// DL-158: WithProbes lists each directory and pattern searched where nothing was found, with {env} filled in, in
// search order. Without it there are none.
func TestReportProbes(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/etc/myapp/config.json": `{}`}
	scope := cc.Options(
		cc.WithScope("sys", cc.FixedDir("/etc/myapp",
			cc.Pattern{Template: "config.json"}, cc.Pattern{Template: "config.{env}.json", Variant: cc.VariantEnv})),
		cc.WithScopes("sys"), cc.WithEnvs("dev"))

	res, err := newLoader(t, files, nil, scope, cc.WithProbes()).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var want []cc.Probe
	for _, env := range []string{"unix", "linux", "linux-x64", "dev"} {
		want = append(want, cc.Probe{Scope: "sys", Dir: "/etc/myapp", Pattern: "config." + env + ".json"})
	}
	if got := res.Describe().Probes; !reflect.DeepEqual(got, want) {
		t.Errorf("Probes = %+v\nwant %+v", got, want)
	}

	res, err = newLoader(t, files, nil, scope).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Describe().Probes; got != nil {
		t.Errorf("Probes without WithProbes = %+v, want none", got)
	}
}

// DL-158: the report's scopes say which are selected, and why a selected scope wasn't searched; its envs say where
// they came from; its bindings say whether each variable is set, never its value.
func TestReportContext(t *testing.T) {
	t.Parallel()
	env := map[string]string{"MYAPP_OFF": "1", "MYAPP_ENV": "staging", "MYAPP_CEILING_PATHS": "/SECRET-ceiling"}
	l := newLoader(t, nil, env, twoScopes,
		cc.WithBinding("OFF", ""), cc.WithBinding(cc.BindEnv, ""), cc.WithBinding(cc.BindCeilingPaths, ""),
		cc.WithScope("off", cc.FixedDir("/srv", cc.Pattern{Template: "x.json"}), cc.DisabledBy("OFF")),
		cc.WithScope("unused", cc.FixedDir("/srv", cc.Pattern{Template: "y.json"})),
		cc.WithScopes("sys", "proj", "off"),
	)
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rep := res.Describe()
	scopes := map[cc.ScopeName]cc.ScopeInfo{}
	for _, s := range rep.Scopes {
		scopes[s.Name] = s
	}
	if s := scopes["off"]; !s.Selected || !strings.Contains(s.Skipped, "MYAPP_OFF") {
		t.Errorf("off = %+v, want selected and skipped because of MYAPP_OFF", s)
	}
	if s := scopes["unused"]; s.Selected || s.Kind != "FixedDir" {
		t.Errorf("unused = %+v, want an unselected FixedDir", s)
	}
	if s := scopes["proj"]; !s.Selected || s.Skipped != "" || s.Trust != cc.TrustProject {
		t.Errorf("proj = %+v, want selected and searched", s)
	}
	wantEnvs := cc.EnvInfo{Selected: []string{"staging"}, Source: "MYAPP_ENV", Platform: []string{"unix", "linux", "linux-x64"}}
	if !reflect.DeepEqual(rep.Envs, wantEnvs) {
		t.Errorf("Envs = %+v, want %+v", rep.Envs, wantEnvs)
	}
	set := map[string]bool{}
	for _, b := range rep.Bindings {
		set[b.Var] = b.Set
	}
	if !set["MYAPP_OFF"] || !set["MYAPP_ENV"] || !set["MYAPP_CEILING_PATHS"] || len(set) != 3 {
		t.Errorf("Bindings = %+v, want the three declared, all set", rep.Bindings)
	}
	js, _ := json.Marshal(rep)
	noSecret(t, "the report text", rep.String(), "SECRET")
	noSecret(t, "the report JSON", string(js), "SECRET")

	res, err = newLoader(t, nil, nil, twoScopes, cc.WithEnvs("a")).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if src := res.Describe().Envs.Source; src != "WithEnvs" {
		t.Errorf("Envs.Source = %q, want WithEnvs", src)
	}
}

// DL-158: a scope that explicit files replace says so.
func TestReportReplacedScope(t *testing.T) {
	t.Parallel()
	res, err := newLoader(t, map[string]string{"/srv/explicit.json": `{}`}, nil, twoScopes,
		cc.WithScope("exp", cc.FixedDir("/nowhere", cc.Pattern{Template: "x.json"}), cc.ReplacedByConfigFiles()),
		cc.WithScopes("sys", "proj", "exp"), cc.WithConfigFiles("/srv/explicit.json"),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, s := range res.Describe().Scopes {
		if s.Name == "exp" && !strings.Contains(s.Skipped, "explicit") {
			t.Errorf("exp = %+v, want skipped because explicit files replace it", s)
		}
	}
	if l := layerAt(t, res.Describe(), "/srv/explicit.json"); l.Scope != "exp" || l.Status != cc.LayerLoaded {
		t.Errorf("explicit file = %+v, want loaded in exp", l)
	}
}

// DL-158: the text forms of the enums and Path, used by the report's JSON.
func TestTextForms(t *testing.T) {
	t.Parallel()
	type texter interface {
		String() string
		MarshalText() ([]byte, error)
	}
	cases := map[texter]string{
		cc.VariantShared: "shared", cc.VariantEnv: "env", cc.VariantLocal: "local", cc.VariantEnvLocal: "env_local",
		cc.TrustProject: "project", cc.TrustUser: "user", cc.TrustSystem: "system",
		cc.LayerLoaded: "loaded", cc.LayerIgnored: "ignored", cc.LayerEmpty: "empty", cc.LayerUnreadable: "unreadable",
		cc.LayerFailed: "failed", cc.LayerDuplicate: "duplicate",
		cc.OriginSet: "set", cc.OriginMerged: "merged", cc.OriginAppended: "appended", cc.OriginPrepended: "prepended",
		cc.OriginNull: "null", cc.OriginFunc: "func", cc.OriginUnset: "unset",
	}
	for v, want := range cases {
		b, err := v.MarshalText()
		if v.String() != want || string(b) != want || err != nil {
			t.Errorf("%#v: String %q, MarshalText %q, %v; want %q", v, v.String(), b, err, want)
		}
	}

	p := cc.Path{"plugins", "foo.bar"}
	b, err := json.Marshal(p)
	if err != nil || string(b) != `"plugins.\"foo.bar\""` {
		t.Errorf("json.Marshal(%v) = %s, %v", p, b, err)
	}
	var back cc.Path
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, p) {
		t.Errorf("json.Unmarshal(%s) = %v, %v; want %v", b, back, err, p)
	}
}

// DL-76: the loader is safe for concurrent loads and decodes, and changing Tree's result changes neither the report,
// later decodes, nor later loads.
func TestConcurrentUse(t *testing.T) {
	t.Parallel()
	l := newLoader(t, map[string]string{sysFile: `{"server": {"host": "a"}}`, projFile: `{"server": {"port": 1}}`}, nil,
		twoScopes, cc.WithDefaults(map[string]any{"debug": true}))
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			res, err := l.Load(t.Context())
			if err != nil {
				t.Errorf("Load: %v", err)
				return
			}
			var v appConfig
			for range 4 {
				if err := res.Decode(&v); err != nil {
					t.Errorf("Decode: %v", err)
				}
				_ = res.Describe().String()
			}
			if v.Server.Host != "a" || v.Server.Port != 1 || !v.Debug {
				t.Errorf("decoded %+v", v)
			}
		})
	}
	wg.Wait()

	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := res.Describe().String()
	tree := res.Tree()
	if s, ok := tree["server"].(map[string]any); ok {
		s["host"] = "changed"
	}
	tree["extra"] = 1
	var v appConfig
	if err := res.Decode(&v); err != nil || v.Server.Host != "a" {
		t.Errorf("after changing Tree, Decode = %+v, %v", v, err)
	}
	if after := res.Describe().String(); after != before {
		t.Errorf("changing Tree changed the report:\n%s\nwant:\n%s", after, before)
	}
	again, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := again.Tree()["extra"]; ok {
		t.Errorf("changing Tree changed a later load")
	}
}
