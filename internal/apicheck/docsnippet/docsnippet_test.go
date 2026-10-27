package docsnippet_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck/docsnippet"
)

// fence turns three single quotes into a code fence, since Go's raw strings can't hold backquotes.
func fence(s string) string { return strings.ReplaceAll(s, "'''", "```") }

// DL-250: lines are trimmed, runs of spaces made one and blank lines dropped; a snippet's import lines go.
func TestNormalise(t *testing.T) {
	t.Parallel()
	text := "\timport (\n\t\t\"fmt\"\n\t)\n\timport \"os\"\n\n\tx  :=   1\n\t\tfmt.Println(x)  \n"
	if got, want := docsnippet.Normalise(text, true), []string{"x := 1", "fmt.Println(x)"}; !slices.Equal(got, want) {
		t.Errorf("as a snippet: %q, want %q", got, want)
	}
	want := []string{"import (", `"fmt"`, ")", `import "os"`, "x := 1", "fmt.Println(x)"}
	if got := docsnippet.Normalise(text, false); !slices.Equal(got, want) {
		t.Errorf("as a source: %q, want %q", got, want)
	}
}

// DL-266: a snippet is a Go block after an <!-- example --> marker, with blank lines allowed between; its line is the
// opening fence's. Unmarked blocks, and marked blocks that aren't Go, aren't snippets.
func TestFind(t *testing.T) {
	t.Parallel()
	doc := fence(`# Doc

<!-- example -->
'''go
a := 1
'''

'''go
unmarked()
'''

<!-- example -->

'''go
b()
'''

<!-- example -->
'''text
not go
'''
`)
	got := docsnippet.Find("doc.md", []byte(doc))
	if len(got) != 2 {
		t.Fatalf("found %d snippets, want 2: %v", len(got), got)
	}
	for i, want := range []docsnippet.Snippet{
		{Doc: "doc.md", Line: 4, Lines: []string{"a := 1"}},
		{Doc: "doc.md", Line: 14, Lines: []string{"b()"}},
	} {
		if g := got[i]; g.Doc != want.Doc || g.Line != want.Line || !slices.Equal(g.Lines, want.Lines) {
			t.Errorf("snippet %d = %+v, want %+v", i, g, want)
		}
	}
}

// DL-250: a snippet's lines must appear as a run; a source line may put an assignment, or anything ending in a space,
// in front of a snippet line, but nothing else may differ.
func TestContains(t *testing.T) {
	t.Parallel()
	src := []string{"func Example() {", "l, err := cc.New(\"app\",", "cc.WithTag(\"x\"),", ")", "}"}
	for _, c := range []struct {
		snippet []string
		want    bool
	}{
		{[]string{"cc.WithTag(\"x\"),", ")"}, true},
		{[]string{"cc.New(\"app\",", "cc.WithTag(\"x\"),"}, true}, // the source assigns the first line
		{[]string{"cc.WithTag(\"y\"),"}, false},
		{[]string{"cc.New(\"app\",", ")"}, false}, // not a run
		{[]string{"New(\"app\","}, false},         // a prefix must end in a space
		{append(slices.Clone(src), "extra"), false},
	} {
		if got := docsnippet.Contains(src, c.snippet); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.snippet, got, c.want)
		}
	}
}

// DL-266: Check reports each snippet no source holds, by doc and line, and each doc with the wrong number of snippets;
// a doc or source it can't read is an error.
func TestCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(fence(content)), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.md", "<!-- example -->\n'''go\nheld()\n'''\n\n<!-- example -->\n'''go\nmissing()\n'''\n")
	b := write("b.md", "<!-- example -->\n'''go\nheld()\n'''\n")
	src := write("example_test.go", "package x\n\nfunc Example() {\n\theld()\n}\n")

	problems, err := docsnippet.Check(map[string]int{a: 2, b: 2}, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 || !strings.Contains(problems[0]+problems[1], a+":7") ||
		!strings.Contains(problems[0]+problems[1], "missing()") || !strings.Contains(problems[0]+problems[1], b) {
		t.Errorf("problems %q, want one for %s:7 (missing()) and one for %s's count", problems, a, b)
	}
	if problems, err := docsnippet.Check(map[string]int{a: 2}, []string{src, filepath.Join(dir, "nope.go")}); err == nil {
		t.Errorf("an unreadable source gave no error, and problems %q", problems)
	}
	if _, err := docsnippet.Check(map[string]int{filepath.Join(dir, "nope.md"): 1}, []string{src}); err == nil {
		t.Error("an unreadable doc gave no error")
	}
}
