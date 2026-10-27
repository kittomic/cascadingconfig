package cascadingconfig_test

import (
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-131, DL-132, DL-134: bootstrap options that can only be mistakes.
func TestBootstrapOptionErrors(t *testing.T) {
	values := cc.Values(cc.Tree{})
	rc := cc.Pattern{Template: ".{name}rc.json"}
	runOptionErrorCases(t, []optionErrorCase{
		{name: "BootstrapFiles on Values", opts: []cc.Option{cc.WithScope("k", values, cc.BootstrapFiles(rc)), cc.WithScopes("k")}, ops: []string{"BootstrapFiles"}},
		{
			name: "BootstrapFiles without patterns",
			opts: []cc.Option{cc.WithScope("k", cc.FixedDir("/x", rc), cc.BootstrapFiles()), cc.WithScopes("k"), cc.WithDecoder(jsonDec)},
			ops:  []string{"BootstrapFiles"},
		},
		{
			name: "a bad bootstrap pattern",
			opts: []cc.Option{cc.WithScope("k", cc.FixedDir("/x", rc), cc.BootstrapFiles(cc.Pattern{Template: "../rc.json"})), cc.WithScopes("k"), cc.WithDecoder(jsonDec)},
			ops:  []string{"BootstrapFiles"},
		},
		{name: "an unknown key", opts: []cc.Option{scope, cc.WithBootstrapKeys("nope")}, ops: []string{"WithBootstrapKeys"}, contains: []string{"nope"}},
		{name: "turning off an unknown key", opts: []cc.Option{scope, cc.WithoutBootstrapKeys("nope")}, ops: []string{"WithoutBootstrapKeys"}},
		{name: "a limit on an unknown scope", opts: []cc.Option{scope, cc.WithBootstrapLimit("nope", cc.BootstrapEnv)}, ops: []string{"WithBootstrapLimit"}},
		{name: "a limit with an unknown key", opts: []cc.Option{scope, cc.WithBootstrapLimit("s", "nope")}, ops: []string{"WithBootstrapLimit"}},
		{name: "a nil transform", opts: []cc.Option{scope, cc.WithBootstrapTransform(nil)}, ops: []string{"WithBootstrapTransform"}},
	})
}
