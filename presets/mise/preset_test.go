package mise_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/presets/internal/presettest"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
}

// DL-248: the preset is named mise.Preset, and the report says so.
func TestName(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	if got := mise.Preset.Name(); got != "mise.Preset" {
		t.Errorf("Name() = %q", got)
	}
	res, err := presettest.Load(t, mise.Preset, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Describe().Preset; got != "mise.Preset" {
		t.Errorf("Report.Preset = %q", got)
	}
}

// DL-248, DL-251: TOML, YAML and JSON files all load.
func TestFormats(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	w := presettest.Work + "/"
	res, err := presettest.Load(t, mise.Preset, map[string]string{
		w + "myapp.toml": "t = 1\n", w + "myapp.yaml": "y: 2\n", w + "myapp.json": `{"j": 3}`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := res.Tree()
	if tree["t"] != int64(1) || tree["y"] != int64(2) || tree["j"] != int64(3) {
		t.Errorf("Tree() = %v, want t, y and j", tree)
	}
}

// DL-248, DL-251: http includes are fetched; git and oci references reach their resolvers, which fail to connect
// (a 502), rather than having none (a 501).
func TestRemotes(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("remote = true\n"))
	}))
	defer srv.Close()
	cache := cascadingconfig.WithIncludeCache(t.TempDir(), time.Hour)
	include := func(ref string) map[string]string {
		return map[string]string{presettest.Work + "/myapp.toml": fmt.Sprintf("include = [%q]\n", ref)}
	}
	res, err := presettest.Load(t, mise.Preset, include(srv.URL+"/a.toml"), nil, cache)
	if err != nil {
		t.Fatalf("http include: %v", err)
	}
	if res.Tree()["remote"] != true {
		t.Errorf("http include: Tree() = %v", res.Tree())
	}
	for _, ref := range []string{"git::https://127.0.0.1:1/r.git//a.toml", "oci::127.0.0.1:1/r:1//a.toml"} {
		_, err := presettest.Load(t, mise.Preset, include(ref), nil, cache)
		if code := cascadingconfig.CodeOf(err); code != cascadingconfig.CodeBadGateway {
			t.Errorf("%s: code %d, want 502 from its resolver: %v", ref, code, err)
		}
	}
}

// DL-248, DL-251: the default notifier reloads a real directory.
func TestWatchesByDefault(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	presettest.WatchReal(t, mise.Preset, "work/myapp.toml", "top = \"before\"\n", "top = \"after\"\n", "work")
}

// DL-251: the preset ranks the fixture exactly as MiseLayout does with the same decoders.
func TestOrderMatchesLayout(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	files := presettest.Fixture(t, "MiseLayout", []string{"toml", "yaml", "yml", "json"})
	env := map[string]string{"XDG_CONFIG_HOME": presettest.XDG, "MYAPP_ENV": "dev"}
	presettest.SameOrder(t, mise.Preset, cascadingconfig.MiseLayout,
		[]cascadingconfig.Option{cascadingconfig.WithDecoder(toml.Decoder()), cascadingconfig.WithDecoder(yaml.Decoder())}, files, env, 200)
}
