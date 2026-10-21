package mise_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// DL-250: every snippet marked <!-- example --> in docs/design/design.md and docs/design/api.md appears in the example
// sources, unchanged but for indentation, runs of spaces, and an assignment in front of a line. Its import lines are
// left out, since examples have their own.
func TestDocExamples(t *testing.T) {
	var src []string
	for _, f := range []string{"docexamples_test.go", "../internal/docexample/main.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src = append(src, normalise(string(data), false)...)
	}
	block := regexp.MustCompile("(?s)<!-- example -->\\s*```go\\n(.*?)```")
	count := 0
	for _, doc := range []string{"../../docs/design/design.md", "../../docs/design/api.md"} {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range block.FindAllStringSubmatch(string(data), -1) {
			count++
			snippet := normalise(m[1], true)
			if !contains(src, snippet) {
				t.Errorf("%s: no example holds the snippet starting %q", doc, snippet[0])
			}
		}
	}
	if count != 18 {
		t.Errorf("found %d marked snippets, want 18 (DL-250, DL-256)", count)
	}
}

var spaces = regexp.MustCompile(`\s+`)

// normalise returns text's non-empty lines, trimmed, with runs of spaces made one; for a snippet, without its import
// lines.
func normalise(text string, snippet bool) []string {
	var out []string
	inImport := false
	for _, line := range strings.Split(text, "\n") {
		line = spaces.ReplaceAllString(strings.TrimSpace(line), " ")
		switch {
		case line == "":
			continue
		case snippet && line == "import (":
			inImport = true
			continue
		case inImport:
			inImport = line != ")"
			continue
		case snippet && strings.HasPrefix(line, "import "):
			continue
		}
		out = append(out, line)
	}
	return out
}

// contains reports whether snippet's lines appear as a run in src, where a source line may put an assignment, or
// anything else ending in a space, in front of a snippet line.
func contains(src, snippet []string) bool {
	for i := 0; i+len(snippet) <= len(src); i++ {
		ok := true
		for j, want := range snippet {
			got := src[i+j]
			if got != want && !strings.HasSuffix(got, " "+want) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
