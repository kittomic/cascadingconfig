package guide_test

import (
	"testing"

	"github.com/kittomic/cascadingconfig/internal/docsnippet"
)

// DL-266: every snippet marked <!-- example --> in docs/guide appears in docexamples_test.go, unchanged but for
// indentation, runs of spaces, and an assignment in front of a line; import lines are left out.
func TestGuideSnippets(t *testing.T) {
	problems, err := docsnippet.Check(map[string]int{
		"../../docs/guide/getting-started.md": 3,
		"../../docs/guide/sources.md":         5,
		"../../docs/guide/decoding.md":        5,
	}, []string{"docexamples_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
