package cascadingconfig_test

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// hasRecord reports whether r holds a record at level with the warning code.
func hasRecord(r *records, level slog.Level, code cc.WarningCode) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.got {
		if rec.Level == level && attrs(rec)["code"] == string(code) {
			return true
		}
	}
	return false
}

// ops returns the attributes of r's Debug records whose op is op (DL-263).
func ops(r *records, op string) []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]string
	for _, rec := range r.got {
		if a := attrs(rec); rec.Level == slog.LevelDebug && a["op"] == op {
			out = append(out, a)
		}
	}
	return out
}

func (r *records) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = nil
}

func (r *records) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// skippedEnvVar is a loader whose env scope skips MYAPP_A____B with a WarnEnvVarSkipped warning, logging to opts.
func skippedEnvVar(t *testing.T, opts ...cc.Option) *cc.Loader {
	t.Helper()
	base := []cc.Option{cc.WithScope("env", cc.EnvVars("MYAPP_")), cc.WithScopes("env")}
	return newLoaderFS(t, cctest.NewFS(nil), map[string]string{"MYAPP_A____B": "1"}, append(base, opts...)...)
}

// DL-263: warnings are logged at their code's level, end to end.
func TestLogLevelsEndToEnd(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	t.Run("untrusted is Info", func(t *testing.T) {
		r := &records{}
		_, err := incLoad(t, cctest.NewFS(map[string]string{rootCfg: `{"a": 1}`}), cc.WithLogger(slog.New(r)),
			cc.WithScopePolicy("proj", cc.InheritDefault(), cc.RequireTrust(cc.TrustPaths())))
		if err != nil {
			t.Fatal(err)
		}
		if !hasRecord(r, slog.LevelInfo, cc.WarnUntrusted) {
			t.Error("no Info record for an untrusted file")
		}
	})
	t.Run("no-home is Info", func(t *testing.T) {
		r := &records{}
		h := cctest.Host{GOOS: "linux", GOARCH: "amd64", Cwd: work, Env: map[string]string{}}
		l := newLoaderHost(t, cctest.NewFS(nil), h, cc.WithLogger(slog.New(r)),
			cc.WithPlatformDirs(cc.XDGDirs), cc.WithScope("u", cc.FixedDir("{user}", cc.Pattern{Template: "config.json"})),
			cc.WithScopes("u"))
		if _, err := l.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !hasRecord(r, slog.LevelInfo, cc.WarnNoHome) {
			t.Error("no Info record for a host without a home")
		}
	})
	t.Run("env-var-skipped is Warn", func(t *testing.T) {
		r := &records{}
		if _, err := skippedEnvVar(t, cc.WithLogger(slog.New(r))).Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !hasRecord(r, slog.LevelWarn, cc.WarnEnvVarSkipped) {
			t.Error("no Warn record for a skipped variable")
		}
	})
	t.Run("unknown-key is Warn", func(t *testing.T) {
		r := &records{}
		l := newLoaderFS(t, cctest.NewFS(map[string]string{rootCfg: `{"a": 1, "b": 2}`}), nil, incScopes,
			cc.WithLogger(slog.New(r)))
		if _, err := cc.Load[struct {
			A int `koanf:"a"`
		}](t.Context(), l); err != nil {
			t.Fatal(err)
		}
		if !hasRecord(r, slog.LevelWarn, cc.WarnUnknownKey) {
			t.Error("no Warn record for an unknown key")
		}
	})
}

// DL-263: WithLogLevel drops records below its level before the logger, and a *slog.LevelVar changes it between
// loads.
func TestWithLogLevel(t *testing.T) {
	t.Parallel()
	r := &records{}
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	l := skippedEnvVar(t, cc.WithLogger(slog.New(r)), cc.WithLogLevel(level))
	load := func() {
		t.Helper()
		r.reset()
		if _, err := l.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	load()
	if !hasRecord(r, slog.LevelWarn, cc.WarnEnvVarSkipped) || len(ops(r, "load")) != 0 {
		t.Errorf("at Warn: the warning logged %v, Debug records %d; want it logged and none",
			hasRecord(r, slog.LevelWarn, cc.WarnEnvVarSkipped), len(ops(r, "load")))
	}
	level.Set(slog.LevelError)
	load()
	if r.len() != 0 {
		t.Errorf("at Error: %d records, want none", r.len())
	}
	level.Set(slog.LevelDebug)
	load()
	if len(ops(r, "load")) != 1 {
		t.Errorf("at Debug: %d load records, want 1", len(ops(r, "load")))
	}
}

// DL-263: a nil level is an *OptionError from WithLogLevel.
func TestWithLogLevelNil(t *testing.T) {
	t.Parallel()
	_, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithLogLevel(nil))
	for _, e := range cc.Errors(err) {
		var oe *cc.OptionError
		if errors.As(e, &oe) && oe.Op == "WithLogLevel" {
			return
		}
	}
	t.Errorf("WithLogLevel(nil): %v, want an *OptionError from WithLogLevel", err)
}

// logScope is a custom scope that logs at Debug and Warn through its Discovery's logger.
type logScope struct{}

func (logScope) Discover(_ context.Context, d cc.Discovery) ([]cc.Source, error) {
	d.Logger.Debug("custom debug")
	d.Logger.Warn("custom warn")
	return nil, nil
}

// DL-263: a custom scope's Discovery.Logger is filtered by WithLogLevel too.
func TestCustomScopeLoggerFiltered(t *testing.T) {
	t.Parallel()
	r := &records{}
	l := newLoaderFS(t, cctest.NewFS(nil), nil, cc.WithScope("c", cc.Custom(logScope{})), cc.WithScopes("c"),
		cc.WithLogger(slog.New(r)), cc.WithLogLevel(slog.LevelWarn))
	if _, err := l.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var got []string
	for _, rec := range r.got {
		got = append(got, rec.Message)
	}
	if len(got) != 1 || got[0] != "custom warn" {
		t.Errorf("records %q, want only \"custom warn\"", got)
	}
}

// design.md Warnings and logging: WithLogger with slog.DiscardHandler silences the loader; without it, the default
// logger gets the warning, so the test can fail.
func TestDiscardHandlerSilences(t *testing.T) {
	r := &records{}
	prev := slog.Default()
	slog.SetDefault(slog.New(r))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, err := skippedEnvVar(t).Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !hasRecord(r, slog.LevelWarn, cc.WarnEnvVarSkipped) {
		t.Fatal("the default logger got no warning, so this test can't show silence")
	}
	r.reset()
	if _, err := skippedEnvVar(t, cc.WithLogger(slog.New(slog.DiscardHandler))).Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.len() != 0 {
		t.Errorf("the default logger got %d records with a discarding logger set", r.len())
	}
}

// DL-263: a load traces its summary and each layer at Debug.
func TestLoadTracing(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	r := &records{}
	files := map[string]string{rootCfg: `{"a": 1}`, sysDir + "/config.json": `{"b": 1}`}
	if _, err := incLoad(t, cctest.NewFS(files), cc.WithLogger(slog.New(r)),
		cc.WithScopePolicy("sys", cc.InheritDefault(), cc.RequireTrust(cc.TrustPaths()))); err != nil {
		t.Fatal(err)
	}
	loads := ops(r, "load")
	if len(loads) != 1 || loads[0]["loaded"] != "1" || loads[0]["skipped"] != "1" || loads[0]["duration"] == "" {
		t.Errorf("load records %v, want one with loaded 1, skipped 1 and a duration", loads)
	}
	status := map[string]string{}
	for _, a := range ops(r, "layer") {
		status[a["path"]+" "+a["scope"]] = a["status"]
	}
	if status[rootCfg+" proj"] != "loaded" || status[sysDir+"/config.json sys"] != "untrusted" {
		t.Errorf("layer records %v, want %s loaded in proj and the sys file untrusted", status, rootCfg)
	}
}

// DL-263: each remote include is traced with its cache outcome.
func TestRemoteTracing(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	const ref = "https://h.example/a.json"
	c := newCacheEnv(t)
	f := newFake("https").serve(ref, `{"v": 1}`)
	for _, want := range []string{"fetched", "cached"} {
		r := &records{}
		c.ok(t, f, []string{ref}, cc.WithLogger(slog.New(r)))
		got := ops(r, "remote")
		if len(got) != 1 || got[0]["ref"] != ref || got[0]["cache"] != want {
			t.Errorf("remote records %v, want one for %s, %s", got, ref, want)
		}
	}
}

// DL-263: a watcher traces its watch set, each event, and each reload with its number of changes.
func TestWatcherTracing(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.watch(nil)
	e.save(rootCfg, `{"a": 2, "b": 1}`)
	e.settle()

	if sets := ops(e.logs, "watch-set"); len(sets) == 0 {
		t.Error("no watch-set record")
	} else if n, err := strconv.Atoi(sets[0]["dirs"]); err != nil || n == 0 {
		t.Errorf("watch-set record %v, want a positive dirs", sets[0])
	}
	found := false
	for _, a := range ops(e.logs, "event") {
		found = found || a["path"] == rootCfg
	}
	if !found {
		t.Errorf("event records %v, none for %s", ops(e.logs, "event"), rootCfg)
	}
	if len(ops(e.logs, "reload")) == 0 {
		t.Error("no reload record")
	}
	reloaded := ops(e.logs, "reloaded")
	if len(reloaded) == 0 || reloaded[len(reloaded)-1]["changes"] != "2" {
		t.Errorf("reloaded records %v, want the last with 2 changes", reloaded)
	}
}
