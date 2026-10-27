package apicheck_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// DL-274: project-layout.md's repository tree lists every module in go.work, as "module …/<path>", so the tree can't
// fall behind the repository it describes.
func TestLayoutTreeListsModules(t *testing.T) {
	t.Parallel()
	work, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "contributing", "project-layout.md"))
	if err != nil {
		t.Fatal(err)
	}
	tree := regexp.MustCompile("(?s)## Repository tree\n\n```text\n(.*?)```").FindSubmatch(doc)
	if tree == nil {
		t.Fatal("project-layout.md has no repository tree")
	}
	uses := regexp.MustCompile(`(?m)^\s+\./(\S+)\s*$`).FindAllSubmatch(work, -1)
	if len(uses) == 0 {
		t.Fatal("go.work lists no modules besides the core")
	}
	for _, u := range uses {
		if want := "module …/" + string(u[1]); !strings.Contains(string(tree[1]), want) {
			t.Errorf("project-layout.md's tree doesn't list %q", want)
		}
	}
}
