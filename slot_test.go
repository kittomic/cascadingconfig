package cascadingconfig

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testDecoder is a Decoder with only metadata. orderSlot ranks by format and extension and never decodes.
type testDecoder struct {
	format string
	exts   []string
}

func (d testDecoder) Format() string                  { return d.format }
func (d testDecoder) Extensions() []string            { return d.exts }
func (d testDecoder) MediaTypes() []string            { return nil }
func (d testDecoder) Decode([]byte) (Document, error) { return Document{Empty: true}, nil }

var (
	tomlDecoder = testDecoder{"toml", []string{"toml"}}
	yamlDecoder = testDecoder{"yaml", []string{"yaml", "yml"}}
	jsonDecoder = testDecoder{"json", []string{"json"}}
	miseOrder   = []Decoder{tomlDecoder, yamlDecoder, jsonDecoder}
)

// slotFiles returns layers for files in one slot, with each Format taken from the extension.
func slotFiles(paths ...string) []Layer {
	formats := map[string]string{".toml": "toml", ".yaml": "yaml", ".yml": "yaml", ".json": "json"}
	layers := make([]Layer, len(paths))
	for i, p := range paths {
		layers[i] = Layer{Path: p, Scope: ScopeProject, Format: formats[filepath.Ext(p)]}
	}
	return layers
}

func paths(layers []Layer) []string {
	out := make([]string, len(layers))
	for i, l := range layers {
		out[i] = l.Path
	}
	return out
}

func orderOK(t *testing.T, files []Layer, order []Decoder, policy FormatConflict) []Layer {
	t.Helper()
	got, err := orderSlot(files, order, policy)
	if err != nil {
		t.Fatalf("orderSlot: %v", err)
	}
	return got
}

// DL-3, design.md File format precedence: under ConflictMerge, every file in the slot loads, lowest-ranked first, so
// the highest-ranked format is applied last.
func TestSlotMergeOrder(t *testing.T) {
	tests := []struct {
		name  string
		order []Decoder
		want  []string
	}{
		{"mise order", miseOrder, []string{"app.json", "app.yaml", "app.toml"}},
		{"another order", []Decoder{jsonDecoder, tomlDecoder, yamlDecoder}, []string{"app.yaml", "app.toml", "app.json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := orderOK(t, slotFiles("app.toml", "app.json", "app.yaml"), tc.order, ConflictMerge)
			if !reflect.DeepEqual(paths(got), tc.want) {
				t.Errorf("order = %q, want %q", paths(got), tc.want)
			}
			for _, l := range got {
				if l.Status != LayerLoaded {
					t.Errorf("%s has status %v, want LayerLoaded", l.Path, l.Status)
				}
			}
		})
	}
}

// DL-3: merged in slot order, the highest-ranked format wins where the files overlap, and each file stays its own
// layer.
func TestSlotMergeHighestWins(t *testing.T) {
	trees := map[string]string{
		"app.json": `{"port": 1, "fromJSON": true}`,
		"app.yaml": `{"port": 2}`,
		"app.toml": `{"port": 3}`,
	}
	var layers []mergeLayer
	for _, l := range orderOK(t, slotFiles("app.toml", "app.yaml", "app.json"), miseOrder, ConflictMerge) {
		layers = append(layers, mergeLayer{layer: l, tree: tr(t, trees[l.Path])})
	}
	res := mergeOK(t, defaultMergeConfig(), layers)
	wantTree(t, res, `{"port": 3, "fromJSON": true}`)
	wantOrigin(t, res, "port", "set by app.toml after app.json set, app.yaml set")
}

// design.md File format precedence: within a format, extensions rank as the decoder lists them, so .yaml is applied
// after .yml.
func TestSlotExtensionRank(t *testing.T) {
	got := orderOK(t, slotFiles("app.yaml", "app.yml"), miseOrder, ConflictMerge)
	if want := []string{"app.yml", "app.yaml"}; !reflect.DeepEqual(paths(got), want) {
		t.Errorf("order = %q, want %q", paths(got), want)
	}
}

// DL-93: under ConflictFirst, only the highest-ranked file loads. The others are listed first, as LayerIgnored, each
// with a reason and a WarnFormatConflict warning.
func TestSlotFirst(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		order   []string
		ignored int
	}{
		{"three formats", []string{"app.json", "app.toml", "app.yaml"}, []string{"app.json", "app.yaml", "app.toml"}, 2},
		{"two extensions", []string{"app.yaml", "app.yml"}, []string{"app.yml", "app.yaml"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := orderOK(t, slotFiles(tc.files...), miseOrder, ConflictFirst)
			if !reflect.DeepEqual(paths(got), tc.order) {
				t.Fatalf("order = %q, want %q", paths(got), tc.order)
			}
			for i, l := range got {
				if i < tc.ignored {
					if l.Status != LayerIgnored || l.Reason == "" {
						t.Errorf("%s: Status, Reason = %v, %q; want LayerIgnored and a reason", l.Path, l.Status, l.Reason)
					}
					if len(l.Warnings) != 1 || l.Warnings[0].Code != WarnFormatConflict || l.Warnings[0].Path != l.Path {
						t.Errorf("%s: warnings = %#v, want one WarnFormatConflict for the file", l.Path, l.Warnings)
					}
					continue
				}
				if l.Status != LayerLoaded || len(l.Warnings) != 0 {
					t.Errorf("%s: Status, Warnings = %v, %#v; want LayerLoaded and none", l.Path, l.Status, l.Warnings)
				}
			}
		})
	}
}

// DL-93: under ConflictFail, a second file in the slot, even of the same format, is a *ConflictError. Path is the
// highest-ranked file, and Files lists them all, highest-ranked first.
func TestSlotFail(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"two formats", []string{"app.yaml", "app.toml"}, []string{"app.toml", "app.yaml"}},
		{"two extensions", []string{"app.yml", "app.yaml"}, []string{"app.yaml", "app.yml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := orderSlot(slotFiles(tc.files...), miseOrder, ConflictFail)
			var ce *ConflictError
			if !errors.As(err, &ce) {
				t.Fatalf("error = %v, want a *ConflictError", err)
			}
			if ce.Path != tc.want[0] || !reflect.DeepEqual(ce.Files, tc.want) {
				t.Errorf("Path, Files = %q, %q; want %q, %q", ce.Path, ce.Files, tc.want[0], tc.want)
			}
			for _, f := range tc.want {
				if !strings.Contains(err.Error(), f) {
					t.Errorf("error %q doesn't name %s", err.Error(), f)
				}
			}
			if CodeOf(err) != CodeConflict {
				t.Errorf("CodeOf = %d, want 409", CodeOf(err))
			}
		})
	}
}

// A slot with one file loads it under every policy.
func TestSlotSingleFile(t *testing.T) {
	for _, policy := range []FormatConflict{ConflictMerge, ConflictFirst, ConflictFail} {
		got := orderOK(t, slotFiles("app.yaml"), miseOrder, policy)
		if len(got) != 1 || got[0].Path != "app.yaml" || got[0].Status != LayerLoaded || len(got[0].Warnings) != 0 {
			t.Errorf("policy %d: got %#v, want app.yaml, loaded, with no warnings", policy, got)
		}
	}
}
