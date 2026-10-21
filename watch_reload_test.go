package cascadingconfig_test

import (
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// plan M9, "New files": creating a candidate that didn't exist, myapp.local.json or a file in a new conf.d folder,
// reloads with it.
func TestWatchNewFiles(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"a": 1}`})
	n := cctest.NewNotifier()
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout,
		cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), cctest.WithClock(cctest.NewClock(t0)),
		cc.WithNotifier(n.New),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w, err := l.Watch(t.Context(), nil, cc.WithDebounce(0, 0))
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()

	fsys.WriteFile(work+"/myapp.local.json", []byte(`{"local": true}`), 0o644)
	n.Send(work + "/myapp.local.json")
	wantValue(t, w.Current(), "local", true)

	fsys.WriteFile(work+"/.myapp/conf.d/a.json", []byte(`{"confd": true}`), 0o644)
	n.Send(work + "/.myapp")
	wantValue(t, w.Current(), "confd", true)

	fsys.WriteFile(work+"/.myapp/conf.d/b.json", []byte(`{"confd2": true}`), 0o644)
	n.Send(work + "/.myapp/conf.d/b.json")
	wantValue(t, w.Current(), "confd2", true)
}

// DL-37, DL-222, DL-227: a half-written file keeps the last good config; the handler gets Err, with Old and New the
// last good snapshot and the failed Result; the failure is logged as WarnReloadFailed; the next event retries.
func TestWatchFailedReload(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	e.save(rootCfg, `{"a": 2, "b": `)
	e.settle()
	c := e.last()
	if c.Err == nil {
		t.Fatalf("Change.Err is nil after a broken save")
	}
	wantValue(t, w.Current(), "a", int64(1))
	wantValue(t, c.Old, "a", int64(1))
	wantValue(t, c.New, "a", int64(1))
	if c.Result == nil || c.Result.Tree() != nil {
		t.Errorf("Change.Result = %v, want the failed result, with a nil tree", c.Result)
	}
	if len(c.Diff) != 0 {
		t.Errorf("Diff = %v, want none", c.Diff)
	}
	if !e.logged(slog.LevelWarn, cc.WarnReloadFailed) {
		t.Errorf("no %s warning was logged", cc.WarnReloadFailed)
	}

	e.save(rootCfg, `{"a": 2, "b": 3}`)
	e.settle()
	c = e.last()
	if c.Err != nil {
		t.Fatalf("Change.Err = %v after the fix", c.Err)
	}
	wantValue(t, w.Current(), "b", int64(3))
	wantValue(t, c.Old, "a", int64(1))
	wantValue(t, c.New, "a", int64(2))
}

// DL-222: every failed reload calls the handler, and so does the first good reload after a failure, even when it brings
// back the same values.
func TestWatchRecovery(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.watch(nil)
	e.save(rootCfg, `{"a": `)
	e.settle()
	e.save(rootCfg, `{"a": 1`)
	e.settle()
	got := e.wantChanges(2, "two failed reloads")
	for i, c := range got {
		if c.Err == nil {
			t.Errorf("call %d: Err is nil", i)
		}
	}
	e.save(rootCfg, `{"a": 1}`)
	e.settle()
	got = e.wantChanges(3, "the recovery")
	if c := got[len(got)-1]; c.Err != nil || len(c.Diff) != 0 {
		t.Errorf("recovery: Err = %v, Diff = %v; want neither", c.Err, c.Diff)
	}
	e.save(rootCfg, `{"a": 1}`)
	e.settle()
	e.wantChanges(3, "a good reload after the recovery")
}

// watchCfg is the typed config the DL-72 tests decode into.
type watchCfg struct {
	Port int    `koanf:"port"`
	Name string `koanf:"name"`
}

// DL-72: every reload decodes into a fresh value, so a key deleted from a file loses its value, and defaults still
// apply.
func TestWatchFreshValue(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"port": 9000, "name": "svc"}`})
	l := e.loader(cc.WithDefaults(watchCfg{Port: 8080}))
	var got calls[cc.Change[watchCfg]]
	w, err := cc.Watch[watchCfg](t.Context(), l, got.add)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	if c := w.Current(); c.Port != 9000 || c.Name != "svc" {
		t.Fatalf("Current() = %+v, want port 9000 and name svc", c)
	}
	e.save(rootCfg, `{}`)
	e.settle()
	if c := w.Current(); c.Port != 8080 || c.Name != "" {
		t.Errorf("after deleting both keys: Current() = %+v, want the default port and no name", c)
	}
	if all := got.all(); len(all) != 1 || all[0].Old.Name != "svc" || all[0].New.Port != 8080 {
		t.Errorf("changes = %+v, want one from svc:9000 to :8080", all)
	}
}

// DL-222, api.md Load and Watch: a decode failure during a reload is a failed reload.
func TestWatchDecodeFailure(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"port": 9000}`})
	var got calls[cc.Change[watchCfg]]
	w, err := cc.Watch[watchCfg](t.Context(), e.loader(), got.add)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	e.save(rootCfg, `{"port": "not a number"}`)
	e.settle()
	all := got.all()
	if len(all) != 1 || all[0].Err == nil {
		t.Fatalf("changes = %+v, want one failed reload", all)
	}
	var de *cc.DecodeError
	if !errors.As(all[0].Err, &de) || de.Key.String() != "port" {
		t.Errorf("Err = %v, want a *DecodeError for port", all[0].Err)
	}
	if w.Current().Port != 9000 {
		t.Errorf("Current() = %+v, want the last good port", w.Current())
	}
}

// DL-40, DL-233: a touch, or an edit that changes no value, doesn't call the handler; WithReloadOnAnyEvent calls it
// after every reload.
func TestWatchNoChange(t *testing.T) {
	const content = `{"a": 1, "b": [1, 2]}`
	for _, anyEvent := range []bool{false, true} {
		e := newWatchEnv(t, map[string]string{rootCfg: content})
		var opts []cc.WatchOption
		if anyEvent {
			opts = append(opts, cc.WithReloadOnAnyEvent())
		}
		e.watch(opts)
		e.save(rootCfg, content)
		e.settle()
		e.save(rootCfg, "{\n  \"b\": [1, 2],\n  \"a\": 1\n}\n")
		e.settle()
		e.wantLoads(3, "two saves")
		want := 0
		if anyEvent {
			want = 2
		}
		for _, c := range e.wantChanges(want, "two saves that change nothing") {
			if c.Err != nil || len(c.Diff) != 0 {
				t.Errorf("WithReloadOnAnyEvent: Err = %v, Diff = %v; want neither", c.Err, c.Diff)
			}
		}
	}
}

// DL-77, DL-40: a value that moves to another file isn't a change, though its origin changed.
func TestWatchValueMoves(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`, sysFile: `{}`})
	e.watch(nil)
	e.fs.WriteFile(sysFile, []byte(`{"a": 1}`), 0o644)
	e.n.Send(sysFile)
	e.save(rootCfg, `{}`)
	e.settle()
	e.wantLoads(2, "the move")
	e.wantChanges(0, "a value moved to another file")
}

// DL-233: values are compared exactly: the same instant written differently is a change, and NaN equals NaN.
func TestWatchCompare(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"at": "2026-01-01T00:00:00Z"}`})
	e.setVals(cc.Tree{"x": math.NaN()})
	w := e.watch(nil)
	if err := w.Reload(t.Context()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	e.wantChanges(0, "NaN reloaded")
	e.save(rootCfg, `{"at": "2026-01-01T00:00:00+00:00"}`)
	e.settle()
	got := e.wantChanges(1, "the same instant written differently")
	if len(got) == 1 && (len(got[0].Diff) != 1 || got[0].Diff[0].Path.String() != "at") {
		t.Errorf("Diff = %v, want at", got[0].Diff)
	}
}

// diffLines describes a diff as "kind path old→new", for comparing.
func diffLines(d []cc.KeyDiff) []string {
	var out []string
	for _, k := range d {
		out = append(out, k.Kind.String()+" "+k.Path.String()+" "+k.OldOrigin.Layer.Path+"→"+k.NewOrigin.Layer.Path)
	}
	return out
}

// DL-221: Diff lists added, removed and changed leaves, sorted by path, with their origins; an array is one leaf; a
// table that becomes a scalar is one change; values are canonical.
func TestWatchDiff(t *testing.T) {
	e := newWatchEnv(t, map[string]string{
		rootCfg: `{"keep": 1, "gone": 1, "num": 1, "list": [1, 2], "tbl": {"x": 1, "y": 2}, "deep": {"a": {"b": 1}}}`,
		sysFile: `{"sys": "s"}`,
	})
	e.watch(nil)
	e.fs.WriteFile(sysFile, []byte(`{"sys": "s", "new": {"n": 1}}`), 0o644)
	e.n.Send(sysFile)
	e.save(rootCfg, `{"keep": 1, "num": 2, "list": [1, 3], "tbl": "flat", "deep": {"a": {"b": 2}}}`)
	e.settle()
	c := e.last()
	want := []string{
		"changed deep.a.b " + rootCfg + "→" + rootCfg,
		"removed gone " + rootCfg + "→",
		"changed list " + rootCfg + "→" + rootCfg,
		"added new.n →" + sysFile,
		"changed num " + rootCfg + "→" + rootCfg,
		"changed tbl " + rootCfg + "→" + rootCfg,
	}
	if got := diffLines(c.Diff); !slices.Equal(got, want) {
		t.Errorf("Diff =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, d := range c.Diff {
		switch d.Path.String() {
		case "num":
			if d.Old != int64(1) || d.New != int64(2) {
				t.Errorf("num: %#v → %#v, want 1 → 2 as int64", d.Old, d.New)
			}
		case "gone":
			if d.Kind != cc.DiffRemoved || d.Old != int64(1) || d.New != nil {
				t.Errorf("gone: %v %#v → %#v, want removed 1 → nil", d.Kind, d.Old, d.New)
			}
		case "list":
			if !slices.Equal(d.New.([]any), []any{int64(1), int64(3)}) {
				t.Errorf("list: New = %#v, want [1 3]", d.New)
			}
		case "tbl":
			if _, ok := d.Old.(map[string]any); !ok || d.New != "flat" {
				t.Errorf("tbl: %#v → %#v, want a table → flat", d.Old, d.New)
			}
		}
	}
}

// DL-221: Diff values are copies, so changing one can't reach the snapshots.
func TestWatchDiffCopies(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"list": [1]}`})
	w := e.watch(nil)
	e.save(rootCfg, `{"list": [2]}`)
	e.settle()
	c := e.last()
	if len(c.Diff) != 1 {
		t.Fatalf("Diff = %v, want one entry", c.Diff)
	}
	c.Diff[0].New.([]any)[0] = "changed"
	wantValue(t, cc.Tree{"v": w.Current()["list"].([]any)[0]}, "v", int64(2))
}

// api.md Results, "No config values in the report": KeyDiff.String shows the kind, the path and the files, never a
// value.
func TestWatchDiffString(t *testing.T) {
	e := newWatchEnv(t, map[string]string{rootCfg: `{"token": "s3cr3t-old", "port": 1}`})
	e.watch(nil)
	e.save(rootCfg, `{"token": "s3cr3t-new", "port": 31337}`)
	e.settle()
	c := e.last()
	if len(c.Diff) != 2 {
		t.Fatalf("Diff = %v, want two entries", c.Diff)
	}
	for _, d := range c.Diff {
		s := d.String()
		for _, secret := range []string{"s3cr3t", "31337"} {
			if strings.Contains(s, secret) {
				t.Errorf("String() = %q shows a value", s)
			}
		}
		if !strings.Contains(s, d.Path.String()) || !strings.Contains(s, rootCfg) || !strings.Contains(s, "changed") {
			t.Errorf("String() = %q, want the kind, the path and the file", s)
		}
	}
	for k, want := range map[cc.DiffKind]string{cc.DiffAdded: "added", cc.DiffRemoved: "removed", cc.DiffChanged: "changed"} {
		if got := k.String(); got != want {
			t.Errorf("DiffKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
