package mise_test

import (
	"testing"

	"github.com/kittomic/cascadingconfig/internal/docsnippet"
)

// DL-250, DL-266: every snippet marked <!-- example --> in the design records appears in the example sources,
// unchanged but for indentation, runs of spaces, and an assignment in front of a line; import lines are left out.
func TestDocExamples(t *testing.T) {
	problems, err := docsnippet.Check(map[string]int{
		"../../docs/design/design.md": 11,
		"../../docs/design/api.md":    7,
	}, []string{"docexamples_test.go", "../internal/docexample/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
