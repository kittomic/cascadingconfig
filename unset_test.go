package cascadingconfig

import "testing"

// unsetCase merges lower then higher with the default config and checks the tree.
type unsetCase struct {
	name          string
	lower, higher string
	want          string
}

func runUnsetCases(t *testing.T, cfg mergeConfig, tests []unsetCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mergeOK(t, cfg, ly(t, "low.json", tc.lower, "high.json", tc.higher))
			wantTree(t, res, tc.want)
		})
	}
}

// DL-11, design.md Unsetting keys: the documented example. The layer unsets proxy and server.tls, then its own values
// merge on top, so the rest of server is still inherited.
func TestUnset(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), ly(t,
		"low.json", `{"proxy": "p", "server": {"host": "h", "tls": {"cert": "c"}}}`,
		"high.json", `{"_unset": ["proxy", "server.tls"], "server": {"port": 8080}}`,
	))
	wantTree(t, res, `{"server": {"host": "h", "port": 8080}}`)
}

// design.md Unsetting keys, "Value and placement": dotted strings, lists of segments for keys with dots, quoted
// segments, and _unset in nested tables, relative to the table that contains it.
func TestUnsetPathForms(t *testing.T) {
	t.Parallel()
	runUnsetCases(t, defaultMergeConfig(), []unsetCase{
		{
			"dotted string",
			`{"plugins": {"foo": 1, "bar": 2}}`, `{"_unset": ["plugins.foo"]}`, `{"plugins": {"bar": 2}}`,
		},
		{
			"list of segments",
			`{"plugins": {"foo.bar": 1, "foo": {"bar": 2}}}`, `{"_unset": [["plugins", "foo.bar"]]}`,
			`{"plugins": {"foo": {"bar": 2}}}`,
		},
		{
			"quoted segment",
			`{"plugins": {"foo.bar": 1, "foo": {"bar": 2}}}`, `{"_unset": ["plugins.\"foo.bar\""]}`,
			`{"plugins": {"foo": {"bar": 2}}}`,
		},
		{
			"in a nested table",
			`{"server": {"host": "h", "tls": {"cert": "c"}}}`, `{"server": {"_unset": ["tls"]}}`,
			`{"server": {"host": "h"}}`,
		},
		{
			"deeper, with a dotted path",
			`{"a": {"b": {"c": {"d": 1, "e": 2}}}}`, `{"a": {"b": {"_unset": ["c.d"]}}}`, `{"a": {"b": {"c": {"e": 2}}}}`,
		},
	})
}

// design.md Unsetting keys, "Merge order": removal comes before the layer's own values, so unsetting and redefining a
// key is a one-off Replace.
func TestUnsetBeforeOwnValues(t *testing.T) {
	t.Parallel()
	runUnsetCases(t, defaultMergeConfig(), []unsetCase{
		{
			"one-off replace",
			`{"server": {"host": "h", "port": 0}}`, `{"_unset": ["server"], "server": {"port": 1}}`,
			`{"server": {"port": 1}}`,
		},
		{"then null", `{"port": 1}`, `{"_unset": ["port"], "port": null}`, `{"port": null}`},
	})
}

// design.md Unsetting keys: only lower layers are affected. A higher layer can set the key again, and an unset can't
// reach up into higher layers.
func TestUnsetOnlyLowerLayers(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), ly(t,
		"a.json", `{"a": 1}`,
		"b.json", `{"_unset": ["a"]}`,
		"c.json", `{"a": 3}`,
	))
	wantTree(t, res, `{"a": 3}`)

	res = mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"_unset": ["b"]}`, "b.json", `{"b": 2}`))
	wantTree(t, res, `{"b": 2}`)
}

// design.md Unsetting keys, "Missing keys": unsetting a key no lower layer set does nothing and isn't an error, even
// through a scalar or an array.
func TestUnsetMissingKey(t *testing.T) {
	t.Parallel()
	runUnsetCases(t, defaultMergeConfig(), []unsetCase{
		{
			"missing keys",
			`{"port": 1, "list": [1], "server": {}}`,
			`{"_unset": ["nope", "server.nope", "port.x", "list.x"]}`,
			`{"port": 1, "list": [1], "server": {}}`,
		},
		{"nothing below", `{}`, `{"_unset": ["a"]}`, `{}`},
		{"an empty list", `{"a": 1}`, `{"_unset": []}`, `{"a": 1}`},
	})
}

// design.md Unsetting keys, "Arrays": unsetting removes the whole array.
func TestUnsetRemovesWholeArray(t *testing.T) {
	t.Parallel()
	runUnsetCases(t, defaultMergeConfig(), []unsetCase{
		{"array", `{"list": [1, 2], "keep": 1}`, `{"_unset": ["list"]}`, `{"keep": 1}`},
	})
}

// design.md Unsetting keys, "Never in the result": _unset is stripped at every depth, including from the lowest layer.
func TestUnsetKeyIsStripped(t *testing.T) {
	t.Parallel()
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"_unset": ["x"], "b": {"_unset": ["c"], "d": 1}}`))
	wantTree(t, res, `{"b": {"d": 1}}`)
}

// design.md Unsetting keys, "Merge rules": removal happens before rules run, and rules, including functions, only run
// when both sides still have the path.
func TestUnsetBeforeRules(t *testing.T) {
	t.Parallel()
	calls := 0
	fn := func(Path, any, any) (any, error) {
		calls++
		return "func", nil
	}
	cfg := withRules(t, funcRule(t, "a", fn), rule(t, "list", Append))
	res := mergeOK(t, cfg, ly(t,
		"low.json", `{"a": 1, "list": [1]}`,
		"high.json", `{"_unset": ["a", "list"], "a": 2, "list": [2]}`,
	))
	wantTree(t, res, `{"a": 2, "list": [2]}`)
	if calls != 0 {
		t.Errorf("the function was called %d times, want 0", calls)
	}
}

// design.md Unsetting keys, "Configuration": the key can be renamed, and _unset is then plain data.
func TestUnsetKeyRenamed(t *testing.T) {
	t.Parallel()
	cfg := defaultMergeConfig()
	cfg.unsetKey = "$unset"
	runUnsetCases(t, cfg, []unsetCase{
		{
			"renamed",
			`{"a": 1, "b": 2}`, `{"$unset": ["a"], "_unset": ["b"]}`, `{"b": 2, "_unset": ["b"]}`,
		},
	})
}

// design.md Unsetting keys, "Configuration": with unsetting turned off, _unset is plain data, even inside arrays.
func TestUnsetTurnedOff(t *testing.T) {
	t.Parallel()
	cfg := defaultMergeConfig()
	cfg.unsetKey = ""
	runUnsetCases(t, cfg, []unsetCase{
		{"top level", `{"a": 1}`, `{"_unset": ["a"]}`, `{"a": 1, "_unset": ["a"]}`},
		{"malformed is data too", `{}`, `{"_unset": 5}`, `{"_unset": 5}`},
		{"inside an array", `{}`, `{"list": [{"_unset": ["x"]}]}`, `{"list": [{"_unset": ["x"]}]}`},
	})
}

// DL-92, api.md Errors: a malformed _unset is a *ParseError naming the file.
func TestUnsetMalformed(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"_unset": 5}`,
		`{"_unset": "a"}`,
		`{"_unset": null}`,
		`{"_unset": [5]}`,
		`{"_unset": [["a", 5]]}`,
		`{"_unset": [""]}`,
		`{"_unset": [[]]}`,
		`{"_unset": ["a..b"]}`,
		`{"server": {"_unset": true}}`,
	} {
		t.Run(data, func(t *testing.T) {
			err := mergeFails(t, defaultMergeConfig(), ly(t, "low.json", `{"a": 1}`, "high.json", data))
			wantParseErrorIn(t, err, "high.json")
		})
	}
	t.Run("in the lowest layer", func(t *testing.T) {
		err := mergeFails(t, defaultMergeConfig(), ly(t, "low.json", `{"_unset": 5}`))
		wantParseErrorIn(t, err, "low.json")
	})
}

// DL-92: _unset in a table inside an array is a *ParseError, because arrays are never merged.
func TestUnsetInsideArray(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"list": [{"_unset": ["x"]}]}`,
		`{"a": [[{"b": {"_unset": ["x"]}}]]}`,
	} {
		t.Run(data, func(t *testing.T) {
			err := mergeFails(t, defaultMergeConfig(), ly(t, "low.json", `{}`, "high.json", data))
			wantParseErrorIn(t, err, "high.json")
		})
	}
}

// DL-92, design.md Unsetting keys: a table holding only _unset doesn't count as setting its key, recursively. It still
// unsets. An explicitly empty table does count.
func TestTableHoldingOnlyUnset(t *testing.T) {
	t.Parallel()
	runUnsetCases(t, defaultMergeConfig(), []unsetCase{
		{"over a scalar", `{"server": 5}`, `{"server": {"_unset": ["tls"]}}`, `{"server": 5}`},
		{"over nothing", `{}`, `{"server": {"_unset": ["tls"]}}`, `{}`},
		{"recursively", `{}`, `{"a": {"b": {"_unset": ["x"]}}}`, `{}`},
		{"still unsets", `{"a": {"b": {"x": 1, "y": 2}}}`, `{"a": {"b": {"_unset": ["x"]}}}`, `{"a": {"b": {"y": 2}}}`},
		{"an empty table counts", `{"server": 5}`, `{"server": {}}`, `{"server": {}}`},
	})

	res := mergeOK(t, defaultMergeConfig(), ly(t, "low.json", `{"server": 5}`, "high.json", `{"server": {"_unset": ["tls"]}}`))
	wantOrigin(t, res, "server", "set by low.json")
}
