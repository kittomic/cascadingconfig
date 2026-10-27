package cascadingconfig_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// wantWatched checks the notifier's watched directories.
func (e *watchEnv) wantWatched(when string, want ...string) {
	e.t.Helper()
	slices.Sort(want)
	if got := e.n.Watched(); !slices.Equal(got, want) {
		e.t.Errorf("%s: watched %q, want %q", when, got, want)
	}
}

// DL-224: each scope's directory is watched, and nothing else.
func TestWatchSetScopeDirs(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`, sysFile: `{"b": 1}`})
	e.watch(nil)
	e.wantWatched("after Watch", sysDir, work)
}

// DL-224: an event whose name the directory's filter doesn't pass doesn't reload; one it passes does.
func TestWatchSetFilter(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`, sysFile: `{"b": 1}`})
	e.watch(nil)
	e.save(work+"/notes.txt", "hello")
	e.save(work+"/myapp.json.swp", "x")
	e.save(sysDir+"/other.json", `{}`)
	e.settle()
	e.wantLoads(1, "events for other files")
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	e.wantLoads(2, "an event for the config file")
}

// DL-224: a missing directory is watched through its nearest existing ancestor, for the next path segment only; once
// it appears, the reload re-plans the set.
func TestWatchSetMissingDir(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	e.wantWatched("with no /etc/myapp", "/etc", work)
	e.save("/etc/passwd", "root")
	e.settle()
	e.wantLoads(1, "an event for another name in /etc")

	e.fs.WriteFile(sysFile, []byte(`{"b": 2}`), 0o644)
	e.n.Send(sysDir)
	e.settle()
	e.wantLoads(2, "/etc/myapp appearing")
	wantValue(t, w.Current(), "b", int64(2))
	e.wantWatched("once /etc/myapp exists", sysDir, work)
}

// DL-224: an ancestor several levels up is watched when the directories between are missing too.
func TestWatchSetDistantAncestor(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{}`})
	l := newLoaderFS(t, e.fs, nil, e.options(
		cc.WithScope("deep", cc.FixedDir("/srv/a/b/c", cc.Pattern{Template: "config.json"})),
		cc.WithScopes("vals", "sys", "proj", "deep"),
	)...)
	w, err := l.Watch(t.Context(), nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	e.wantWatched("with no /srv", "/", "/etc", work)
	e.save("/tmp", "")
	e.settle()
	e.wantLoads(1, "an event for /tmp")
	e.fs.WriteFile("/srv/a/b/c/config.json", []byte(`{"deep": true}`), 0o644)
	e.n.Send("/srv")
	e.settle()
	wantValue(t, w.Current(), "deep", true)
}

// DL-224: an explicit config file's directory is watched, filtered to that file's name.
func TestWatchSetExplicitFile(t *testing.T) {
	const explicit = "/srv/conf/app.json"
	e := newWatchEnv(t, map[string]string{rootCfg: `{}`, explicit: `{"x": 1}`})
	w := e.watch(nil, cc.WithConfigFiles(explicit))
	if !slices.Contains(e.n.Watched(), "/srv/conf") {
		t.Errorf("watched %q, want /srv/conf among them", e.n.Watched())
	}
	e.save("/srv/conf/other.json", `{}`)
	e.settle()
	e.wantLoads(1, "an event for another file")
	e.save(explicit, `{"x": 2}`)
	e.settle()
	wantValue(t, w.Current(), "x", int64(2))
}

// DL-224: a local include's directory is watched, filtered to the included file's name.
func TestWatchSetLocalInclude(t *testing.T) {
	const inc = work + "/inc/base.json"
	e := newWatchEnv(t, map[string]string{rootCfg: includeOf(`"a": 1`, "inc/base.json"), inc: `{"b": 1}`})
	w := e.watch(nil)
	e.wantWatched("with a local include", "/etc", work, work+"/inc")
	e.save(work+"/inc/other.json", `{}`)
	e.settle()
	e.wantLoads(1, "an event for another file")
	e.save(inc, `{"b": 2}`)
	e.settle()
	wantValue(t, w.Current(), "b", int64(2))
}

// DL-224: a symlinked config file's target directory is watched too, filtered to the target's name.
func TestWatchSetSymlinkTarget(t *testing.T) {
	const target = "/home/me/dotfiles/myapp.json"
	e := newWatchEnv(t, map[string]string{target: `{"a": 1}`, sysFile: `{}`})
	e.fs.Symlink(target, rootCfg)
	w := e.watch(nil)
	e.wantWatched("with a symlinked file", sysDir, work, "/home/me/dotfiles")
	e.save("/home/me/dotfiles/other.json", `{}`)
	e.settle()
	e.wantLoads(1, "an event for another file")
	e.save(target, `{"a": 2}`)
	e.settle()
	wantValue(t, w.Current(), "a", int64(2))
}

// design.md Hot reload, "Symlink swaps": an event for any name in a directory holding a symlinked candidate resolves
// the symlink again, so a Kubernetes ConfigMap ..data swap is a change.
func TestWatchSetSymlinkSwap(t *testing.T) {
	e := newWatchEnv(t, map[string]string{
		sysDir + "/..2026_10_21_1/config.json": `{"v": 1}`,
		sysDir + "/..2026_10_21_2/config.json": `{"v": 2}`,
		rootCfg:                                `{}`,
	})
	e.fs.Symlink(sysDir+"/..2026_10_21_1", sysDir+"/..data")
	e.fs.Symlink(sysDir+"/..data/config.json", sysFile)
	w := e.watch(nil)
	wantValue(t, w.Current(), "v", int64(1))
	e.fs.Remove(sysDir + "/..data")
	e.fs.Symlink(sysDir+"/..2026_10_21_2", sysDir+"/..data")
	e.n.Send(sysDir + "/..data")
	e.settle()
	wantValue(t, w.Current(), "v", int64(2))
}

// DL-224: a failed reload re-plans from the previous set plus what it recorded, and the next good reload drops what's
// no longer needed.
func TestWatchSetFailedReload(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.watch(nil)
	e.wantWatched("with no /etc/myapp", "/etc", work)
	e.fs.WriteFile(sysFile, []byte(`{"b": `), 0o644)
	e.n.Send(sysDir)
	e.settle()
	if c := e.last(); c.Err == nil {
		t.Fatalf("the reload with a broken file succeeded")
	}
	e.wantWatched("after the failed reload", "/etc", sysDir, work)
	e.save(sysFile, `{"b": 2}`)
	e.settle()
	e.wantWatched("after the good reload", sysDir, work)
}

// DL-224: a changed bootstrap env re-plans the watch set, so an env file that was filtered out before is
// watched after.
func TestWatchSetBootstrapEnv(t *testing.T) {
	const rc, prod = work + "/.myapprc.json", work + "/myapp.prod.json"
	fsys := cctest.NewFS(map[string]string{rc: `{"env": ["dev"]}`, rootCfg: `{"a": 1}`})
	n := cctest.NewNotifier()
	var loads atomic.Int64
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout,
		cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), cctest.WithClock(cctest.NewClock(t0)),
		cc.WithNotifier(n.New),
		cc.WithLayerTransform(func(_ context.Context, l cc.Layer, t cc.Tree) (cc.Tree, error) {
			if l.Path == rootCfg {
				loads.Add(1)
			}
			return t, nil
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w, err := l.Watch(t.Context(), nil, cc.WithDebounce(0, 0))
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()

	fsys.WriteFile(prod, []byte(`{"a": 2}`), 0o644)
	n.Send(prod)
	if got := loads.Load(); got != 1 {
		t.Errorf("an event for myapp.prod.json with env dev: %d loads, want 1", got)
	}
	fsys.WriteFile(rc, []byte(`{"env": ["prod"]}`), 0o644)
	n.Send(rc)
	wantValue(t, w.Current(), "a", int64(2))
	fsys.WriteFile(prod, []byte(`{"a": 3}`), 0o644)
	n.Send(prod)
	wantValue(t, w.Current(), "a", int64(3))
}
