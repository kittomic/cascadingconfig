package apicheck_test

import (
	"flag"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"

	// Every released module, so go list finds each released package's export data, also with GOWORK=off.
	_ "github.com/kittomic/cascadingconfig"
	_ "github.com/kittomic/cascadingconfig/format/toml"
	_ "github.com/kittomic/cascadingconfig/format/yaml"
	_ "github.com/kittomic/cascadingconfig/ociresolver"
	_ "github.com/kittomic/cascadingconfig/presets/mise"
	_ "github.com/kittomic/cascadingconfig/watch/fsnotify"
)

const module = "github.com/kittomic/cascadingconfig"

// root is the repository root, from this package's directory.
const root = "../.."

var update = flag.Bool("update", false, "rewrite the API files under api/ from the packages (DL-257)")

// released is every released package, by its path from the repository root, "." being the core. TestReleased checks
// it against go list, so a new package can't escape the guard.
var released = []string{
	".", "cctest", "format/json", "format/toml", "format/yaml", "gitresolver", "httpresolver", "ociresolver",
	"presets/cosmiconfig", "presets/mise", "presets/misehardened", "watch/fsnotify",
}

func importPath(rel string) string {
	if rel == "." {
		return module
	}
	return module + "/" + rel
}

var (
	realOnce sync.Once
	realPkgs map[string]*types.Package
	realErr  error
)

// loadReleased loads every released package from its export data, once.
func loadReleased(t *testing.T) map[string]*types.Package {
	t.Helper()
	realOnce.Do(func() {
		var paths []string
		for _, rel := range released {
			paths = append(paths, importPath(rel))
		}
		realPkgs, realErr = apicheck.Load(".", paths...)
	})
	if realErr != nil {
		t.Fatalf("Load: %v", realErr)
	}
	return realPkgs
}

// DL-257: released lists exactly the repository's packages outside internal/ and examples/.
func TestReleased(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "list", module+"/...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK="+filepath.Join(mustAbs(t, root), "go.work"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var got []string
	for _, p := range strings.Fields(string(out)) {
		rel := strings.TrimPrefix(strings.TrimPrefix(p, module), "/")
		if rel == "" {
			rel = "."
		}
		if strings.HasPrefix(rel, "internal/") || strings.Contains(rel, "/internal/") || rel == "examples" ||
			strings.HasPrefix(rel, "examples/") {
			continue
		}
		got = append(got, rel)
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(released))
	if !slices.Equal(got, want) {
		t.Errorf("released packages per go list:\n%s\nwant (released):\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// designRecords are the docs whose listings are checked (DL-256).
var designRecords = []string{"docs/design/api.md", "docs/design/design.md"}

// DL-256: every listing in the design records matches the real API.
func TestDesignRecordListings(t *testing.T) {
	t.Parallel()
	pkgs := loadReleased(t)
	for _, doc := range designRecords {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range apicheck.Listings(module, pkgs, doc, data) {
			t.Error(f)
		}
	}
}

// apiFile is the guard file for the released package at rel (DL-257): api/ mirrors the package paths, and the core is
// api/cascadingconfig.txt.
func apiFile(rel string) string {
	if rel == "." {
		return filepath.Join(root, "api", "cascadingconfig.txt")
	}
	return filepath.Join(root, "api", filepath.FromSlash(rel)+".txt")
}

// DL-257: every released package's API matches its file under api/, both ways. With -update, the files are rewritten.
func TestAPIFiles(t *testing.T) {
	t.Parallel()
	pkgs := loadReleased(t)
	for _, rel := range released {
		t.Run(rel, func(t *testing.T) {
			pkg := pkgs[importPath(rel)]
			if pkg == nil {
				t.Fatalf("%s wasn't loaded", importPath(rel))
			}
			rendered := apicheck.API(pkg)
			if len(rendered) == 0 {
				t.Fatalf("%s rendered no API", rel)
			}
			path := apiFile(rel)
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.Join(rendered, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test -update in internal/apicheck to write it)", err)
			}
			missing, extra := apicheck.Diff(strings.Split(string(data), "\n"), rendered)
			for _, l := range missing {
				t.Errorf("%s lacks: %s", path, l)
			}
			for _, l := range extra {
				t.Errorf("%s lists what's gone: %s", path, l)
			}
			if len(missing)+len(extra) > 0 {
				t.Log("if the change is deliberate, run go test -update in internal/apicheck and commit the diff")
			}
		})
	}
}

// DL-256: each planted error in the real design records is caught. Each mutation must apply exactly once, so the test
// fails loudly if the docs change under it.
func TestDesignRecordMutations(t *testing.T) {
	t.Parallel()
	pkgs := loadReleased(t)
	for _, m := range []struct{ name, doc, old, new string }{
		{"parameter type", "api.md", "func WithScopes(names ...ScopeName) Option", "func WithScopes(names ...string) Option"},
		{"result type", "api.md", "func (l *Loader) Load(ctx context.Context) (*Result, error)", "func (l *Loader) Load(ctx context.Context) (Result, error)"},
		{"constant value", "api.md", `BindEnv                     Binding = "ENV"`, `BindEnv                     Binding = "ENVIRONMENT"`},
		{"interface method dropped", "api.md", "\tFormat() string        // \"toml\"\n", ""},
		{"field type", "api.md", "\tEmpty    bool      //", "\tEmpty    int       //"},
		{"renamed function", "api.md", "func CodeOf(err error) Code", "func CodeFor(err error) Code"},
		{"unknown package", "api.md", "// package format/toml (DL-169, DL-172)", "// package format/tomll (DL-169, DL-172)"},
		{"kind", "api.md", "var Preset cascadingconfig.Preset", "func Preset() cascadingconfig.Preset"},
		{"interface method signature", "design.md", "Trusted(ctx context.Context, path, digest string) (bool, error)", "Trusted(ctx context.Context, path, digest string) bool"},
	} {
		t.Run(m.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "docs", "design", m.doc))
			if err != nil {
				t.Fatal(err)
			}
			doc := string(data)
			if n := strings.Count(doc, m.old); n != 1 {
				t.Fatalf("%s holds %q %d times, want once", m.doc, m.old, n)
			}
			doc = strings.Replace(doc, m.old, m.new, 1)
			if f := apicheck.Listings(module, pkgs, m.doc, []byte(doc)); len(f) == 0 {
				t.Errorf("the mutation %q in %s wasn't caught", m.name, m.doc)
			}
		})
	}
}
