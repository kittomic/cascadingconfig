package cascadingconfig

import (
	"runtime"
	"testing"
)

// skipUnixPaths skips a test that uses Unix paths in cctest's world, which aren't absolute on Windows (DL-282).
func skipUnixPaths(tb testing.TB) {
	tb.Helper()
	if runtime.GOOS == "windows" {
		tb.Skip("the test uses Unix paths")
	}
}
