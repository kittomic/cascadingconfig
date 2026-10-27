package yaml_test

import (
	"testing"

	"github.com/kittomic/cascadingconfig/format/yaml"
)

// An alias counts the values it expands to, not itself: here the sequence and its two scalars, so three.
func TestAliasCountIsExact(t *testing.T) {
	t.Parallel()
	src := "a: &a [x, x]\nb: *a\n"
	mustDecode(t, src, yaml.WithMaxAliasValues(3))
	wantParseError(t, src, "alias", yaml.WithMaxAliasValues(2))

	tagged := "a: &a !!seq [x]\nb: *a\nc: *a\n"
	mustDecode(t, tagged, yaml.WithMaxAliasValues(4))
	wantParseError(t, tagged, "alias", yaml.WithMaxAliasValues(3))
}
