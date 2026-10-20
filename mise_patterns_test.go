package cascadingconfig

import (
	"reflect"
	"testing"
)

// DL-130, config-locations.md 3.4: mise's global-directory patterns, lowest precedence first.
func TestMiseGlobalPatterns(t *testing.T) {
	want := []Pattern{
		{Template: "conf.d/*.{ext}"},
		{Template: "conf.d/*/{name}.{ext}", Root: RootFileDir},
		{Template: "config.{ext}"},
		{Template: "{name}.{ext}"},
		{Template: "conf.d/*.{env}.{ext}", Variant: VariantEnv},
		{Template: "conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
		{Template: "config.{env}.{ext}", Variant: VariantEnv},
		{Template: "{name}.{env}.{ext}", Variant: VariantEnv},
		{Template: "conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
		{Template: "config.local.{ext}", Variant: VariantLocal},
		{Template: "{name}.local.{ext}", Variant: VariantLocal},
		{Template: "conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
		{Template: "conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
		{Template: "config.{env}.local.{ext}", Variant: VariantEnvLocal},
		{Template: "{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
	}
	if got := MiseGlobalPatterns(); !reflect.DeepEqual(got, want) {
		t.Errorf("MiseGlobalPatterns() =\n%#v\nwant\n%#v", got, want)
	}
}

// DL-130: each call returns a fresh copy, so changing one can't change MiseLayout or a later call.
func TestMiseGlobalPatternsIsACopy(t *testing.T) {
	first := MiseGlobalPatterns()
	if len(first) == 0 {
		t.Fatal("MiseGlobalPatterns() is empty")
	}
	first[0].Template = "changed"
	if got := MiseGlobalPatterns()[0].Template; got != "conf.d/*.{ext}" {
		t.Errorf("after changing a copy, the first template is %q", got)
	}
}

// DL-124, DL-130: MiseLayout's system and user scopes use exactly these patterns.
func TestMiseLayoutUsesTheGlobalPatterns(t *testing.T) {
	c := cfgOf(t, "myapp", MiseLayout)
	for _, name := range []ScopeName{ScopeSystem, ScopeUser} {
		k, ok := c.scopeKind(name).(fixedDirKind)
		if !ok {
			t.Fatalf("%s isn't a FixedDir", name)
		}
		if !reflect.DeepEqual(k.patterns, MiseGlobalPatterns()) {
			t.Errorf("%s's patterns aren't MiseGlobalPatterns()", name)
		}
	}
}
