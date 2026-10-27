package golden_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
)

// DL-274: importing golden, and so cctest, registers no flag, so a test binary can define its own -update. This runs
// first, before TestUpdateByFlag defines one.
func TestRegistersNoFlag(t *testing.T) {
	if f := flag.Lookup("update"); f != nil {
		t.Errorf("a flag named update is registered: %q", f.Usage)
	}
}

// DL-274: CCTEST_UPDATE=1 rewrites golden files.
func TestUpdateByEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CCTEST_UPDATE", "1")
	golden.Check(t, "a.golden", []byte("from the env\n"))
	wantFile(t, "a.golden", "from the env\n")
}

// DL-274: an -update flag the test binary defines rewrites golden files.
func TestUpdateByFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	flag.Bool("update", true, "rewrite golden files")
	golden.Check(t, "b.golden", []byte("from the flag\n"))
	wantFile(t, "b.golden", "from the flag\n")
}

func wantFile(t *testing.T, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("the golden file wasn't written: %v", err)
	}
	if string(got) != want {
		t.Errorf("testdata/%s = %q, want %q", name, got, want)
	}
}
