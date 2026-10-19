package json_test

import (
	"errors"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// Plan M1: the JSON decoder never panics, and only ever returns canonical value types or a *ParseError.
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		``, `null`, `{}`, `[]`, `{"a": 1}`, `{"a": 1, "a": 2}`, `{"a": [1, 2.5, "x", null, true, {"b": {}}]}`,
		`{"big": 9223372036854775808}`, `{"f": 1e400}`, "\xEF\xBB\xBF{}", "{\"a\": \"\xff\"}", `{"a": 1,}`, `{"a"`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := cc.DecodeDocument(ccjson.Decoder(), "fuzz.json", data)
		if err != nil {
			if !errors.Is(err, cc.ErrParse) {
				t.Fatalf("error %v is not a parse error", err)
			}
			return
		}
		if doc.Empty {
			if doc.Tree != nil {
				t.Fatalf("an empty document has a tree: %#v", doc.Tree)
			}
			return
		}
		if doc.Tree == nil {
			t.Fatalf("a non-empty document has no tree")
		}
		checkCanonical(t, doc.Tree)
	})
}

// checkCanonical fails the test for any value that isn't one of the canonical types documented on cc.Tree.
func checkCanonical(t *testing.T, v any) {
	t.Helper()
	switch v := v.(type) {
	case nil, bool, int64, float64, string:
	case []any:
		for _, e := range v {
			checkCanonical(t, e)
		}
	case map[string]any:
		for _, e := range v {
			checkCanonical(t, e)
		}
	default:
		t.Fatalf("non-canonical type %T", v)
	}
}
