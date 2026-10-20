package cascadingconfig_test

import (
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

func fixedKind(patterns ...cc.Pattern) cc.Option {
	return cc.Options(cc.WithScope("k", cc.FixedDir("/x", patterns...)), cc.WithScopes("k"), cc.WithDecoder(jsonDec))
}

// DL-117 to DL-127: pattern fields, scope options and walk options that can only be mistakes.
func TestDiscoveryOptionErrors(t *testing.T) {
	values := cc.Values(cc.Tree{})
	runOptionErrorCases(t, []optionErrorCase{
		{name: "{env} in a shared pattern", opts: []cc.Option{fixedKind(cc.Pattern{Template: "a.{env}.json"})}, ops: []string{"FixedDir"}, contains: []string{"{env}"}},
		{name: "an env pattern without {env}", opts: []cc.Option{fixedKind(cc.Pattern{Template: "a.json", Variant: cc.VariantEnv})}, ops: []string{"FixedDir"}},
		{name: "an env-local pattern without {env}", opts: []cc.Option{fixedKind(cc.Pattern{Template: "a.local.json", Variant: cc.VariantEnvLocal})}, ops: []string{"FixedDir"}},
		{name: "an unknown variant", opts: []cc.Option{fixedKind(cc.Pattern{Template: "a.json", Variant: cc.Variant(9)})}, ops: []string{"FixedDir"}},
		{name: "an unknown root mode", opts: []cc.Option{fixedKind(cc.Pattern{Template: "a.json", Root: cc.RootMode(9)})}, ops: []string{"FixedDir"}},
		{name: "**", opts: []cc.Option{fixedKind(cc.Pattern{Template: "conf.d/**.json"})}, ops: []string{"FixedDir"}},
		{name: "a bad key", opts: []cc.Option{fixedKind(cc.Pattern{Template: "package.json", Key: "a..b"})}, ops: []string{"FixedDir"}},
		{name: "a Walk without patterns", opts: []cc.Option{useKind(cc.Walk()), cc.WithDecoder(jsonDec)}, ops: []string{"Walk"}},
		{name: "an ExplicitFile without paths", opts: []cc.Option{useKind(cc.ExplicitFile())}, ops: []string{"ExplicitFile"}},
		{name: "an empty explicit path", opts: []cc.Option{useKind(cc.ExplicitFile("/a.json", ""))}, ops: []string{"ExplicitFile"}},
		{name: "an empty config root", opts: []cc.Option{cc.WithScope("k", values, cc.ConfigRoot("")), cc.WithScopes("k")}, ops: []string{"ConfigRoot"}},
		{name: "a relative config root", opts: []cc.Option{cc.WithScope("k", values, cc.ConfigRoot("rel")), cc.WithScopes("k")}, ops: []string{"ConfigRoot"}},
		{name: "ReplacedByFile without a key", opts: []cc.Option{cc.WithScope("k", values, cc.ReplacedByFile("")), cc.WithScopes("k")}, ops: []string{"ReplacedByFile"}},
		{name: "DisabledBy without a key", opts: []cc.Option{cc.WithScope("k", values, cc.DisabledBy("")), cc.WithScopes("k")}, ops: []string{"DisabledBy"}},
		{
			name: "two scopes for explicit files",
			opts: []cc.Option{
				cc.WithScope("a", values, cc.ReplacedByConfigFiles()), cc.WithScope("b", values, cc.ReplacedByConfigFiles()),
				cc.WithScopes("a", "b"),
			},
			ops: []string{"ReplacedByConfigFiles"},
		},
		{name: "an empty start dir", opts: []cc.Option{scope, cc.WithStartDir("")}, ops: []string{"WithStartDir"}},
		{name: "an unknown walk stop", opts: []cc.Option{scope, cc.WithWalkStop(cc.WalkStop(9))}, ops: []string{"WithWalkStop"}},
	})
}

// DL-123: empty ceiling and ignored paths are dropped, not errors.
func TestEmptyPathListEntries(t *testing.T) {
	if _, err := cc.New("myapp", scope, cc.WithCeilingPaths("", "/x"), cc.WithIgnoredPaths("")); err != nil {
		t.Errorf("New: %v", err)
	}
}
