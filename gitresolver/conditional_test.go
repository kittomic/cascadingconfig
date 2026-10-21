package gitresolver_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/gitresolver"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// subcommandLogger is a wrapper body that appends each git invocation's subcommand to log.
func subcommandLogger(log string) string {
	return `cmd=""; skip=0
for a in "$@"; do
  if [ "$skip" = 1 ]; then skip=0; continue; fi
  case "$a" in
    -c|-C|--git-dir|--work-tree) skip=1 ;;
    -*) ;;
    *) cmd="$a"; break ;;
  esac
done
echo "$cmd" >> '` + log + `'`
}

// subcommands returns the subcommands logged so far, and clears the log.
func subcommands(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	_ = os.Remove(log)
	return strings.Fields(string(b))
}

// DL-229: the validator is the commit id. Once the TTL has passed, an unchanged branch is checked with ls-remote and
// nothing is fetched; a moved branch is fetched.
func TestRevalidateBranch(t *testing.T) {
	r := newRepo(t, "sha1").write("a.json", `{"v": 1}`)
	first := r.commit("one")
	log := marker(t, "subcommands")
	res := fileGit(gitresolver.WithGit(wrapper(t, subcommandLogger(log))))
	ref := gitRef(r.url(), "a.json", "main")
	dir, clock := filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	opts := []cc.Option{cc.WithIncludeCache(dir, time.Hour), cctest.WithClock(clock)}

	if got := layerAt(t, loadOK(t, res, []string{ref}, opts...), ref).Remote.Identity; got != first {
		t.Fatalf("identity = %q, want %s", got, first)
	}
	subcommands(t, log)

	clock.Advance(2 * time.Hour)
	l := layerAt(t, loadOK(t, res, []string{ref}, opts...), ref)
	if l.Remote.Cache != "revalidated" {
		t.Errorf("an unchanged branch: Cache = %q, want revalidated", l.Remote.Cache)
	}
	if got := subcommands(t, log); !slices.Contains(got, "ls-remote") || slices.Contains(got, "fetch") {
		t.Errorf("an unchanged branch ran %q, want ls-remote and no fetch", got)
	}

	r.write("a.json", `{"v": 2}`)
	second := r.commit("two")
	clock.Advance(2 * time.Hour)
	got := loadOK(t, res, []string{ref}, opts...)
	if v := got.Tree()["v"]; v != int64(2) {
		t.Errorf("a moved branch: v = %v, want 2", v)
	}
	if id := layerAt(t, got, ref).Remote.Identity; id != second {
		t.Errorf("a moved branch: identity = %q, want %s", id, second)
	}
	if cmds := subcommands(t, log); !slices.Contains(cmds, "fetch") {
		t.Errorf("a moved branch ran %q, want a fetch", cmds)
	}
}

// DL-229: a tag is checked the same way.
func TestRevalidateTag(t *testing.T) {
	r := newRepo(t, "sha1").write("a.json", `{"v": 1}`)
	r.commit("one")
	r.git("tag", "v1")
	log := marker(t, "subcommands")
	res := fileGit(gitresolver.WithGit(wrapper(t, subcommandLogger(log))))
	ref := gitRef(r.url(), "a.json", "v1")
	dir, clock := filepath.Join(t.TempDir(), "includes"), cctest.NewClock(t0)
	opts := []cc.Option{cc.WithIncludeCache(dir, time.Hour), cctest.WithClock(clock)}
	loadOK(t, res, []string{ref}, opts...)
	subcommands(t, log)
	clock.Advance(2 * time.Hour)
	if got := layerAt(t, loadOK(t, res, []string{ref}, opts...), ref).Remote.Cache; got != "revalidated" {
		t.Errorf("Cache = %q, want revalidated", got)
	}
	if got := subcommands(t, log); slices.Contains(got, "fetch") {
		t.Errorf("an unchanged tag ran %q, want no fetch", got)
	}
}
