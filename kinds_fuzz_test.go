package cascadingconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

// FuzzPatternTemplates checks that a template the checks accept always expands, for any safe app name and any
// extension a decoder could claim, to a path inside the searched directory.
func FuzzPatternTemplates(f *testing.F) {
	for _, s := range []string{"config.{ext}", "conf.d/*.{env}.{ext}", "{name}.{env}.local.{ext}", "{name}/conf.d/x.{ext}", "../x", "/etc/x", "a/{ext}/b", "{nope}", "{", "{name}"} {
		f.Add(s, "myapp", "toml")
	}
	f.Fuzz(func(t *testing.T, tpl, name, ext string) {
		if checkName(name) != nil || ext == "" || strings.ContainsAny(ext, `/\`) || ext == "." || ext == ".." {
			return
		}
		c := newConfig(name)
		variant := VariantShared
		if strings.Contains(tpl, "{env}") {
			variant = VariantEnv
		}
		checkPatterns(c, "FixedDir", []Pattern{{Template: tpl, Variant: variant}})
		if len(c.errs) > 0 {
			return
		}
		if got := filepath.FromSlash(expand(tpl, name, name, ext)); !filepath.IsLocal(got) {
			t.Fatalf("template %q with name %q and ext %q expands to %q, outside the directory", tpl, name, ext, got)
		}
	})
}
