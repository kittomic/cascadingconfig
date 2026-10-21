package apicheck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"
)

// DL-257: the rendered API has one line per declaration, sorted: constants with values (untyped ones without a type),
// functions and methods with parameter types only, the method set of each type, structs and interfaces with a line per
// exported field or method, embedded fields marked, sealed interfaces marked, constraint interfaces whole, aliases,
// and variables with types. Unexported names don't appear.
func TestAPIRender(t *testing.T) {
	want := []string{
		`const CodeNotFound Code = 404`,
		`const MaxDepth = 8`,
		`const ScopeProject ScopeName = "project"`,
		`func New(string, ...Option) (*Loader, error)`,
		`func Open(fs.FS) error`,
		`func WithMap(func(string, string) (string, bool)) Option`,
		`func WithRule[P PathSpec](P, int) Option`,
		`method (*BaseError) Unwrap() error`,
		`method (*FetchError) Error() string`,
		`method (*FetchError) Unwrap() error`,
		`method (*Loader) Load(context.Context) (Tree, error)`,
		`method (*Watcher[T]) Current() T`,
		`method (ScopeName) String() string`,
		`type Alias = Layer`,
		`type BaseError struct`,
		`type BaseError struct, Err error`,
		`type BaseError struct, Op string`,
		`type Code int`,
		`type Decoder interface`,
		`type Decoder interface, Decode([]byte) (Tree, error)`,
		`type Decoder interface, Format() string`,
		`type FetchError struct`,
		`type FetchError struct, Timeout bool`,
		`type FetchError struct, embedded BaseError`,
		`type Layer struct`,
		`type Layer struct, Path string`,
		`type Layer struct, Scope ScopeName`,
		`type Layer struct, Size int64`,
		`type Loader struct`,
		`type MapFunc func(string, string) (string, bool)`,
		`type Option interface`,
		`type Option interface, unexported methods`,
		`type PathSpec interface{~string | []string}`,
		`type ScopeName string`,
		`type Tree map[string]any`,
		`type Watcher[T any] struct`,
		`var ErrParse error`,
	}
	got := apicheck.API(fixture(t)[fixtureModule])
	if !slices.Equal(got, want) {
		t.Errorf("API:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// DL-257: another package's types are qualified by its name.
func TestAPIRenderQualifies(t *testing.T) {
	want := []string{
		`func New(...Option) m.Decoder`,
		`type Option interface`,
		`type Option interface, unexported methods`,
		`var Preset m.Option`,
	}
	got := apicheck.API(fixture(t)[fixtureModule+"/sub"])
	if !slices.Equal(got, want) {
		t.Errorf("API:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// DL-257: Diff reports rendered lines the file lacks and file lines that no longer render, ignoring blank lines.
func TestDiff(t *testing.T) {
	for _, c := range []struct {
		name                 string
		file, rendered       []string
		wantMissing, wantExt []string
	}{
		{"same", []string{"a", "b"}, []string{"a", "b"}, nil, nil},
		{"blank lines", []string{"a", "", "b", ""}, []string{"a", "b"}, nil, nil},
		{"added", []string{"a"}, []string{"a", "b"}, []string{"b"}, nil},
		{"removed", []string{"a", "b"}, []string{"a"}, nil, []string{"b"}},
		{"changed", []string{"a", "b", "d"}, []string{"a", "c", "d"}, []string{"c"}, []string{"b"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			missing, extra := apicheck.Diff(c.file, c.rendered)
			if !slices.Equal(missing, c.wantMissing) || !slices.Equal(extra, c.wantExt) {
				t.Errorf("Diff = %q, %q; want %q, %q", missing, extra, c.wantMissing, c.wantExt)
			}
		})
	}
}
