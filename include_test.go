package cascadingconfig_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// DL-12, design.md Includes, "Rank": an included file ranks directly below its includer, above every lower scope, and
// a later entry beats an earlier one. The include key is removed from the result.
func TestIncludeRank(t *testing.T) {
	res := incOK(t, map[string]string{
		sysFile:              `{"a": "sys", "b": "sys", "c": "sys", "d": "sys"}`,
		rootCfg:              `{"include": ["base.json", "extra.json"], "a": "own"}`,
		work + "/base.json":  `{"a": "base", "b": "base", "c": "base"}`,
		work + "/extra.json": `{"b": "extra"}`,
	})
	wantTreeJSON(t, res.Tree(), `{"a": "own", "b": "extra", "c": "base", "d": "sys"}`)
	wantLayers(t, res.Describe(),
		"sys loaded "+sysFile,
		"proj loaded "+work+"/base.json <- "+rootCfg,
		"proj loaded "+work+"/extra.json <- "+rootCfg,
		"proj loaded "+rootCfg,
	)
}

// DL-187, design.md Includes, "Merge behaviour": included files rank below their includer, so its _unset removes
// their keys, and any lower layer's, but not its own.
func TestIncludeAndUnset(t *testing.T) {
	res := incOK(t, map[string]string{
		sysFile:             `{"proxy": "sys", "keep": "sys"}`,
		rootCfg:             `{"include": "base.json", "_unset": ["proxy", "own"], "own": 1}`,
		work + "/base.json": `{"proxy": "base", "port": 80}`,
	})
	wantTreeJSON(t, res.Tree(), `{"keep": "sys", "port": 80, "own": 1}`)
}

// DL-79, DL-184, design.md Includes, "Placement" and "Value": a string or a list at the top level; elsewhere the key is
// data; an empty list includes nothing.
func TestIncludePlacementAndValues(t *testing.T) {
	base := work + "/base.json"
	tests := []struct {
		name, root, want string
	}{
		{"a string", `{"include": "base.json"}`, `{"b": 1}`},
		{"a list", `{"include": ["base.json"]}`, `{"b": 1}`},
		{"an empty list", `{"include": [], "a": 1}`, `{"a": 1}`},
		{"not at the top level", `{"server": {"include": "base.json"}}`, `{"server": {"include": "base.json"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := incOK(t, map[string]string{rootCfg: tc.root, base: `{"b": 1}`})
			wantTreeJSON(t, res.Tree(), tc.want)
		})
	}
}

// DL-184: an empty string, a non-string element or any other type is a *ParseError naming the including file.
func TestIncludeBadValues(t *testing.T) {
	for _, root := range []string{
		`{"include": ""}`,
		`{"include": ["base.json", ""]}`,
		`{"include": [1]}`,
		`{"include": 3}`,
		`{"include": {"a": "base.json"}}`,
		`{"include": null}`,
	} {
		t.Run(root, func(t *testing.T) {
			_, err := incFails(t, map[string]string{rootCfg: root, work + "/base.json": `{}`})
			if !errors.Is(err, cc.ErrParse) {
				t.Fatalf("error = %v, want a *ParseError", err)
			}
			wantErr[*cc.ParseError](t, err, cc.CodeBadRequest, rootCfg, "include")
		})
	}
}

// DL-184, design.md Includes, "Placement": for a pattern with a Key, the include key is read at the top of the keyed
// table, where _unset is read; the file's own top level isn't config.
func TestIncludeInKeyedTable(t *testing.T) {
	files := map[string]string{
		work + "/package.json": `{"include": "outer.json", "myapp": {"include": "base.json", "a": 1}}`,
		work + "/base.json":    `{"b": 2}`,
		work + "/outer.json":   `{"c": 3}`,
	}
	res, err := newLoader(t, files, nil,
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "package.json", Key: "myapp"})),
		cc.WithScopes("proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2}`)
}

// DL-184, design.md Includes, "Metadata": an included layer takes Scope, Variant, Env, Trust and ConfigRoot from its
// includer; its Path is as joined, its Pattern is empty, its Format is its own, and IncludedBy names the includer.
func TestIncludedLayerMetadata(t *testing.T) {
	envFile := work + "/myapp.dev.json"
	files := map[string]string{
		envFile:                 `{"include": ["sub/base.toml"]}`,
		work + "/sub/base.toml": `{"b": 1}`,
	}
	res, err := newLoader(t, files, nil,
		cc.WithDecoder(jsonNamed{"toml", []string{"toml"}}),
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.{env}.json", Variant: cc.VariantEnv}),
			cc.Trust(cc.TrustUser), cc.ConfigRoot("/home/me")),
		cc.WithScopes("proj"),
		cc.WithEnvs("dev"),
		cc.WithIncludeKey("include", cc.IncludeFlat),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rep := res.Describe()
	root := layerAt(t, rep, envFile)
	inc := layerAt(t, rep, work+"/sub/base.toml")
	if inc.Scope != root.Scope || inc.Variant != cc.VariantEnv || inc.Env != "dev" || inc.Trust != cc.TrustUser ||
		inc.ConfigRoot != "/home/me" {
		t.Errorf("included layer = %+v, want the includer's scope, variant, env, trust and config root (%+v)", inc, root)
	}
	if inc.Pattern != "" || inc.Format != "toml" || inc.IncludedBy != envFile || inc.Status != cc.LayerLoaded {
		t.Errorf("included layer = %+v, want no pattern, format toml, included by %s, loaded", inc, envFile)
	}
}

// DL-184, design.md Includes, "Formats": an included file's format comes from WithFileFormat rules and extensions,
// whatever the format order; a file with no decoder is a 415 naming it.
func TestIncludeFormats(t *testing.T) {
	files := map[string]string{
		rootCfg:             `{"include": ["base.toml", "app.conf"]}`,
		work + "/base.toml": `{"a": 1}`,
		work + "/app.conf":  `{"b": 2}`,
	}
	res := incOK(t, files,
		cc.WithDecoder(jsonNamed{"toml", []string{"toml"}}),
		cc.WithFormats("json"),
		cc.WithFileFormat(cc.Extension("conf"), "json"),
	)
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2}`)

	_, err := incFails(t, map[string]string{rootCfg: `{"include": "base.xml"}`, work + "/base.xml": `{}`})
	if !errors.Is(err, cc.ErrUnsupportedFormat) {
		t.Errorf("error = %v, want an *UnsupportedFormatError", err)
	}
}

// DL-187, design.md Includes, "Where includes work": EmbeddedFS sources and value scopes keep the key as data; an
// ExplicitFile scope's files include.
func TestWhereIncludesWork(t *testing.T) {
	t.Run("EmbeddedFS", func(t *testing.T) {
		embedded := fstest.MapFS{"config.json": {Data: []byte(`{"include": "base.json", "a": 1}`)}}
		res, err := newLoader(t, map[string]string{"/base.json": `{"b": 2}`}, nil,
			cc.WithScope("emb", cc.EmbeddedFS(embedded, cc.Pattern{Template: "config.json"})), cc.WithScopes("emb"),
			cc.WithIncludeKey("include", cc.IncludeFlat),
		).Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"include": "base.json", "a": 1}`)
	})
	t.Run("Values", func(t *testing.T) {
		res, err := newLoader(t, map[string]string{work + "/base.json": `{"b": 2}`}, nil,
			valuesScope(cc.Tree{"include": work + "/base.json"}),
			cc.WithIncludeKey("include", cc.IncludeFlat),
		).Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"include": "/home/me/work/base.json"}`)
	})
	t.Run("ExplicitFile", func(t *testing.T) {
		res, err := newLoader(t, map[string]string{"/srv/app.json": `{"include": "base.json"}`, "/srv/base.json": `{"b": 2}`}, nil,
			cc.WithScope("x", cc.ExplicitFile("/srv/app.json")), cc.WithScopes("x"),
			cc.WithIncludeKey("include", cc.IncludeFlat),
		).Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		wantTreeJSON(t, res.Tree(), `{"b": 2}`)
	})
}

// DL-187: a bootstrap file's include key isn't followed; the bootstrap file only has its own keys.
func TestBootstrapFileDoesNotInclude(t *testing.T) {
	files := map[string]string{
		"/home/me/.config/myapp/myapprc.json": `{"include": "/home/me/extra.json"}`,
		"/home/me/extra.json":                 `{"secret": 1}`,
	}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(cctest.NewFS(files)),
		cctest.WithHost(testHost(nil)))
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	r, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := layersAt(r.Describe(), "/home/me/extra.json"); len(got) != 0 {
		t.Errorf("the bootstrap file's include loaded: %+v", got)
	}
	wantTreeJSON(t, r.Tree(), `{}`)
}

// DL-187: under FirstMatch, the matched file's includes load below it, and the search still stops at the match: the
// lower scope's file isn't read.
func TestIncludeUnderFirstMatch(t *testing.T) {
	files := map[string]string{
		work + "/.myapprc.json": `{"include": "base.json", "a": 1}`,
		work + "/base.json":     `{"a": 0, "b": 2}`,
		sysFile:                 `{"c": 3}`,
	}
	res, err := newLoader(t, files, nil,
		cc.WithScope("sys", cc.FixedDir("/etc/myapp", cc.Pattern{Template: "config.json"})),
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: ".myapprc.json"})),
		cc.WithScopes("sys", "proj"),
		cc.WithResolution(cc.FirstMatch),
		cc.WithIncludeKey("include", cc.IncludeFlat),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2}`)
	inc := layerAt(t, res.Describe(), work+"/base.json")
	if inc.IncludedBy != work+"/.myapprc.json" {
		t.Errorf("base.json = %+v, want it included by .myapprc.json, not matched", inc)
	}
}

// DL-179: a reference listed twice loads once, at its later, higher position; the earlier route is LayerDuplicate.
func TestIncludeListedTwice(t *testing.T) {
	base := work + "/base.json"
	res := incOK(t, map[string]string{
		rootCfg:              `{"include": ["base.json", "extra.json", "base.json"]}`,
		base:                 `{"k": "base"}`,
		work + "/extra.json": `{"k": "extra"}`,
	})
	wantTreeJSON(t, res.Tree(), `{"k": "base"}`)
	wantLayers(t, res.Describe(),
		"proj duplicate "+base+" <- "+rootCfg,
		"proj loaded "+work+"/extra.json <- "+rootCfg,
		"proj loaded "+base+" <- "+rootCfg,
		"proj loaded "+rootCfg,
	)
}

// DL-179, DL-110: a file both found and included loads once, at its highest position, here the include's, with the
// scope of its most trusted route.
func TestIncludeOfAFoundFile(t *testing.T) {
	res := incOK(t, map[string]string{
		sysFile:             `{"x": "sys"}`,
		"/home/me/mid.json": `{"x": "mid"}`,
		rootCfg:             `{"include": "/etc/myapp/config.json"}`,
	},
		cc.WithScope("mid", cc.FixedDir("/home/me", cc.Pattern{Template: "mid.json"}), cc.After("sys")),
		cc.WithScopes("sys", "mid", "proj"),
	)
	wantTreeJSON(t, res.Tree(), `{"x": "sys"}`)
	got := layersAt(res.Describe(), sysFile)
	if len(got) != 2 || got[0].Status != cc.LayerDuplicate || got[1].Status != cc.LayerLoaded ||
		got[1].IncludedBy != rootCfg || got[1].Scope != "sys" {
		t.Errorf("layers for %s = %+v, want a duplicate, then the include, loaded with scope sys", sysFile, got)
	}
}

// DL-179: in a diamond, the shared file loads once, at its highest position, under the later branch. Under the old
// "first rank" rule it would have loaded under b, and b would win.
func TestIncludeDiamond(t *testing.T) {
	d := work + "/d.json"
	res := incOK(t, map[string]string{
		rootCfg:          `{"include": ["b.json", "c.json"]}`,
		work + "/b.json": `{"include": "d.json", "k": "b"}`,
		work + "/c.json": `{"include": "d.json"}`,
		d:                `{"k": "d"}`,
	}, nested)
	wantTreeJSON(t, res.Tree(), `{"k": "d"}`)
	wantLayers(t, res.Describe(),
		"proj duplicate "+d+" <- "+work+"/b.json",
		"proj loaded "+work+"/b.json <- "+rootCfg,
		"proj loaded "+d+" <- "+work+"/c.json",
		"proj loaded "+work+"/c.json <- "+rootCfg,
		"proj loaded "+rootCfg,
	)
}

// DL-179: a file that includes itself is a cycle, even with flat includes.
func TestIncludeSelf(t *testing.T) {
	_, err := incFails(t, map[string]string{rootCfg: `{"include": "myapp.json"}`})
	if !errors.Is(err, cc.ErrIncludeCycle) {
		t.Fatalf("error = %v, want a *CycleError", err)
	}
	wantErr[*cc.CycleError](t, err, cc.CodeLoopDetected, rootCfg, "myapp.json")
}

// DL-178, design.md Includes, "Nesting": MiseLayout's includes are flat, so an include key in an included file is a
// *ParseError naming that file and the key.
func TestMiseLayoutRejectsNesting(t *testing.T) {
	base := work + "/base.json"
	files := map[string]string{
		rootCfg:             `{"include": "base.json"}`,
		base:                `{"include": "more.json"}`,
		work + "/more.json": `{}`,
	}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(testHost(nil)))
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	_, err = l.Load(t.Context())
	if !errors.Is(err, cc.ErrParse) {
		t.Fatalf("error = %v, want a *ParseError", err)
	}
	wantErr[*cc.ParseError](t, err, cc.CodeBadRequest, base, "include")
}

// DL-178: MiseLayout and MiseHardenedLayout read "include"; CosmiconfigLayout has no include key, so it's data.
func TestLayoutIncludeKeys(t *testing.T) {
	for _, tc := range []struct {
		preset cc.Preset
		file   string
		want   string
	}{
		{cc.MiseLayout, rootCfg, `{"b": 1}`},
		{cc.MiseHardenedLayout, rootCfg, `{"b": 1}`},
		{cc.CosmiconfigLayout, work + "/.myapprc.json", `{"include": "base.json"}`},
	} {
		t.Run(tc.preset.Name(), func(t *testing.T) {
			files := map[string]string{tc.file: `{"include": "base.json"}`, work + "/base.json": `{"b": 1}`}
			l, err := cc.NewWithPreset("myapp", tc.preset, cc.WithFileSystem(cctest.NewFS(files)),
				cctest.WithHost(testHost(nil)))
			if err != nil {
				t.Fatalf("NewWithPreset: %v", err)
			}
			res, err := l.Load(t.Context())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantTreeJSON(t, res.Tree(), tc.want)
		})
	}
}

// DL-178, DL-184: with IncludeNested, included files include; relative references resolve against each file's own
// directory; every layer of the chain takes the root file's scope, and IncludedBy names its direct includer.
func TestNestedIncludes(t *testing.T) {
	x, y := work+"/a/x.json", work+"/a/y.json"
	res := incOK(t, map[string]string{
		rootCfg: `{"include": "a/x.json"}`,
		x:       `{"include": "y.json", "x": 1}`,
		y:       `{"y": 2}`,
	}, nested)
	wantTreeJSON(t, res.Tree(), `{"x": 1, "y": 2}`)
	wantLayers(t, res.Describe(),
		"proj loaded "+y+" <- "+x,
		"proj loaded "+x+" <- "+rootCfg,
		"proj loaded "+rootCfg,
	)
}

// DL-178, DL-185: a cycle through nested includes is a 508 naming the file whose reference closes it.
func TestNestedIncludeCycle(t *testing.T) {
	a, b := work+"/a.json", work+"/b.json"
	_, err := incFails(t, map[string]string{
		rootCfg: `{"include": "a.json"}`,
		a:       `{"include": "b.json"}`,
		b:       `{"include": "a.json"}`,
	}, nested)
	wantErr[*cc.CycleError](t, err, cc.CodeLoopDetected, b, "a.json")
}

// DL-183, design.md Local files, "References": file:// URLs with an empty host or localhost, and ~/ paths, are local;
// C:\… is a path, not a scheme.
func TestLocalReferences(t *testing.T) {
	for _, tc := range []struct{ ref, file string }{
		{"file:///srv/base.json", "/srv/base.json"},
		{"file://localhost/srv/base.json", "/srv/base.json"},
		{"~/shared/base.json", "/home/me/shared/base.json"},
		{`C:\cfg\base.json`, work + `/C:\cfg\base.json`},
		{"../base.json", "/home/me/base.json"},
		{"/srv/base.json", "/srv/base.json"},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			ref, _ := json.Marshal(tc.ref)
			res := incOK(t, map[string]string{rootCfg: `{"include": ` + string(ref) + `}`, tc.file: `{"b": 1}`})
			wantTreeJSON(t, res.Tree(), `{"b": 1}`)
			if l := layerAt(t, res.Describe(), tc.file); l.IncludedBy != rootCfg {
				t.Errorf("layer = %+v, want it included by %s", l, rootCfg)
			}
		})
	}
}

// DL-182, DL-183: with no resolver registered for its scheme, a remote reference is an *UnsupportedSchemeError (501)
// naming the including file.
func TestRemoteReferencesUnsupported(t *testing.T) {
	for _, ref := range []string{
		"git::https://github.com/acme/config.git//myapp/base.json?ref=v1",
		"oci::ghcr.io/acme/myapp-config:1.4.0",
		"https://config.example.com/base.json",
		"http://config.example.com/base.json",
	} {
		t.Run(ref, func(t *testing.T) {
			_, err := incFails(t, map[string]string{rootCfg: `{"include": "` + ref + `"}`})
			if !errors.Is(err, cc.ErrUnsupportedScheme) {
				t.Fatalf("error = %v, want an *UnsupportedSchemeError", err)
			}
			wantErr[*cc.UnsupportedSchemeError](t, err, cc.CodeNotImplemented, rootCfg, ref)
		})
	}
}

// DL-185: errors about the reference name the including file, with Op "include" and the reference as written.
func TestIncludeMissing(t *testing.T) {
	_, err := incFails(t, map[string]string{rootCfg: `{"include": "nope/base.json"}`})
	wantErr[*cc.NotFoundError](t, err, cc.CodeNotFound, rootCfg, "nope/base.json")
	var nf *cc.NotFoundError
	if errors.As(err, &nf); baseOp(nf) != "include" {
		t.Errorf("Op = %q, want include", baseOp(nf))
	}
}

// DL-185: errors inside the included file name that file, and its LayerFailed entry shows IncludedBy. Includes are
// never skipped, so an unreadable file or a directory is an *IOError, not a warning. A broken symlink points at
// nothing, so it's a missing target: a 404 naming the including file.
func TestIncludedFileErrors(t *testing.T) {
	base := work + "/base.json"
	t.Run("parse", func(t *testing.T) {
		res, err := incFails(t, map[string]string{rootCfg: `{"include": "base.json"}`, base: `{"a": SECRET}`})
		wantErr[*cc.ParseError](t, err, cc.CodeBadRequest, base)
		l := layerAt(t, res.Describe(), base)
		if l.Status != cc.LayerFailed || l.IncludedBy != rootCfg {
			t.Errorf("layer = %+v, want LayerFailed, included by %s", l, rootCfg)
		}
		noSecret(t, "the reason", l.Reason, "SECRET")
	})
	t.Run("too large", func(t *testing.T) {
		_, err := incFails(t, map[string]string{
			rootCfg: `{"include": "base.json"}`,
			base:    `{"padding": "` + strings.Repeat("x", 64) + `"}`,
		}, cc.WithMaxFileSize(40))
		wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, base)
	})
	t.Run("unreadable", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{rootCfg: `{"include": "base.json"}`, base: `{}`})
		fsys.Chmod(base, 0)
		_, err := incLoad(t, fsys)
		wantErr[*cc.IOError](t, err, cc.CodeInternal, base)
	})
	t.Run("a directory", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{rootCfg: `{"include": "base.json"}`})
		fsys.Mkdir(base, fs.FileMode(0o755))
		_, err := incLoad(t, fsys)
		wantErr[*cc.IOError](t, err, cc.CodeInternal, base)
	})
	t.Run("a broken symlink", func(t *testing.T) {
		fsys := cctest.NewFS(map[string]string{rootCfg: `{"include": "base.json"}`})
		fsys.Symlink("gone.json", base)
		_, err := incLoad(t, fsys)
		wantErr[*cc.NotFoundError](t, err, cc.CodeNotFound, rootCfg, "base.json")
	})
}

// DL-185: an empty included file is LayerEmpty, and the load succeeds.
func TestIncludedFileEmpty(t *testing.T) {
	base := work + "/base.json"
	res := incOK(t, map[string]string{rootCfg: `{"include": "base.json", "a": 1}`, base: "  \n"})
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	if l := layerAt(t, res.Describe(), base); l.Status != cc.LayerEmpty {
		t.Errorf("layer = %+v, want LayerEmpty", l)
	}
}
