package conformance_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/conformance"
)

// typed is a toml-test typed value.
func typed(typ, value string) any { return map[string]any{"type": typ, "value": value} }

// DL-270: TOML values match toml-test's typed JSON by type: integers, floats with nan and inf, strings and booleans
// exactly, offset date-times as instants with the same offset, local date-times, dates and times as values, and
// tables and arrays element by element.
func TestMatchTOML(t *testing.T) {
	for _, c := range []struct {
		name  string
		got   any
		want  any
		match bool
	}{
		{"integer", int64(42), typed("integer", "42"), true},
		{"integer value", int64(41), typed("integer", "42"), false},
		{"integer type", float64(42), typed("integer", "42"), false},
		{"float", 1.5, typed("float", "1.5"), true},
		{"float exponent", 1e6, typed("float", "1000000"), true},
		{"inf", math.Inf(1), typed("float", "inf"), true},
		{"+inf", math.Inf(1), typed("float", "+inf"), true},
		{"-inf", math.Inf(-1), typed("float", "-inf"), true},
		{"nan", math.NaN(), typed("float", "nan"), true},
		{"-nan", math.NaN(), typed("float", "-nan"), true},
		{"float value", 1.25, typed("float", "1.5"), false},
		{"string", "a b", typed("string", "a b"), true},
		{"string value", "a", typed("string", "b"), false},
		{"string type", int64(1), typed("string", "1"), false},
		{"bool", true, typed("bool", "true"), true},
		{"bool value", false, typed("bool", "true"), false},
		{"datetime", "1979-05-27T07:32:00Z", typed("datetime", "1979-05-27T07:32:00Z"), true},
		{"datetime fraction", "1979-05-27T07:32:00.5Z", typed("datetime", "1979-05-27T07:32:00.500Z"), true},
		{"datetime space", "1979-05-27T07:32:00Z", typed("datetime", "1979-05-27 07:32:00Z"), true},
		{"datetime offset", "1979-05-27T00:32:00-07:00", typed("datetime", "1979-05-27T07:32:00Z"), false},
		{"datetime instant", "1979-05-27T07:32:01Z", typed("datetime", "1979-05-27T07:32:00Z"), false},
		{"datetime-local", "1979-05-27T07:32:00", typed("datetime-local", "1979-05-27T07:32:00.000"), true},
		{"datetime-local value", "1979-05-27T07:32:01", typed("datetime-local", "1979-05-27T07:32:00"), false},
		{"date-local", "1979-05-27", typed("date-local", "1979-05-27"), true},
		{"time-local", "07:32:00", typed("time-local", "07:32:00.000"), true},
		{"time-local without seconds", "07:32", typed("time-local", "07:32:00"), true},
		{"time-local value", "07:32:01", typed("time-local", "07:32:00"), false},
		{
			"table", map[string]any{"a": int64(1), "b": map[string]any{"c": "x"}},
			map[string]any{"a": typed("integer", "1"), "b": map[string]any{"c": typed("string", "x")}}, true,
		},
		{"table extra key", map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{"a": typed("integer", "1")}, false},
		{"table missing key", map[string]any{}, map[string]any{"a": typed("integer", "1")}, false},
		{
			"a table with keys type and value", map[string]any{"type": "x", "value": "y"},
			map[string]any{"type": typed("string", "x"), "value": typed("string", "y")}, true,
		},
		{"array", []any{int64(1), "x"}, []any{typed("integer", "1"), typed("string", "x")}, true},
		{"array length", []any{int64(1)}, []any{typed("integer", "1"), typed("integer", "2")}, false},
		{"array type", map[string]any{}, []any{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if why := conformance.MatchTOML(c.got, c.want); (why == "") != c.match {
				t.Errorf("MatchTOML(%#v, %#v) = %q, want a match: %v", c.got, c.want, why, c.match)
			}
		})
	}
}

// DL-270: a tree matches yaml-test-suite's JSON with numbers compared by value, and everything else exactly.
func TestMatchJSON(t *testing.T) {
	n := func(s string) any { return json.Number(s) }
	for _, c := range []struct {
		name  string
		got   any
		want  any
		match bool
	}{
		{"integer", int64(1), n("1"), true},
		{"float", 1.5, n("1.5"), true},
		{"float and integer by value", float64(1), n("1"), true},
		{"integer and float by value", int64(1), n("1.0"), true},
		{"exponent", 1e3, n("1e3"), true},
		{"number value", int64(1), n("1.5"), false},
		{"number type", "1", n("1"), false},
		{"string", "a", "a", true},
		{"string value", "a", "b", false},
		{"bool", true, true, true},
		{"bool type", "true", true, false},
		{"null", nil, nil, true},
		{"null type", "", nil, false},
		{"map", map[string]any{"a": []any{int64(1), "b"}}, map[string]any{"a": []any{n("1"), "b"}}, true},
		{"map extra key", map[string]any{"a": "x", "b": "y"}, map[string]any{"a": "x"}, false},
		{"map missing key", map[string]any{}, map[string]any{"a": "x"}, false},
		{"array order", []any{"a", "b"}, []any{"b", "a"}, false},
		{"array length", []any{"a"}, []any{"a", "a"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if why := conformance.MatchJSON(c.got, c.want); (why == "") != c.match {
				t.Errorf("MatchJSON(%#v, %#v) = %q, want a match: %v", c.got, c.want, why, c.match)
			}
		})
	}
}

// DL-271: an outcomes file lists cases with a category and a reason; blank lines and comments are skipped, and a bad
// category, a missing reason or a case listed twice is an error.
func TestReadOutcomes(t *testing.T) {
	got, err := conformance.ReadOutcomes([]byte(`# toml-test
valid/a.toml  deviation  the top level must be a table (DL-53)

invalid/b.toml failure goccy accepts it
i_x.json accepted the decoder takes it
i_y.json rejected the decoder refuses it
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]conformance.Outcome{
		"valid/a.toml":   {Category: conformance.Deviation, Reason: "the top level must be a table (DL-53)"},
		"invalid/b.toml": {Category: conformance.Failure, Reason: "goccy accepts it"},
		"i_x.json":       {Category: conformance.Accepted, Reason: "the decoder takes it"},
		"i_y.json":       {Category: conformance.Rejected, Reason: "the decoder refuses it"},
	}
	if len(got) != len(want) {
		t.Fatalf("outcomes %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %+v, want %+v", k, got[k], v)
		}
	}
	for _, bad := range []string{
		"a.toml maybe it's fine",
		"a.toml deviation",
		"a.toml deviation one\na.toml failure two",
	} {
		if _, err := conformance.ReadOutcomes([]byte(bad)); err == nil {
			t.Errorf("ReadOutcomes(%q) gave no error", strings.ReplaceAll(bad, "\n", `\n`))
		}
	}
}
