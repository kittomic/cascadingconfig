package apicheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"
)

// userDocNamesAllowed are backticked names in user docs that aren't in a released package and aren't caught by the
// rules, each with the reason.
var userDocNamesAllowed = []string{
	"Config", // the reader's own config type, in the guide's snippets
}

// DL-267: every name in the user docs exists.
func TestUserDocNames(t *testing.T) {
	pkgs := loadReleased(t)
	docs := []string{filepath.Join(root, "README.md"), filepath.Join(root, "docs", "precedence.md")}
	guide, err := filepath.Glob(filepath.Join(root, "docs", "guide", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range append(docs, guide...) {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range apicheck.Names(pkgs, doc, data, userDocNamesAllowed) {
			t.Error(f)
		}
	}
}
