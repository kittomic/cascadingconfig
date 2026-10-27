package yaml_test

import (
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/yaml"
)

// DL-175: a document of directives belongs to the document after it. %YAML 1.2 is accepted, other 1.x versions are
// parsed as 1.2 with a warning, and 2.x is a *ParseError.
func TestYAMLDirective(t *testing.T) {
	t.Parallel()
	doc, err := decode(t, "%YAML 1.2\n---\na: 1\n")
	if err != nil || !reflect.DeepEqual(doc.Tree, cc.Tree{"a": int64(1)}) || len(doc.Warnings) != 0 {
		t.Errorf("%%YAML 1.2: %#v, %v; want the tree, no warnings", doc, err)
	}
	for _, v := range []string{"1.1", "1.3"} {
		doc, err := decode(t, "%YAML "+v+"\n---\na: 1\n")
		if err != nil || !reflect.DeepEqual(doc.Tree, cc.Tree{"a": int64(1)}) {
			t.Errorf("%%YAML %s: %#v, %v; want the tree", v, doc, err)
			continue
		}
		if len(doc.Warnings) != 1 || doc.Warnings[0].Code != yaml.WarnVersion || doc.Warnings[0].Path == "" {
			t.Errorf("%%YAML %s: warnings = %#v, want one WarnVersion with the file", v, doc.Warnings)
		}
	}
	wantParseError(t, "%YAML 2.0\n---\na: 1\n", "2.0")
	wantParseError(t, "%YAML 1.2\n---\na: 1\n---\nb: 2\n", "document")
	// goccy v1.19.2 can't parse two directives before one document, such as %YAML then %TAG; that's a known library
	// failure, not tested here.
}

// DL-164: tags are judged after expanding %TAG handles.
func TestTagHandles(t *testing.T) {
	t.Parallel()
	ok := []struct {
		src  string
		want cc.Tree
	}{
		{"%TAG !e! tag:yaml.org,2002:\n---\nv: !e!str 5\nw: !e!int '7'\n", cc.Tree{"v": "5", "w": int64(7)}},
		{"%TAG !! tag:yaml.org,2002:\n---\nv: !!str 5\n", cc.Tree{"v": "5"}},
		{"%TAG ! tag:yaml.org,2002:\n---\nv: !str 5\n", cc.Tree{"v": "5"}},
	}
	for _, tc := range ok {
		if got := mustDecode(t, tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Decode(%q) = %#v, want %#v", tc.src, got, tc.want)
		}
	}
	wantParseError(t, "%TAG !! tag:example.com,2000:\n---\nv: !!str 5\n", "tag:example.com,2000:str")
	wantParseError(t, "%TAG !e! tag:example.com,2000:\n---\nv: !e!x 5\n", "tag:example.com,2000:x")
	wantParseError(t, "v: !e!str 5\n", "!e!str")
}
