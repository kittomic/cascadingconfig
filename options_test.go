package cascadingconfig

import (
	"reflect"
	"testing"
)

// The option tests that need to see an effect with no public output yet, such as the selected scopes in order, read
// the loader's private config (DL-96). Errors are tested from outside the package, in options_errors_test.go.

// oneScope defines and selects a scope, so a loader passes the "no scopes" check.
var oneScope = Options(WithScope("only", Values(Tree{})), WithScopes("only"))

// defineScopes defines Values scopes in order and selects them all.
func defineScopes(names ...ScopeName) Option {
	opts := make([]Option, 0, len(names)+1)
	for _, n := range names {
		opts = append(opts, WithScope(n, Values(Tree{})))
	}
	return Options(append(opts, WithScopes(names...))...)
}

func cfgOf(t *testing.T, name string, preset Preset, opts ...Option) *config {
	t.Helper()
	l, err := NewWithPreset(name, preset, opts...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	if l == nil || l.cfg == nil {
		t.Fatalf("NewWithPreset returned no loader")
	}
	return l.cfg
}

func wantScopes(t *testing.T, c *config, want ...ScopeName) {
	t.Helper()
	if got := c.scopeOrder(); !reflect.DeepEqual(got, want) {
		t.Errorf("selected scopes = %q, want %q", got, want)
	}
}

// DL-29, api.md Preset and Extend: options run in order, so WithScopes then WithoutScopes selects what's left, and the
// reverse selects everything.
func TestOptionsAppliedInOrder(t *testing.T) {
	c := cfgOf(t, "app", Preset{}, defineScopes("a", "b", "c"), WithoutScopes("b"))
	wantScopes(t, c, "a", "c")

	c = cfgOf(t, "app", Preset{}, defineScopes("a", "b", "c"), WithoutScopes("b"), WithScopes("a", "b", "c"))
	wantScopes(t, c, "a", "b", "c")
}

// design.md Choosing scopes: selecting doesn't reorder; precedence stays as the scopes were defined. Removing a scope
// that isn't selected does nothing. A defined scope is left out once WithoutScopes removes it (DL-261).
func TestScopeSelection(t *testing.T) {
	c := cfgOf(t, "app", Preset{}, defineScopes("a", "b", "c"), WithScopes("c", "a"))
	wantScopes(t, c, "a", "c")

	c = cfgOf(t, "app", Preset{}, defineScopes("a", "b", "c"), WithScopes("a", "c"), WithoutScopes("b"))
	wantScopes(t, c, "a", "c")

	c = cfgOf(t, "app", Preset{}, defineScopes("a"), WithScope("unselected", Values(Tree{})), WithoutScopes("unselected"))
	wantScopes(t, c, "a")
}

// DL-100, design.md Custom scopes: After(x) inserts directly above x and Before(x) directly below it; a later scope
// placed After(x) sits between x and earlier ones; with neither, a scope goes above every scope defined so far.
func TestCustomScopePlacement(t *testing.T) {
	all := []ScopeName{"a", "b", "c", "x", "y", "z", "w"}
	c := cfgOf(t, "app", Preset{},
		WithScope("a", Values(Tree{})),
		WithScope("b", Values(Tree{})),
		WithScope("c", Values(Tree{})),
		WithScope("x", Values(Tree{}), After("a")),
		WithScope("y", Values(Tree{}), After("a")),
		WithScope("z", Values(Tree{}), Before("a")),
		WithScope("w", Values(Tree{}), Before("c")),
		WithScopes(all...),
	)
	wantScopes(t, c, "z", "a", "y", "x", "b", "w", "c")
}

// DL-103, design.md Custom scopes: Values scopes default to TrustUser, and Trust overrides it.
func TestScopeTrust(t *testing.T) {
	c := cfgOf(t, "app", Preset{},
		WithScope("flags", Values(Tree{})),
		WithScope("defaults", Values(Tree{}), Trust(TrustSystem)),
		WithScopes("flags", "defaults"),
	)
	for name, want := range map[ScopeName]TrustLevel{"flags": TrustUser, "defaults": TrustSystem} {
		if got, ok := c.scopeTrust(name); !ok || got != want {
			t.Errorf("trust of %s = %v, %v; want %v", name, got, ok, want)
		}
	}
}

// DL-103: Values copies its tree when called, so changing the tree afterwards has no effect.
func TestValuesCopiesTree(t *testing.T) {
	tree := Tree{"server": map[string]any{"port": int64(1)}}
	kind := Values(tree)
	tree["server"].(map[string]any)["port"] = int64(2)
	tree["new"] = true

	c := cfgOf(t, "app", Preset{}, WithScope("flags", kind), WithScopes("flags"))
	vk, ok := c.scopeKind("flags").(valuesKind)
	if !ok {
		t.Fatalf("scope kind = %#v, want a Values kind", c.scopeKind("flags"))
	}
	if want := (Tree{"server": map[string]any{"port": int64(1)}}); !reflect.DeepEqual(vk.tree, want) {
		t.Errorf("Values tree = %#v, want %#v", vk.tree, want)
	}
}

// api.md Preset and Extend: Extend never changes its receiver, and two extensions of one preset don't share options.
func TestExtendDoesNotChangeReceiver(t *testing.T) {
	p := NewPreset("p", defineScopes("a", "b", "c"))
	withoutA := p.Extend(WithoutScopes("a"))
	withoutB := p.Extend(WithoutScopes("b"))

	wantScopes(t, cfgOf(t, "app", p), "a", "b", "c")
	wantScopes(t, cfgOf(t, "app", withoutA), "b", "c")
	wantScopes(t, cfgOf(t, "app", withoutB), "a", "c")
	wantScopes(t, cfgOf(t, "app", withoutA.Extend(WithoutScopes("c"))), "b")
}

// summary is the part of a config the equivalence tests compare.
func summary(c *config) any {
	return []any{c.name, c.envName, c.presets, c.scopeOrder(), c.formatOrder(), c.bindingNames(), c.envs, c.conflict}
}

// api.md New and NewWithPreset: NewWithPreset(n, p, a) behaves exactly like NewWithPreset(n, p.Extend(a)), and a zero
// Preset is the same as New.
func TestNewWithPresetEquivalence(t *testing.T) {
	p := NewPreset("p", defineScopes("a", "b"), WithBinding(BindEnv, ""), WithDecoder(tomlDecoder))
	extra := []Option{WithoutScopes("a"), WithEnvs("dev"), WithFormats("toml")}

	direct := summary(cfgOf(t, "app", p, extra...))
	extended := summary(cfgOf(t, "app", p.Extend(extra...)))
	if !reflect.DeepEqual(direct, extended) {
		t.Errorf("NewWithPreset(n, p, a) = %v\nNewWithPreset(n, p.Extend(a)) = %v", direct, extended)
	}

	l, err := New("app", oneScope)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := summary(l.cfg), summary(cfgOf(t, "app", Preset{}, oneScope)); !reflect.DeepEqual(got, want) {
		t.Errorf("New = %v, NewWithPreset with a zero Preset = %v", got, want)
	}
}

// DL-97: NewPreset names a preset; Extend keeps the name; ExtendAs names an extension and records its base, nearest
// first; a zero Preset has no name.
func TestPresetNames(t *testing.T) {
	base := NewPreset("base", oneScope)
	child := base.ExtendAs("child")
	grandchild := child.ExtendAs("grandchild")

	tests := []struct {
		preset  Preset
		name    string
		lineage []string
	}{
		{base, "base", []string{"base"}},
		{base.Extend(WithEnvs("dev")), "base", []string{"base"}},
		{child, "child", []string{"child", "base"}},
		{child.Extend(), "child", []string{"child", "base"}},
		{grandchild, "grandchild", []string{"grandchild", "child", "base"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.preset.Name(); got != tc.name {
				t.Errorf("Name() = %q, want %q", got, tc.name)
			}
			if got := cfgOf(t, "app", tc.preset).presets; !reflect.DeepEqual(got, tc.lineage) {
				t.Errorf("presets = %q, want %q", got, tc.lineage)
			}
		})
	}

	if name := (Preset{}).Name(); name != "" {
		t.Errorf("zero Preset Name() = %q, want empty", name)
	}
	if got := cfgOf(t, "app", Preset{}, oneScope).presets; len(got) != 0 {
		t.Errorf("presets without a preset = %q, want none", got)
	}
}

// Options bundles options, which run in place, in order.
func TestOptionsGroup(t *testing.T) {
	group := Options(WithScopes("a", "b"), WithoutScopes("a"))
	c := cfgOf(t, "app", Preset{}, defineScopes("a", "b", "c"), group)
	wantScopes(t, c, "b")
}

// DL-73, DL-99: WithEnvs and WithConfigFiles with no arguments, or only empty strings, mean not set, so the env vars
// stay in charge. As With…s options, a later call replaces an earlier one, including with not set.
func TestEmptyEnvsAndConfigFiles(t *testing.T) {
	tests := []struct {
		name  string
		opts  []Option
		envs  []string
		files []string
	}{
		{"not called", nil, nil, nil},
		{"no arguments", []Option{WithEnvs(), WithConfigFiles()}, nil, nil},
		{"only empty strings", []Option{WithEnvs("", ""), WithConfigFiles("")}, nil, nil},
		{"empty entries dropped", []Option{WithEnvs("", "dev"), WithConfigFiles("a.toml", "")}, []string{"dev"}, []string{"a.toml"}},
		{"replaced by not set", []Option{WithEnvs("dev"), WithEnvs(), WithConfigFiles("a.toml"), WithConfigFiles("")}, nil, nil},
		{"replaced", []Option{WithEnvs("dev"), WithEnvs("ci", "dev.linux")}, []string{"ci", "dev.linux"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cfgOf(t, "app", Preset{}, append([]Option{oneScope}, tc.opts...)...)
			if !reflect.DeepEqual(c.envs, tc.envs) || !reflect.DeepEqual(c.configFiles, tc.files) {
				t.Errorf("envs, configFiles = %q, %q; want %q, %q", c.envs, c.configFiles, tc.envs, tc.files)
			}
		})
	}
}

// DL-101, DL-105, design.md File format precedence: the format order that is searched.
func TestFormatOrder(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want []string
	}{
		{"registration order by default", []Option{WithDecoder(tomlDecoder), WithDecoder(jsonDecoder)}, []string{"toml", "json"}},
		{
			"WithFormats", []Option{WithDecoder(jsonDecoder), WithDecoder(tomlDecoder), WithFormats("toml", "json")},
			[]string{"toml", "json"},
		},
		{"WithFormats only enables what it lists", []Option{WithDecoder(jsonDecoder), WithDecoder(tomlDecoder), WithFormats("json")}, []string{"json"}},
		{
			"the env var's syntax", []Option{WithDecoder(jsonDecoder), WithDecoder(yamlDecoder), WithFormats(" yml", ".json ")},
			[]string{"yaml", "json"},
		},
		{
			"decoders may come later", []Option{WithFormats("json", "toml"), WithDecoder(tomlDecoder), WithDecoder(jsonDecoder)},
			[]string{"json", "toml"},
		},
		{
			"WithDefaultFormats skips formats with no decoder",
			[]Option{WithDecoder(jsonDecoder), WithDefaultFormats("toml", "yaml", "json")}, []string{"json"},
		},
		{
			"WithFormats replaces WithDefaultFormats, whatever their order",
			[]Option{WithDecoder(jsonDecoder), WithDecoder(tomlDecoder), WithFormats("json"), WithDefaultFormats("toml", "json")},
			[]string{"json"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cfgOf(t, "app", Preset{}, append([]Option{oneScope}, tc.opts...)...)
			if got := c.formatOrder(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("formatOrder() = %q, want %q", got, tc.want)
			}
		})
	}
}

// DL-101: WithDecoder for a format that already has a decoder replaces it.
func TestWithDecoderReplaces(t *testing.T) {
	replacement := testDecoder{"json", []string{"json", "json5"}}
	c := cfgOf(t, "app", Preset{}, oneScope, WithDecoder(jsonDecoder), WithDecoder(replacement))
	if got := c.decoder("json"); !reflect.DeepEqual(got, replacement) {
		t.Errorf("decoder(json) = %#v, want the replacement", got)
	}
	if got := c.formatOrder(); !reflect.DeepEqual(got, []string{"json"}) {
		t.Errorf("formatOrder() = %q, want [json]", got)
	}
}

// api.md A loader without a preset: the engine defaults.
func TestEngineDefaults(t *testing.T) {
	c := cfgOf(t, "app", Preset{}, oneScope)
	if c.conflict != ConflictMerge {
		t.Errorf("conflict = %v, want ConflictMerge", c.conflict)
	}
	if c.merge.arrays != Replace || c.merge.unsetKey != "_unset" || c.merge.rules != nil {
		t.Errorf("merge = %+v, want arrays replaced, _unset, and no rules", c.merge)
	}
	if got := c.bindingNames(); len(got) != 0 {
		t.Errorf("bindings = %v, want none", got)
	}
	if got := c.formatOrder(); len(got) != 0 {
		t.Errorf("formats = %q, want none", got)
	}
	if c.name != "app" || c.envName != "APP" {
		t.Errorf("name, envName = %q, %q; want app, APP", c.name, c.envName)
	}

	c = cfgOf(t, "app", Preset{}, oneScope, WithFormatConflict(ConflictFirst))
	if c.conflict != ConflictFirst {
		t.Errorf("conflict = %v, want ConflictFirst", c.conflict)
	}
}

// DL-102: the merge options set up M2's engine. Rules take either path form, and a later rule for the same path
// replaces an earlier one.
func TestMergeOptions(t *testing.T) {
	fn := func(Path, any, any) (any, error) { return "func", nil }
	c := cfgOf(t, "app", Preset{}, oneScope,
		WithMergeRule("tools.*", Replace),
		WithMergeRule(Path{"plugins", "foo.bar"}, Append),
		WithMergeRule("list", Append),
		WithMergeRule(Path{"list"}, Prepend),
		WithMergeFunc("f", fn),
		WithArrayMerge(Append),
	)
	res, err := merge(c.merge, ly(t,
		"low.json", `{"tools": {"node": {"v": 1, "x": 1}}, "plugins": {"foo.bar": [1]}, "list": [1], "f": 1, "other": [1]}`,
		"high.json", `{"tools": {"node": {"v": 2}}, "plugins": {"foo.bar": [2]}, "list": [2], "f": 2, "other": [2]}`,
	))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	wantTree(t, res, `{
		"tools": {"node": {"v": 2}}, "plugins": {"foo.bar": [1, 2]}, "list": [2, 1], "f": "func", "other": [1, 2]
	}`)
}

// DL-102: WithUnsetKey and WithoutUnset are one setting, so the last one wins.
func TestUnsetOptions(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"renamed", []Option{WithUnsetKey("$unset")}, "$unset"},
		{"off", []Option{WithoutUnsetKey()}, ""},
		{"off, then renamed", []Option{WithoutUnsetKey(), WithUnsetKey("$unset")}, "$unset"},
		{"renamed, then off", []Option{WithUnsetKey("$unset"), WithoutUnsetKey()}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cfgOf(t, "app", Preset{}, append([]Option{oneScope}, tc.opts...)...)
			if c.merge.unsetKey != tc.want {
				t.Errorf("unset key = %q, want %q", c.merge.unsetKey, tc.want)
			}
		})
	}
}

// DL-15, DL-98, design.md Environment variables: {NAME} is the app name in upper case, with every character outside
// [A-Z0-9] replaced by _, and a binding's default variable is {NAME}_<suffix>.
func TestBindingNameDerivation(t *testing.T) {
	for app, want := range map[string]string{
		"myapp":  "MYAPP_ENV",
		"my-app": "MY_APP_ENV",
		"my.app": "MY_APP_ENV",
		"myApp2": "MYAPP2_ENV",
		"é-x":    "__X_ENV",
	} {
		t.Run(app, func(t *testing.T) {
			c := cfgOf(t, app, Preset{}, oneScope, WithBinding(BindEnv, ""))
			if got := c.bindingNames()[BindEnv]; got != want {
				t.Errorf("ENV binding = %q, want %q", got, want)
			}
		})
	}
}

// DL-98: bindings can be declared, renamed, renamed back to the default and turned off. Turning off a built-in binding
// that isn't declared does nothing. A custom key gets the same default name.
func TestBindings(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want map[Binding]string
	}{
		{"declared", []Option{WithBinding(BindEnv, "")}, map[Binding]string{BindEnv: "MYAPP_ENV"}},
		{"declared with a name", []Option{WithBinding(BindEnv, "MYAPP_CONFIG_ENV")}, map[Binding]string{BindEnv: "MYAPP_CONFIG_ENV"}},
		{
			"renamed", []Option{WithBinding(BindEnv, ""), WithBinding(BindEnv, "OTHER")},
			map[Binding]string{BindEnv: "OTHER"},
		},
		{
			"renamed back", []Option{WithBinding(BindEnv, "OTHER"), WithBinding(BindEnv, "")},
			map[Binding]string{BindEnv: "MYAPP_ENV"},
		},
		{
			"turned off", []Option{WithBinding(BindEnv, ""), WithBinding(BindConfigDir, ""), WithoutBinding(BindEnv)},
			map[Binding]string{BindConfigDir: "MYAPP_CONFIG_DIR"},
		},
		{"turning off one not declared", []Option{WithoutBinding(BindConfigFormats)}, map[Binding]string{}},
		{"a custom key", []Option{WithBinding("MOUNTED_DIR", "")}, map[Binding]string{"MOUNTED_DIR": "MYAPP_MOUNTED_DIR"}},
		{
			"a custom key turned off", []Option{WithBinding("MOUNTED_DIR", ""), WithoutBinding("MOUNTED_DIR")},
			map[Binding]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cfgOf(t, "myapp", Preset{}, append([]Option{oneScope}, tc.opts...)...)
			got := c.bindingNames()
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("bindings = %v, want %v", got, tc.want)
			}
		})
	}
}

// DL-71, DL-94: the layouts are built only from exported identifiers. The one exception is the private JSON decoder,
// a value passed to the exported WithDecoder (DL-95).
func TestLayoutsUseOnlyExportedIdentifiers(t *testing.T) {
	for _, ident := range unexportedIdents(t, "layouts.go") {
		if ident != "builtinJSONDecoder" {
			t.Errorf("layouts.go uses the unexported identifier %s", ident)
		}
	}
}
