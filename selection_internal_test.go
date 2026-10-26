package cascadingconfig

import "testing"

// DL-261: defining a scope selects it. Options apply in order, so a later WithScopes that leaves it out deselects it, a
// WithScope after WithScopes adds to the selection, and WithoutScopes after the definition removes it. Placement is
// unchanged by selection.
func TestDefiningSelects(t *testing.T) {
	v := func() ScopeKind { return Values(Tree{}) }

	c := cfgOf(t, "app", Preset{}, WithScope("a", v()), WithScope("b", v()))
	wantScopes(t, c, "a", "b")

	c = cfgOf(t, "app", Preset{}, WithScope("a", v()), WithScope("b", v()), WithScopes("a"))
	wantScopes(t, c, "a")

	c = cfgOf(t, "app", Preset{}, WithScope("a", v()), WithScopes("a"), WithScope("b", v()))
	wantScopes(t, c, "a", "b")

	c = cfgOf(t, "app", Preset{}, WithScope("a", v()), WithScope("b", v()), WithoutScopes("b"))
	wantScopes(t, c, "a")

	c = cfgOf(t, "app", Preset{}, WithScope("a", v()), WithScope("z", v(), Before("a")))
	wantScopes(t, c, "z", "a")

	// A layout extended with one scope keeps its own selection and adds the new one.
	c = cfgOf(t, "app", MiseLayout, WithScope("env", EnvVars("APP_")))
	wantScopes(t, c, ScopeSystem, ScopeUser, ScopeProject, ScopeOverride, "env")
}
