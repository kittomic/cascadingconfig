package cascadingconfig

import (
	"reflect"
	"testing"
)

// design.md Null values: a higher null replaces the lower value whatever its type, and stays in the result as a key
// with a nil value.
func TestNullReplacesAnyType(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t,
		"low.json", `{"s": 1, "a": [1], "t": {"x": 1}}`,
		"high.json", `{"s": null, "a": null, "t": null}`,
	))
	wantTree(t, res, `{"s": null, "a": null, "t": null}`)
	if v, ok := res.Tree()["t"]; !ok || v != nil {
		t.Errorf("t = %#v, %v; want a key with a nil value", v, ok)
	}
}

// design.md Null values: a higher value replaces a lower null; a table over a null is taken as it is. This holds with
// the Append array default too.
func TestNullIsReplacedByAnyValue(t *testing.T) {
	cfg := defaultMergeConfig()
	cfg.arrays = Append
	res := mergeOK(t, cfg, ly(t,
		"low.json", `{"s": null, "a": null, "t": null}`,
		"high.json", `{"s": 1, "a": [1], "t": {"x": 1}}`,
	))
	wantTree(t, res, `{"s": 1, "a": [1], "t": {"x": 1}}`)
}

// design.md Null values: null is never a type mismatch. Whatever rule matches, DeepMerge, Append or Prepend, the merge
// is a Replace and the higher value wins.
func TestNullIsNeverAMismatch(t *testing.T) {
	cfg := withRules(t, rule(t, "a", Append), rule(t, "p", Prepend), rule(t, "t", DeepMerge), rule(t, "u", DeepMerge))
	res := mergeOK(t, cfg, ly(t,
		"low.json", `{"a": null, "p": [1], "t": null, "u": {"x": 1}}`,
		"high.json", `{"a": [1], "p": null, "t": {"x": 1}, "u": null}`,
	))
	wantTree(t, res, `{"a": [1], "p": null, "t": {"x": 1}, "u": null}`)
}

// design.md Null values: a MergeFunc is the exception. It receives nil and decides for itself.
func TestMergeFuncReceivesNull(t *testing.T) {
	type call struct{ lower, higher any }
	var calls []call
	fn := func(_ Path, lower, higher any) (any, error) {
		calls = append(calls, call{lower, higher})
		return "func", nil
	}
	res := mergeOK(t, withRules(t, funcRule(t, "*", fn)), ly(t,
		"low.json", `{"a": null, "b": 1}`,
		"high.json", `{"a": 1, "b": null}`,
	))
	wantTree(t, res, `{"a": "func", "b": "func"}`)
	want := map[call]bool{{nil, int64(1)}: true, {int64(1), nil}: true}
	got := map[call]bool{}
	for _, c := range calls {
		got[c] = true
	}
	if !reflect.DeepEqual(got, want) || len(calls) != 2 {
		t.Errorf("calls = %v, want (nil, 1) and (1, nil)", calls)
	}
}
