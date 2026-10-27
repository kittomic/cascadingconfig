package toml_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
)

func decode(src string) (cc.Document, error) {
	return cc.DecodeDocument(toml.Decoder(), "/etc/myapp/config.toml", []byte(src))
}

func mustDecode(t *testing.T, src string) cc.Tree {
	t.Helper()
	doc, err := decode(src)
	if err != nil {
		t.Fatalf("Decode(%q): %v", src, err)
	}
	return doc.Tree
}

func parseErrorOf(t *testing.T, src string) *cc.ParseError {
	t.Helper()
	_, err := decode(src)
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("Decode(%q) = %v, want a *ParseError", src, err)
	}
	return pe
}

// wantParseError checks that decoding src fails with a *ParseError.
func wantParseError(t *testing.T, src string) {
	t.Helper()
	parseErrorOf(t, src) //nolint:errcheck // the check is the call itself
}

// DL-172: the decoder's format, extension and media type.
func TestDecoderMetadata(t *testing.T) {
	t.Parallel()
	d := toml.Decoder()
	if d.Format() != "toml" || !reflect.DeepEqual(d.Extensions(), []string{"toml"}) ||
		!reflect.DeepEqual(d.MediaTypes(), []string{"application/toml"}) {
		t.Errorf("metadata = %q, %q, %q", d.Format(), d.Extensions(), d.MediaTypes())
	}
}

// DL-48, DL-169: offset date-times become RFC 3339 text; local date-times, dates and times their TOML text.
func TestDates(t *testing.T) {
	t.Parallel()
	tests := []struct{ src, want string }{
		{"1979-05-27T07:32:00Z", "1979-05-27T07:32:00Z"},
		{"1979-05-27T07:32:00+00:00", "1979-05-27T07:32:00Z"},
		{"1979-05-27 07:32:00Z", "1979-05-27T07:32:00Z"},
		{"1979-05-27T00:32:00.999999-07:00", "1979-05-27T00:32:00.999999-07:00"},
		{"1979-05-27T07:32:00.500Z", "1979-05-27T07:32:00.5Z"},
		{"1979-05-27T07:32:00", "1979-05-27T07:32:00"},
		{"1979-05-27T07:32:00.25", "1979-05-27T07:32:00.25"},
		{"1979-05-27", "1979-05-27"},
		{"07:32:00", "07:32:00"},
		{"07:32:00.999", "07:32:00.999"},
		{"07:32", "07:32:00"},
	}
	for _, tc := range tests {
		if got := mustDecode(t, "v = "+tc.src)["v"]; got != tc.want {
			t.Errorf("v = %s gave %#v, want %q", tc.src, got, tc.want)
		}
	}
	got := mustDecode(t, "a = [1979-05-27, 07:32:00]\n[t]\nb = 1979-05-27T07:32:00Z\n")
	want := cc.Tree{"a": []any{"1979-05-27", "07:32:00"}, "t": map[string]any{"b": "1979-05-27T07:32:00Z"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nested dates = %#v, want %#v", got, want)
	}
}

// DL-49, api.md Tree and values: integers are int64, floats float64, and tables and arrays nest as the canonical types.
func TestCanonicalTypes(t *testing.T) {
	t.Parallel()
	got := mustDecode(t, `
int = 1
hex = 0x1F
oct = 0o17
bin = 0b11
sep = 1_000
float = 1.5
exp = 1e3
bool = true
str = "s"
arr = [1, "two", [3]]
inline = {a = 1}
[table]
b = 2
[[list]]
c = 3
[[list]]
c = 4
`)
	want := cc.Tree{
		"int": int64(1), "hex": int64(31), "oct": int64(15), "bin": int64(3), "sep": int64(1000),
		"float": 1.5, "exp": 1000.0, "bool": true, "str": "s",
		"arr": []any{int64(1), "two", []any{int64(3)}}, "inline": map[string]any{"a": int64(1)},
		"table": map[string]any{"b": int64(2)},
		"list":  []any{map[string]any{"c": int64(3)}, map[string]any{"c": int64(4)}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tree = %#v\nwant %#v", got, want)
	}
	special := mustDecode(t, "a = inf\nb = -inf\nc = nan\n")
	if f, ok := special["a"].(float64); !ok || !math.IsInf(f, 1) {
		t.Errorf("inf = %#v", special["a"])
	}
	if f, ok := special["b"].(float64); !ok || !math.IsInf(f, -1) {
		t.Errorf("-inf = %#v", special["b"])
	}
	if f, ok := special["c"].(float64); !ok || !math.IsNaN(f) {
		t.Errorf("nan = %#v", special["c"])
	}
	wantParseError(t, "a = 9223372036854775808")
}

// DL-51, DL-52: duplicate keys are *ParseErrors with their position; the TOML 1.1 syntax the pinned go-toml accepts
// parses; a byte-order mark is stripped.
func TestDialect(t *testing.T) {
	t.Parallel()
	pe := parseErrorOf(t, "a = 1\na = 2\n")
	if pe.Line != 2 || pe.Column != 1 {
		t.Errorf("duplicate key at %d:%d, want 2:1", pe.Line, pe.Column)
	}
	pe = parseErrorOf(t, "a = 1\nb = = 2\n")
	if pe.Line != 2 || pe.Column != 5 {
		t.Errorf("syntax error at %d:%d, want 2:5", pe.Line, pe.Column)
	}
	got := mustDecode(t, "a = {\n  x = 1,\n  y = 2,\n}\nb = \"\\e\"\n")
	if want := (cc.Tree{"a": map[string]any{"x": int64(1), "y": int64(2)}, "b": "\x1b"}); !reflect.DeepEqual(got, want) {
		t.Errorf("TOML 1.1 = %#v, want %#v", got, want)
	}
	if got := mustDecode(t, "\ufeffa = 1\n"); !reflect.DeepEqual(got, cc.Tree{"a": int64(1)}) {
		t.Errorf("byte-order mark: Tree = %#v", got)
	}
}

// DL-53: a file with no keys is empty; an empty table isn't.
func TestEmpty(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"", "  \n", "# only a comment\n"} {
		doc, err := decode(src)
		if err != nil || !doc.Empty || doc.Tree != nil {
			t.Errorf("Decode(%q) = %#v, %v; want empty", src, doc, err)
		}
	}
	doc, err := decode("[t]\n")
	if err != nil || doc.Empty || !reflect.DeepEqual(doc.Tree, cc.Tree{"t": map[string]any{}}) {
		t.Errorf("Decode([t]) = %#v, %v; want a non-empty tree", doc, err)
	}
}

// DL-170: messages never quote the file.
func TestErrorsDontQuoteTheFile(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"password = \"SECRET-one\"\npassword = \"SECRET-two\"\n",
		"token = \"SECRET-three\" trailing\n",
		"a = SECRET-four\n",
	} {
		_, err := decode(src)
		var pe *cc.ParseError
		if !errors.As(err, &pe) || pe.Line == 0 {
			t.Errorf("Decode(%q) = %v, want a *ParseError with its position", src, err)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("the message quotes the file: %q", err)
		}
	}
}

// DL-171: whatever the input, the decoder never panics and only produces canonical types: any error is a *ParseError.
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		"a = 1\n", "[t]\nb = [1, {c = 2}]\n", "[[l]]\nx = 1979-05-27T07:32:00Z\n", "a = 07:32\n", "a = {\n x = 1,\n}\n",
		"a = 1\na = 2\n", "a = inf\n", "\"quoted key\" = 1\n", "a.b.c = 1\n", "a = '''\nmulti\n'''\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, err := cc.DecodeDocument(toml.Decoder(), "fuzz.toml", data)
		if err == nil {
			return
		}
		var pe *cc.ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("Decode(%q) = %v, want nil or a *ParseError", data, err)
		}
	})
}
