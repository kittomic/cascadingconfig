package cascadingconfig_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	cc "github.com/kittomic/cascadingconfig"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// loadReal loads one FixedDir scope over dir from the real filesystem, the default, with format/json's decoder.
func loadReal(t *testing.T, dir string, templates ...string) (*cc.Result, *records) {
	t.Helper()
	logs := &records{}
	l, err := cc.New("myapp", cc.WithDecoder(ccjson.Decoder()), cc.WithLogger(slogFor(logs)), useKind(cc.FixedDir(dir, p(templates...)...)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res, logs
}

func slogFor(r *records) *slog.Logger { return slog.New(r) }

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// design.md Filesystem hook: the default filesystem reads real files, following a symlink out of the directory.
func TestOSFileSystem(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app", "config.json"), `{"a": 1}`)
	writeFile(t, filepath.Join(dir, "shared", "real.json"), `{"b": 2}`)
	if err := os.Symlink(filepath.Join(dir, "shared", "real.json"), filepath.Join(dir, "app", "linked.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	res, _ := loadReal(t, filepath.Join(dir, "app"), "config.json", "linked.json")
	if got := res.Tree(); got["a"] != int64(1) || got["b"] != int64(2) {
		t.Errorf("Tree() = %#v, want a and b", got)
	}
}

// DL-66: on a real filesystem, even a case-insensitive one such as macOS's, MyApp.JSON doesn't match myapp.json.
func TestOSCaseSensitiveMatching(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "MyApp.JSON"), `{"a": 1}`)
	res, _ := loadReal(t, dir, "{name}.json")
	if got := res.Tree(); len(got) != 0 {
		t.Errorf("Tree() = %#v, want nothing", got)
	}
}

// D16: on a real filesystem, an unreadable file is skipped with a warning.
func TestOSUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeFile(t, path, `{"a": 1}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	res, logs := loadReal(t, dir, "config.json")
	if len(res.Tree()) != 0 || len(logs.got) != 1 || attrs(logs.got[0])["code"] != string(cc.WarnUnreadable) {
		t.Errorf("Tree() = %#v, logs = %v; want nothing loaded and one unreadable warning", res.Tree(), logs.got)
	}
}

// design.md Filesystem hook: MountFS makes an fs.FS appear at an absolute directory, and nothing else exists.
func TestMountFS(t *testing.T) {
	fsys := cc.MountFS("/virtual", fstest.MapFS{"myapp/config.json": {Data: []byte(`{"a": 1}`)}})
	l, err := cc.New("myapp", cc.WithFileSystem(fsys), cc.WithDecoder(ccjson.Decoder()),
		cc.WithScope("in", cc.FixedDir("/virtual/{name}", p("config.json")...)),
		cc.WithScope("out", cc.FixedDir("/elsewhere", p("config.json")...)),
		cc.WithScopes("in", "out"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Tree(); len(got) != 1 || got["a"] != int64(1) {
		t.Errorf("Tree() = %#v, want {a: 1}", got)
	}
}
