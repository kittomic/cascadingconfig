package conformance

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MatchTOML compares got, a value in the TOML decoder's tree, with want, the same value in toml-test's typed JSON as
// encoding/json decodes it, and returns "" when they match, or why they don't (DL-270).
func MatchTOML(got, want any) string { return matchTOML("", got, want) }

func matchTOML(at string, got, want any) string {
	switch w := want.(type) {
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: want an array, got %T", where(at), got)
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s: want %d elements, got %d", where(at), len(w), len(g))
		}
		for i := range w {
			if why := matchTOML(fmt.Sprintf("%s[%d]", at, i), g[i], w[i]); why != "" {
				return why
			}
		}
		return ""
	case map[string]any:
		if typ, value, ok := typedValue(w); ok {
			return matchTyped(at, got, typ, value)
		}
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: want a table, got %T", where(at), got)
		}
		return matchKeys(at, g, w, matchTOML)
	}
	return fmt.Sprintf("%s: unexpected expected value %T", where(at), want)
}

// typedValue reports whether w is a toml-test typed value, {"type": …, "value": …} with string fields.
func typedValue(w map[string]any) (typ, value string, ok bool) {
	if len(w) != 2 {
		return "", "", false
	}
	typ, ok1 := w["type"].(string)
	value, ok2 := w["value"].(string)
	return typ, value, ok1 && ok2
}

func matchTyped(at string, got any, typ, value string) string {
	mismatch := func() string { return fmt.Sprintf("%s: want %s %s, got %T %v", where(at), typ, value, got, got) }
	switch typ {
	case "integer":
		g, ok := got.(int64)
		w, err := strconv.ParseInt(value, 10, 64)
		if !ok || err != nil || g != w {
			return mismatch()
		}
	case "float":
		g, ok := got.(float64)
		if !ok {
			return mismatch()
		}
		switch strings.TrimLeft(value, "+-") {
		case "nan":
			if !math.IsNaN(g) {
				return mismatch()
			}
			return ""
		case "inf":
			if !math.IsInf(g, 1) && !math.IsInf(g, -1) || (strings.HasPrefix(value, "-") != (g < 0)) {
				return mismatch()
			}
			return ""
		}
		w, err := strconv.ParseFloat(value, 64)
		if err != nil || g != w {
			return mismatch()
		}
	case "string":
		if g, ok := got.(string); !ok || g != value {
			return mismatch()
		}
	case "bool":
		if g, ok := got.(bool); !ok || strconv.FormatBool(g) != value {
			return mismatch()
		}
	case "datetime":
		g, ok := got.(string)
		gt, err1 := time.Parse(time.RFC3339Nano, normaliseTime(g))
		wt, err2 := time.Parse(time.RFC3339Nano, normaliseTime(value))
		if !ok || err1 != nil || err2 != nil || !gt.Equal(wt) {
			return mismatch()
		}
		if _, gOff := gt.Zone(); gOff != offsetOf(wt) {
			return mismatch()
		}
	case "datetime-local", "date-local", "time-local":
		g, ok := got.(string)
		layouts := map[string][]string{
			"datetime-local": {"2006-01-02T15:04:05.999999999", "2006-01-02T15:04"},
			"date-local":     {"2006-01-02"},
			"time-local":     {"15:04:05.999999999", "15:04"},
		}[typ]
		gt, err1 := parseAny(layouts, normaliseTime(g))
		wt, err2 := parseAny(layouts, normaliseTime(value))
		if !ok || err1 != nil || err2 != nil || !gt.Equal(wt) {
			return mismatch()
		}
	default:
		return fmt.Sprintf("%s: unknown type %s", where(at), typ)
	}
	return ""
}

func offsetOf(t time.Time) int {
	_, off := t.Zone()
	return off
}

// normaliseTime writes a TOML date-time as RFC 3339 does: T between date and time, and T and Z in capitals.
func normaliseTime(s string) string {
	s = strings.ToUpper(s)
	if len(s) > 10 && s[10] == ' ' {
		s = s[:10] + "T" + s[11:]
	}
	return s
}

func parseAny(layouts []string, s string) (time.Time, error) {
	var err error
	for _, l := range layouts {
		var t time.Time
		if t, err = time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// MatchJSON compares got, a value in a decoder's tree, with want, the value a yaml-test-suite case's in.json holds, as
// encoding/json decodes it with UseNumber, and returns "" when they match, or why they don't (DL-270).
func MatchJSON(got, want any) string { return matchJSON("", got, want) }

func matchJSON(at string, got, want any) string {
	mismatch := func() string { return fmt.Sprintf("%s: want %T %v, got %T %v", where(at), want, want, got, got) }
	switch w := want.(type) {
	case nil:
		if got != nil {
			return mismatch()
		}
	case bool, string:
		if got != want {
			return mismatch()
		}
	case json.Number:
		wf, err := strconv.ParseFloat(string(w), 64)
		var gf float64
		switch g := got.(type) {
		case int64:
			gf = float64(g)
		case float64:
			gf = g
		default:
			return mismatch()
		}
		if err != nil || gf != wf {
			return mismatch()
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return mismatch()
		}
		for i := range w {
			if why := matchJSON(fmt.Sprintf("%s[%d]", at, i), g[i], w[i]); why != "" {
				return why
			}
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return mismatch()
		}
		return matchKeys(at, g, w, matchJSON)
	default:
		return fmt.Sprintf("%s: unexpected expected value %T", where(at), want)
	}
	return ""
}

// matchKeys compares two tables' keys, then each value with match.
func matchKeys(at string, got, want map[string]any, match func(string, any, any) string) string {
	for _, k := range slices.Sorted(keysOf(want)) {
		g, ok := got[k]
		if !ok {
			return fmt.Sprintf("%s: missing key %q", where(at), k)
		}
		if why := match(join(at, k), g, want[k]); why != "" {
			return why
		}
	}
	for _, k := range slices.Sorted(keysOf(got)) {
		if _, ok := want[k]; !ok {
			return fmt.Sprintf("%s: unexpected key %q", where(at), k)
		}
	}
	return ""
}

func keysOf(m map[string]any) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func join(at, k string) string {
	if at == "" {
		return k
	}
	return at + "." + k
}

func where(at string) string {
	if at == "" {
		return "the top level"
	}
	return at
}

// Outcome is one case's line in an outcomes file (DL-271).
type Outcome struct {
	Category string
	Reason   string
}

// The categories an outcomes file uses.
const (
	Deviation = "deviation" // deliberate, citing its decision
	Failure   = "failure"   // a known library failure
	Accepted  = "accepted"  // a JSON i_ case the decoder accepts
	Rejected  = "rejected"  // a JSON i_ case the decoder rejects
)

// ReadOutcomes parses an outcomes file: one case per line, as "<case> <category> <reason>", with blank lines and lines
// starting with # ignored. An unknown category, a missing reason, or a case listed twice is an error.
func ReadOutcomes(data []byte) (map[string]Outcome, error) {
	out := map[string]Outcome{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return nil, fmt.Errorf("line %d: want a case, a category and a reason: %q", i+1, line)
		}
		switch f[1] {
		case Deviation, Failure, Accepted, Rejected:
		default:
			return nil, fmt.Errorf("line %d: unknown category %q", i+1, f[1])
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("line %d: %s is listed twice", i+1, f[0])
		}
		out[f[0]] = Outcome{Category: f[1], Reason: strings.Join(f[2:], " ")}
	}
	return out, nil
}
