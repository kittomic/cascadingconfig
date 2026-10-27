package yaml_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/yaml"
)

// decode runs the YAML decoder through DecodeDocument, as the loader does.
func decode(t *testing.T, src string, opts ...yaml.Option) (cc.Document, error) {
	t.Helper()
	return cc.DecodeDocument(yaml.Decoder(opts...), "/etc/myapp/config.yaml", []byte(src))
}

func mustDecode(t *testing.T, src string, opts ...yaml.Option) cc.Tree {
	t.Helper()
	doc, err := decode(t, src, opts...)
	if err != nil {
		t.Fatalf("Decode(%q): %v", src, err)
	}
	return doc.Tree
}

// parseError checks that err is a *ParseError, and returns it.
func parseError(t *testing.T, src string, err error) *cc.ParseError {
	t.Helper()
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("Decode(%q) = %v, want a *ParseError", src, err)
	}
	return pe
}

func parseErrorOf(t *testing.T, src string, contains string, opts ...yaml.Option) *cc.ParseError {
	t.Helper()
	_, err := decode(t, src, opts...)
	pe := parseError(t, src, err)
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Errorf("Decode(%q) = %q, want it to mention %q", src, err, contains)
	}
	return pe
}

// wantParseError checks that decoding src fails with a *ParseError that mentions contains.
func wantParseError(t *testing.T, src string, contains string, opts ...yaml.Option) {
	t.Helper()
	parseErrorOf(t, src, contains, opts...) //nolint:errcheck // the check is the call itself
}

// DL-172: the decoder's format, extensions and media types.
func TestDecoderMetadata(t *testing.T) {
	t.Parallel()
	d := yaml.Decoder()
	if d.Format() != "yaml" || !reflect.DeepEqual(d.Extensions(), []string{"yaml", "yml"}) ||
		!reflect.DeepEqual(d.MediaTypes(), []string{"application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml"}) {
		t.Errorf("metadata = %q, %q, %q", d.Format(), d.Extensions(), d.MediaTypes())
	}
}

// DL-50, DL-163: unquoted values get their types from the YAML 1.2 core schema. Quoted values are strings.
func TestCoreSchemaValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want any
	}{
		{"yes", "yes"}, {"no", "no"}, {"on", "on"}, {"off", "off"}, {"y", "y"}, {"n", "n"},
		{"true", true}, {"True", true}, {"TRUE", true}, {"false", false}, {"False", false}, {"FALSE", false},
		{"tRUE", "tRUE"},
		{"null", nil}, {"Null", nil}, {"NULL", nil}, {"~", nil}, {"", nil}, {"nULL", "nULL"},
		{"0", int64(0)}, {"17", int64(17)}, {"-17", int64(-17)}, {"+17", int64(17)},
		{"017", int64(17)}, {"0o17", int64(15)}, {"0x1F", int64(31)}, {"0x1f", int64(31)},
		{"9223372036854775807", int64(math.MaxInt64)}, {"-9223372036854775808", int64(math.MinInt64)},
		{"1.5", 1.5}, {"-1.5", -1.5}, {".5", 0.5}, {"1.", 1.0}, {"1e3", 1000.0}, {"1.5E-3", 0.0015}, {"+1e3", 1000.0},
		{"0b11", "0b11"}, {"1_000", "1_000"}, {"0x", "0x"}, {"1e", "1e"}, {"1.2.3", "1.2.3"},
		{"'1e3'", "1e3"}, {`"017"`, "017"}, {"'true'", "true"}, {`"~"`, "~"}, {"''", ""},
		{"12:30", "12:30"}, {"hello world", "hello world"}, {"|\n  line one\n  line two\n", "line one\nline two\n"},
		{">-\n  folded\n  text\n", "folded text"},
	}
	for _, tc := range tests {
		got := mustDecode(t, "v: "+tc.src)["v"]
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("v: %s = %#v, want %#v", tc.src, got, tc.want)
		}
	}
	special := mustDecode(t, "a: .inf\nb: +.inf\nc: -.Inf\nd: .NaN\ne: .nan\nf: .INF\n")
	for _, k := range []string{"a", "b", "f"} {
		if f, ok := special[k].(float64); !ok || !math.IsInf(f, 1) {
			t.Errorf("%s = %#v, want +Inf", k, special[k])
		}
	}
	if f, ok := special["c"].(float64); !ok || !math.IsInf(f, -1) {
		t.Errorf("c = %#v, want -Inf", special["c"])
	}
	for _, k := range []string{"d", "e"} {
		if f, ok := special[k].(float64); !ok || !math.IsNaN(f) {
			t.Errorf("%s = %#v, want NaN", k, special[k])
		}
	}
}

// DL-163: WithLegacyNumbers accepts the YAML 1.1 integer forms; without it they keep their core schema meaning.
func TestLegacyNumbers(t *testing.T) {
	t.Parallel()
	src := "a: 017\nb: 0b11\nc: 1_000\nd: 0o17\ne: 0x1F\nf: 1e3\ng: -017\n"
	want := cc.Tree{"a": int64(15), "b": int64(3), "c": int64(1000), "d": int64(15), "e": int64(31), "f": 1000.0, "g": int64(-15)}
	if got := mustDecode(t, src, yaml.WithLegacyNumbers()); !reflect.DeepEqual(got, want) {
		t.Errorf("with WithLegacyNumbers: %#v\nwant %#v", got, want)
	}
	core := cc.Tree{"a": int64(17), "b": "0b11", "c": "1_000", "d": int64(15), "e": int64(31), "f": 1000.0, "g": int64(-17)}
	if got := mustDecode(t, src); !reflect.DeepEqual(got, core) {
		t.Errorf("without: %#v\nwant %#v", got, core)
	}
}

// DL-49: an integer outside the int64 range is a *ParseError, in every form.
func TestIntegerRange(t *testing.T) {
	t.Parallel()
	if got := mustDecode(t, "v: 9223372036854775807\nw: -9223372036854775808\n"); got["v"] != int64(math.MaxInt64) || got["w"] != int64(math.MinInt64) {
		t.Errorf("the int64 limits = %#v", got)
	}
	for _, v := range []string{"9223372036854775808", "-9223372036854775809", "18446744073709551615", "0x8000000000000000", "0o1000000000000000000000"} {
		wantParseError(t, "v: "+v, "")
	}
	wantParseError(t, "v: 1_000_000_000_000_000_000_000", "", yaml.WithLegacyNumbers())
}

// DL-165: the core schema has no timestamp type, so dates stay strings exactly as written.
func TestDatesStayStrings(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"2001-12-14", "2001-12-14t21:59:43.10-05:00", "2001-12-14 21:59:43.10 -5", "2001-12-14T21:59:43Z"} {
		if got := mustDecode(t, "v: "+v)["v"]; got != v {
			t.Errorf("v: %s = %#v, want the string as written", v, got)
		}
	}
}

// DL-164: the core schema's tags force their type; any other tag is a *ParseError naming it.
func TestTags(t *testing.T) {
	t.Parallel()
	ok := []struct {
		src  string
		want any
	}{
		{"!!str 1", "1"}, {"!!str true", "true"}, {"!!int '12'", int64(12)}, {"!!int 0x1F", int64(31)},
		{"!!float 1", 1.0}, {"!!float '1.5'", 1.5}, {"!!bool 'true'", true}, {"!!null ~", nil}, {"!!null ''", nil},
		{"!!map {a: 1}", map[string]any{"a": int64(1)}}, {"!!seq [1]", []any{int64(1)}}, {"! 12", "12"},
		{"!<tag:yaml.org,2002:str> 5", "5"},
	}
	for _, tc := range ok {
		got := mustDecode(t, "v: "+tc.src)["v"]
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("v: %s = %#v, want %#v", tc.src, got, tc.want)
		}
	}
	bad := []struct{ src, contains string }{
		{"!!int abc", "!!int"}, {"!!int 1.5", "!!int"}, {"!!bool yes", "!!bool"}, {"!!float x", "!!float"},
		{"!!null 5", "!!null"},
		// goccy's parser rejects these before the tag is checked, so its message doesn't name the tag.
		{"!!map 5", ""}, {"!!seq {a: 1}", ""},
		{"!!binary aGVsbG8=", "!!binary"}, {"!!timestamp 2001-12-14", "!!timestamp"}, {"!!set {a, b}", "!!set"},
		{"!!omap [{a: 1}]", "!!omap"}, {"!foo bar", "!foo"}, {"!<tag:example.com,2000:x> 1", "tag:example.com,2000:x"},
	}
	for _, tc := range bad {
		wantParseError(t, "v: "+tc.src, tc.contains)
	}
}

// DL-166: keys are resolved by the core schema, then printed; keys that clash, before or after printing, and tables or
// sequences as keys, are *ParseErrors.
func TestKeys(t *testing.T) {
	t.Parallel()
	got := mustDecode(t, "0x1F: a\n~: b\ntrue: c\n1.5: d\n017: e\n'quoted': f\n1e3: g\n")
	want := cc.Tree{"31": "a", "null": "b", "true": "c", "1.5": "d", "17": "e", "quoted": "f", "1000": "g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %#v\nwant %#v", got, want)
	}
	if got := mustDecode(t, "017: a\n", yaml.WithLegacyNumbers()); !reflect.DeepEqual(got, cc.Tree{"15": "a"}) {
		t.Errorf("with WithLegacyNumbers, keys = %#v, want 15", got)
	}
	for _, src := range []string{
		"a: 1\na: 2\n",
		"1: a\n0o1: b\n",
		"1: a\n'1': b\n",
		"true: a\n'true': b\n",
		"? [1, 2]\n: a\n",
		"? {a: 1}\n: b\n",
	} {
		wantParseError(t, src, "")
	}
}

// DL-167: merge keys merge tables; the table's own keys win, then earlier entries of a list. A merge of anything but a
// table is a *ParseError.
func TestMergeKeys(t *testing.T) {
	t.Parallel()
	got := mustDecode(t, `
a: &a {x: 1, y: 1}
b: &b {x: 2, z: 2}
one:
  <<: *a
  w: 0
list:
  <<: [*a, *b]
own:
  <<: [*a, *b]
  x: 3
nested:
  inner:
    <<: *a
`)
	want := cc.Tree{
		"a":      map[string]any{"x": int64(1), "y": int64(1)},
		"b":      map[string]any{"x": int64(2), "z": int64(2)},
		"one":    map[string]any{"x": int64(1), "y": int64(1), "w": int64(0)},
		"list":   map[string]any{"x": int64(1), "y": int64(1), "z": int64(2)},
		"own":    map[string]any{"x": int64(3), "y": int64(1), "z": int64(2)},
		"nested": map[string]any{"inner": map[string]any{"x": int64(1), "y": int64(1)}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %#v\nwant %#v", got, want)
	}
	for _, src := range []string{
		"a: &a 5\nc:\n  <<: *a\n",
		"a: &a [1]\nc:\n  <<: *a\n",
		"a: &a {x: 1}\nc:\n  <<: [*a, 5]\n",
		"c:\n  <<: 5\n",
	} {
		wantParseError(t, src, "<<")
	}
}

// bomb builds an alias bomb with levels levels of nine aliases each: 9^levels values from aliases.
func bomb(levels int) string {
	var b strings.Builder
	b.WriteString("l0: &l0 [x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < levels; i++ {
		prev := "*l" + string(rune('0'+i-1))
		b.WriteString("l" + string(rune('0'+i)) + ": &l" + string(rune('0'+i)) + " [")
		b.WriteString(strings.TrimSuffix(strings.Repeat(prev+", ", 9), ", "))
		b.WriteString("]\n")
	}
	return b.String()
}

// DL-168: at most 10,000 values may come from aliases and merge keys; WithMaxAliasValues moves the cap; a self-alias
// and an unknown alias are *ParseErrors. A cap below 1 fails every Decode.
func TestAliases(t *testing.T) {
	t.Parallel()
	got := mustDecode(t, "a: &a {x: 1}\nb: *a\nc: *a\n")
	if !reflect.DeepEqual(got["b"], got["a"]) || !reflect.DeepEqual(got["c"], got["a"]) {
		t.Errorf("aliases = %#v, want copies of a", got)
	}

	small := bomb(4) // 9 + 81 + 729 + 6561 values: under the cap
	mustDecode(t, small)
	big := bomb(5) // over 59,000 values from aliases
	pe := parseErrorOf(t, big, "alias")
	if pe.Line == 0 {
		t.Errorf("the alias error has no position: %v", pe)
	}
	mustDecode(t, big, yaml.WithMaxAliasValues(100_000))
	wantParseError(t, small, "alias", yaml.WithMaxAliasValues(100))

	wantParseError(t, "a: &a [*a]\n", "")
	wantParseError(t, "a: &a {b: *a}\n", "")
	wantParseError(t, "a: *nope\n", "")

	if _, err := decode(t, "a: 1\n", yaml.WithMaxAliasValues(0)); err == nil || !strings.Contains(err.Error(), "WithMaxAliasValues") {
		t.Errorf("WithMaxAliasValues(0): %v, want an error naming the option", err)
	}
}

// DL-50, DL-53: a file holds one document; an empty or null document is empty; the top level must be a table.
func TestDocuments(t *testing.T) {
	t.Parallel()
	pe := parseErrorOf(t, "a: 1\n---\nb: 2\n", "document")
	if pe.Line != 2 {
		t.Errorf("several documents: line %d, want 2", pe.Line)
	}
	if got := mustDecode(t, "---\na: 1\n...\n"); !reflect.DeepEqual(got, cc.Tree{"a": int64(1)}) {
		t.Errorf("explicit markers: %#v", got)
	}
	for _, src := range []string{"", "   \n", "# just a comment\n", "~\n", "null\n", "---\n", "--- # c\n...\n"} {
		doc, err := decode(t, src)
		if err != nil || !doc.Empty || doc.Tree != nil {
			t.Errorf("Decode(%q) = %#v, %v; want an empty document", src, doc, err)
		}
	}
	if doc, err := decode(t, "{}\n"); err != nil || doc.Empty || doc.Tree == nil {
		t.Errorf("Decode({}) = %#v, %v; want an empty table that isn't empty", doc, err)
	}
	for _, src := range []string{"- 1\n", "5\n", "'x'\n"} {
		wantParseError(t, src, "")
	}
}

// DL-52: a leading byte-order mark is stripped, so it doesn't end up in the first key.
func TestByteOrderMark(t *testing.T) {
	t.Parallel()
	if got := mustDecode(t, "\ufeffa: 1\n"); !reflect.DeepEqual(got, cc.Tree{"a": int64(1)}) {
		t.Errorf("Tree = %#v, want the key a", got)
	}
}

// Positions reach the *ParseError, and messages never quote the file (DL-170).
func TestErrors(t *testing.T) {
	t.Parallel()
	pe := parseErrorOf(t, "a: 1\na: 2\n", "")
	if pe.Line != 2 || pe.Column != 1 {
		t.Errorf("duplicate key at %d:%d, want 2:1", pe.Line, pe.Column)
	}
	for _, src := range []string{
		"password: SECRET-one\npassword: SECRET-two\n",
		"token: SECRET-three\nlist: [1, 2\n",
		"key: SECRET-four\n  bad: indent\n",
		"a: !!int SECRET-five\n",
	} {
		_, err := decode(t, src)
		if err == nil {
			t.Errorf("Decode(%q) succeeded", src)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "SECRET") || strings.Contains(msg, "> ") {
			t.Errorf("the message quotes the file: %q", msg)
		}
	}
}

// DL-171: whatever the input, the decoder never panics and only produces canonical types, which DecodeDocument
// checks: any error is a *ParseError, never an internal error.
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		"a: 1\n", "a: [1, {b: c}]\n", "a: &x {b: 1}\nc: *x\n", "a:\n  <<: *x\n", "? [1]\n: 2\n", "a: !!int x\n",
		"a: 1\n---\nb: 2\n", "- 1\n", "a: |\n  text\n", "a: 0x1F\nb: 1e3\nc: .inf\n", "\ufeffa: 1\n", bomb(3),
		"a: \"\\u00e9\"\n", "{a: [b, {c: d}]}\n", "a: 'x\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, opts := range [][]yaml.Option{nil, {yaml.WithLegacyNumbers()}} {
			_, err := cc.DecodeDocument(yaml.Decoder(opts...), "fuzz.yaml", data)
			if err == nil {
				continue
			}
			var pe *cc.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Decode(%q) = %v, want nil or a *ParseError", data, err)
			}
		}
	})
}
