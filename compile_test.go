package cascadingconfig_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DL-96, api.md Option kinds: options that only make sense in one place have their own type, so the compiler rejects
// them anywhere else, and Option is sealed. Each program under testdata/compile must fail to build with the type error
// its first line names; ok must build, so that a failure is always the misuse and never the setup.
func TestOptionKindsDontCompileInTheWrongPlace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool isn't installed")
	}
	dirs, err := filepath.Glob("testdata/compile/*")
	if err != nil || len(dirs) < 2 {
		t.Fatalf("no programs under testdata/compile: %v", err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			out, err := exec.Command(goTool, "build", "-o", os.DevNull, "./"+dir).CombinedOutput()
			want := wantedError(t, filepath.Join(dir, "main.go"))
			if want == "" {
				if err != nil {
					t.Fatalf("the control program doesn't build:\n%s", out)
				}
				return
			}
			if err == nil {
				t.Fatalf("built, want the error %q", want)
			}
			if !strings.Contains(string(out), want) {
				t.Errorf("build output:\n%s\nwant it to contain %q", out, want)
			}
		})
	}
}

// wantedError returns the error named on a program's first line, "// want: <error>", or "" for the control program.
func wantedError(t *testing.T, file string) string {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Scan()
	if want, ok := strings.CutPrefix(s.Text(), "// want: "); ok {
		return want
	}
	return ""
}
