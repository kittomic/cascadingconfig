package cascadingconfig_test

import (
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-198, DL-202: a WithFileFormat rule matches a remote reference as an allow-list does, so FileName and Extension
// see its file path, not the query or the ref.
func TestRemoteFormatRuleFilePath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ref string
		rule      cc.Matcher
	}{
		{"FileName on an inner path", "git::https://h.example/r.git//dir/config?ref=v1", cc.FileName("config")},
		{"Extension on a URL with a query", "https://h.example/a.cfg?v=1", cc.Extension("cfg")},
		{"Prefix on the normalised reference", "https://h.example/raw/x", cc.Prefix("https://h.example/raw/")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake("https", "git")
			f.fetch = anyFile(`{"a": 1}`, "")
			res := remOK(t, f, []string{tc.ref}, cc.WithFileFormat(tc.rule, "toml"))
			if l := layerAt(t, res.Describe(), tc.ref); l.Format != "toml" {
				t.Errorf("Format = %q, want toml", l.Format)
			}
		})
	}
}
