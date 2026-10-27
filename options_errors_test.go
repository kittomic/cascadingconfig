package cascadingconfig_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// metaDecoder is a Decoder with only metadata, for registering formats.
type metaDecoder struct {
	format string
	exts   []string
}

func (d metaDecoder) Format() string                     { return d.format }
func (d metaDecoder) Extensions() []string               { return d.exts }
func (d metaDecoder) MediaTypes() []string               { return nil }
func (d metaDecoder) Decode([]byte) (cc.Document, error) { return cc.Document{Empty: true}, nil }

var (
	tomlDec = metaDecoder{"toml", []string{"toml"}}
	yamlDec = metaDecoder{"yaml", []string{"yaml", "yml"}}
	jsonDec = metaDecoder{"json", []string{"json"}}
)

// scope defines and selects one Values scope, so a loader passes the "no scopes" check.
var scope = cc.Options(cc.WithScope("s", cc.Values(cc.Tree{})), cc.WithScopes("s"))

// optionErrorCase is a New call that must fail with *OptionErrors whose Op fields are ops, in any order, and whose
// messages contain each of contains.
type optionErrorCase struct {
	name     string
	app      string
	opts     []cc.Option
	ops      []string
	contains []string
}

func runOptionErrorCases(t *testing.T, tests []optionErrorCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := tc.app
			if app == "" {
				app = "myapp"
			}
			l, err := cc.New(app, tc.opts...)
			if err == nil {
				t.Fatalf("New succeeded, want *OptionErrors from %q", tc.ops)
			}
			if l != nil {
				t.Errorf("New returned a loader with an error")
			}
			var ops []string
			for _, e := range cc.Errors(err) {
				var oe *cc.OptionError
				if !errors.As(e, &oe) {
					t.Errorf("error %v isn't an *OptionError", e)
					continue
				}
				ops = append(ops, oe.Op)
			}
			slices.Sort(ops)
			want := slices.Sorted(slices.Values(tc.ops))
			if !slices.Equal(ops, want) {
				t.Errorf("errors from %q, want %q: %v", ops, want, err)
			}
			for _, part := range tc.contains {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q doesn't mention %q", err.Error(), part)
				}
			}
		})
	}
}

func values() cc.ScopeKind { return cc.Values(cc.Tree{}) }

// DL-100, DL-104, design.md Choosing scopes and Custom scopes: scope options that can only be mistakes.
func TestScopeOptionErrors(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "no scopes", ops: []string{"New"}},
		{name: "no scopes left", opts: []cc.Option{scope, cc.WithoutScopes("s")}, ops: []string{"New"}},
		{name: "unknown scope in WithScopes", opts: []cc.Option{scope, cc.WithScopes("s", "nope")}, ops: []string{"WithScopes"}, contains: []string{"nope"}},
		{name: "unknown scope in WithoutScopes", opts: []cc.Option{scope, cc.WithoutScopes("nope")}, ops: []string{"WithoutScopes"}, contains: []string{"nope"}},
		{name: "a scope listed twice", opts: []cc.Option{scope, cc.WithScopes("s", "s")}, ops: []string{"WithScopes"}},
		{name: "empty WithScopes", opts: []cc.Option{scope, cc.WithScopes()}, ops: []string{"WithScopes"}},
		{name: "a name clash", opts: []cc.Option{scope, cc.WithScope("s", values())}, ops: []string{"WithScope"}, contains: []string{`"s"`}},
		{
			name: "an anchor not yet defined",
			opts: []cc.Option{scope, cc.WithScope("x", values(), cc.After("later")), cc.WithScope("later", values())},
			ops:  []string{"WithScope"}, contains: []string{"later"},
		},
		{name: "Before and After", opts: []cc.Option{scope, cc.WithScope("x", values(), cc.Before("s"), cc.After("s"))}, ops: []string{"WithScope"}},
		{
			name: "every problem at once",
			opts: []cc.Option{cc.WithScopes("nope"), cc.WithoutScopes("nope"), cc.WithScope("x", values(), cc.After("nope"))},
			ops:  []string{"WithScopes", "WithoutScopes", "WithScope", "New"},
		},
	})
}

// DL-99, design.md Safe environment names: the app name and WithEnvs names must be safe in a filename.
func TestNameErrors(t *testing.T) {
	bad := []string{
		".", "..", "a/b", `a\b`, "a\x00b", "a\x1fb", "a\x7fb", "a<b", "a>b", "a:b", `a"b`, "a|b", "a?b", "a*b",
		strings.Repeat("a", 256),
	}
	var tests []optionErrorCase
	for _, name := range bad {
		tests = append(tests,
			optionErrorCase{name: "app " + name, app: name, opts: []cc.Option{scope}, ops: []string{"New"}},
			optionErrorCase{name: "env " + name, opts: []cc.Option{scope, cc.WithEnvs("dev", name)}, ops: []string{"WithEnvs"}},
		)
	}
	runOptionErrorCases(t, tests)

	if _, err := cc.New("", scope); !errors.Is(err, cc.ErrInvalidOption) {
		t.Errorf("New with an empty app name: error = %v, want an *OptionError", err)
	}
}

// DL-99: names that are safe, including dotted names, as mise allows.
func TestSafeNames(t *testing.T) {
	for _, name := range []string{"myapp", "my-app", "my.app", "MyApp", "app_1", "é", strings.Repeat("a", 255)} {
		if _, err := cc.New(name, scope, cc.WithEnvs(name, "dev.linux")); err != nil {
			t.Errorf("New(%q) with WithEnvs(%q): %v", name, name, err)
		}
	}
}

// DL-73, DL-101, DL-105: format options that can only be mistakes.
func TestFormatOptionErrors(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "an unknown format", opts: []cc.Option{scope, cc.WithDecoder(jsonDec), cc.WithFormats("json", "xml")}, ops: []string{"WithFormats"}, contains: []string{"xml"}},
		{name: "a format with no decoder", opts: []cc.Option{scope, cc.WithDecoder(jsonDec), cc.WithFormats("toml")}, ops: []string{"WithFormats"}, contains: []string{"toml"}},
		{name: "a format listed twice", opts: []cc.Option{scope, cc.WithDecoder(yamlDec), cc.WithFormats("yaml", "yml")}, ops: []string{"WithFormats"}},
		{name: "empty WithFormats", opts: []cc.Option{scope, cc.WithFormats()}, ops: []string{"WithFormats"}},
		{name: "empty WithDefaultFormats", opts: []cc.Option{scope, cc.WithDefaultFormats()}, ops: []string{"WithDefaultFormats"}},
		{name: "a default format listed twice", opts: []cc.Option{scope, cc.WithDefaultFormats("toml", ".toml")}, ops: []string{"WithDefaultFormats"}},
		{
			name: "an extension claimed twice",
			opts: []cc.Option{scope, cc.WithDecoder(yamlDec), cc.WithDecoder(metaDecoder{"other", []string{"yml"}})},
			ops:  []string{"WithDecoder"}, contains: []string{"yml"},
		},
		{name: "a nil decoder", opts: []cc.Option{scope, cc.WithDecoder(nil)}, ops: []string{"WithDecoder"}},
		{name: "an unknown conflict policy", opts: []cc.Option{scope, cc.WithFormatConflict(cc.FormatConflict(9))}, ops: []string{"WithFormatConflict"}},
	})
}

// DL-102: merge options that can only be mistakes.
func TestMergeOptionErrors(t *testing.T) {
	fn := func(cc.Path, any, any) (any, error) { return nil, nil }
	runOptionErrorCases(t, []optionErrorCase{
		{name: "a bad path", opts: []cc.Option{scope, cc.WithMergeRule("a..b", cc.Replace)}, ops: []string{"WithMergeRule"}},
		{name: "a rule on the root", opts: []cc.Option{scope, cc.WithMergeRule("", cc.Replace)}, ops: []string{"WithMergeRule"}},
		{name: "an unknown strategy", opts: []cc.Option{scope, cc.WithMergeRule("a", cc.MergeStrategy(0))}, ops: []string{"WithMergeRule"}},
		{name: "a bad function path", opts: []cc.Option{scope, cc.WithMergeFunc(".a", fn)}, ops: []string{"WithMergeFunc"}},
		{name: "a nil function", opts: []cc.Option{scope, cc.WithMergeFunc("a", nil)}, ops: []string{"WithMergeFunc"}},
		{
			name: "ambiguous rules",
			opts: []cc.Option{scope, cc.WithMergeRule("a.*", cc.Append), cc.WithMergeRule("*.b", cc.Prepend)},
			ops:  []string{"WithMergeRule"}, contains: []string{"a.*", "*.b"},
		},
		{name: "DeepMerge for arrays", opts: []cc.Option{scope, cc.WithArrayMerge(cc.DeepMerge)}, ops: []string{"WithArrayMerge"}},
		{name: "an unknown array strategy", opts: []cc.Option{scope, cc.WithArrayMerge(cc.MergeStrategy(9))}, ops: []string{"WithArrayMerge"}},
		{name: "an empty unset key", opts: []cc.Option{scope, cc.WithUnsetKey("")}, ops: []string{"WithUnsetKey"}},
	})
}

// DL-103: a Values tree must hold only canonical values. The error names the key.
func TestValuesErrors(t *testing.T) {
	values := func(tree cc.Tree) cc.Option {
		return cc.Options(cc.WithScope("flags", cc.Values(tree)), cc.WithScopes("flags"))
	}
	runOptionErrorCases(t, []optionErrorCase{
		{name: "an int", opts: []cc.Option{values(cc.Tree{"port": 8080})}, ops: []string{"Values"}, contains: []string{"port"}},
		{
			name: "nested", opts: []cc.Option{values(cc.Tree{"a": map[string]any{"b": []any{int32(1)}}})},
			ops: []string{"Values"}, contains: []string{"a.b"},
		},
	})
	if _, err := cc.New("myapp", values(cc.Tree{"port": int64(8080), "list": []any{"x", nil}, "t": map[string]any{}})); err != nil {
		t.Errorf("a canonical Values tree: %v", err)
	}
}

// DL-98: binding options that can only be mistakes.
func TestBindingErrors(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "turning off an unknown key", opts: []cc.Option{scope, cc.WithoutBinding("CONFIG_FORMAT")}, ops: []string{"WithoutBinding"}, contains: []string{"CONFIG_FORMAT"}},
		{name: "an empty key", opts: []cc.Option{scope, cc.WithBinding("", "X")}, ops: []string{"WithBinding"}},
		{name: "a bad key", opts: []cc.Option{scope, cc.WithBinding("A-B", "")}, ops: []string{"WithBinding"}},
		{name: "a leading digit", opts: []cc.Option{scope, cc.WithBinding(cc.BindEnv, "1ABC")}, ops: []string{"WithBinding"}},
		{name: "a dash", opts: []cc.Option{scope, cc.WithBinding(cc.BindEnv, "A-B")}, ops: []string{"WithBinding"}},
		{name: "a space", opts: []cc.Option{scope, cc.WithBinding(cc.BindEnv, "A B")}, ops: []string{"WithBinding"}},
		{name: "an equals sign", opts: []cc.Option{scope, cc.WithBinding(cc.BindEnv, "A=B")}, ops: []string{"WithBinding"}},
		{
			name: "two bindings, one variable",
			opts: []cc.Option{scope, cc.WithBinding(cc.BindEnv, "MYAPP_CONFIG_DIR"), cc.WithBinding(cc.BindConfigDir, "")},
			ops:  []string{"WithBinding"}, contains: []string{"MYAPP_CONFIG_DIR"},
		},
	})
}

// api.md Option rules: an error's Op is the option's name, and the message follows the documented form.
func TestOptionErrorMessage(t *testing.T) {
	_, err := cc.New("myapp", scope, cc.WithDecoder(jsonDec), cc.WithFormats("xml"))
	if err == nil || !strings.HasPrefix(err.Error(), `cascadingconfig: WithFormats: `) || !strings.Contains(err.Error(), `"xml"`) {
		t.Errorf("error = %v, want it to start \"cascadingconfig: WithFormats: \" and name \"xml\"", err)
	}
	if cc.CodeOf(err) != cc.CodeBadRequest {
		t.Errorf("CodeOf = %d, want 400", cc.CodeOf(err))
	}
}

// DL-94: layouts are usable from outside the package.
func TestLayoutsFromOutside(t *testing.T) {
	for _, layout := range []cc.Preset{cc.MiseLayout, cc.MiseHardenedLayout, cc.CosmiconfigLayout} {
		if _, err := cc.NewWithPreset("myapp", layout, scope, cc.WithDecoder(tomlDec), cc.WithDecoder(yamlDec)); err != nil {
			t.Errorf("%s: %v", layout.Name(), err)
		}
	}
}
