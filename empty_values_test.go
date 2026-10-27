package cascadingconfig_test

import (
	"context"
	"errors"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// treeScope is a custom scope that returns one tree source.
type treeScope struct{ tree cc.Tree }

func (s treeScope) Discover(context.Context, cc.Discovery) ([]cc.Source, error) {
	return []cc.Source{{Tree: s.tree}}, nil
}

// layerOf returns the report's layer for scope, failing the test if there isn't exactly one.
func layerOf(t *testing.T, res *cc.Result, scope cc.ScopeName) cc.Layer {
	t.Helper()
	var found []cc.Layer
	for _, l := range res.Describe().Layers {
		if l.Scope == scope {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d layers for %s, want 1: %v", len(found), scope, res.Describe().Layers)
	}
	return found[0]
}

// DL-264: a value layer with no keys is LayerEmpty, and one with keys LayerLoaded, for every kind of value layer.
func TestEmptyValueLayers(t *testing.T) {
	t.Parallel()
	empty := func(context.Context) (cc.Tree, error) { return cc.Tree{}, nil }
	full := func(context.Context) (cc.Tree, error) { return cc.Tree{"a": "1"}, nil }
	type defaults struct {
		A string `koanf:"a"`
	}
	for _, c := range []struct {
		name  string
		scope cc.ScopeName
		opts  []cc.Option
		env   map[string]string
		want  cc.LayerStatus
	}{
		{"EnvVars, no variable", "s", []cc.Option{cc.WithScope("s", cc.EnvVars("MYAPP_"))}, nil, cc.LayerEmpty},
		{"EnvVars, a variable", "s", []cc.Option{cc.WithScope("s", cc.EnvVars("MYAPP_"))}, map[string]string{"MYAPP_A": "1"}, cc.LayerLoaded},
		{"Values, empty", "s", []cc.Option{cc.WithScope("s", cc.Values(cc.Tree{}))}, nil, cc.LayerEmpty},
		{"Values, a key", "s", []cc.Option{cc.WithScope("s", cc.Values(cc.Tree{"a": "1"}))}, nil, cc.LayerLoaded},
		{"ValuesFunc, empty", "s", []cc.Option{cc.WithScope("s", cc.ValuesFunc(empty))}, nil, cc.LayerEmpty},
		{"ValuesFunc, a key", "s", []cc.Option{cc.WithScope("s", cc.ValuesFunc(full))}, nil, cc.LayerLoaded},
		{"custom tree, empty", "s", []cc.Option{cc.WithScope("s", cc.Custom(treeScope{cc.Tree{}}))}, nil, cc.LayerEmpty},
		{"custom tree, a key", "s", []cc.Option{cc.WithScope("s", cc.Custom(treeScope{cc.Tree{"a": "1"}}))}, nil, cc.LayerLoaded},
		{"defaults, all zero", cc.ScopeDefaults, []cc.Option{cc.WithDefaults(defaults{})}, nil, cc.LayerEmpty},
		{"defaults, a value", cc.ScopeDefaults, []cc.Option{cc.WithDefaults(defaults{A: "1"})}, nil, cc.LayerLoaded},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := c.opts
			if c.scope != cc.ScopeDefaults {
				opts = append(opts, cc.WithScopes(c.scope))
			}
			res, err := newLoaderFS(t, cctest.NewFS(nil), c.env, opts...).Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got := layerOf(t, res, c.scope).Status; got != c.want {
				t.Errorf("status %v, want %v", got, c.want)
			}
		})
	}
}

// DL-264: an empty value layer keeps its warnings.
func TestEmptyValueLayerKeepsWarnings(t *testing.T) {
	t.Parallel()
	res, err := skippedEnvVar(t).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	l := layerOf(t, res, "env")
	if l.Status != cc.LayerEmpty || len(l.Warnings) != 1 || l.Warnings[0].Code != cc.WarnEnvVarSkipped {
		t.Errorf("layer %v %v, want empty with one %s warning", l.Status, l.Warnings, cc.WarnEnvVarSkipped)
	}
}

// DL-264: empty value layers still don't satisfy RequireAny.
func TestEmptyValueLayersDontMeetRequirements(t *testing.T) {
	t.Parallel()
	_, err := newLoaderFS(t, cctest.NewFS(nil), nil, cc.WithScope("s", cc.EnvVars("MYAPP_")),
		cc.WithScope("v", cc.Values(cc.Tree{})), cc.WithScopes("s", "v"), cc.RequireConfig(cc.RequireAny)).Load(t.Context())
	var nf *cc.NotFoundError
	if !errors.As(err, &nf) {
		t.Errorf("Load: %v, want a *NotFoundError", err)
	}
}
