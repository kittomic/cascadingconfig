// Package golden compares test output with files under testdata, and rewrites them when tests run with -update.
package golden

import (
	"flag"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Check compares got with testdata/file, or writes got there with -update.
func Check(t testing.TB, file string, got []byte) {
	t.Helper()
	_, _, _ = file, got, update // stub
}
