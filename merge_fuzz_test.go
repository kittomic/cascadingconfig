package cascadingconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// FuzzMerge merges two arbitrary JSON trees under several configs and checks properties that must always hold: no
// panic; the result is canonical; the layers' own trees are unchanged; the unset key never reaches the result; and every
// key in the result has an origin whose latest step isn't an unset.
func FuzzMerge(f *testing.F) {
	f.Add(`{"a": 1, "s": {"x": [1, 2]}}`, `{"a": {"b": null}, "s": {"x": [3]}}`)
	f.Add(`{"_unset": ["a"], "a": 1}`, `{"_unset": ["a.b", ["s", "x"]], "s": {"_unset": ["y"]}}`)
	f.Add(`{"list": [{"k": 1}], "n": null}`, `{"list": null, "n": {"_unset": []}}`)
	f.Add(`{"server": 5}`, `{"server": {"_unset": ["tls"]}}`)

	f.Fuzz(func(t *testing.T, low, high string) {
		lowTree, ok1 := fuzzTree(low)
		highTree, ok2 := fuzzTree(high)
		if !ok1 || !ok2 {
			return
		}
		rs, err := newRuleSet([]mergeRule{
			{path: Path{"*"}, strategy: Append},
			{path: Path{"s", "*"}, strategy: DeepMerge},
			{path: Path{"f"}, fn: func(_ Path, lower, higher any) (any, error) { return []any{lower, higher}, nil }},
		})
		if err != nil {
			t.Fatal(err)
		}
		configs := []mergeConfig{defaultMergeConfig(), {arrays: Append, unsetKey: "_unset"}, {arrays: Prepend}}
		withRules := defaultMergeConfig()
		withRules.rules = rs
		configs = append(configs, withRules)

		for _, cfg := range configs {
			layers := []mergeLayer{
				{layer: Layer{Path: "low.json"}, tree: tree.CopyMap(lowTree)},
				{layer: Layer{Path: "high.json"}, tree: tree.CopyMap(highTree)},
			}
			res, err := merge(cfg, layers)
			if !reflect.DeepEqual(layers[0].tree, lowTree) || !reflect.DeepEqual(layers[1].tree, highTree) {
				t.Fatalf("merge changed a layer's own tree")
			}
			if err != nil {
				continue // malformed _unset, or a rule mismatch
			}
			got := res.Tree()
			if err := checkCanonical(got, nil); err != nil {
				t.Fatalf("non-canonical result: %v", err)
			}
			checkKeys(t, res, got, Path{}, cfg.unsetKey)
		}
	})
}

func fuzzTree(s string) (Tree, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil || v == nil {
		return nil, false
	}
	return canonical(v).(map[string]any), true
}

func checkKeys(t *testing.T, res *Result, table map[string]any, at Path, unsetKey string) {
	t.Helper()
	for k, v := range table {
		key := appendPath(at, k)
		if unsetKey != "" && k == unsetKey {
			t.Fatalf("the unset key reached the result at %s", key)
		}
		o, ok := OriginOf(res, key)
		if !ok || o.Kind == OriginUnset {
			t.Fatalf("key %s in the result has origin %v, %v", key, describe(o), ok)
		}
		if (v == nil) != (o.Kind == OriginNull) && o.Kind != OriginFunc {
			t.Fatalf("key %s: value %#v but origin kind %s", key, v, kindName(o.Kind))
		}
		if a, isArray := v.([]any); isArray && (o.Kind == OriginAppended || o.Kind == OriginPrepended) &&
			len(o.Elements) != len(a) {
			t.Fatalf("key %s: %d elements but %d element layers", key, len(a), len(o.Elements))
		}
		if m, isTable := v.(map[string]any); isTable {
			checkKeys(t, res, m, key, unsetKey)
		}
	}
}
