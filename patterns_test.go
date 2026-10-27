package cascadingconfig

import (
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// DL-117: within one directory, patterns group by variant: shared, env once per environment, local, then env-local,
// whatever order they're listed in. Within a group they keep their order.
func TestPatternsGroupByVariant(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(
		"/d/a.json", "/d/b.json", "/d/a.dev.json", "/d/a.ci.json", "/d/a.local.json", "/d/a.dev.local.json", "/d/a.ci.local.json",
	))
	res := mustLoad(t, fsys,
		hostWith(map[string]string{}, nil), WithEnvs("dev", "ci"),
		WithScope("s", FixedDir("/d",
			Pattern{Template: "a.{env}.local.json", Variant: VariantEnvLocal},
			Pattern{Template: "a.local.json", Variant: VariantLocal},
			Pattern{Template: "a.{env}.json", Variant: VariantEnv},
			Pattern{Template: "b.json"},
			Pattern{Template: "a.json"},
		)),
		WithScopes("s"),
	)
	golden.Check(t, "discovery/variants.golden", []byte(detail(res)))
}

// DL-118: * matches within one segment: a dotless file name, or a directory name without a leading dot. Matches apply
// alphabetically, and each stem is its own slot.
func TestGlobs(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet(
		"/d/conf.d/20-b.json", "/d/conf.d/10-a.json", "/d/conf.d/10-a.toml", "/d/conf.d/.hidden.json",
		"/d/conf.d/30-c.dev.json", "/d/conf.d/zz/myapp.json", "/d/conf.d/aa/myapp.json", "/d/conf.d/.git/myapp.json",
	))
	res := mustLoad(t, fsys, WithDecoder(tomlAsJSON), WithFormats("toml", "json"), WithEnvs("dev"),
		WithScope("s", FixedDir("/d",
			Pattern{Template: "conf.d/*.{ext}"},
			Pattern{Template: "conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: "conf.d/*.{env}.{ext}", Variant: VariantEnv},
		)),
		WithScopes("s"),
	)
	golden.Check(t, "discovery/globs.golden", []byte(detail(res)))
}

// DL-119: RootSearchDir is the searched directory, or the scope's ConfigRoot; RootFileDir is the file's own
// directory.
func TestConfigRoots(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(fileSet("/d/.config/myapp/config.json", "/d/.config/myapp/myapp.json"))
	pats := []Pattern{
		{Template: ".config/{name}/config.json"},
		{Template: ".config/{name}/{name}.json", Root: RootFileDir},
	}
	res := mustLoad(t, fsys, WithScope("s", FixedDir("/d", pats...)), WithScopes("s"))
	if got := layerFor(t, res, "/d/.config/myapp/config.json").ConfigRoot; got != "/d" {
		t.Errorf("RootSearchDir root = %q, want /d", got)
	}
	if got := layerFor(t, res, "/d/.config/myapp/myapp.json").ConfigRoot; got != "/d/.config/myapp" {
		t.Errorf("RootFileDir root = %q, want /d/.config/myapp", got)
	}

	res = mustLoad(t, fsys, WithScope("s", FixedDir("/d", pats...), ConfigRoot("~")), WithScopes("s"))
	if got := layerFor(t, res, "/d/.config/myapp/config.json").ConfigRoot; got != "/home/me" {
		t.Errorf("RootSearchDir root with ConfigRoot(~) = %q, want /home/me", got)
	}
	if got := layerFor(t, res, "/d/.config/myapp/myapp.json").ConfigRoot; got != "/d/.config/myapp" {
		t.Errorf("RootFileDir root with ConfigRoot(~) = %q, want /d/.config/myapp", got)
	}
	l := layerFor(t, res, "/d/.config/myapp/myapp.json")
	if l.Pattern != ".config/{name}/{name}.json" || l.Variant != VariantShared || l.Env != "" {
		t.Errorf("Pattern, Variant, Env = %q, %v, %q", l.Pattern, l.Variant, l.Env)
	}
}

// DL-120: Pattern.Key takes the config from a table inside the file. A missing key means the file isn't found; a
// value that isn't a table is a *ParseError naming the file and the key.
func TestPatternKey(t *testing.T) {
	t.Parallel()
	pkg := Pattern{Template: "package.json", Key: "{name}"}
	fsys := memfs.New(map[string]string{
		"/a/package.json":   `{"name": "x", "myapp": {"port": 1}}`,
		"/b/package.json":   `{"name": "x"}`,
		"/c/package.json":   `{"myapp": 5}`,
		"/d/pyproject.json": `{"tool": {"myapp": {"port": 2}}}`,
	})
	res := mustLoad(t, fsys,
		WithScope("a", FixedDir("/a", pkg)), WithScope("b", FixedDir("/b", pkg)),
		WithScope("d", FixedDir("/d", Pattern{Template: "pyproject.json", Key: "tool.{name}"})),
		WithScopes("a", "b", "d"),
	)
	wantTree(t, res, `{"port": 2}`)
	wantFound(t, res, "a /a/package.json loaded", "d /d/pyproject.json loaded")

	err := loadFails(t, fsys, WithScope("c", FixedDir("/c", pkg)), WithScopes("c"))
	pe, ok := asErr[*ParseError](err)
	if !ok || pe.Path != "/c/package.json" {
		t.Fatalf("error = %v, want a *ParseError for /c/package.json", err)
	}
	mustContainAll(t, err.Error(), "myapp")
}

func mustContainAll(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Errorf("%q doesn't contain %q", s, p)
		}
	}
}
