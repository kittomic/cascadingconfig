package cascadingconfig_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// The M7 tests use only the public API, as the M5 tests do. Most of them load a project scope over /home/me/work
// whose file is myapp.json, above a system scope over /etc/myapp, with the include key "include".

const (
	work    = "/home/me/work"
	rootCfg = work + "/myapp.json"
)

// incScopes defines and selects sys (sysFile) below proj (rootCfg), with the include key "include", flat.
var incScopes = cc.Options(
	cc.WithScope("sys", cc.FixedDir("/etc/myapp", cc.Pattern{Template: "config.json"}), cc.Trust(cc.TrustSystem)),
	cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.json"})),
	cc.WithScopes("sys", "proj"),
	cc.WithIncludeKey("include", cc.IncludeFlat),
)

// nested turns on nested includes; it goes after incScopes, which incLoad and incOK add.
var nested = cc.WithIncludeKey("include", cc.IncludeNested)

// incLoad loads files over fsys with incScopes and opts.
func incLoad(t *testing.T, fsys *cctest.FS, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	return newLoaderFS(t, fsys, nil, append([]cc.Option{incScopes}, opts...)...).Load(t.Context())
}

// incOK loads files with incScopes and opts, and fails the test on an error.
func incOK(t *testing.T, files map[string]string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := incLoad(t, cctest.NewFS(files), opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// incFails loads files with incScopes and opts, and fails the test if the load succeeds.
func incFails(t *testing.T, files map[string]string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	res, err := incLoad(t, cctest.NewFS(files), opts...)
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	if res == nil {
		t.Fatalf("Load returned a nil Result")
	}
	return res, err
}

// newLoaderFS is newLoader over an existing cctest.FS, so a test can change modes, owners and links first.
func newLoaderFS(t *testing.T, fsys *cctest.FS, env map[string]string, opts ...cc.Option) *cc.Loader {
	t.Helper()
	return newLoaderHost(t, fsys, testHost(env), opts...)
}

// newLoaderHost is newLoaderFS with a host of the test's own.
func newLoaderHost(t *testing.T, fsys *cctest.FS, h cctest.Host, opts ...cc.Option) *cc.Loader {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	base := []cc.Option{cc.WithFileSystem(fsys), cctest.WithHost(h), cc.WithDecoder(ccjson.Decoder())}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// wantTreeJSON checks that got equals want, a JSON object decoded with format/json, so integers are int64.
func wantTreeJSON(t *testing.T, got cc.Tree, want string) {
	t.Helper()
	doc, err := ccjson.Decoder().Decode([]byte(want))
	if err != nil {
		t.Fatalf("bad want %q: %v", want, err)
	}
	w := doc.Tree
	if w == nil {
		w = cc.Tree{}
	}
	if got == nil {
		got = cc.Tree{}
	}
	if !reflect.DeepEqual(got, w) {
		g, _ := json.Marshal(got)
		t.Errorf("tree = %s, want %s", g, want)
	}
}

// layerLines writes the report's layers, one per line: scope, status, path and, for included layers, "<- includer".
func layerLines(rep *cc.Report) string {
	var b strings.Builder
	for _, l := range rep.Layers {
		line := fmt.Sprintf("%s %s %s", l.Scope, l.Status, l.Path)
		if l.IncludedBy != "" {
			line += " <- " + l.IncludedBy
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// wantLayers checks the report's layers against want, one line each, as layerLines writes them.
func wantLayers(t *testing.T, rep *cc.Report, want ...string) {
	t.Helper()
	got := strings.TrimSpace(layerLines(rep))
	if w := strings.Join(want, "\n"); got != w {
		t.Errorf("layers:\n%s\nwant:\n%s", got, w)
	}
}

// layersAt returns every layer of the report with path, lowest precedence first.
func layersAt(rep *cc.Report, path string) []cc.Layer {
	var out []cc.Layer
	for _, l := range rep.Layers {
		if l.Path == path {
			out = append(out, l)
		}
	}
	return out
}

// wantErr checks that err holds an error of type T with code, whose base Path is path and whose message contains
// each of contains.
func wantErr[T interface {
	error
	Code() cc.Code
}](t *testing.T, err error, code cc.Code, path string, contains ...string) {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Fatalf("error %v (%T) holds no %T", err, err, target)
	}
	if got := target.Code(); got != code {
		t.Errorf("code = %d, want %d", got, code)
	}
	if got := basePath(target); got != path {
		t.Errorf("Path = %q, want %q (error: %v)", got, path, target)
	}
	for _, s := range contains {
		if !strings.Contains(target.Error(), s) {
			t.Errorf("error %q doesn't contain %q", target.Error(), s)
		}
	}
}

// basePath returns the Path of the BaseError that err embeds.
func basePath(err error) string { return baseField(err, "Path") }

// baseOp returns the Op of the BaseError that err embeds.
func baseOp(err error) string { return baseField(err, "Op") }

func baseField(err error, name string) string {
	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if f := v.FieldByName(name); f.IsValid() {
		return f.String()
	}
	return ""
}

// jsonNamed is format/json's decoder under another format's name and extensions, so the tests can include .toml and
// .conf files without those modules.
type jsonNamed struct {
	format string
	exts   []string
}

func (d jsonNamed) Format() string                          { return d.format }
func (d jsonNamed) Extensions() []string                    { return d.exts }
func (d jsonNamed) MediaTypes() []string                    { return nil }
func (d jsonNamed) Decode(data []byte) (cc.Document, error) { return ccjson.Decoder().Decode(data) }

// calls records the arguments a callback saw, safely for concurrent loads.
type calls[T any] struct {
	mu  sync.Mutex
	got []T
}

func (c *calls[T]) add(v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, v)
}

func (c *calls[T]) all() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]T(nil), c.got...)
}
