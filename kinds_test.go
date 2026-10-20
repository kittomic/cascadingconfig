package cascadingconfig

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// valueLayer returns the found layer for scope.
func valueLayer(t *testing.T, res *Result, scope ScopeName) Layer {
	t.Helper()
	for _, l := range res.found {
		if l.Scope == scope {
			return l
		}
	}
	t.Fatalf("no layer for scope %s in:\n%s", scope, found(res))
	return Layer{}
}

// DL-113: ValuesFunc runs on every load, so a reload sees new values. Its default trust is TrustUser.
func TestValuesFunc(t *testing.T) {
	calls := 0
	f := func(context.Context) (Tree, error) {
		calls++
		return Tree{"calls": int64(calls)}, nil
	}
	l := loader(t, memfs.New(nil), WithScope("flags", ValuesFunc(f)), WithScopes("flags"))
	for want := int64(1); want <= 2; want++ {
		res, err := l.Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := res.Tree()["calls"]; got != want {
			t.Errorf("calls = %v, want %d", got, want)
		}
		if got := valueLayer(t, res, "flags").Trust; got != TrustUser {
			t.Errorf("trust = %v, want TrustUser", got)
		}
	}
}

// DL-113: an error from a ValuesFunc, or a non-canonical value, is a 500 whose Op is ValuesFunc.
func TestValuesFuncErrors(t *testing.T) {
	appErr := errors.New("flags unavailable")
	tests := []struct {
		name string
		f    func(context.Context) (Tree, error)
	}{
		{"an error", func(context.Context) (Tree, error) { return nil, appErr }},
		{"a non-canonical value", func(context.Context) (Tree, error) { return Tree{"port": 8080}, nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := loadFails(t, memfs.New(nil), WithScope("flags", ValuesFunc(tc.f)), WithScopes("flags"))
			wantCode(t, err, CodeInternal)
			if be, ok := asErr[*BaseError](err); !ok || be.Op != "ValuesFunc" {
				t.Errorf("error = %v, want a *BaseError with Op ValuesFunc", err)
			}
		})
	}
	err := loadFails(t, memfs.New(nil), WithScope("flags", ValuesFunc(tests[0].f)), WithScopes("flags"))
	if !errors.Is(err, appErr) {
		t.Errorf("the error doesn't wrap the function's error")
	}
}

// DL-113: EmbeddedFS searches the root of an fs.FS. Its default trust is TrustProject.
func TestEmbeddedFS(t *testing.T) {
	fsys := fstest.MapFS{
		"myapp.json":      {Data: []byte(`{"a": "embedded", "b": 1}`)},
		"conf/extra.json": {Data: []byte(`{"b": 2}`)},
	}
	res := mustLoad(t, memfs.New(nil),
		WithScope("defaults", EmbeddedFS(fsys, Pattern{"{name}.{ext}"}, Pattern{"conf/extra.{ext}"})),
		WithScopes("defaults"),
	)
	wantTree(t, res, `{"a": "embedded", "b": 2}`)
	for _, l := range res.found {
		if l.Trust != TrustProject {
			t.Errorf("%s: trust = %v, want TrustProject", l.Path, l.Trust)
		}
	}
}

func envLoad(t *testing.T, env map[string]string, change func(*hooks.Host), opts ...Option) *Result {
	t.Helper()
	all := append([]Option{hostWith(env, change), WithScope("env", EnvVars("MYAPP_")), WithScopes("env")}, opts...)
	return mustLoad(t, memfs.New(nil), all...)
}

// DL-113, design.md Value overrides: EnvVars strips the prefix, lowercases, and nests on __. Values are strings.
// Variables bound to discovery settings are never read, and other prefixes are ignored. Its default trust is
// TrustUser.
func TestEnvVars(t *testing.T) {
	res := envLoad(t, map[string]string{
		"MYAPP_SERVER__PORT": "8080",
		"MYAPP_LOG_LEVEL":    "debug",
		"MYAPP_CONFIG_DIR":   "/elsewhere",
		"OTHER_X":            "y",
		"myapp_lower":        "z",
	}, nil, WithEnvBinding(BindConfigDir, ""))
	wantTree(t, res, `{"server": {"port": "8080"}, "log_level": "debug"}`)
	if got := valueLayer(t, res, "env").Trust; got != TrustUser {
		t.Errorf("trust = %v, want TrustUser", got)
	}
}

// DL-113: a name with an empty segment is skipped with a warning; when two names clash, the later name wins, with a
// warning.
func TestEnvVarsEdgeCases(t *testing.T) {
	res := envLoad(t, map[string]string{"MYAPP_A____B": "x", "MYAPP_": "y", "MYAPP_OK": "1"}, nil)
	wantTree(t, res, `{"ok": "1"}`)
	if l := valueLayer(t, res, "env"); !hasWarning(l.Warnings, WarnEnvVarSkipped) {
		t.Errorf("warnings = %#v, want WarnEnvVarSkipped", l.Warnings)
	}

	res = envLoad(t, map[string]string{"MYAPP_A": "1", "MYAPP_A__B": "2"}, nil)
	wantTree(t, res, `{"a": {"b": "2"}}`)
	if l := valueLayer(t, res, "env"); !hasWarning(l.Warnings, WarnEnvVarClash) {
		t.Errorf("warnings = %#v, want WarnEnvVarClash", l.Warnings)
	}
}

// DL-113: the prefix is matched case-sensitively, except on Windows.
func TestEnvVarsPrefixCase(t *testing.T) {
	res := envLoad(t, map[string]string{"myapp_x": "1"}, func(h *hooks.Host) { h.GOOS = "windows" })
	wantTree(t, res, `{"x": "1"}`)
}

// DL-113: MapEnv replaces the mapping: it can type values and skip variables. A non-canonical value is a 500.
func TestMapEnv(t *testing.T) {
	mapper := func(name, value string) (Path, any, bool) {
		if name != "MYAPP_PORT" {
			return nil, nil, false
		}
		return Path{"server", "port"}, int64(len(value)), true
	}
	res := mustLoad(t, memfs.New(nil),
		hostWith(map[string]string{"MYAPP_PORT": "8080", "MYAPP_OTHER": "x"}, nil),
		WithScope("env", EnvVars("MYAPP_", MapEnv(mapper))), WithScopes("env"),
	)
	wantTree(t, res, `{"server": {"port": 4}}`)

	bad := func(string, string) (Path, any, bool) { return Path{"port"}, 8080, true }
	err := loadFails(t, memfs.New(nil),
		hostWith(map[string]string{"MYAPP_PORT": "8080"}, nil),
		WithScope("env", EnvVars("MYAPP_", MapEnv(bad))), WithScopes("env"),
	)
	wantCode(t, err, CodeInternal)
}

// design.md Value overrides, DL-113: a value scope can't remove keys, so the unset key is dropped with a warning,
// wherever it appears.
func TestValueScopesDropTheUnsetKey(t *testing.T) {
	res := mustLoad(t, memfs.New(map[string]string{"/d/config.json": `{"a": 1, "b": {"c": 1}}`}),
		fixed("files", "/d", "config.json"),
		WithScope("flags", Values(Tree{"_unset": []any{"a"}, "b": map[string]any{"_unset": []any{"c"}, "d": int64(2)}})),
		WithScopes("files", "flags"),
	)
	wantTree(t, res, `{"a": 1, "b": {"c": 1, "d": 2}}`)
	if l := valueLayer(t, res, "flags"); !hasWarning(l.Warnings, WarnReservedKey) {
		t.Errorf("warnings = %#v, want WarnReservedKey", l.Warnings)
	}

	res = envLoad(t, map[string]string{"MYAPP__UNSET": "a"}, nil)
	wantTree(t, res, `{}`)
	if l := valueLayer(t, res, "env"); !hasWarning(l.Warnings, WarnReservedKey) {
		t.Errorf("warnings = %#v, want WarnReservedKey", l.Warnings)
	}
}

// The trust of FixedDir defaults to TrustProject.
func TestFixedDirTrust(t *testing.T) {
	res := mustLoad(t, memfs.New(map[string]string{"/d/config.json": `{}`}), fixed("s", "/d", "config.json"), WithScopes("s"))
	if len(res.found) != 1 {
		t.Fatalf("found:\n%s\nwant one layer", found(res))
	}
	if got := res.found[0].Trust; got != TrustProject {
		t.Errorf("trust = %v, want TrustProject", got)
	}
	if !reflect.DeepEqual(res.found[0].Scope, ScopeName("s")) {
		t.Errorf("scope = %q, want s", res.found[0].Scope)
	}
}
