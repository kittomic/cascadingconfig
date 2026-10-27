package cascadingconfig

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// The load tests run against internal/memfs, which also backs cctest.FS, and a fake host, so they can read each
// layer's status and warnings from the private layer list (DL-107).

// memFS adapts memfs to FileSystem, as cctest.FS does.
type memFS struct{ *memfs.FS }

func (m memFS) OpenRoot(dir string) (Root, error) {
	r, err := m.FS.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// testHost is the fake host the load tests start from: Linux, with a home directory.
func testHost() hooks.Host {
	return hooks.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work", Env: map[string]string{}}
}

// jsonAs is a decoder for another format that reads JSON, so the tests can use TOML and YAML file names without
// those modules.
type jsonAs struct {
	format string
	exts   []string
}

func (d jsonAs) Format() string                       { return d.format }
func (d jsonAs) Extensions() []string                 { return d.exts }
func (d jsonAs) MediaTypes() []string                 { return nil }
func (d jsonAs) Decode(data []byte) (Document, error) { return builtinJSONDecoder.Decode(data) }

var (
	tomlAsJSON = jsonAs{"toml", []string{"toml"}}
	yamlAsJSON = jsonAs{"yaml", []string{"yaml", "yml"}}
)

// fixed defines a FixedDir scope over dir with templates, lowest precedence first.
func fixed(name ScopeName, dir string, templates ...string) Option {
	patterns := make([]Pattern, len(templates))
	for i, tpl := range templates {
		patterns[i] = Pattern{Template: tpl}
	}
	return WithScope(name, FixedDir(dir, patterns...))
}

// loader builds a loader over fsys and the test host, with JSON registered. opts come last, so they can override
// the host or add scopes; the tests select their scopes themselves.
func loader(t *testing.T, fsys *memfs.FS, opts ...Option) *Loader {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the load tests use Unix paths")
	}
	base := []Option{WithFileSystem(memFS{fsys}), withHost(fakeHost{testHost()}), WithDecoder(builtinJSONDecoder)}
	l, err := New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

func mustLoad(t *testing.T, fsys *memfs.FS, opts ...Option) *Result {
	t.Helper()
	res, err := loader(t, fsys, opts...).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res == nil {
		t.Fatalf("Load returned a nil Result")
	}
	return res
}

func loadFails(t *testing.T, fsys *memfs.FS, opts ...Option) error {
	t.Helper()
	_, err := loader(t, fsys, opts...).Load(t.Context())
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	return err
}

var statusNames = map[LayerStatus]string{
	LayerLoaded: "loaded", LayerIgnored: "ignored", LayerEmpty: "empty", LayerUnreadable: "unreadable",
}

// found writes the layers a load considered, one per line: scope, path, status and warning codes.
func found(res *Result) string {
	var b strings.Builder
	for _, l := range res.found {
		var codes []string
		for _, w := range l.Warnings {
			codes = append(codes, string(w.Code))
		}
		line := fmt.Sprintf("%-8s %-40s %s %s", l.Scope, l.Path, statusNames[l.Status], strings.Join(codes, ","))
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

func wantFound(t *testing.T, res *Result, want ...string) {
	t.Helper()
	got := found(res)
	normalise := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var gotLines []string
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if line != "" {
			gotLines = append(gotLines, normalise(line))
		}
	}
	var wantLines []string
	for _, w := range want {
		wantLines = append(wantLines, normalise(w))
	}
	if strings.Join(gotLines, "\n") != strings.Join(wantLines, "\n") {
		t.Errorf("found layers:\n%s\nwant:\n%s", strings.Join(gotLines, "\n"), strings.Join(wantLines, "\n"))
	}
}

// layerFor returns the found layer with path.
func layerFor(t *testing.T, res *Result, path string) Layer {
	t.Helper()
	for _, l := range res.found {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no layer for %s in:\n%s", path, found(res))
	return Layer{}
}

func hasWarning(ws []Warning, code WarningCode) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

func wantCode(t *testing.T, err error, code Code) {
	t.Helper()
	if got := CodeOf(err); got != code {
		t.Errorf("CodeOf(%v) = %d, want %d", err, got, code)
	}
}

func asErr[T error](err error) (T, bool) {
	var target T
	ok := errors.As(err, &target)
	return target, ok
}
