package cascadingconfig

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The merge tests live in the package, because the merge engine has no public entry point until Load (DL-86). They
// call merge, newRuleSet and orderSlot, and check the results only through the public Result.Tree and OriginOf.
//
// Trees are written as JSON. Core tests can't import format/json, which imports this package, so tr decodes them with
// encoding/json and converts numbers to the canonical int64 and float64.

// tr decodes a JSON object into a canonical Tree.
func tr(t *testing.T, s string) Tree {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("bad test tree %s: %v", s, err)
	}
	return canonical(v).(map[string]any)
}

func canonical(v any) any {
	switch v := v.(type) {
	case json.Number:
		if !strings.ContainsAny(string(v), ".eE") {
			if i, err := v.Int64(); err == nil {
				return i
			}
		}
		f, _ := v.Float64()
		return f
	case map[string]any:
		for k, e := range v {
			v[k] = canonical(e)
		}
		return v
	case []any:
		for i, e := range v {
			v[i] = canonical(e)
		}
		return v
	default:
		return v
	}
}

// ly builds layers from alternating file names and JSON trees, lowest precedence first.
func ly(t *testing.T, pairs ...string) []mergeLayer {
	t.Helper()
	if len(pairs)%2 != 0 {
		t.Fatalf("ly needs file and tree pairs")
	}
	var layers []mergeLayer
	for i := 0; i < len(pairs); i += 2 {
		layers = append(layers, mergeLayer{
			layer: Layer{Path: pairs[i], Scope: ScopeProject, Format: "json", Status: LayerLoaded},
			tree:  tr(t, pairs[i+1]),
		})
	}
	return layers
}

func mustPath(t *testing.T, s string) Path {
	t.Helper()
	p, err := ParsePath(s)
	if err != nil {
		t.Fatalf("bad test path %q: %v", s, err)
	}
	return p
}

func rule(t *testing.T, path string, s MergeStrategy) mergeRule {
	t.Helper()
	return mergeRule{path: mustPath(t, path), strategy: s}
}

func funcRule(t *testing.T, path string, f MergeFunc) mergeRule {
	t.Helper()
	return mergeRule{path: mustPath(t, path), fn: f}
}

// withRules returns the default config with rules, which must be valid.
func withRules(t *testing.T, rules ...mergeRule) mergeConfig {
	t.Helper()
	rs, err := newRuleSet(rules)
	if err != nil {
		t.Fatalf("newRuleSet: %v", err)
	}
	cfg := defaultMergeConfig()
	cfg.rules = rs
	return cfg
}

func mergeOK(t *testing.T, cfg mergeConfig, layers []mergeLayer) *Result {
	t.Helper()
	res, err := merge(cfg, layers)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res == nil {
		t.Fatalf("merge returned a nil Result")
	}
	return res
}

func mergeFails(t *testing.T, cfg mergeConfig, layers []mergeLayer) error {
	t.Helper()
	_, err := merge(cfg, layers)
	if err == nil {
		t.Fatalf("merge succeeded, want an error")
	}
	return err
}

func wantTree(t *testing.T, res *Result, want string) {
	t.Helper()
	if got := res.Tree(); !reflect.DeepEqual(got, tr(t, want)) {
		t.Errorf("Tree() =\n%#v\nwant\n%s", got, want)
	}
}

// wantParseErrorIn checks that err is a *ParseError for file.
func wantParseErrorIn(t *testing.T, err error, file string) {
	t.Helper()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a *ParseError", err)
	}
	if pe.Path != file {
		t.Errorf("ParseError Path = %q, want %q", pe.Path, file)
	}
}

var originKindNames = map[OriginKind]string{
	OriginSet:       "set",
	OriginMerged:    "merged",
	OriginAppended:  "appended",
	OriginPrepended: "prepended",
	OriginNull:      "null",
	OriginFunc:      "func",
	OriginUnset:     "unset",
}

func kindName(k OriginKind) string {
	if name, ok := originKindNames[k]; ok {
		return name
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// describe writes an Origin compactly, for comparison in tests:
//
//	appended by c.json [elements a.json b.json c.json] after a.json set, b.json appended
func describe(o Origin) string {
	var b strings.Builder
	b.WriteString(kindName(o.Kind) + " by " + o.Layer.Path)
	if len(o.Elements) > 0 {
		paths := make([]string, len(o.Elements))
		for i, l := range o.Elements {
			paths[i] = l.Path
		}
		b.WriteString(" [elements " + strings.Join(paths, " ") + "]")
	}
	if len(o.History) > 0 {
		steps := make([]string, len(o.History))
		for i, s := range o.History {
			steps[i] = s.Layer.Path + " " + kindName(s.Kind)
		}
		b.WriteString(" after " + strings.Join(steps, ", "))
	}
	return b.String()
}

func wantOrigin(t *testing.T, res *Result, key, want string) {
	t.Helper()
	o, ok := OriginOf(res, key)
	if !ok {
		t.Errorf("OriginOf(%q) = false, want %q", key, want)
		return
	}
	if got := describe(o); got != want {
		t.Errorf("OriginOf(%q) = %q, want %q", key, got, want)
	}
}

func wantNoOrigin(t *testing.T, res *Result, key string) {
	t.Helper()
	if o, ok := OriginOf(res, key); ok {
		t.Errorf("OriginOf(%q) = %q, want false", key, describe(o))
	}
}
