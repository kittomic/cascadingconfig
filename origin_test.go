package cascadingconfig

import "testing"

// The origin tests write each Origin compactly: "<kind> by <file>", then the elements of an appended or prepended
// array, then the history, oldest first. See describe in merge_helpers_test.go.

// DL-91, api.md Origin: a value set by one layer, and a value overridden by later layers, with its history.
func TestOriginSet(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"port": 1, "only": {"x": 1}}`))
	wantOrigin(t, res, "port", "set by a.json")
	wantOrigin(t, res, "only", "set by a.json")
	wantOrigin(t, res, "only.x", "set by a.json")

	res = mergeOK(t, defaultMergeConfig(), ly(t,
		"a.json", `{"port": 1}`,
		"b.json", `{"port": 2}`,
		"c.json", `{"port": 3}`,
	))
	wantOrigin(t, res, "port", "set by c.json after a.json set, b.json set")
}

// DL-91: a table more than one layer contributed to is merged, attributed to the latest. Its keys keep their own
// origins. A table no later layer touched stays set by its layer.
func TestOriginMerged(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t,
		"a.json", `{"server": {"host": "h"}, "other": {"x": 1}}`,
		"b.json", `{"server": {"port": 2}}`,
	))
	wantOrigin(t, res, "server", "merged by b.json after a.json set")
	wantOrigin(t, res, "server.host", "set by a.json")
	wantOrigin(t, res, "server.port", "set by b.json")
	wantOrigin(t, res, "other", "set by a.json")
}

// DL-91, design.md Per-key merge rules, "Provenance": appended and prepended arrays record each element's layer.
func TestOriginAppendedAndPrepended(t *testing.T) {
	low, high := `{"list": [1, 2]}`, `{"list": [3]}`

	res := mergeOK(t, withRules(t, rule(t, "list", Append)), ly(t, "a.json", low, "b.json", high))
	wantOrigin(t, res, "list", "appended by b.json [elements a.json a.json b.json] after a.json set")

	res = mergeOK(t, withRules(t, rule(t, "list", Prepend)), ly(t, "a.json", low, "b.json", high))
	wantOrigin(t, res, "list", "prepended by b.json [elements b.json a.json a.json] after a.json set")

	res = mergeOK(t, withRules(t, rule(t, "list", Append)), ly(t,
		"a.json", `{"list": [1]}`, "b.json", `{"list": [2]}`, "c.json", `{"list": [3]}`,
	))
	wantOrigin(t, res, "list", "appended by c.json [elements a.json b.json c.json] after a.json set, b.json appended")

	cfg := defaultMergeConfig()
	cfg.arrays = Append
	res = mergeOK(t, cfg, ly(t, "a.json", low, "b.json", high))
	wantOrigin(t, res, "list", "appended by b.json [elements a.json a.json b.json] after a.json set")
}

// DL-91: a function's result is attributed to the higher layer, with the lower one in the history.
func TestOriginFunc(t *testing.T) {
	fn := func(Path, any, any) (any, error) { return "func", nil }
	res := mergeOK(t, withRules(t, funcRule(t, "a", fn)), ly(t, "a.json", `{"a": 1}`, "b.json", `{"a": 2}`))
	wantOrigin(t, res, "a", "func by b.json after a.json set")
}

// design.md Null values: Describe shows "set to null by <file>".
func TestOriginNull(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"a": 1}`, "b.json", `{"a": null, "n": null}`))
	wantOrigin(t, res, "a", "null by b.json after a.json set")
	wantOrigin(t, res, "n", "null by b.json")
}

// DL-91, design.md Unsetting keys: an unset key reports kind unset until a higher layer sets it again. A no-op unset
// leaves no trace.
func TestOriginUnset(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"a": 1}`, "b.json", `{"_unset": ["a", "nope"]}`))
	wantOrigin(t, res, "a", "unset by b.json after a.json set")
	wantNoOrigin(t, res, "nope")

	res = mergeOK(t, defaultMergeConfig(), ly(t,
		"a.json", `{"a": 1}`,
		"b.json", `{"_unset": ["a"]}`,
		"c.json", `{"a": 3}`,
	))
	wantOrigin(t, res, "a", "set by c.json after a.json set, b.json unset")
}

// DL-91: history restarts when an enclosing table is replaced, set to null or unset. That event is in the table's own
// history, and keys that were below it have no origin until set again.
func TestOriginHistoryRestarts(t *testing.T) {
	t.Run("null", func(t *testing.T) {
		res := mergeOK(t, defaultMergeConfig(), ly(t,
			"a.json", `{"server": {"tls": {"cert": "c"}}}`,
			"b.json", `{"server": null}`,
		))
		wantOrigin(t, res, "server", "null by b.json after a.json set")
		wantNoOrigin(t, res, "server.tls")

		res = mergeOK(t, defaultMergeConfig(), ly(t,
			"a.json", `{"server": {"tls": {"cert": "c"}}}`,
			"b.json", `{"server": null}`,
			"c.json", `{"server": {"tls": {"key": "k"}}}`,
		))
		wantOrigin(t, res, "server", "set by c.json after a.json set, b.json null")
		wantOrigin(t, res, "server.tls", "set by c.json")
		wantNoOrigin(t, res, "server.tls.cert")
	})

	t.Run("unset", func(t *testing.T) {
		res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"server": {"port": 1}}`, "b.json", `{"_unset": ["server"]}`))
		wantOrigin(t, res, "server", "unset by b.json after a.json set")
		wantNoOrigin(t, res, "server.port")
	})

	t.Run("Replace rule", func(t *testing.T) {
		res := mergeOK(t, withRules(t, rule(t, "server", Replace)), ly(t,
			"a.json", `{"server": {"host": "h", "port": 1}}`,
			"b.json", `{"server": {"port": 2}}`,
		))
		wantOrigin(t, res, "server", "set by b.json after a.json set")
		wantOrigin(t, res, "server.port", "set by b.json")
		wantNoOrigin(t, res, "server.host")
	})

	t.Run("a table over a scalar", func(t *testing.T) {
		res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"a": 5}`, "b.json", `{"a": {"x": 1}}`))
		wantOrigin(t, res, "a", "set by b.json after a.json set")
		wantOrigin(t, res, "a.x", "set by b.json")
	})
}

// DL-91: OriginOf is false for keys never set, the root, and path strings that don't parse.
func TestOriginNotFound(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"a": {"b": 1}}`))
	for _, key := range []string{"nope", "a.nope", "a.b.c", "", "a..b"} {
		wantNoOrigin(t, res, key)
	}
}

// api.md Path: OriginOf takes either path form.
func TestOriginPathForms(t *testing.T) {
	res := mergeOK(t, defaultMergeConfig(), ly(t, "a.json", `{"plugins": {"foo.bar": 1}}`))
	wantOrigin(t, res, `plugins."foo.bar"`, "set by a.json")
	if o, ok := OriginOf(res, Path{"plugins", "foo.bar"}); !ok || describe(o) != "set by a.json" {
		t.Errorf("OriginOf(Path{plugins, foo.bar}) = %q, %v; want \"set by a.json\"", describe(o), ok)
	}
}
