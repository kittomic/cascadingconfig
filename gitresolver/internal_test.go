package gitresolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// DL-211, design.md git, "Fetching by commit": when a server refuses to fetch a commit id that isn't a branch or tag
// tip, as protocol v0 servers do by default, the resolver fetches every branch and tag and finds the commit there.
// Not parallel, because it changes protocolVersion.
func TestFetchByCommitFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := filepath.Join(t.TempDir(), "repo")
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"v": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.json")
	run("commit", "-q", "-m", "one")
	old := run("rev-parse", "HEAD")
	run("commit", "-q", "--allow-empty", "-m", "two")

	saved := protocolVersion
	protocolVersion = "0"
	t.Cleanup(func() { protocolVersion = saved })

	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.json": fmt.Sprintf(`{"include": %q}`,
		"git::file://"+filepath.ToSlash(dir)+"//a.json?ref="+old)})
	l, err := cc.New("myapp", cc.WithFileSystem(fsys), cc.WithDecoder(ccjson.Decoder()),
		cctest.WithHost(cctest.Host{GOOS: "linux", Home: "/home/me", Cwd: "/home/me/work"}),
		cc.WithScope("proj", cc.FixedDir("/home/me/work", cc.Pattern{Template: "myapp.json"})), cc.WithScopes("proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat), cc.WithIncludeResolver(New(WithProtocols("file"))),
		cc.WithoutIncludeCache())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Tree()["v"]; got != int64(1) {
		t.Errorf("v = %v, want 1", got)
	}
}

// DL-211: ls-tree lines parse into their fields.
func TestParseLsTree(t *testing.T) {
	t.Parallel()
	oid := strings.Repeat("ab", 20)
	for _, tc := range []struct {
		line                  string
		mode, kind, oid, path string
		ok                    bool
	}{
		{"100644 blob " + oid + "\ta.json", "100644", "blob", oid, "a.json", true},
		{"100755 blob " + oid + "\tdir/x.json", "100755", "blob", oid, "dir/x.json", true},
		{"120000 blob " + oid + "\tlink.json", "120000", "blob", oid, "link.json", true},
		{"160000 commit " + oid + "\tsub", "160000", "commit", oid, "sub", true},
		{"040000 tree " + oid + "\tdir", "040000", "tree", oid, "dir", true},
		{"100644 blob " + strings.Repeat("cd", 32) + "\ta.json", "100644", "blob", strings.Repeat("cd", 32), "a.json", true},
		{"", "", "", "", "", false},
		{"100644 blob " + oid + " a.json", "", "", "", "", false},
		{"100644 blob xyz\ta.json", "", "", "", "", false},
		{"10064 blob " + oid + "\ta.json", "", "", "", "", false},
		{"100644 blob " + oid + "\t", "", "", "", "", false},
	} {
		mode, kind, oid, path, ok := parseLsTree(tc.line)
		if mode != tc.mode || kind != tc.kind || oid != tc.oid || path != tc.path || ok != tc.ok {
			t.Errorf("parseLsTree(%q) = %q %q %q %q %v, want %q %q %q %q %v",
				tc.line, mode, kind, oid, path, ok, tc.mode, tc.kind, tc.oid, tc.path, tc.ok)
		}
	}
}

var (
	lsMode = regexp.MustCompile(`^[0-7]{6}$`)
	lsOID  = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// FuzzLsTree checks that the ls-tree parser never panics, and accepts only well-formed lines, which it splits so they
// can be put back together.
func FuzzLsTree(f *testing.F) {
	oid := strings.Repeat("ab", 20)
	for _, s := range []string{"100644 blob " + oid + "\ta.json", "160000 commit " + oid + "\tsub", "x", "100644 blob\t"} {
		f.Add(s)
	}
	good := "100644 blob " + oid + "\ta.json"
	f.Fuzz(func(t *testing.T, line string) {
		mode, kind, oid, path, ok := parseLsTree(line)
		if line == good && !ok {
			t.Fatalf("the well-formed line %q was rejected", line)
		}
		if !ok {
			return
		}
		if mode+" "+kind+" "+oid+"\t"+path != line {
			t.Errorf("%q parsed into %q %q %q %q", line, mode, kind, oid, path)
		}
		if !lsMode.MatchString(mode) || !slices.Contains([]string{"blob", "tree", "commit"}, kind) ||
			!lsOID.MatchString(oid) || path == "" || strings.ContainsAny(path, "\x00\n\t") {
			t.Errorf("%q was accepted: %q %q %q %q", line, mode, kind, oid, path)
		}
	})
}

// FuzzGitArgs checks the git commands built from a reference (DL-212): there are five, each has --end-of-options,
// everything before it is the same whatever the reference, and ls-tree gets the path last, after --.
func FuzzGitArgs(f *testing.F) {
	commit, blob := strings.Repeat("1", 40), strings.Repeat("2", 40)
	fixed := commands("https://h.example/r.git", "main", "a.json", commit, blob)
	for _, s := range [][3]string{
		{"https://h.example/r.git", "main", "a.json"},
		{"file:///srv/r", "-x", "--upload-pack=x"},
		{"ssh://git@h.example/r.git", "", "-a.json"},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, repo, ref, path string) {
		cmds := commands(repo, ref, path, commit, blob)
		if len(cmds) != 5 || len(fixed) != 5 {
			t.Fatalf("%d commands, want 5", len(cmds))
		}
		for i, cmd := range cmds {
			sep := slices.Index(cmd, "--end-of-options")
			if sep < 0 {
				t.Fatalf("command %q has no --end-of-options", cmd)
			}
			fsep := slices.Index(fixed[i], "--end-of-options")
			if !slices.Equal(cmd[:sep], fixed[i][:fsep]) {
				t.Errorf("command %q has options %q that depend on the reference, want %q", cmd, cmd[:sep], fixed[i][:fsep])
			}
		}
		if ls := cmds[2]; len(ls) < 2 || ls[len(ls)-1] != path || ls[len(ls)-2] != "--" {
			t.Errorf("ls-tree command %q doesn't pass the path %q last, after --", ls, path)
		}
	})
}
