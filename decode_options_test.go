package cascadingconfig_test

import (
	"math"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-149, DL-151, DL-153, DL-157: decoding, defaults and requirement options that can only be mistakes.
func TestDecodeOptionErrors(t *testing.T) {
	type withFunc struct {
		F func() `koanf:"f"`
	}
	type withChan struct {
		C chan int `koanf:"c"`
	}
	type withComplex struct {
		C complex128 `koanf:"c"`
	}
	type withHuge struct {
		U uint64 `koanf:"u"`
	}
	type nested struct {
		Inner withFunc `koanf:"inner"`
	}
	runOptionErrorCases(t, []optionErrorCase{
		{name: "an empty tag", opts: []cc.Option{scope, cc.WithTag("")}, ops: []string{"WithTag"}},
		{name: "an unknown unknown-keys mode", opts: []cc.Option{scope, cc.WithUnknownKeys(cc.UnknownKeys(9))}, ops: []string{"WithUnknownKeys"}},
		{name: "nil defaults", opts: []cc.Option{scope, cc.WithDefaults(nil)}, ops: []string{"WithDefaults"}},
		{name: "scalar defaults", opts: []cc.Option{scope, cc.WithDefaults(5)}, ops: []string{"WithDefaults"}},
		{name: "a nil pointer", opts: []cc.Option{scope, cc.WithDefaults((*withFunc)(nil))}, ops: []string{"WithDefaults"}},
		{name: "a map with int keys", opts: []cc.Option{scope, cc.WithDefaults(map[int]any{1: "a"})}, ops: []string{"WithDefaults"}},
		{name: "a func field", opts: []cc.Option{scope, cc.WithDefaults(withFunc{F: func() {}})}, ops: []string{"WithDefaults"}, contains: []string{"f"}},
		{name: "a chan field", opts: []cc.Option{scope, cc.WithDefaults(withChan{C: make(chan int)})}, ops: []string{"WithDefaults"}, contains: []string{"c"}},
		{name: "a complex field", opts: []cc.Option{scope, cc.WithDefaults(withComplex{C: 1i})}, ops: []string{"WithDefaults"}, contains: []string{"c"}},
		{name: "a uint64 too big", opts: []cc.Option{scope, cc.WithDefaults(withHuge{U: math.MaxUint64})}, ops: []string{"WithDefaults"}, contains: []string{"u"}},
		{
			name: "a nested bad field", opts: []cc.Option{scope, cc.WithDefaults(nested{Inner: withFunc{F: func() {}}})},
			ops: []string{"WithDefaults"}, contains: []string{"inner.f"},
		},
		{
			name: "a clash with a defaults scope", opts: []cc.Option{scope, cc.WithScope("defaults", cc.Values(cc.Tree{})), cc.WithDefaults(map[string]any{"a": 1})},
			ops: []string{"WithDefaults"},
		},
		{name: "an unknown Require flag", opts: []cc.Option{scope, cc.RequireConfig(cc.Require(8))}, ops: []string{"RequireConfig"}},
		{name: "an undefined required scope", opts: []cc.Option{scope, cc.RequireConfig(cc.RequireAny, "nope")}, ops: []string{"RequireConfig"}, contains: []string{"nope"}},
		{
			name: "an unselected required scope", opts: []cc.Option{scope, cc.WithScope("other", cc.Values(cc.Tree{})), cc.WithoutScopes("other"), cc.RequireConfig(cc.RequireAny, "other")},
			ops: []string{"RequireConfig"}, contains: []string{"other"},
		},
	})
}
