package cosmiconfig_test

import (
	"runtime"
	"testing"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/presets/cosmiconfig"
	"github.com/kittomic/cascadingconfig/presets/internal/presettest"
)

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
}

// DL-248: the preset is named cosmiconfig.Preset, and the report says so.
func TestName(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	if got := cosmiconfig.Preset.Name(); got != "cosmiconfig.Preset" {
		t.Errorf("Name() = %q", got)
	}
	res, err := presettest.Load(t, cosmiconfig.Preset, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Describe().Preset; got != "cosmiconfig.Preset" {
		t.Errorf("Report.Preset = %q", got)
	}
}

// DL-248, DL-251: .myapprc is YAML, and a YAML file loads; TOML isn't searched, as in cosmiconfig.
func TestFormats(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	w := presettest.Work + "/"
	res, err := presettest.Load(t, cosmiconfig.Preset, map[string]string{w + ".myapprc": "rc: yaml\n"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tree()["rc"] != "yaml" {
		t.Errorf(".myapprc: Tree() = %v", res.Tree())
	}
	res, err = presettest.Load(t, cosmiconfig.Preset, map[string]string{w + ".myapprc.toml": "t = 1\n", w + ".myapprc.yml": "y: 2\n"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree := res.Tree(); tree["y"] != int64(2) || tree["t"] != nil {
		t.Errorf("Tree() = %v, want y from YAML and nothing from TOML", tree)
	}
}

// DL-248, DL-251: the default notifier reloads a real directory.
func TestWatchesByDefault(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	presettest.WatchReal(t, cosmiconfig.Preset, "work/.myapprc.json", `{"top": "before"}`, `{"top": "after"}`, "work")
}

// DL-251: the preset ranks the fixture exactly as CosmiconfigLayout does with the same decoders, searching globally so
// the user scope takes part.
func TestOrderMatchesLayout(t *testing.T) {
	t.Parallel()
	skipWindows(t)
	files := presettest.Fixture(t, "CosmiconfigLayout", []string{"yaml", "yml", "json"})
	global := cascadingconfig.WithSearchStrategy(cascadingconfig.SearchGlobal)
	got, err := presettest.Load(t, cosmiconfig.Preset, files, nil, global)
	if err != nil {
		t.Fatal(err)
	}
	want, err := presettest.Load(t, cascadingconfig.CosmiconfigLayout, files, nil, global, cascadingconfig.WithDecoder(yaml.Decoder()))
	if err != nil {
		t.Fatal(err)
	}
	if g, w := presettest.Order(got), presettest.Order(want); len(g) < 10 || !equal(g, w) {
		t.Errorf("cosmiconfig.Preset orders the fixture differently from CosmiconfigLayout:\n%v\n%v", g, w)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
