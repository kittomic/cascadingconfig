// Package docsnippet checks that the usage snippets marked <!-- example --> in docs appear in the example sources that
// hold them, unchanged but for indentation, runs of spaces, and an assignment in front of a line (DL-250, DL-266). A
// snippet's import lines are left out, since its example has its own. The design records' and the user guide's tests
// share it.
package docsnippet

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Snippet is one marked snippet: the doc it's in, the line of its opening fence, and its lines, normalised.
type Snippet struct {
	Doc   string
	Line  int
	Lines []string
}

var (
	marker     = regexp.MustCompile(`^\s*<!-- example -->\s*$`)
	fenceOpen  = regexp.MustCompile("^\\s*```(\\S*)\\s*$")
	fenceClose = regexp.MustCompile("^\\s*```\\s*$")
	spaces     = regexp.MustCompile(`\s+`)
)

// Find returns the snippets marked in doc, the markdown file called name.
func Find(name string, doc []byte) []Snippet {
	var out []Snippet
	lines := strings.Split(string(doc), "\n")
	marked := false
	for i := 0; i < len(lines); i++ {
		switch m := fenceOpen.FindStringSubmatch(lines[i]); {
		case marker.MatchString(lines[i]):
			marked = true
		case m != nil:
			start := i
			i++
			for i < len(lines) && !fenceClose.MatchString(lines[i]) {
				i++
			}
			if marked && m[1] == "go" {
				body := strings.Join(lines[start+1:min(i, len(lines))], "\n")
				out = append(out, Snippet{Doc: name, Line: start + 1, Lines: Normalise(body, true)})
			}
			marked = false
		case strings.TrimSpace(lines[i]) != "":
			marked = false
		}
	}
	return out
}

// Normalise returns text's non-empty lines, trimmed, with runs of spaces made one; for a snippet, without its import
// lines.
func Normalise(text string, snippet bool) []string {
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

// Contains reports whether snippet's lines appear as a run in src, where a source line may put an assignment, or
// anything else ending in a space, in front of a snippet line. Both are normalised.
func Contains(src, snippet []string) bool {
	for i := 0; i+len(snippet) <= len(src); i++ {
		ok := true
		for j, want := range snippet {
			if got := src[i+j]; got != want && !strings.HasSuffix(got, " "+want) {
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

// Check reads each doc in docs, which maps a doc's path to the number of snippets it should mark, and the example
// sources, and returns a problem for each snippet no source holds and each doc with the wrong number of snippets.
func Check(docs map[string]int, sources []string) ([]string, error) {
	var src []string
	for _, f := range sources {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		src = append(src, Normalise(string(data), false)...)
	}
	var problems []string
	for _, doc := range slices.Sorted(func(yield func(string) bool) {
		for d := range docs {
			if !yield(d) {
				return
			}
		}
	}) {
		data, err := os.ReadFile(doc)
		if err != nil {
			return nil, err
		}
		snippets := Find(doc, data)
		if len(snippets) != docs[doc] {
			problems = append(problems, fmt.Sprintf("%s marks %d snippets, want %d", doc, len(snippets), docs[doc]))
		}
		for _, s := range snippets {
			if len(s.Lines) > 0 && !Contains(src, s.Lines) {
				problems = append(problems, fmt.Sprintf("%s:%d: no example holds the snippet starting %q", doc, s.Line, s.Lines[0]))
			}
		}
	}
	return problems, nil
}
