package cascadingconfig_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-54, DL-157, api.md Requiring config: what each level counts, globally and per scope.
func TestRequireConfig(t *testing.T) {
	flags := cc.Options(cc.WithScope("flags", cc.Values(cc.Tree{"a": "b"})), cc.WithScopes("sys", "proj", "flags"))
	noVars := cc.Options(cc.WithScope("env", cc.EnvVars("MYAPP_")), cc.WithScopes("sys", "proj", "env"))
	embedded := cc.Options(
		cc.WithScope("embedded", cc.EmbeddedFS(fstest.MapFS{"myapp.json": {Data: []byte(`{"a": 1}`)}}, cc.Pattern{Template: "{name}.json"})),
		cc.WithScopes("sys", "proj", "embedded"),
	)
	replaced := cc.Options(
		cc.WithScope("exp", cc.FixedDir("/nowhere", cc.Pattern{Template: "x.json"}), cc.ReplacedByConfigFiles()),
		cc.WithScopes("sys", "proj", "exp"),
		cc.WithConfigFiles("/srv/explicit.json"),
	)
	tests := []struct {
		name  string
		files map[string]string
		opts  []cc.Option
		fails int // the number of unmet requirements
	}{
		{name: "nothing found, nothing required"},
		{name: "nothing found", opts: []cc.Option{cc.RequireConfig(cc.RequireAny)}, fails: 1},
		{name: "flags satisfy RequireAny", opts: []cc.Option{flags, cc.RequireConfig(cc.RequireAny)}},
		{name: "flags don't satisfy RequireFile", opts: []cc.Option{flags, cc.RequireConfig(cc.RequireFile)}, fails: 1},
		{name: "defaults satisfy RequireAny", opts: []cc.Option{cc.WithDefaults(map[string]any{"a": 1}), cc.RequireConfig(cc.RequireAny)}},
		{name: "defaults don't satisfy RequireFile", opts: []cc.Option{cc.WithDefaults(map[string]any{"a": 1}), cc.RequireConfig(cc.RequireFile)}, fails: 1},
		{name: "all-zero defaults count as nothing", opts: []cc.Option{cc.WithDefaults(struct{ A int }{}), cc.RequireConfig(cc.RequireAny)}, fails: 1},
		{name: "an EnvVars scope with no variables counts as nothing", opts: []cc.Option{noVars, cc.RequireConfig(cc.RequireAny)}, fails: 1},
		{name: "an embedded file isn't a file", opts: []cc.Option{embedded, cc.RequireConfig(cc.RequireFile)}, fails: 1},
		{name: "an empty file satisfies RequireFile", files: map[string]string{projFile: "  \n"}, opts: []cc.Option{cc.RequireConfig(cc.RequireFile)}},
		{
			name: "a whitespace-only file fails RequireNonEmpty", files: map[string]string{projFile: "  \n"},
			opts: []cc.Option{cc.RequireConfig(cc.RequireFile | cc.RequireNonEmpty)}, fails: 1,
		},
		{name: "{} isn't empty", files: map[string]string{projFile: "{}"}, opts: []cc.Option{cc.RequireConfig(cc.RequireFile | cc.RequireNonEmpty)}},
		{
			name: "an ignored file doesn't count", files: map[string]string{projFile: `{"a": 1}`},
			opts: []cc.Option{cc.WithIgnoredPaths(projFile), cc.RequireConfig(cc.RequireFile)}, fails: 1,
		},
		{name: "a scope must have its own", files: map[string]string{projFile: `{"a": 1}`}, opts: []cc.Option{cc.RequireConfig(cc.RequireFile, "sys")}, fails: 1},
		{name: "each listed scope must qualify", files: map[string]string{projFile: `{"a": 1}`}, opts: []cc.Option{cc.RequireConfig(cc.RequireFile, "sys", "proj")}, fails: 1},
		{
			name: "each listed scope qualifies", files: map[string]string{sysFile: `{"a": 1}`, projFile: `{"a": 2}`},
			opts: []cc.Option{cc.RequireConfig(cc.RequireFile, "sys", "proj")},
		},
		{
			name: "several requirements all apply", opts: []cc.Option{flags, cc.RequireConfig(cc.RequireAny), cc.RequireConfig(cc.RequireFile, "sys")},
			fails: 1,
		},
		{name: "several unmet requirements are joined", opts: []cc.Option{cc.RequireConfig(cc.RequireAny), cc.RequireConfig(cc.RequireFile, "sys")}, fails: 2},
		{
			name: "explicit files count for the scope they replace", files: map[string]string{"/srv/explicit.json": `{"a": 1}`},
			opts: []cc.Option{replaced, cc.RequireConfig(cc.RequireFile, "exp")},
		},
		{
			name: "under WithConfigFilesOnly, explicit files count for every scope", files: map[string]string{"/srv/explicit.json": `{"a": 1}`},
			opts: []cc.Option{cc.WithConfigFiles("/srv/explicit.json"), cc.WithConfigFilesOnly(), cc.RequireConfig(cc.RequireFile, "sys", "proj")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := newLoader(t, tc.files, nil, append([]cc.Option{twoScopes}, tc.opts...)...).Load(t.Context())
			if tc.fails == 0 {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				return
			}
			if res == nil {
				t.Errorf("Load returned a nil Result")
			}
			errs := cc.Errors(err)
			if len(errs) != tc.fails {
				t.Fatalf("got %d errors, want %d: %v", len(errs), tc.fails, err)
			}
			for _, e := range errs {
				var nf *cc.NotFoundError
				if !errors.As(e, &nf) || nf.Op != "RequireConfig" || cc.CodeOf(e) != cc.CodeNotFound {
					t.Errorf("error %v isn't a 404 *NotFoundError from RequireConfig", e)
				}
			}
		})
	}
}

// api.md Requiring config: an unmet per-scope requirement names the scope.
func TestRequireConfigNamesTheScope(t *testing.T) {
	_, err := newLoader(t, nil, nil, twoScopes, cc.RequireConfig(cc.RequireFile, "sys")).Load(t.Context())
	if err == nil || !strings.Contains(err.Error(), `"sys"`) {
		t.Errorf("Load = %v, want an error naming the sys scope", err)
	}
}
