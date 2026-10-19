package cascadingconfig_test

import (
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-85: parsing the dotted string form.
func TestParsePath(t *testing.T) {
	tests := []struct {
		in   string
		want cc.Path
	}{
		{"", cc.Path{}},
		{"server", cc.Path{"server"}},
		{"server.tls", cc.Path{"server", "tls"}},
		{"tools.*", cc.Path{"tools", "*"}},
		{`plugins."foo.bar"`, cc.Path{"plugins", "foo.bar"}},
		{`"foo.bar".baz`, cc.Path{"foo.bar", "baz"}},
		{`a."b\"c".d`, cc.Path{"a", `b"c`, "d"}},
		{`"a\\b"`, cc.Path{`a\b`}},
		{`""`, cc.Path{""}},
		{`a."".b`, cc.Path{"a", "", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := cc.ParsePath(tc.in)
			if err != nil {
				t.Fatalf("ParsePath(%q) error: %v", tc.in, err)
			}
			if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Errorf("ParsePath(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// DL-85: empty segments and malformed quoting are errors.
func TestParsePathErrors(t *testing.T) {
	for _, in := range []string{
		"a..b",
		".a",
		"a.",
		".",
		`a."unterminated`,
		`a."b"c`,
		`a.b"c"`,
		`"a\x"`,
	} {
		t.Run(in, func(t *testing.T) {
			if got, err := cc.ParsePath(in); err == nil {
				t.Errorf("ParsePath(%q) = %#v, want an error", in, got)
			}
		})
	}
}

// DL-85: String gives the dotted form, quoting only where needed.
func TestPathString(t *testing.T) {
	tests := []struct {
		in   cc.Path
		want string
	}{
		{cc.Path{}, ""},
		{cc.Path{"server", "tls"}, "server.tls"},
		{cc.Path{"tools", "*"}, "tools.*"},
		{cc.Path{"plugins", "foo.bar"}, `plugins."foo.bar"`},
		{cc.Path{"a", ""}, `a.""`},
		{cc.Path{`b"c`}, `"b\"c"`},
		{cc.Path{`a\b`}, `"a\\b"`},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Errorf("%#v.String() = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// DL-85, plan M1: every Path round-trips through String and ParsePath, including keys that contain dots.
func TestPathRoundTrip(t *testing.T) {
	for _, p := range []cc.Path{
		{"server", "tls"},
		{"plugins", "foo.bar"},
		{"tools", "*"},
		{""},
		{"a", "", "b"},
		{`with "quotes"`},
		{`back\slash`},
		{"…unicode", "ключ"},
	} {
		t.Run(p.String(), func(t *testing.T) {
			got, err := cc.ParsePath(p.String())
			if err != nil {
				t.Fatalf("ParsePath(%q) error: %v", p.String(), err)
			}
			if !reflect.DeepEqual(got, p) {
				t.Errorf("round trip of %#v gave %#v (via %q)", p, got, p.String())
			}
		})
	}
}
