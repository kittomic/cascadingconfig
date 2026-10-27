package cascadingconfig

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// DL-8, design.md Merging: tables merge key by key at every depth; scalars and arrays are replaced by the higher
// layer; keys set only below survive; and without a rule, a value of another type replaces the lower one with no error
// (DL-88).
func TestDeepMerge(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), ly(t,
		"system.json", `{
			"name": "sys", "list": [1, 2],
			"server": {"host": "a", "port": 1, "tls": {"cert": "c", "key": "k"}},
			"kind": "string", "shape": {"x": 1}, "back": {"t": 1}
		}`,
		"user.json", `{
			"list": [3],
			"server": {"port": 2, "tls": {"key": "k2"}},
			"kind": {"now": "table"}, "shape": [1], "back": 5
		}`,
		"project.json", `{"server": {"tls": {"ca": "ca"}}, "extra": true}`,
	))
	wantTree(t, res, `{
		"name": "sys", "list": [3],
		"server": {"host": "a", "port": 2, "tls": {"cert": "c", "key": "k2", "ca": "ca"}},
		"kind": {"now": "table"}, "shape": [1], "back": 5, "extra": true
	}`)
}

// api.md Errors: a load that finds nothing gives an empty result, not an error.
func TestMergeNoLayers(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), nil)
	if got := res.Tree(); len(got) != 0 {
		t.Errorf("Tree() = %#v, want empty", got)
	}
}

// design.md Merging, "Changing the array default": with Append as the array default, arrays concatenate wherever no
// rule applies. An array meeting another type is still replaced, and a Replace rule beats the default.
func TestArrayMergeDefault(t *testing.T) {
	t.Parallel()
	cfg := withRules(t, rule(t, "fixed", Replace))
	cfg.arrays = Append
	res := mergeOK(t, cfg, ly(t,
		"low.json", `{"list": [1], "fixed": [1], "mixed": [1], "nested": {"list": ["a"]}}`,
		"high.json", `{"list": [2], "fixed": [2], "mixed": "x", "nested": {"list": ["b"]}}`,
	))
	wantTree(t, res, `{"list": [1, 2], "fixed": [2], "mixed": "x", "nested": {"list": ["a", "b"]}}`)
}

// DL-10, DL-87, design.md Per-key merge rules: every strategy, and a MergeFunc.
func TestMergeStrategies(t *testing.T) {
	t.Parallel()
	sum := func(_ Path, lower, higher any) (any, error) { return lower.(int64) + higher.(int64), nil }
	tests := []struct {
		name          string
		rule          mergeRule
		lower, higher string
		want          string
	}{
		{
			"DeepMerge", rule(t, "server", DeepMerge),
			`{"server": {"a": 1}}`, `{"server": {"b": 2}}`, `{"server": {"a": 1, "b": 2}}`,
		},
		{
			"Replace", rule(t, "tools.*", Replace),
			`{"tools": {"node": {"version": "18", "opts": {"x": 1}}, "go": {"version": "1.25"}}}`,
			`{"tools": {"node": {"version": "20"}}}`,
			`{"tools": {"node": {"version": "20"}, "go": {"version": "1.25"}}}`,
		},
		{"Append", rule(t, "list", Append), `{"list": [1, 2]}`, `{"list": [3]}`, `{"list": [1, 2, 3]}`},
		{"Prepend", rule(t, "list", Prepend), `{"list": [1, 2]}`, `{"list": [3]}`, `{"list": [3, 1, 2]}`},
		{"MergeFunc", funcRule(t, "count", sum), `{"count": 2}`, `{"count": 3}`, `{"count": 5}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mergeOK(t, withRules(t, tc.rule), ly(t, "low.json", tc.lower, "high.json", tc.higher))
			wantTree(t, res, tc.want)
		})
	}
}

// design.md Per-key merge rules, "Paths": * matches exactly one segment, so *.list applies to a.list but neither to the
// top-level list nor to a.b.list.
func TestWildcardMatchesOneSegment(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, withRules(t, rule(t, "*.list", Append)), ly(t,
		"low.json", `{"list": [1], "a": {"list": [1], "b": {"list": [1]}}}`,
		"high.json", `{"list": [2], "a": {"list": [2], "b": {"list": [2]}}}`,
	))
	wantTree(t, res, `{"list": [2], "a": {"list": [1, 2], "b": {"list": [2]}}}`)
}

// DL-88, design.md "Matching rules": at a given key, an exact path beats a wildcard, and fewer wildcards beat more,
// whatever order the rules were set in.
func TestMostSpecificRuleWins(t *testing.T) {
	t.Parallel()
	fn := func(Path, any, any) (any, error) { return "func", nil }
	exact := rule(t, "a.b", Prepend)
	one := rule(t, "a.*", Append)
	two := funcRule(t, "*.*", fn)
	for name, order := range map[string][]mergeRule{
		"specific first": {exact, one, two},
		"specific last":  {two, one, exact},
	} {
		t.Run(name, func(t *testing.T) {
			res := mergeOK(t, withRules(t, order...), ly(t,
				"low.json", `{"a": {"b": [1], "c": [1]}, "z": {"c": [1]}}`,
				"high.json", `{"a": {"b": [2], "c": [2]}, "z": {"c": [2]}}`,
			))
			wantTree(t, res, `{"a": {"b": [2, 1], "c": [1, 2]}, "z": {"c": "func"}}`)
		})
	}
}

// DL-88: a rule set again for the same path replaces the earlier one, whether it is a strategy or a function.
func TestSamePathReplacesRule(t *testing.T) {
	t.Parallel()
	fn := func(Path, any, any) (any, error) { return "func", nil }
	tests := []struct {
		name  string
		rules []mergeRule
		want  string
	}{
		{"strategy then strategy", []mergeRule{rule(t, "list", Append), rule(t, "list", Prepend)}, `{"list": [2, 1]}`},
		{"function then strategy", []mergeRule{funcRule(t, "list", fn), rule(t, "list", Append)}, `{"list": [1, 2]}`},
		{"strategy then function", []mergeRule{rule(t, "list", Append), funcRule(t, "list", fn)}, `{"list": "func"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mergeOK(t, withRules(t, tc.rules...), ly(t, "low.json", `{"list": [1]}`, "high.json", `{"list": [2]}`))
			wantTree(t, res, tc.want)
		})
	}
}

// DL-88, design.md "Matching rules": two rules are ambiguous when they have the same length and the same number of
// wildcards, and some key matches both. That is an *OptionError when the rules are set, naming both rules.
func TestAmbiguousRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b      string
		ambiguous bool
	}{
		{"a.*", "*.b", true},
		{"a.*.c", "a.b.*", true},
		{"x.*.*", "*.y.*", true},
		{"a.*", "a.b", false},   // different wildcard counts: the exact path wins
		{"a.*", "b.*", false},   // no key matches both
		{"*.b", "*.c", false},   // no key matches both
		{"a.*", "a.*.c", false}, // different lengths
		{"*.*", "*.*", false},   // the same path: the second replaces the first
	}
	for _, tc := range tests {
		t.Run(tc.a+" and "+tc.b, func(t *testing.T) {
			_, err := newRuleSet([]mergeRule{rule(t, tc.a, Append), rule(t, tc.b, Prepend)})
			if !tc.ambiguous {
				if err != nil {
					t.Errorf("newRuleSet error: %v, want none", err)
				}
				return
			}
			var oe *OptionError
			if !errors.As(err, &oe) {
				t.Fatalf("newRuleSet error = %v, want an *OptionError", err)
			}
			if msg := err.Error(); !strings.Contains(msg, tc.a) || !strings.Contains(msg, tc.b) {
				t.Errorf("error %q doesn't name both rules", msg)
			}
		})
	}
}

// DL-88: a rule on the root is an *OptionError, and every problem in a rule set is reported together.
func TestRootRuleAndJoinedRuleErrors(t *testing.T) {
	t.Parallel()
	_, err := newRuleSet([]mergeRule{{path: Path{}, strategy: Replace}})
	if !errors.Is(err, ErrInvalidOption) {
		t.Errorf("root rule: error = %v, want ErrInvalidOption", err)
	}

	_, err = newRuleSet([]mergeRule{
		{path: Path{}, strategy: Replace},
		rule(t, "a.*", Append), rule(t, "*.b", Append),
	})
	errs := Errors(err)
	if len(errs) != 2 {
		t.Fatalf("got %d errors, want 2 (the root rule and the ambiguity): %v", len(errs), err)
	}
	for _, e := range errs {
		if !errors.Is(e, ErrInvalidOption) {
			t.Errorf("error %v isn't an *OptionError", e)
		}
	}
}

// design.md "Matching rules": Replace and functions cover their whole subtree, so rules below them aren't consulted.
func TestRulesCoverSubtree(t *testing.T) {
	t.Parallel()
	low := `{"server": {"host": "a", "list": [1]}}`
	high := `{"server": {"list": [2]}}`

	t.Run("Replace", func(t *testing.T) {
		res := mergeOK(t, withRules(t, rule(t, "server", Replace), rule(t, "server.list", Append)),
			ly(t, "low.json", low, "high.json", high))
		wantTree(t, res, high)
	})

	t.Run("MergeFunc", func(t *testing.T) {
		var gotLower any
		fn := func(_ Path, lower, higher any) (any, error) {
			gotLower = lower
			return higher, nil
		}
		res := mergeOK(t, withRules(t, funcRule(t, "server", fn), rule(t, "server.list", Append)),
			ly(t, "low.json", low, "high.json", high))
		wantTree(t, res, high)
		if want := tr(t, low)["server"]; !reflect.DeepEqual(gotLower, want) {
			t.Errorf("the function got lower = %#v, want the whole table %#v", gotLower, want)
		}
	})
}

// DL-88, DL-90: an explicit rule that meets the wrong types is a *MergeError with the key, the higher file as Path and
// the lower file as Lower, and a message naming both files.
func TestRuleTypeMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		rule          mergeRule
		lower, higher string
		key           Path
	}{
		{"Append on a string", rule(t, "list", Append), `{"list": [1]}`, `{"list": "x"}`, Path{"list"}},
		{"Prepend on tables", rule(t, "list", Prepend), `{"list": {"a": 1}}`, `{"list": {"b": 2}}`, Path{"list"}},
		{"DeepMerge on a scalar", rule(t, "s.t", DeepMerge), `{"s": {"t": {"a": 1}}}`, `{"s": {"t": 5}}`, Path{"s", "t"}},
		{"DeepMerge on arrays", rule(t, "s", DeepMerge), `{"s": [1]}`, `{"s": [2]}`, Path{"s"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mergeFails(t, withRules(t, tc.rule), ly(t, "low.json", tc.lower, "high.json", tc.higher))
			var me *MergeError
			if !errors.As(err, &me) {
				t.Fatalf("error = %v, want a *MergeError", err)
			}
			if !reflect.DeepEqual(me.Key, tc.key) || me.Path != "high.json" || me.Lower != "low.json" {
				t.Errorf("Key, Path, Lower = %q, %q, %q; want %q, \"high.json\", \"low.json\"",
					me.Key, me.Path, me.Lower, tc.key)
			}
			if msg := err.Error(); !strings.Contains(msg, "low.json") || !strings.Contains(msg, "high.json") ||
				!strings.Contains(msg, tc.key.String()) {
				t.Errorf("error %q doesn't name both files and the key", msg)
			}
			if CodeOf(err) != CodeUnprocessable {
				t.Errorf("CodeOf = %d, want 422", CodeOf(err))
			}
		})
	}
}

// DL-90: the merge carries on after a mismatch, and every mismatch is reported.
func TestEveryMismatchIsReported(t *testing.T) {
	t.Parallel()
	err := mergeFails(t, withRules(t, rule(t, "a", Append), rule(t, "b", Append)), ly(t,
		"low.json", `{"a": [1], "b": [1]}`,
		"high.json", `{"a": 1, "b": 2}`,
	))
	errs := Errors(err)
	if len(errs) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(errs), err)
	}
	for _, e := range errs {
		if !errors.Is(e, ErrMerge) {
			t.Errorf("error %v isn't a *MergeError", e)
		}
	}
}

// DL-87, DL-89, api.md MergeFunc: a function gets the concrete path and copies of both values, runs only when both
// layers set the path, and its result is used.
func TestMergeFunc(t *testing.T) {
	t.Parallel()
	t.Run("path and copies", func(t *testing.T) {
		var gotPath Path
		fn := func(path Path, lower, higher any) (any, error) {
			gotPath = path
			lower.(map[string]any)["changed"] = true
			higher.(map[string]any)["changed"] = true
			return "result", nil
		}
		layers := ly(t, "low.json", `{"plugins": {"foo": {"a": 1}}}`, "high.json", `{"plugins": {"foo": {"b": 2}}}`)
		res := mergeOK(t, withRules(t, funcRule(t, "plugins.*", fn)), layers)
		wantTree(t, res, `{"plugins": {"foo": "result"}}`)
		if !reflect.DeepEqual(gotPath, Path{"plugins", "foo"}) {
			t.Errorf("path = %q, want plugins.foo", gotPath)
		}
		if !reflect.DeepEqual(layers[0].tree, tr(t, `{"plugins": {"foo": {"a": 1}}}`)) ||
			!reflect.DeepEqual(layers[1].tree, tr(t, `{"plugins": {"foo": {"b": 2}}}`)) {
			t.Errorf("the function changed the layers' own trees: it didn't get copies")
		}
	})

	t.Run("only when both layers set the path", func(t *testing.T) {
		calls := 0
		fn := func(Path, any, any) (any, error) {
			calls++
			return "func", nil
		}
		res := mergeOK(t, withRules(t, funcRule(t, "*", fn)), ly(t,
			"low.json", `{"lowOnly": 1}`,
			"high.json", `{"highOnly": 2}`,
		))
		wantTree(t, res, `{"lowOnly": 1, "highOnly": 2}`)
		if calls != 0 {
			t.Errorf("the function was called %d times, want 0", calls)
		}
	})
}

// DL-89: an error from a function is a *MergeError that wraps it, with the key and both files.
func TestMergeFuncError(t *testing.T) {
	t.Parallel()
	appErr := errors.New("can't combine these")
	fn := func(Path, any, any) (any, error) { return nil, appErr }
	err := mergeFails(t, withRules(t, funcRule(t, "a", fn)), ly(t, "low.json", `{"a": 1}`, "high.json", `{"a": 2}`))
	var me *MergeError
	if !errors.As(err, &me) {
		t.Fatalf("error = %v, want a *MergeError", err)
	}
	if !errors.Is(err, appErr) {
		t.Errorf("the *MergeError doesn't wrap the function's error")
	}
	if !reflect.DeepEqual(me.Key, Path{"a"}) || me.Path != "high.json" || me.Lower != "low.json" {
		t.Errorf("Key, Path, Lower = %q, %q, %q; want a, high.json, low.json", me.Key, me.Path, me.Lower)
	}
}

// DL-89: a function that returns a value outside the canonical types is a bug in the app: a *BaseError with
// CodeInternal, not a *MergeError.
func TestMergeFuncNonCanonicalResult(t *testing.T) {
	t.Parallel()
	fn := func(Path, any, any) (any, error) { return 5, nil } // int, not int64
	err := mergeFails(t, withRules(t, funcRule(t, "a", fn)), ly(t, "low.json", `{"a": 1}`, "high.json", `{"a": 2}`))
	if CodeOf(err) != CodeInternal {
		t.Errorf("CodeOf = %d, want 500", CodeOf(err))
	}
	if errors.Is(err, ErrMerge) {
		t.Errorf("error is a *MergeError, want a *BaseError")
	}
}

// api.md Concurrency and ownership: Tree returns a copy, so changing it doesn't change the result.
func TestResultTreeIsACopy(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"server": {"list": [1], "port": 1}}`))
	first := res.Tree()
	if !reflect.DeepEqual(first, tr(t, `{"server": {"list": [1], "port": 1}}`)) {
		t.Fatalf("Tree() = %#v, want the merged tree", first)
	}
	first["new"] = true
	server := first["server"].(map[string]any)
	server["port"] = int64(2)
	server["list"].([]any)[0] = "changed"
	wantTree(t, res, `{"server": {"list": [1], "port": 1}}`)
}
