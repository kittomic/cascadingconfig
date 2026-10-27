package cascadingconfig_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// watchScope is a Watchable scope: it returns a Path source for every .json file in the directory /srv/drop, and
// WatchDirs returns dirs, or err.
type watchScope struct {
	fsys *cctest.FS

	mu        sync.Mutex
	dirs      []string
	err       error
	discovers int
	watches   int
}

func (s *watchScope) Discover(_ context.Context, d cc.Discovery) ([]cc.Source, error) {
	s.mu.Lock()
	s.discovers++
	s.mu.Unlock()
	r, err := d.FS.OpenRoot("/srv/drop")
	if err != nil {
		return nil, nil
	}
	defer r.Close()
	entries, err := r.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []cc.Source
	for _, e := range entries {
		if len(e.Name()) > 5 && e.Name()[len(e.Name())-5:] == ".json" {
			out = append(out, cc.Source{Path: "/srv/drop/" + e.Name()})
		}
	}
	return out, nil
}

func (s *watchScope) WatchDirs(context.Context, cc.Discovery) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watches++
	return slices.Clone(s.dirs), s.err
}

func (s *watchScope) set(dirs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirs, s.err = dirs, err
}

func (s *watchScope) counts() (discovers, watches int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.discovers, s.watches
}

// custWatch watches cust over s with a fake notifier and clock, reloading on every event.
func custWatch(t *testing.T, fsys *cctest.FS, s cc.Scope) (*cc.Watcher[cc.Tree], *cctest.Notifier, *records) {
	t.Helper()
	n, logs := cctest.NewNotifier(), &records{}
	l := newLoaderFS(t, fsys, nil, custOptions(s), cc.WithNotifier(n.New), cctest.WithClock(cctest.NewClock(t0)), cc.WithLogger(slog.New(logs)))
	w, err := l.Watch(t.Context(), nil, cc.WithDebounce(0, 0))
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, n, logs
}

// warned reports whether logs hold a warning with code.
func warned(logs *records, code cc.WarningCode) bool {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return slices.ContainsFunc(logs.got, func(r slog.Record) bool { return attrs(r)["code"] == string(code) })
}

// DL-239: a Path source is watched like an explicit file, without Watchable: its directory, for its name.
func TestCustomWatchPathSources(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/a.json": `{"v": 1}`})
	w, n, _ := custWatch(t, fsys, sources(cc.Source{Path: "/srv/a.json"}))
	if got := n.Watched(); !slices.Equal(got, []string{"/srv"}) {
		t.Errorf("watched %q, want /srv", got)
	}
	fsys.WriteFile("/srv/a.json", []byte(`{"v": 2}`), 0o644)
	n.Send("/srv/a.json")
	wantValue(t, w.Current(), "v", int64(2))
}

// DL-239: WatchDirs directories are watched with no name filter, so a new file there is found; the set is re-planned
// on every reload.
func TestCustomWatchDirs(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/drop/a.json": `{"a": 1}`, "/srv/other/x": ""})
	s := &watchScope{fsys: fsys, dirs: []string{"/srv/drop"}}
	w, n, _ := custWatch(t, fsys, s)
	if got := n.Watched(); !slices.Equal(got, []string{"/srv/drop"}) {
		t.Errorf("watched %q, want /srv/drop", got)
	}
	fsys.WriteFile("/srv/drop/b.json", []byte(`{"b": 2}`), 0o644)
	n.Send("/srv/drop/b.json")
	wantValue(t, w.Current(), "b", int64(2))

	s.set([]string{"/srv/drop", "/srv/other"}, nil)
	fsys.WriteFile("/srv/drop/c.txt", nil, 0o644)
	n.Send("/srv/drop/c.txt")
	if got := n.Watched(); !slices.Equal(got, []string{"/srv/drop", "/srv/other"}) {
		t.Errorf("after a reload, watched %q, want /srv/drop and /srv/other", got)
	}
	before, _ := s.counts()
	n.Send("/srv/other/anything")
	if after, _ := s.counts(); after != before+1 {
		t.Errorf("an event in a WatchDirs directory ran %d discoveries, want 1", after-before)
	}
}

// DL-239: a missing WatchDirs directory is watched through its nearest existing ancestor.
func TestCustomWatchMissingDir(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/x": ""})
	s := &watchScope{fsys: fsys, dirs: []string{"/srv/drop"}}
	w, n, _ := custWatch(t, fsys, s)
	if got := n.Watched(); !slices.Equal(got, []string{"/srv"}) {
		t.Errorf("watched %q, want /srv", got)
	}
	fsys.WriteFile("/srv/drop/a.json", []byte(`{"a": 1}`), 0o644)
	n.Send("/srv/drop")
	wantValue(t, w.Current(), "a", int64(1))
	if got := n.Watched(); !slices.Equal(got, []string{"/srv/drop"}) {
		t.Errorf("once it exists, watched %q, want /srv/drop", got)
	}
}

// DL-239: a relative WatchDirs directory, or an error, is a WarnWatchFailed warning, and the rest is still watched.
func TestCustomWatchDirsProblems(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/drop/a.json": `{}`, "/srv/other/x": ""})
	s := &watchScope{fsys: fsys, dirs: []string{"relative/dir", "/srv/other"}}
	_, n, logs := custWatch(t, fsys, s)
	if !warned(logs, cc.WarnWatchFailed) {
		t.Errorf("a relative directory wasn't warned about")
	}
	if got := n.Watched(); !slices.Equal(got, []string{"/srv/drop", "/srv/other"}) {
		t.Errorf("watched %q, want /srv/drop (its Path source) and /srv/other", got)
	}

	fsys2 := cctest.NewFS(map[string]string{"/srv/drop/a.json": `{}`})
	s2 := &watchScope{fsys: fsys2, err: errors.New("no idea")}
	_, n2, logs2 := custWatch(t, fsys2, s2)
	if !warned(logs2, cc.WarnWatchFailed) {
		t.Errorf("a WatchDirs error wasn't warned about")
	}
	if got := n2.Watched(); !slices.Equal(got, []string{"/srv/drop"}) {
		t.Errorf("watched %q, want /srv/drop, for the Path source", got)
	}
}

// DL-239: a plain Load never calls WatchDirs; a watched load calls it after Discover.
func TestCustomWatchDirsOnlyWhenWatched(t *testing.T) {
	t.Parallel()
	fsys := cctest.NewFS(map[string]string{"/srv/drop/a.json": `{}`})
	s := &watchScope{fsys: fsys, dirs: []string{"/srv/drop"}}
	custOK(t, fsys, s)
	if d, w := s.counts(); d != 1 || w != 0 {
		t.Errorf("a plain Load: %d discoveries and %d WatchDirs calls, want 1 and 0", d, w)
	}
	custWatch(t, fsys, s)
	if d, w := s.counts(); d != 2 || w != 1 {
		t.Errorf("after Watch: %d discoveries and %d WatchDirs calls, want 2 and 1", d, w)
	}
}
