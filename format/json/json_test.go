package json_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// decode runs the JSON decoder through DecodeDocument, as the loader does.
func decode(data string) (cc.Document, error) {
	return cc.DecodeDocument(ccjson.Decoder(), "app.json", []byte(data))
}

func mustDecode(t *testing.T, data string) cc.Document {
	t.Helper()
	doc, err := decode(data)
	if err != nil {
		t.Fatalf("decode(%q) error: %v", data, err)
	}
	return doc
}

// parseError decodes data and returns the *ParseError it must fail with.
func parseError(t *testing.T, data string) *cc.ParseError {
	t.Helper()
	doc, err := decode(data)
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("decode(%q) = %#v, %v; want a *ParseError", data, doc, err)
	}
	return pe
}

// wantParseError checks that decoding data fails with a *ParseError.
func wantParseError(t *testing.T, data string) {
	t.Helper()
	_ = parseError(t, data)
}

// api.md Extension interfaces: the decoder's metadata.
func TestDecoderMetadata(t *testing.T) {
	t.Parallel()
	d := ccjson.Decoder()
	if got := d.Format(); got != "json" {
		t.Errorf("Format() = %q, want \"json\"", got)
	}
	if got := d.Extensions(); !reflect.DeepEqual(got, []string{"json"}) {
		t.Errorf("Extensions() = %q, want [\"json\"]", got)
	}
	if got := d.MediaTypes(); !slices.Contains(got, "application/json") {
		t.Errorf("MediaTypes() = %q, want it to include \"application/json\"", got)
	}
}

// DL-49, api.md Core types: values decode to the canonical types. Integers are int64 only when the literal has no
// fraction or exponent.
func TestCanonicalValues(t *testing.T) {
	t.Parallel()
	doc := mustDecode(t, `{
		"int": 42, "negative": -7, "zero": 0, "negativeZero": -0,
		"max": 9223372036854775807, "min": -9223372036854775808,
		"fraction": 1.5, "wholeFloat": 1.0, "exponent": 1e3, "negativeExponent": 2E-2,
		"string": "x", "true": true, "false": false, "null": null,
		"array": [1, "two", null, [], {}],
		"object": {"nested": {"deep": 1}}
	}`)
	want := cc.Tree{
		"int": int64(42), "negative": int64(-7), "zero": int64(0), "negativeZero": int64(0),
		"max": int64(9223372036854775807), "min": int64(-9223372036854775808),
		"fraction": 1.5, "wholeFloat": 1.0, "exponent": 1000.0, "negativeExponent": 0.02,
		"string": "x", "true": true, "false": false, "null": nil,
		"array":  []any{int64(1), "two", nil, []any{}, map[string]any{}},
		"object": map[string]any{"nested": map[string]any{"deep": int64(1)}},
	}
	if !reflect.DeepEqual(doc.Tree, want) {
		t.Errorf("Tree =\n%#v\nwant\n%#v", doc.Tree, want)
	}
	for _, key := range []string{"wholeFloat", "exponent"} {
		if _, ok := doc.Tree[key].(float64); !ok {
			t.Errorf("%s decoded as %T, want float64", key, doc.Tree[key])
		}
	}
}

// DL-49: an integer literal outside the int64 range is a parse error, as is a float outside the float64 range.
func TestNumbersOutOfRange(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"big": 9223372036854775808}`,
		`{"small": -9223372036854775809}`,
		`{"huge": 18446744073709551616}`,
		`{"float": 1e400}`,
	} {
		t.Run(data, func(t *testing.T) { wantParseError(t, data) })
	}
}

// DL-52: strict RFC 8259. Comments and trailing commas are rejected.
func TestStrictDialect(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"{\n// a comment\n\"a\": 1}",
		`{"a": 1 /* a comment */}`,
		`{"a": 1,}`,
		`{"a": [1, 2,]}`,
		`{'a': 1}`,
		`{a: 1}`,
		`{"a": NaN}`,
		`{"a": 01}`,
	} {
		t.Run(data, func(t *testing.T) { wantParseError(t, data) })
	}
}

// DL-51: a duplicate key keeps its last value and adds a warning naming the key.
func TestDuplicateKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want cc.Tree
		key  cc.Path
	}{
		{"top level", `{"a": 1, "a": 2}`, cc.Tree{"a": int64(2)}, cc.Path{"a"}},
		{"nested", `{"o": {"k": 1, "k": "two"}}`, cc.Tree{"o": map[string]any{"k": "two"}}, cc.Path{"o", "k"}},
		{
			"inside an array", `{"list": [{"k": 1, "k": 2}]}`,
			cc.Tree{"list": []any{map[string]any{"k": int64(2)}}}, nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustDecode(t, tc.data)
			if !reflect.DeepEqual(doc.Tree, tc.want) {
				t.Errorf("Tree = %#v, want %#v", doc.Tree, tc.want)
			}
			if len(doc.Warnings) != 1 {
				t.Fatalf("got %d warnings, want 1: %#v", len(doc.Warnings), doc.Warnings)
			}
			w := doc.Warnings[0]
			if w.Code != cc.WarnDuplicateKey {
				t.Errorf("warning Code = %q, want %q", w.Code, cc.WarnDuplicateKey)
			}
			if w.Path != "app.json" {
				t.Errorf("warning Path = %q, want \"app.json\"", w.Path)
			}
			if tc.key != nil && !reflect.DeepEqual(w.Key, tc.key) {
				t.Errorf("warning Key = %#v, want %#v", w.Key, tc.key)
			}
			if w.Message == "" {
				t.Errorf("warning has no message")
			}
		})
	}
}

// Distinct keys produce no warnings.
func TestNoWarningsWithoutDuplicates(t *testing.T) {
	t.Parallel()
	if doc := mustDecode(t, `{"a": 1, "b": {"a": 2}}`); len(doc.Warnings) != 0 {
		t.Errorf("got warnings %#v, want none", doc.Warnings)
	}
}

// DL-52: a leading UTF-8 byte-order mark is stripped.
func TestByteOrderMark(t *testing.T) {
	t.Parallel()
	doc := mustDecode(t, "\xEF\xBB\xBF{\"a\": 1}")
	if !reflect.DeepEqual(doc.Tree, cc.Tree{"a": int64(1)}) {
		t.Errorf("Tree = %#v, want {a: 1}", doc.Tree)
	}
}

// DL-52: input that isn't UTF-8 is a parse error, even inside a string, where encoding/json would silently replace it.
func TestInvalidUTF8(t *testing.T) {
	t.Parallel()
	wantParseError(t, "{\"a\": \"\xff\"}")
}

// DL-53: the top level must be an object.
func TestTopLevelMustBeAnObject(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`[1, 2]`, `"string"`, `42`, `true`, `[]`} {
		t.Run(data, func(t *testing.T) { wantParseError(t, data) })
	}
}

// DL-53: whitespace-only and null documents are empty. {} is not.
func TestEmptyDocuments(t *testing.T) {
	t.Parallel()
	for _, data := range []string{"", "   ", "\n\t \r\n", "null", " null \n"} {
		t.Run(data, func(t *testing.T) {
			doc := mustDecode(t, data)
			if !doc.Empty || doc.Tree != nil {
				t.Errorf("Empty, Tree = %v, %#v; want true, nil", doc.Empty, doc.Tree)
			}
		})
	}
	t.Run("{}", func(t *testing.T) {
		doc := mustDecode(t, "{}")
		if doc.Empty {
			t.Errorf("{} is Empty, want not empty")
		}
		if doc.Tree == nil || len(doc.Tree) != 0 {
			t.Errorf("Tree = %#v, want an empty, non-nil Tree", doc.Tree)
		}
	})
}

// A document must be a single JSON value: anything after it is an error.
func TestTrailingData(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`{"a": 1} {"b": 2}`, `{"a": 1} x`, `{} null`} {
		t.Run(data, func(t *testing.T) { wantParseError(t, data) })
	}
}

// A syntax error is a *ParseError with its 1-based line and column, shown in the message.
func TestSyntaxErrorPosition(t *testing.T) {
	t.Parallel()
	pe := parseError(t, "{\n  \"a\": 1,\n  \"b\": ?\n}")
	if pe.Line != 3 || pe.Column != 8 {
		t.Errorf("Line, Column = %d, %d; want 3, 8", pe.Line, pe.Column)
	}
	if pe.Format != "json" || pe.Path != "app.json" {
		t.Errorf("Format, Path = %q, %q; want \"json\", \"app.json\"", pe.Format, pe.Path)
	}
	if !strings.Contains(pe.Error(), "app.json:3:8") {
		t.Errorf("Error() = %q, want it to contain \"app.json:3:8\"", pe.Error())
	}
}
