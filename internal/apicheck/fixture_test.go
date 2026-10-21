package apicheck_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"
)

// The fixture module: a core package and two others, type-checked from source, so each test plants its errors in the
// doc alone.
const fixtureModule = "example.com/m"

const fixtureCore = `package m

import (
	"context"
	"io/fs"
)

type Option interface{ option() }

type ScopeName string

func (s ScopeName) String() string { return string(s) }

const ScopeProject ScopeName = "project"

const MaxDepth = 8

type Code int

const CodeNotFound Code = 404

var ErrParse error

type Loader struct{ name string }

func New(name string, opts ...Option) (*Loader, error) { return nil, nil }

func (l *Loader) Load(ctx context.Context) (Tree, error) { return nil, nil }

type Tree map[string]any

type Layer struct {
	Path   string
	Scope  ScopeName
	Size   int64
	hidden bool
}

type Decoder interface {
	Format() string
	Decode(data []byte) (Tree, error)
}

type MapFunc func(name, value string) (string, bool)

func WithMap(f func(name, value string) (string, bool)) Option { return nil }

type PathSpec interface{ ~string | []string }

func WithRule[P PathSpec](path P, n int) Option { return nil }

type BaseError struct {
	Op  string
	Err error
}

func (e *BaseError) Unwrap() error { return e.Err }

type FetchError struct {
	BaseError
	Timeout bool
}

func (e *FetchError) Error() string { return "" }

type Alias = Layer

func Open(fsys fs.FS) error { return nil }

type Watcher[T any] struct{ v T }

func (w *Watcher[T]) Current() T { return w.v }

func helper() {}
`

const fixtureSub = `package sub

import "example.com/m"

type Option interface{ option() }

func New(opts ...Option) m.Decoder { return nil }

var Preset m.Option
`

const fixtureOther = `package other

import "example.com/m"

func New() m.Decoder { return nil }

var Preset m.Option
`

// fixture type-checks the fixture module's packages, by import path.
func fixture(t *testing.T) map[string]*types.Package {
	t.Helper()
	fset := token.NewFileSet()
	std := importer.ForCompiler(fset, "source", nil)
	pkgs := map[string]*types.Package{}
	imp := importerFunc(func(path string) (*types.Package, error) {
		if p, ok := pkgs[path]; ok {
			return p, nil
		}
		return std.Import(path)
	})
	for _, p := range []struct{ path, src string }{
		{fixtureModule, fixtureCore},
		{fixtureModule + "/sub", fixtureSub},
		{fixtureModule + "/other", fixtureOther},
	} {
		f, err := parser.ParseFile(fset, p.path+".go", p.src, 0)
		if err != nil {
			t.Fatal(err)
		}
		conf := types.Config{Importer: imp}
		pkg, err := conf.Check(p.path, fset, []*ast.File{f}, nil)
		if err != nil {
			t.Fatalf("type-checking the fixture %s: %v", p.path, err)
		}
		pkgs[p.path] = pkg
	}
	return pkgs
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// categories are the words a finding's message starts with (DL-256).
var categories = []string{"missing", "differs", "kind", "incomplete", "unparsable", "unknown package"}

// check runs Listings on doc against the fixture, and returns each finding as "line package name: category", with the
// package shortened to its path in the module ("." for the core).
func check(t *testing.T, doc string) []string {
	t.Helper()
	var out []string
	for _, f := range apicheck.Listings(fixtureModule, fixture(t), "doc.md", []byte(doc)) {
		if f.Doc != "doc.md" {
			t.Errorf("finding %v names doc %q, want doc.md", f, f.Doc)
		}
		cat := ""
		for _, c := range categories {
			if strings.HasPrefix(f.Message, c) {
				cat = c
			}
		}
		if cat == "" {
			t.Errorf("finding %v: the message starts with none of %q", f, categories)
		}
		pkg := strings.TrimPrefix(strings.TrimPrefix(f.Package, fixtureModule), "/")
		if f.Package == fixtureModule {
			pkg = "."
		}
		out = append(out, strings.TrimSpace(strings.Join([]string{strconv.Itoa(f.Line), pkg, f.Name}, " "))+": "+cat)
	}
	return out
}

// at is the line of doc holding the first occurrence of text, counting from 1.
func at(t *testing.T, doc, text string) int {
	t.Helper()
	i := strings.Index(doc, text)
	if i < 0 {
		t.Fatalf("the doc doesn't hold %q", text)
	}
	return strings.Count(doc[:i], "\n") + 1
}
