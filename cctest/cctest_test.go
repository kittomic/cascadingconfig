package cctest_test

import (
	"fmt"
	"io/fs"
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// load builds a loader from MiseLayout, which registers JSON, over fsys and h with one scope, and loads it.
func load(t *testing.T, fsys *cctest.FS, h cctest.Host, kind cc.ScopeKind) cc.Tree {
	t.Helper()
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout,
		cc.WithFileSystem(fsys), cctest.WithHost(h), cc.WithPlatformDirs(cc.XDGDirs),
		cc.WithScope("s", kind), cc.WithScopes("s"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res.Tree()
}

var host = cctest.Host{GOOS: "linux", GOARCH: "arm64", Home: "/home/me", Cwd: "/home/me", Env: map[string]string{}}

func dir(templates ...string) cc.ScopeKind {
	patterns := make([]cc.Pattern, len(templates))
	for i, tpl := range templates {
		patterns[i] = cc.Pattern{Template: tpl}
	}
	return cc.FixedDir("/d", patterns...)
}

func wantTree(t *testing.T, got cc.Tree, want cc.Tree) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tree() = %#v, want %#v", got, want)
	}
}

// DL-109: NewFS and WriteFile create files and their directories.
func TestFSFiles(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/d/a.json": `{"a": 1}`})
	fsys.WriteFile("/d/b.json", []byte(`{"b": 2}`), 0o644)
	wantTree(t, load(t, fsys, host, dir("a.json", "b.json")), cc.Tree{"a": int64(1), "b": int64(2)})

	fsys.WriteFile("/d/a.json", []byte(`{"a": 3}`), 0o644)
	fsys.Remove("/d/b.json")
	wantTree(t, load(t, fsys, host, dir("a.json", "b.json")), cc.Tree{"a": int64(3)})
}

// DL-109: Symlink, Link, Mkfifo, Mkdir and Chmod behave as on a real filesystem.
func TestFSSpecialEntries(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/real.json": `{"a": 1}`, "/d/locked.json": `{"x": 1}`})
	fsys.Symlink("/srv/real.json", "/d/link.json")
	fsys.Link("/srv/real.json", "/d/hard.json")
	fsys.Mkfifo("/d/fifo.json")
	fsys.Mkdir("/d/dir.json", 0o755)
	fsys.Chmod("/d/locked.json", 0)
	wantTree(t, load(t, fsys, host, dir("link.json", "fifo.json", "dir.json", "locked.json")), cc.Tree{"a": int64(1)})

	root, err := fsys.OpenRoot("/d")
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()
	link, _ := fsys.EvalSymlinks("/d/link.json")
	if link != "/srv/real.json" {
		t.Errorf("EvalSymlinks = %q, want /srv/real.json", link)
	}
	srv, err := fsys.OpenRoot("/srv")
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer srv.Close()
	hard, err1 := root.Stat("hard.json")
	target, err2 := srv.Stat("real.json")
	if err1 != nil || err2 != nil {
		t.Fatalf("Stat: %v, %v", err1, err2)
	}
	if !fsys.SameFile(hard, target) {
		t.Errorf("SameFile(hard link, its original) = false, want true")
	}
	entries, err := root.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	modes := map[string]fs.FileMode{}
	for _, e := range entries {
		modes[e.Name()] = e.Type()
	}
	want := map[string]fs.FileMode{
		"dir.json": fs.ModeDir, "fifo.json": fs.ModeNamedPipe, "hard.json": 0, "link.json": fs.ModeSymlink, "locked.json": 0,
	}
	if !reflect.DeepEqual(modes, want) {
		t.Errorf("ReadDir types = %v, want %v", modes, want)
	}
}

// DL-109: a symlink that leaves a root can't be followed through it, as with os.Root.
func TestFSRootConfinement(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/real.json": `{}`})
	fsys.Symlink("/srv/real.json", "/d/link.json")
	root, err := fsys.OpenRoot("/d")
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()
	if _, err := root.Stat("link.json"); err == nil {
		t.Errorf("Stat through a symlink leaving the root succeeded")
	}
	if _, err := root.Open("../srv/real.json"); err == nil {
		t.Errorf("Open(../srv/real.json) succeeded")
	}
}

// DL-108, DL-109: WithHost supplies env vars and the home directory; nothing is read from the process.
func TestWithHost(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/xdg/myapp/config.json": `{"from": "xdg"}`, "/home/me/.config/myapp/config.json": `{"from": "home"}`})
	h := host
	h.Env = map[string]string{"XDG_CONFIG_HOME": "/xdg", "MYAPP_KEY": "value"}
	wantTree(t, load(t, fsys, h, cc.FixedDir("{user}", cc.Pattern{Template: "config.json"})), cc.Tree{"from": "xdg"})
	wantTree(t, load(t, fsys, h, cc.EnvVars("MYAPP_")), cc.Tree{"key": "value"})
	wantTree(t, load(t, fsys, host, cc.FixedDir("{user}", cc.Pattern{Template: "config.json"})), cc.Tree{"from": "home"})
}

// failures records what a helper reports through testing.TB.
type failures struct {
	testing.TB
	msgs []string
}

func (f *failures) Helper() {}
func (f *failures) Errorf(format string, args ...any) {
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}
func (f *failures) Fatalf(format string, args ...any) {
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}
func (f *failures) Error(args ...any) { f.msgs = append(f.msgs, fmt.Sprint(args...)) }
func (f *failures) Fatal(args ...any) { f.msgs = append(f.msgs, fmt.Sprint(args...)) }

// DL-109: Golden passes when got matches the file under testdata, and fails when it doesn't or the file is missing.
func TestGolden(t *testing.T) {
	t.Parallel()
	cctest.Golden(t, "hello.golden", []byte("hello\n"))

	f := &failures{TB: t}
	cctest.Golden(f, "hello.golden", []byte("goodbye\n"))
	if len(f.msgs) == 0 {
		t.Errorf("a mismatch passed")
	}
	f = &failures{TB: t}
	cctest.Golden(f, "missing.golden", []byte("x"))
	if len(f.msgs) == 0 {
		t.Errorf("a missing golden file passed")
	}
}
