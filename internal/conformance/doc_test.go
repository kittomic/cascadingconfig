package conformance_test

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/conformance"
)

// updating reports whether the test runs with -update, the flag cctest defines for golden files.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

const conformanceDoc = "../../docs/conformance.md"

var generated = regexp.MustCompile(`(?s)(<!-- generated:[^\n]*-->\n).*?(<!-- end generated -->)`)

// DL-272: docs/conformance.md's numbers and reasons match the suites' results. With -update, they're rewritten.
func TestConformanceDoc(t *testing.T) {
	var b strings.Builder
	suites := []struct {
		name, title string
		results     map[string]result
	}{
		{"toml-test", "toml-test (TOML 1.1)", tomlResults(t)},
		{"yaml-test-suite", "yaml-test-suite", yamlResults(t)},
		{"JSONTestSuite", "JSONTestSuite", jsonResults(t)},
	}
	b.WriteString("\n| Suite | Version | Cases | Pass | Deviations | Library failures | Implementation-defined |\n")
	b.WriteString("| ----- | ------- | ----: | ---: | ---------: | ---------------: | ---------------------: |\n")
	details := map[string]string{}
	for _, s := range suites {
		n := judge(t, s.name, s.results)
		source, err := os.ReadFile(filepath.Join("testdata", s.name, "SOURCE"))
		if err != nil {
			t.Fatal(err)
		}
		version := strings.Fields(string(source))[1]
		if len(version) == 40 {
			version = version[:7]
		}
		impl := "—"
		if n.accepted+n.rejected > 0 {
			impl = fmt.Sprintf("%d (%d accepted, %d rejected)", n.accepted+n.rejected, n.accepted, n.rejected)
		}
		fmt.Fprintf(&b, "| %s | `%s` | %d | %d | %d | %d | %s |\n", s.title, version, n.cases, n.pass, n.deviation, n.failure, impl)
		details[s.name] = detail(t, s.name, s.title)
	}
	for _, s := range suites {
		b.WriteString(details[s.name])
	}
	b.WriteString("\n")

	data, err := os.ReadFile(conformanceDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !generated.Match(data) {
		t.Fatalf("%s has no generated block", conformanceDoc)
	}
	want := generated.ReplaceAll(data, []byte("${1}"+strings.ReplaceAll(b.String(), "$", "$$")+"${2}"))
	if updating() {
		if err := os.WriteFile(conformanceDoc, want, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if string(want) != string(data) {
		t.Errorf("%s is out of date; run go test -run TestConformanceDoc -update in internal/conformance", conformanceDoc)
	}
}

// detail lists a suite's outcomes: deviations and implementation-defined outcomes grouped by reason, with their
// counts, and library failures one by one.
func detail(t *testing.T, suite, title string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("outcomes", suite+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := conformance.ReadOutcomes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 {
		return ""
	}
	type group struct {
		category, reason string
		cases            []string
	}
	groups := map[string]*group{}
	for id, o := range listed {
		key := o.Category + "\x00" + o.Reason
		if groups[key] == nil {
			groups[key] = &group{category: o.Category, reason: o.Reason}
		}
		groups[key].cases = append(groups[key].cases, id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s\n", title)
	for _, c := range []struct{ category, heading string }{
		{conformance.Deviation, "Deviations"},
		{conformance.Failure, "Library failures"},
		{conformance.Accepted, "Accepted, where the suite leaves it to the implementation"},
		{conformance.Rejected, "Rejected, where the suite leaves it to the implementation"},
	} {
		var gs []*group
		for _, g := range groups {
			if g.category == c.category {
				slices.Sort(g.cases)
				gs = append(gs, g)
			}
		}
		if len(gs) == 0 {
			continue
		}
		slices.SortFunc(gs, func(a, b *group) int {
			return cmp.Or(cmp.Compare(len(b.cases), len(a.cases)), cmp.Compare(a.reason, b.reason))
		})
		fmt.Fprintf(&b, "\n**%s:**\n\n", c.heading)
		for _, g := range gs {
			if c.category == conformance.Failure {
				fmt.Fprintf(&b, "- `%s`: %s\n", strings.Join(g.cases, "`, `"), g.reason)
				continue
			}
			fmt.Fprintf(&b, "- %d: %s\n", len(g.cases), g.reason)
		}
	}
	return b.String()
}
