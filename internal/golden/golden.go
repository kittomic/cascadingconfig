// Package golden compares test output with files under testdata, and rewrites them on request: with an -update flag
// the test binary defines, or with CCTEST_UPDATE=1. It registers no flag itself, so it can't clash with one the test
// binary defines (DL-274).
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updating reports whether golden files should be rewritten: the test binary's own -update flag, or CCTEST_UPDATE=1.
func updating() bool {
	if f := flag.Lookup("update"); f != nil && f.Value.String() == "true" {
		return true
	}
	return os.Getenv("CCTEST_UPDATE") == "1"
}

// Check compares got with testdata/file, or writes got there when updating.
func Check(t testing.TB, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", filepath.FromSlash(file))
	if updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Errorf("golden: %v", err)
			return
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Errorf("golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("golden: %v (run the test with -update or CCTEST_UPDATE=1 to create it)", err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden: %s doesn't match (run the test with -update or CCTEST_UPDATE=1 to accept the new output)\ngot:\n%s\nwant:\n%s",
			path, got, want)
	}
}
