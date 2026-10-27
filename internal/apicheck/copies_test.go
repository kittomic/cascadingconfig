package apicheck_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// DL-274: the presets module's tests read their own copy of precedence.md, so they pass from its module download; the
// copy must match the original exactly.
func TestPresetsPrecedenceCopy(t *testing.T) {
	original, err := os.ReadFile(filepath.Join(root, "docs", "precedence.md"))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(root, "presets", "testdata", "precedence.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, copied) {
		t.Error("presets/testdata/precedence.txt differs from docs/precedence.md; copy it again")
	}
}
