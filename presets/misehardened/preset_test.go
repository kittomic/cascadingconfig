package misehardened_test

import (
	"runtime"
	"testing"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/presets/internal/presettest"
	"github.com/kittomic/cascadingconfig/presets/misehardened"
)

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
}

// DL-248: the preset is named misehardened.Preset, and the report says so.
func TestName(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	if got := misehardened.Preset.Name(); got != "misehardened.Preset" {
		t.Errorf("Name() = %q", got)
	}
	res, err := presettest.Load(t, misehardened.Preset, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Describe().Preset; got != "misehardened.Preset" {
		t.Errorf("Report.Preset = %q", got)
	}
}

// DL-248, DL-251: TOML, YAML and JSON files all load.
func TestFormats(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	w := presettest.Work + "/"
	res, err := presettest.Load(t, misehardened.Preset, map[string]string{
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

// DL-205, DL-251: a remote include is a 501: the preset registers no resolvers.
func TestNoRemotes(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	_, err := presettest.Load(t, misehardened.Preset, map[string]string{
		presettest.Work + "/myapp.toml": "include = [\"https://config.example.com/a.toml\"]\n",
	}, nil)
	if code := cascadingconfig.CodeOf(err); code != cascadingconfig.CodeNotImplemented {
		t.Errorf("code %d, want 501: %v", code, err)
	}
}

// DL-248, DL-251: the default notifier reloads a real directory.
func TestWatchesByDefault(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	presettest.WatchReal(t, misehardened.Preset, "work/myapp.toml", "top = \"before\"\n", "top = \"after\"\n", "work")
}

// DL-251: the preset ranks the fixture exactly as MiseHardenedLayout does with the same decoders.
func TestOrderMatchesLayout(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	files := presettest.Fixture(t, "MiseLayout", []string{"toml", "yaml", "yml", "json"})
	env := map[string]string{"XDG_CONFIG_HOME": presettest.XDG, "MYAPP_ENV": "dev"}
	presettest.SameOrder(t, misehardened.Preset, cascadingconfig.MiseHardenedLayout,
		[]cascadingconfig.Option{cascadingconfig.WithDecoder(toml.Decoder()), cascadingconfig.WithDecoder(yaml.Decoder())}, files, env, 200)
}
