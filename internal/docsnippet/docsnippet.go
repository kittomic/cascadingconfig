// Package docsnippet checks that the usage snippets marked <!-- example --> in docs appear in the example sources that
// hold them, unchanged but for indentation, runs of spaces, and an assignment in front of a line (DL-250, DL-266). A
// snippet's import lines are left out, since its example has its own. The design records' and the user guide's tests
// share it.
package docsnippet

// Snippet is one marked snippet: the doc it's in, the line of its opening fence, and its lines, normalised.
type Snippet struct {
	Doc   string
	Line  int
	Lines []string
}

// Find returns the snippets marked in doc, the markdown file called name.
func Find(name string, doc []byte) []Snippet {
	_, _ = name, doc // not implemented yet
	return nil
}

// Normalise returns text's non-empty lines, trimmed, with runs of spaces made one; for a snippet, without its import
// lines.
func Normalise(text string, snippet bool) []string {
	_, _ = text, snippet // not implemented yet
	return nil
}

// Contains reports whether snippet's lines appear as a run in src, where a source line may put an assignment, or
// anything else ending in a space, in front of a snippet line. Both are normalised.
func Contains(src, snippet []string) bool {
	_, _ = src, snippet // not implemented yet
	return false
}

// Check reads each doc in docs, which maps a doc's path to the number of snippets it should mark, and the example
// sources, and returns a problem for each snippet no source holds and each doc with the wrong number of snippets.
func Check(docs map[string]int, sources []string) ([]string, error) {
	_, _ = docs, sources // not implemented yet
	return nil, nil
}
