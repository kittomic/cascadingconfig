package cascadingconfig

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// unexportedIdents parses file and returns the unexported identifiers its declarations refer to, other than Go's
// predeclared names and imported package names.
func unexportedIdents(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	imports := map[string]bool{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = true
	}
	found := map[string]bool{}
	for _, decl := range f.Decls {
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				ast.Inspect(n.X, func(m ast.Node) bool { // the selected name is in another package or type
					if id, ok := m.(*ast.Ident); ok {
						collect(found, imports, id)
					}
					return true
				})
				return false
			case *ast.Ident:
				collect(found, imports, n)
			}
			return true
		})
	}
	var out []string
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collect(found, imports map[string]bool, id *ast.Ident) {
	name := id.Name
	if name == "_" || ast.IsExported(name) || imports[name] || types.Universe.Lookup(name) != nil {
		return
	}
	found[name] = true
}

// allBindings is mise's binding set (design.md Environment variables), for an app called myapp.
var allBindings = map[Binding]string{
	BindConfigFile:              "MYAPP_CONFIG_FILE",
	BindConfigDir:               "MYAPP_CONFIG_DIR",
	BindGlobalConfigFile:        "MYAPP_GLOBAL_CONFIG_FILE",
	BindSystemConfigDir:         "MYAPP_SYSTEM_CONFIG_DIR",
	BindSystemConfigFile:        "MYAPP_SYSTEM_CONFIG_FILE",
	BindEnv:                     "MYAPP_ENV",
	BindCeilingPaths:            "MYAPP_CEILING_PATHS",
	BindIgnoredConfigPaths:      "MYAPP_IGNORED_CONFIG_PATHS",
	BindOverrideConfigFilenames: "MYAPP_OVERRIDE_CONFIG_FILENAMES",
	BindConfigFormats:           "MYAPP_CONFIG_FORMATS",
}

// DL-94, DL-98, DL-105, design.md: what each layout has. The tests select one Values scope in place of the layout's
// scopes.
func TestLayouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		layout   Preset
		name     string
		lineage  []string
		defaults []string // the default format order with only JSON registered, then with TOML and YAML added
		full     []string
		conflict FormatConflict
		arrays   MergeStrategy
		bindings map[Binding]string
	}{
		{
			MiseLayout, "MiseLayout", []string{"MiseLayout"},
			[]string{"json"}, []string{"toml", "yaml", "json"}, ConflictMerge, Replace, allBindings,
		},
		{
			MiseHardenedLayout, "MiseHardenedLayout", []string{"MiseHardenedLayout", "MiseLayout"},
			[]string{"json"}, []string{"toml", "yaml", "json"}, ConflictMerge, Replace, allBindings,
		},
		{
			CosmiconfigLayout, "CosmiconfigLayout", []string{"CosmiconfigLayout"},
			[]string{"json"}, []string{"json", "yaml"}, ConflictFirst, Append,
			map[Binding]string{BindConfigFile: "MYAPP_CONFIG_FILE", BindConfigFormats: "MYAPP_CONFIG_FORMATS"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.layout.Name(); got != tc.name {
				t.Errorf("Name() = %q, want %q", got, tc.name)
			}
			c := cfgOf(t, "myapp", tc.layout, oneScope)
			if !reflect.DeepEqual(c.presets, tc.lineage) {
				t.Errorf("presets = %q, want %q", c.presets, tc.lineage)
			}
			if got := c.formatOrder(); !reflect.DeepEqual(got, tc.defaults) {
				t.Errorf("formatOrder() = %q, want %q", got, tc.defaults)
			}
			full := cfgOf(t, "myapp", tc.layout, oneScope, WithDecoder(tomlDecoder), WithDecoder(yamlDecoder))
			if got := full.formatOrder(); !reflect.DeepEqual(got, tc.full) {
				t.Errorf("formatOrder() with TOML and YAML = %q, want %q", got, tc.full)
			}
			if c.conflict != tc.conflict || c.merge.arrays != tc.arrays {
				t.Errorf("conflict, arrays = %v, %v; want %v, %v", c.conflict, c.merge.arrays, tc.conflict, tc.arrays)
			}
			if got := c.bindingNames(); !reflect.DeepEqual(got, tc.bindings) {
				t.Errorf("bindings = %v, want %v", got, tc.bindings)
			}
		})
	}
}

// DL-95: every layout registers a JSON decoder that decodes as format/json does.
func TestLayoutsRegisterJSON(t *testing.T) {
	t.Parallel()
	for _, layout := range []Preset{MiseLayout, MiseHardenedLayout, CosmiconfigLayout} {
		t.Run(layout.Name(), func(t *testing.T) {
			d := cfgOf(t, "myapp", layout, oneScope).decoder("json")
			if d == nil {
				t.Fatalf("no JSON decoder registered")
			}
			if !reflect.DeepEqual(d.Extensions(), []string{"json"}) || !reflect.DeepEqual(d.MediaTypes(), []string{"application/json"}) {
				t.Errorf("Extensions, MediaTypes = %q, %q", d.Extensions(), d.MediaTypes())
			}
			doc, err := DecodeDocument(d, "app.json", []byte(`{"a": 1, "b": [1.5, null], "a": "x"}`))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if want := (Tree{"a": "x", "b": []any{1.5, nil}}); !reflect.DeepEqual(doc.Tree, want) {
				t.Errorf("Tree = %#v, want %#v", doc.Tree, want)
			}
			if len(doc.Warnings) != 1 || doc.Warnings[0].Code != WarnDuplicateKey {
				t.Errorf("warnings = %#v, want one duplicate-key warning", doc.Warnings)
			}
			if _, err := DecodeDocument(d, "app.json", []byte(`{"a": 1,}`)); err == nil {
				t.Errorf("a trailing comma decoded, want a *ParseError")
			}
		})
	}
}
