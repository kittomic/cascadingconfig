package cascadingconfig_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// recordLayers is a layer transform that records each layer it sees and returns the tree unchanged.
func recordLayers(seen *calls[cc.Layer]) cc.Option {
	return cc.WithLayerTransform(func(_ context.Context, l cc.Layer, t cc.Tree) (cc.Tree, error) {
		seen.add(l)
		return t, nil
	})
}

func seenPaths(seen *calls[cc.Layer]) []string {
	var out []string
	for _, l := range seen.all() {
		p := l.Path
		if p == "" {
			p = "(" + string(l.Scope) + ")"
		}
		out = append(out, p)
	}
	return out
}

// DL-36, design.md Transform hooks: layer transforms run in registration order, on each layer, with its metadata.
func TestLayerTransformOrder(t *testing.T) {
	t.Parallel()
	appendTo := func(s string) cc.Option {
		return cc.WithLayerTransform(func(_ context.Context, _ cc.Layer, t cc.Tree) (cc.Tree, error) {
			v, _ := t["order"].(string)
			t["order"] = v + s
			return t, nil
		})
	}
	res := incOK(t, map[string]string{rootCfg: `{}`}, appendTo("1"), appendTo("2"))
	wantTreeJSON(t, res.Tree(), `{"order": "12"}`)
}

// DL-36, DL-193: a layer transform gets the layer's own config root, and runs on included layers, value scopes and
// defaults; never on bootstrap files.
func TestLayerTransformCoverage(t *testing.T) {
	t.Parallel()
	var seen calls[cc.Layer]
	files := map[string]string{
		rootCfg:                 `{"include": "base.json"}`,
		work + "/base.json":     `{}`,
		sysFile:                 `{}`,
		work + "/.myapprc.json": `{"env": ["dev"]}`,
	}
	res, err := newLoader(t, files, nil,
		cc.WithScope("sys", cc.FixedDir("/etc/myapp", cc.Pattern{Template: "config.json"})),
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.json"}), cc.ConfigRoot("/home/me"),
			cc.BootstrapFiles(cc.Pattern{Template: ".myapprc.json"})),
		cc.WithBootstrapKeys(cc.BootstrapEnv),
		cc.WithScope("v", cc.Values(cc.Tree{"v": int64(1)})),
		cc.WithDefaults(map[string]any{"d": "x"}),
		cc.WithScopes("sys", "proj", "v"),
		cc.WithIncludeKey("include", cc.IncludeFlat),
		recordLayers(&seen),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := seenPaths(&seen)
	for _, want := range []string{"(defaults)", sysFile, work + "/base.json", rootCfg, "(v)"} {
		if !slices.Contains(got, want) {
			t.Errorf("transformed %v, want %s among them", got, want)
		}
	}
	if slices.Contains(got, work+"/.myapprc.json") {
		t.Errorf("transformed the bootstrap file: %v", got)
	}
	for _, l := range seen.all() {
		if (l.Path == rootCfg || l.Path == work+"/base.json") && l.ConfigRoot != "/home/me" {
			t.Errorf("%s: config root %q, want /home/me", l.Path, l.ConfigRoot)
		}
		if l.Path == sysFile && l.ConfigRoot != "/etc/myapp" {
			t.Errorf("%s: config root %q, want /etc/myapp", l.Path, l.ConfigRoot)
		}
	}
	wantTreeJSON(t, res.Tree(), `{"d": "x", "v": 1}`)
}

// DL-186: a layer transform sees the include key before it's read, so it can expand variables in references.
func TestLayerTransformBeforeIncludes(t *testing.T) {
	t.Parallel()
	expand := cc.WithLayerTransform(func(_ context.Context, _ cc.Layer, t cc.Tree) (cc.Tree, error) {
		if ref, ok := t["include"].(string); ok {
			t["include"] = strings.ReplaceAll(ref, "${PROFILE}", "prod")
		}
		return t, nil
	})
	res := incOK(t, map[string]string{rootCfg: `{"include": "${PROFILE}.json"}`, work + "/prod.json": `{"p": 1}`}, expand)
	wantTreeJSON(t, res.Tree(), `{"p": 1}`)
}

// DL-193: nil from a layer transform merges nothing, and the layer stays loaded.
func TestLayerTransformNil(t *testing.T) {
	t.Parallel()
	drop := cc.WithLayerTransform(func(_ context.Context, l cc.Layer, t cc.Tree) (cc.Tree, error) {
		if l.Path == rootCfg {
			return nil, nil
		}
		return t, nil
	})
	res := incOK(t, map[string]string{sysFile: `{"a": 1}`, rootCfg: `{"b": 2}`}, drop)
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	if l := layerAt(t, res.Describe(), rootCfg); l.Status != cc.LayerLoaded {
		t.Errorf("layer = %+v, want LayerLoaded", l)
	}
}

// DL-36, DL-193: a layer transform's error, or a tree with a non-canonical value, is a *TransformError (422) naming
// the file.
func TestLayerTransformErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("bad template")
	t.Run("error", func(t *testing.T) {
		_, err := incFails(t, map[string]string{rootCfg: `{}`}, cc.WithLayerTransform(
			func(context.Context, cc.Layer, cc.Tree) (cc.Tree, error) { return nil, boom }))
		wantErr[*cc.TransformError](t, err, cc.CodeUnprocessable, rootCfg)
		if !errors.Is(err, boom) || !errors.Is(err, cc.ErrTransform) {
			t.Errorf("error = %v, want ErrTransform wrapping the transform's error", err)
		}
	})
	t.Run("non-canonical", func(t *testing.T) {
		_, err := incFails(t, map[string]string{rootCfg: `{}`}, cc.WithLayerTransform(
			func(_ context.Context, _ cc.Layer, t cc.Tree) (cc.Tree, error) {
				t["n"] = 1 // an int, not an int64
				return t, nil
			}))
		wantErr[*cc.TransformError](t, err, cc.CodeUnprocessable, rootCfg)
	})
}

// DL-76, DL-193: transforms run only on layers that merge: never on ignored, untrusted, duplicate or empty layers.
func TestLayerTransformSkipped(t *testing.T) {
	t.Parallel()
	var seen calls[cc.Layer]
	files := map[string]string{
		sysFile:                 `{}`,
		rootCfg:                 `{"include": "/etc/myapp/config.json"}`,
		"/srv/a.json":           `{}`,
		"/srv/a.toml":           `{}`,
		"/srv/empty.json":       "",
		"/srv/untrusted/x.json": `{}`,
	}
	_, err := newLoader(t, files, nil, incScopes,
		cc.WithDecoder(jsonNamed{"toml", []string{"toml"}}),
		cc.WithFormatConflict(cc.ConflictFirst),
		cc.WithScope("srv", cc.FixedDir("/srv", cc.Pattern{Template: "a.{ext}"}, cc.Pattern{Template: "empty.json"})),
		cc.WithScope("u", cc.FixedDir("/srv/untrusted", cc.Pattern{Template: "x.json"})),
		cc.WithScopePolicy("u", cc.InheritDefault(), cc.RequireTrust(&testStore{})),
		cc.WithScopes("sys", "srv", "u", "proj"),
		recordLayers(&seen),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := seenPaths(&seen)
	if n := countOf(got, sysFile); n != 1 {
		t.Errorf("transformed %s %d times, want once: %v", sysFile, n, got)
	}
	if n := countOf(got, "/srv/a.json") + countOf(got, "/srv/a.toml"); n != 1 {
		t.Errorf("transformed both files of a ConflictFirst slot: %v", got)
	}
	for _, p := range []string{"/srv/empty.json", "/srv/untrusted/x.json"} {
		if slices.Contains(got, p) {
			t.Errorf("transformed %s: %v", p, got)
		}
	}
}

func countOf(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

// DL-36, DL-193, design.md Transform hooks: the result transform runs once, on the merged result. nil is an empty
// result. OriginOf describes the merge: a key the transform adds has no origin, and the others keep theirs.
func TestResultTransform(t *testing.T) {
	t.Parallel()
	n := 0
	add := cc.WithResultTransform(func(_ context.Context, t cc.Tree) (cc.Tree, error) {
		n++
		t["added"] = "x"
		return t, nil
	})
	res := incOK(t, map[string]string{rootCfg: `{"a": 1}`}, add)
	if n != 1 {
		t.Errorf("the result transform ran %d times, want once", n)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1, "added": "x"}`)
	if _, ok := cc.OriginOf(res, "added"); ok {
		t.Errorf("OriginOf(added) is set, want none")
	}
	if o, ok := cc.OriginOf(res, "a"); !ok || o.Layer.Path != rootCfg {
		t.Errorf("OriginOf(a) = %+v, %v; want the project file", o, ok)
	}

	res = incOK(t, map[string]string{rootCfg: `{"a": 1}`}, cc.WithResultTransform(
		func(context.Context, cc.Tree) (cc.Tree, error) { return nil, nil }))
	if tree := res.Tree(); tree == nil || len(tree) != 0 {
		t.Errorf("Tree = %#v, want an empty, non-nil tree", tree)
	}
}

// DL-193: the result transform runs before the RequireConfig checks, so a failure in it is the only error a load
// with an unmet requirement reports; and before Decode checks for unknown keys.
func TestResultTransformOrder(t *testing.T) {
	t.Parallel()
	boom := errors.New("invalid config")
	_, err := incFails(t, map[string]string{}, cc.RequireConfig(cc.RequireFile, "proj"),
		cc.WithResultTransform(func(context.Context, cc.Tree) (cc.Tree, error) { return nil, boom }))
	if errs := cc.Errors(err); len(errs) != 1 || !errors.Is(errs[0], cc.ErrTransform) || !errors.Is(errs[0], boom) {
		t.Errorf("errors = %v, want only the transform's", errs)
	}
	wantErr[*cc.TransformError](t, err, cc.CodeUnprocessable, "")

	res := incOK(t, map[string]string{rootCfg: `{"a": 1, "gone": 2}`}, cc.WithResultTransform(
		func(_ context.Context, t cc.Tree) (cc.Tree, error) {
			delete(t, "gone")
			t["extra"] = "x"
			return t, nil
		}))
	var v struct {
		A int `koanf:"a"`
	}
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var keys []string
	for _, w := range warningsWith(res.Describe(), cc.WarnUnknownKey) {
		keys = append(keys, w.Key.String())
	}
	if !slices.Equal(keys, []string{"extra"}) {
		t.Errorf("unknown keys = %v, want only extra", keys)
	}
}

// DL-193: a result transform returning a non-canonical value is a *TransformError.
func TestResultTransformNonCanonical(t *testing.T) {
	t.Parallel()
	_, err := incFails(t, map[string]string{rootCfg: `{}`}, cc.WithResultTransform(
		func(_ context.Context, t cc.Tree) (cc.Tree, error) {
			t["f"] = float32(1)
			return t, nil
		}))
	wantErr[*cc.TransformError](t, err, cc.CodeUnprocessable, "")
}

// DL-76, DL-159, api.md Concurrency and ownership: transforms get their own copies, so changing their
// input doesn't change the report or later loads; and parallel loads with transforms agree under -race.
func TestTransformsOwnTheirInput(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"server": {"host": "h", "ports": [1, 2]}}`})
	mutate := cc.Options(
		cc.WithLayerTransform(func(_ context.Context, _ cc.Layer, t cc.Tree) (cc.Tree, error) {
			out := cc.Tree{"server": map[string]any{"host": "h", "ports": []any{int64(1), int64(2)}}, "layer": true}
			srv := t["server"].(map[string]any)
			srv["host"] = "changed"
			srv["ports"].([]any)[0] = int64(99)
			delete(t, "server")
			return out, nil
		}),
		cc.WithResultTransform(func(_ context.Context, t cc.Tree) (cc.Tree, error) {
			out := cc.Tree{"server": map[string]any{"host": "h", "ports": []any{int64(1), int64(2)}}, "result": true}
			t["server"].(map[string]any)["host"] = "changed again"
			return out, nil
		}),
	)
	l := newLoaderFS(t, fsys, nil, incScopes, mutate)
	want := `{"server": {"host": "h", "ports": [1, 2]}, "result": true}`
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			res, err := l.Load(t.Context())
			if err != nil {
				t.Errorf("Load: %v", err)
				return
			}
			wantTreeJSON(t, res.Tree(), want)
		})
	}
	wg.Wait()
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), want)
}
