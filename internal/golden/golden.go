// Package golden compares test output with files under testdata, and rewrites them when tests run with -update.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Check compares got with testdata/file, or writes got there with -update.
func Check(t testing.TB, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", filepath.FromSlash(file))
	if *update {
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
		t.Errorf("golden: %v (run the test with -update to create it)", err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden: %s doesn't match (run the test with -update to accept the new output)\ngot:\n%s\nwant:\n%s",
			path, got, want)
	}
}
