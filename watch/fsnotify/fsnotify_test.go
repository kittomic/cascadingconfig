package fsnotify_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
	"github.com/kittomic/cascadingconfig/watch/fsnotify"
)

// These tests use the real filesystem, real events and real timers (DL-232), so they wait for what they expect with a
// generous deadline rather than a fixed sleep.

const deadline = 10 * time.Second

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// records is a slog handler that keeps every record.
type records struct {
	mu  sync.Mutex
	got []slog.Record
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, rec)
	return nil
}
func (r *records) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *records) WithGroup(string) slog.Handler      { return r }

func (r *records) has(code cc.WarningCode) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.got {
		found := false
		rec.Attrs(func(a slog.Attr) bool {
			found = found || (a.Key == "code" && a.Value.String() == string(code))
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// watchDir watches one FixedDir scope over dir, for config.json, with fsnotify and a short debounce, and returns the
// watcher, a channel of its changes, and its log.
func watchDir(t *testing.T, dir string, wopts ...cc.WatchOption) (*cc.Watcher[cc.Tree], <-chan cc.Change[cc.Tree], *records) {
	t.Helper()
	logs := &records{}
	l, err := cc.New("myapp",
		cc.WithDecoder(ccjson.Decoder()), cc.WithLogger(slog.New(logs)), cc.WithNotifier(fsnotify.New),
		cc.WithScope("s", cc.FixedDir(dir, cc.Pattern{Template: "config.json"})), cc.WithScopes("s"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	changes := make(chan cc.Change[cc.Tree], 16)
	w, err := l.Watch(t.Context(), func(_ context.Context, c cc.Change[cc.Tree]) { changes <- c },
		append([]cc.WatchOption{cc.WithDebounce(20*time.Millisecond, 200*time.Millisecond)}, wopts...)...)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, changes, logs
}

// waitFor waits for a change whose tree has v at key "v".
func waitFor(t *testing.T, changes <-chan cc.Change[cc.Tree], v int64, what string) {
	t.Helper()
	timeout := time.After(deadline)
	for {
		select {
		case c := <-changes:
			if c.Err == nil && c.New["v"] == v {
				return
			}
		case <-timeout:
			t.Fatalf("%s: no change to v=%d within %v", what, v, deadline)
		}
	}
}

// DL-232: the notifier reports a written file in a watched directory, by its full path.
func TestNotifierEvents(t *testing.T) {
	dir := t.TempDir()
	n, err := fsnotify.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()
	if err := n.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	p := filepath.Join(dir, "config.json")
	write(t, p, `{}`)
	timeout := time.After(deadline)
	for {
		select {
		case got := <-n.Events():
			if got == p {
				return
			}
		case err := <-n.Errors():
			t.Fatalf("Errors: %v", err)
		case <-timeout:
			t.Fatalf("no event for %s within %v", p, deadline)
		}
	}
}

// DL-232: Chmod-only events are dropped.
func TestNotifierDropsChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permissions")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	write(t, p, `{}`)
	n, err := fsnotify.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()
	if err := n.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	write(t, marker, "")
	timeout := time.After(deadline)
	for {
		select {
		case got := <-n.Events():
			if got == p {
				t.Fatalf("a chmod reported %s", p)
			}
			if got == marker {
				return
			}
		case <-timeout:
			t.Fatalf("no event for the marker within %v", deadline)
		}
	}
}

// DL-232: Close can be called twice, and closes Events.
func TestNotifierClose(t *testing.T) {
	n, err := fsnotify.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	select {
	case _, ok := <-n.Events():
		if ok {
			t.Errorf("an event after Close")
		}
	case <-time.After(deadline):
		t.Errorf("Events wasn't closed")
	}
}

// plan M9: an editor's save, writing a temporary file and renaming it over the config file, reloads.
func TestEditorRename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	write(t, p, `{"v": 1}`)
	w, changes, _ := watchDir(t, dir)
	tmp := filepath.Join(dir, ".config.json.swp")
	write(t, tmp, `{"v": 2}`)
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	waitFor(t, changes, 2, "a rename over the file")
	write(t, filepath.Join(dir, ".config.json.swp"), `{"v": 3}`)
	if err := os.Rename(filepath.Join(dir, ".config.json.swp"), p); err != nil {
		t.Fatal(err)
	}
	waitFor(t, changes, 3, "a second rename, after the first replaced the file")
	if got := w.Current()["v"]; got != int64(3) {
		t.Errorf("Current v = %v, want 3", got)
	}
}

// plan M9, design.md Hot reload: a Kubernetes ConfigMap update, which the kubelet makes by writing a new timestamped
// directory, pointing a ..data_tmp symlink at it and renaming that over ..data, reloads.
func TestConfigMapSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "..2026_10_21_12_00_00.1", "config.json"), `{"v": 1}`)
	if err := os.Symlink("..2026_10_21_12_00_00.1", filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "config.json"), filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	w, changes, _ := watchDir(t, dir)
	if got := w.Current()["v"]; got != int64(1) {
		t.Fatalf("Current v = %v, want 1", got)
	}
	write(t, filepath.Join(dir, "..2026_10_21_12_05_00.2", "config.json"), `{"v": 2}`)
	if err := os.Symlink("..2026_10_21_12_05_00.2", filepath.Join(dir, "..data_tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "..data_tmp"), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "..2026_10_21_12_00_00.1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, changes, 2, "the ..data swap")
}

// DL-225, DL-227, plan M9: a directory that can't be watched falls back to polling, with WarnWatchFailed.
func TestFallbackToPolling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can watch a mode 000 directory")
	}
	dir := filepath.Join(t.TempDir(), "conf")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, changes, logs := watchDir(t, dir, cc.WithPollInterval(50*time.Millisecond))
	if !logs.has(cc.WarnWatchFailed) {
		t.Errorf("no %s warning was logged", cc.WarnWatchFailed)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "config.json"), `{"v": 1}`)
	waitFor(t, changes, 1, "a file in the polled directory")
}
