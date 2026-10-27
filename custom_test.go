package cascadingconfig_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// The custom scope tests define one custom scope, "cust", from a function, over a cctest.FS on the test host.

// scopeFunc is a Scope from a function, counting its calls.
type scopeFunc struct {
	discover func(ctx context.Context, d cc.Discovery) ([]cc.Source, error)
	mu       sync.Mutex
	calls    int
}

func (s *scopeFunc) Discover(ctx context.Context, d cc.Discovery) ([]cc.Source, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.discover(ctx, d)
}

// sources is a scope that always returns srcs.
func sources(srcs ...cc.Source) *scopeFunc {
	return &scopeFunc{discover: func(context.Context, cc.Discovery) ([]cc.Source, error) { return srcs, nil }}
}

// custOptions defines and selects cust over s, with scopeOpts, and the include key "include".
func custOptions(s cc.Scope, scopeOpts ...cc.ScopeOption) cc.Option {
	return cc.Options(
		cc.WithScope("cust", cc.Custom(s), scopeOpts...), cc.WithScopes("cust"), cc.WithIncludeKey("include", cc.IncludeFlat),
	)
}

// custLoad loads files with cust over s and opts.
func custLoad(t *testing.T, fsys *cctest.FS, s cc.Scope, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	return newLoaderFS(t, fsys, nil, append([]cc.Option{custOptions(s)}, opts...)...).Load(t.Context())
}

// custOK is custLoad that fails the test on an error.
func custOK(t *testing.T, fsys *cctest.FS, s cc.Scope, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := custLoad(t, fsys, s, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// DL-236: a nil Scope, and BootstrapFiles on a custom scope, are *OptionErrors.
func TestCustomOptionErrors(t *testing.T) {
	_, err := cc.New("myapp", cc.WithDecoder(jsonDecoder()), cc.WithScope("cust", cc.Custom(nil)), cc.WithScopes("cust"))
	wantErr[*cc.OptionError](t, err, cc.CodeBadRequest, "", "Custom")
	_, err = cc.New("myapp", cc.WithDecoder(jsonDecoder()),
		cc.WithScope("cust", cc.Custom(sources()), cc.BootstrapFiles(cc.Pattern{Template: "{name}rc.{ext}"})), cc.WithScopes("cust"))
	wantErr[*cc.OptionError](t, err, cc.CodeBadRequest, "", "BootstrapFiles")
}

// DL-236, DL-240: the kind is "Scope", its default trust is TrustProject, Trust overrides it, and its policy is shown.
func TestCustomKind(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{}`})
	s := sources(cc.Source{Path: "/srv/a.json"})
	info := scopeInfo(t, custOK(t, fsys, s).Describe(), "cust")
	if info.Kind != "Scope" || info.Trust != cc.TrustProject || info.Policy == nil {
		t.Errorf("ScopeInfo = %+v, want kind Scope, TrustProject and a policy", info)
	}
	l := newLoaderFS(t, fsys, nil, cc.WithScope("cust", cc.Custom(s), cc.Trust(cc.TrustUser)), cc.WithScopes("cust"))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := scopeInfo(t, res.Describe(), "cust").Trust; got != cc.TrustUser {
		t.Errorf("with Trust(TrustUser): %v", got)
	}
}

// DL-237: Discovery comes from the loader and its host.
func TestCustomDiscovery(t *testing.T) {
	var got cc.Discovery
	s := &scopeFunc{discover: func(_ context.Context, d cc.Discovery) ([]cc.Source, error) {
		got = d
		return nil, nil
	}}
	fsys := cctest.NewFS(nil)
	logger := slog.New(&records{})
	l := newLoaderFS(t, fsys, map[string]string{"MYAPP_REGION": "eu"}, custOptions(s), cc.WithEnvs("dev", "ci"), cc.WithLogger(logger))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	envs := res.Describe().Envs
	switch {
	case got.Name != "myapp", got.Scope != "cust", got.StartDir != work, got.Home != "/home/me":
		t.Errorf("Discovery = %+v, want myapp, cust, %s and /home/me", got, work)
	case !slices.Equal(got.Envs, []string{"dev", "ci"}) || !slices.Equal(got.PlatformEnvs, envs.Platform):
		t.Errorf("Envs = %q, PlatformEnvs = %q; want [dev ci] and %q", got.Envs, got.PlatformEnvs, envs.Platform)
	case !slices.Equal(got.Formats, []string{"json"}):
		t.Errorf("Formats = %q, want [json]", got.Formats)
	case got.FS != cc.FileSystem(fsys), got.Logger != logger:
		t.Errorf("FS or Logger isn't the loader's")
	case got.LookupEnv == nil:
		t.Errorf("LookupEnv is nil")
	default:
		if v, ok := got.LookupEnv("MYAPP_REGION"); !ok || v != "eu" {
			t.Errorf("LookupEnv(MYAPP_REGION) = %q, %v; want the host's eu", v, ok)
		}
	}
}

// DL-238: a broken Source is a 500 naming the scope.
func TestCustomBrokenSources(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{}`, "/srv/b.yaml": `a: 1`})
	for name, src := range map[string]cc.Source{
		"both Path and Tree":     {Path: "/srv/a.json", Tree: cc.Tree{"a": int64(1)}},
		"neither Path nor Tree":  {Env: "dev"},
		"a relative Path":        {Path: "srv/a.json"},
		"an unregistered Format": {Path: "/srv/b.yaml", Format: "yaml"},
		"a non-canonical Tree":   {Tree: cc.Tree{"a": 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := custLoad(t, fsys, sources(cc.Source{Tree: cc.Tree{"ok": true}}, src))
			if code := cc.CodeOf(err); code != cc.CodeInternal || !strings.Contains(err.Error(), `"cust"`) {
				t.Errorf("err = %v (code %d), want a 500 naming the scope", err, code)
			}
		})
	}
}

// DL-238: an error from Discover with a cascadingconfig code keeps it; any other is a 500 naming the scope.
func TestCustomDiscoverErrors(t *testing.T) {
	failing := func(err error) *scopeFunc {
		return &scopeFunc{discover: func(context.Context, cc.Discovery) ([]cc.Source, error) { return nil, err }}
	}
	fsys := cctest.NewFS(nil)
	_, err := custLoad(t, fsys, failing(&cc.PolicyError{BaseError: cc.BaseError{Op: "discover", Err: errors.New("no")}}))
	if code := cc.CodeOf(err); code != cc.CodeForbidden {
		t.Errorf("a *PolicyError: code %d, want 403: %v", code, err)
	}
	_, err = custLoad(t, fsys, failing(errors.New("the database is down")))
	if code := cc.CodeOf(err); code != cc.CodeInternal || !strings.Contains(err.Error(), `"cust"`) || !strings.Contains(err.Error(), "the database is down") {
		t.Errorf("a plain error: %v (code %d), want a 500 naming the scope and wrapping the error", err, code)
	}
}

// DL-238, DL-240: Path sources load in the order returned, with their Variant and Env; a Format overrides the
// extension; the config root is the file's directory unless the Source gives one; layers have no Pattern.
func TestCustomPathSources(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{
		"/srv/a.json": `{"v": 1, "a": 1}`, "/srv/sub/b.json": `{"v": 2}`, "/srv/c.conf": `{"c": true}`,
	})
	res := custOK(t, fsys, sources(
		cc.Source{Path: "/srv/a.json"},
		cc.Source{Path: "/srv/sub/b.json", Variant: cc.VariantEnv, Env: "dev", ConfigRoot: "/srv"},
		cc.Source{Path: "/srv/c.conf", Format: "json"},
	))
	wantTreeJSON(t, res.Tree(), `{"v": 2, "a": 1, "c": true}`)
	var paths []string
	for _, l := range res.Describe().Layers {
		paths = append(paths, l.Path)
	}
	if want := []string{"/srv/a.json", "/srv/sub/b.json", "/srv/c.conf"}; !slices.Equal(paths, want) {
		t.Errorf("layers %q, want %q", paths, want)
	}
	b := layerAt(t, res.Describe(), "/srv/sub/b.json")
	if b.Variant != cc.VariantEnv || b.Env != "dev" || b.ConfigRoot != "/srv" || b.Pattern != "" || b.Scope != "cust" {
		t.Errorf("b.json layer = %+v, want env dev, config root /srv, no pattern, scope cust", b)
	}
	if a := layerAt(t, res.Describe(), "/srv/a.json"); a.ConfigRoot != "/srv" || a.Format != "json" {
		t.Errorf("a.json layer = %+v, want config root /srv and json", a)
	}
	if c := layerAt(t, res.Describe(), "/srv/c.conf"); c.Format != "json" {
		t.Errorf("c.conf format = %q, want json", c.Format)
	}
}

// DL-238: a Path that doesn't exist is a *NotFoundError naming the file.
func TestCustomMissingPath(t *testing.T) {
	_, err := custLoad(t, cctest.NewFS(nil), sources(cc.Source{Path: "/srv/missing.json"}))
	wantErr[*cc.NotFoundError](t, err, cc.CodeNotFound, "/srv/missing.json")
}

// DL-238: Path sources go through the pipeline: symlinks, ignored paths, dedup, includes and FirstMatch.
func TestCustomPathPipeline(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/data/real.json": `{"v": 1}`})
		fsys.Symlink("/data/real.json", "/srv/link.json")
		wantTreeJSON(t, custOK(t, fsys, sources(cc.Source{Path: "/srv/link.json"})).Tree(), `{"v": 1}`)
	})
	t.Run("ignored", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`, "/srv/b.json": `{"b": 1}`})
		res := custOK(t, fsys, sources(cc.Source{Path: "/srv/a.json"}, cc.Source{Path: "/srv/b.json"}), cc.WithIgnoredPaths("/srv/b.json"))
		wantTreeJSON(t, res.Tree(), `{"a": 1}`)
		if l := layerAt(t, res.Describe(), "/srv/b.json"); l.Status != cc.LayerIgnored {
			t.Errorf("b.json status = %v, want ignored", l.Status)
		}
	})
	t.Run("dedup", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`})
		res := custOK(t, fsys, sources(cc.Source{Path: "/srv/a.json"}, cc.Source{Path: "/srv/a.json"}))
		var statuses []cc.LayerStatus
		for _, l := range res.Describe().Layers {
			statuses = append(statuses, l.Status)
		}
		if !slices.Equal(statuses, []cc.LayerStatus{cc.LayerDuplicate, cc.LayerLoaded}) {
			t.Errorf("statuses = %v, want the lower copy a duplicate", statuses)
		}
	})
	t.Run("includes", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": includeOf(`"a": 1`, "inc/b.json"), "/srv/inc/b.json": `{"b": 1}`})
		wantTreeJSON(t, custOK(t, fsys, sources(cc.Source{Path: "/srv/a.json"})).Tree(), `{"a": 1, "b": 1}`)
	})
	t.Run("first match", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`, "/srv/b.json": `{"b": `})
		res := custOK(t, fsys, sources(cc.Source{Path: "/srv/b.json"}, cc.Source{Path: "/srv/a.json"}), cc.WithResolution(cc.FirstMatch))
		wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	})
}

// DL-238: permission checks and the trust store apply to Path sources, under the scope's policy.
func TestCustomPathPolicies(t *testing.T) {
	t.Run("permissions", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`, "/srv/b.json": `{"b": 1}`})
		fsys.Chmod("/srv/b.json", 0o666)
		res, err := newLoaderHost(t, fsys, hardenedHost(), custOptions(sources(cc.Source{Path: "/srv/a.json"}, cc.Source{Path: "/srv/b.json"})),
			cc.WithScopePolicy("cust", cc.FilePermissions(cc.StrictModes))).Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"a": 1}`)
		if l := layerAt(t, res.Describe(), "/srv/b.json"); !hasWarningCode(l.Warnings, cc.WarnFilePermissions) {
			t.Errorf("b.json layer = %+v, want a permissions warning", l)
		}
	})
	t.Run("trust store", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`, "/srv/b.json": `{"b": 1}`})
		store := &testStore{trusted: []string{"/srv/a.json"}}
		res := custOK(t, fsys, sources(cc.Source{Path: "/srv/a.json"}, cc.Source{Path: "/srv/b.json"}),
			cc.WithScopePolicy("cust", cc.RequireTrust(store)))
		wantTreeJSON(t, res.Tree(), `{"a": 1}`)
		if l := layerAt(t, res.Describe(), "/srv/b.json"); l.Status != cc.LayerUntrusted {
			t.Errorf("b.json status = %v, want untrusted", l.Status)
		}
	})
}

// DL-238: Path sources count for RequireConfig(RequireFile); Tree sources don't.
func TestCustomRequireFile(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`})
	custOK(t, fsys, sources(cc.Source{Path: "/srv/a.json"}), cc.RequireConfig(cc.RequireFile))
	_, err := custLoad(t, fsys, sources(cc.Source{Tree: cc.Tree{"a": int64(1)}}), cc.RequireConfig(cc.RequireFile))
	if err == nil {
		t.Errorf("a Tree source satisfied RequireFile")
	}
}

// DL-238, DL-240: Tree sources are like Values: each is its own layer with no Path, copied, with _unset stripped and
// warned about, and an include key kept as data.
func TestCustomTreeSources(t *testing.T) {
	first := cc.Tree{"a": int64(1), "t": map[string]any{"x": int64(1)}, "include": []any{"x.json"}}
	res := custOK(t, cctest.NewFS(nil), sources(
		cc.Source{Tree: first},
		cc.Source{Tree: cc.Tree{"b": int64(2), "_unset": []any{"a"}}, Variant: cc.VariantLocal},
	))
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2, "t": {"x": 1}, "include": ["x.json"]}`)
	first["t"].(map[string]any)["x"] = int64(99)
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2, "t": {"x": 1}, "include": ["x.json"]}`)
	layers := res.Describe().Layers
	if len(layers) != 2 || layers[0].Path != "" || layers[1].Variant != cc.VariantLocal {
		t.Fatalf("layers = %+v, want two with no Path, the second local", layers)
	}
	if !hasWarningCode(layers[1].Warnings, cc.WarnReservedKey) {
		t.Errorf("the second layer has no %s warning", cc.WarnReservedKey)
	}
}

// DL-240: custom scopes record no probes, though they load.
func TestCustomNoProbes(t *testing.T) {
	res := custOK(t, cctest.NewFS(nil), sources(cc.Source{Tree: cc.Tree{"a": int64(1)}}), cc.WithProbes())
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	for _, p := range res.Describe().Probes {
		if p.Scope == "cust" {
			t.Errorf("a probe for the custom scope: %+v", p)
		}
	}
}

// DL-238: Discover can run from several goroutines at once, with parallel loads.
func TestCustomConcurrentLoads(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"a": 1}`})
	s := sources(cc.Source{Path: "/srv/a.json"}, cc.Source{Tree: cc.Tree{"b": int64(2)}})
	l := newLoaderFS(t, fsys, nil, custOptions(s))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			res, err := l.Load(t.Context())
			if err != nil {
				t.Errorf("Load: %v", err)
				return
			}
			if got := res.Tree(); got["a"] != int64(1) || got["b"] != int64(2) {
				t.Errorf("Tree() = %v", got)
			}
		})
	}
	wg.Wait()
	if s.calls != 8 {
		t.Errorf("Discover was called %d times, want 8", s.calls)
	}
}
