package cascadingconfig_test

import (
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// DL-154: first-match resolution chooses between files only. A Values scope above the chosen file still merges, and the
// defaults sit below it; with no file, both still apply.
func TestFirstMatchKeepsValueLayers(t *testing.T) {
	t.Parallel()
	load := func(files map[string]string) cc.Tree {
		t.Helper()
		l, err := cc.NewWithPreset("myapp", cc.CosmiconfigLayout,
			cc.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(testHost(nil)),
			cc.WithDefaults(map[string]any{"b": "default", "c": "default"}),
			cc.WithScope("flags", cc.Values(cc.Tree{"a": "flag"})),
			cc.WithScopes(cc.ScopeProject, "meta", "flags"),
		)
		if err != nil {
			t.Fatalf("NewWithPreset: %v", err)
		}
		res, err := l.Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return res.Tree()
	}

	got := load(map[string]string{
		"/home/me/work/.myapprc.json":        `{"a": "file", "b": "file"}`,
		"/home/me/work/.config/myapprc.json": `{"a": "lower file", "d": "lower file"}`,
	})
	want := cc.Tree{"a": "flag", "b": "file", "c": "default"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with a file, Tree = %v, want %v", got, want)
	}

	got = load(nil)
	want = cc.Tree{"a": "flag", "b": "default", "c": "default"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("without a file, Tree = %v, want %v", got, want)
	}
}
