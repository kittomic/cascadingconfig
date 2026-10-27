package cascadingconfig_test

import (
	"net/netip"
	"reflect"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

type logConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

type withLog struct {
	Server server    `koanf:"server"`
	Log    logConfig `koanf:"log"`
}

// DL-72, api.md Defaults: WithDefaults is the lowest layer of every load, for Load, Decode and KoanfProvider alike.
// Only non-zero fields become defaults.
func TestWithDefaults(t *testing.T) {
	t.Parallel()
	defaults := cc.WithDefaults(withLog{Server: server{Port: 8080, Timeout: 5 * time.Second}, Log: logConfig{Level: "info"}})
	files := map[string]string{projFile: `{"server": {"port": 9000}}`}
	l := newLoader(t, files, nil, twoScopes, defaults)

	got, err := cc.Load[withLog](t.Context(), l)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := withLog{Server: server{Port: 9000, Timeout: 5 * time.Second}, Log: logConfig{Level: "info"}}
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}

	wantTree := cc.Tree{"server": map[string]any{"port": int64(9000), "timeout": "5s"}, "log": map[string]any{"level": "info"}}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tree := res.Tree(); !reflect.DeepEqual(tree, wantTree) {
		t.Errorf("Tree = %v, want %v", tree, wantTree)
	}
	var decoded withLog
	if err := res.Decode(&decoded); err != nil || decoded != want {
		t.Errorf("Decode = %+v, %v; want %+v", decoded, err, want)
	}
	if tree, err := l.KoanfProvider().Read(); err != nil || !reflect.DeepEqual(tree, wantTree) {
		t.Errorf("KoanfProvider = %v, %v; want %v", tree, err, wantTree)
	}
}

type Base struct {
	Name string `koanf:"name"`
}

type inner struct {
	N int `koanf:"n"`
	Z int `koanf:"z"`
}

// api.md Defaults, DL-153: defaults convert to canonical values with the decoding tag. Zero fields, nil pointers,
// skipped and unexported fields are left out; a non-nil pointer to a zero value is kept.
func TestDefaultsCanonical(t *testing.T) {
	t.Parallel()
	zero := 0
	v := struct {
		Base     `koanf:",squash"`
		Dur      time.Duration  `koanf:"dur"`
		At       time.Time      `koanf:"at"`
		Addr     netip.Addr     `koanf:"addr"`
		Int      int            `koanf:"int"`
		Small    uint8          `koanf:"small"`
		F32      float32        `koanf:"f32"`
		List     []int          `koanf:"list"`
		Map      map[string]int `koanf:"map"`
		Inner    inner          `koanf:"inner"`
		Empty    inner          `koanf:"empty"`
		ZeroPtr  *int           `koanf:"zero_ptr"`
		NilPtr   *int           `koanf:"nil_ptr"`
		Skip     string         `koanf:"-"`
		Zero     string         `koanf:"zero"`
		Any      any            `koanf:"any"`
		Untagged string
		hidden   string
	}{
		Base: Base{Name: "n"}, Dur: 90 * time.Second, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Addr: netip.MustParseAddr("10.0.0.1"), Int: 1, Small: 2, F32: 1.5, List: []int{1, 2}, Map: map[string]int{"a": 1},
		Inner: inner{N: 3}, ZeroPtr: &zero, Skip: "s", Any: "x", Untagged: "u", hidden: "h",
	}
	_ = v.hidden
	want := cc.Tree{
		"name": "n", "dur": "1m30s", "at": "2026-01-02T03:04:05Z", "addr": "10.0.0.1", "int": int64(1), "small": int64(2),
		"f32": 1.5, "list": []any{int64(1), int64(2)}, "map": map[string]any{"a": int64(1)},
		"inner": map[string]any{"n": int64(3)}, "zero_ptr": int64(0), "any": "x", "Untagged": "u",
	}
	if got := loadOK(t, nil, cc.WithDefaults(v)).Tree(); !reflect.DeepEqual(got, want) {
		t.Errorf("Tree = %#v\nwant %#v", got, want)
	}

	m := map[string]any{"a": 1, "b": map[string]any{"c": []string{"x"}}}
	wantMap := cc.Tree{"a": int64(1), "b": map[string]any{"c": []any{"x"}}}
	opt := cc.WithDefaults(m)
	l := newLoader(t, nil, nil, twoScopes, opt)
	m["a"] = 2
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := res.Tree(); !reflect.DeepEqual(got, wantMap) {
		t.Errorf("Tree = %#v, want %#v, converted once", got, wantMap)
	}
}

// DL-153: the tag is read after every option, so WithTag can come later.
func TestDefaultsUseTheTag(t *testing.T) {
	t.Parallel()
	v := struct {
		Port int `config:"port"`
	}{Port: 1}
	if got := loadOK(t, nil, cc.WithDefaults(v), cc.WithTag("config")).Tree(); !reflect.DeepEqual(got, cc.Tree{"port": int64(1)}) {
		t.Errorf("Tree = %v, want the config tag's key", got)
	}
}

// api.md Defaults: a higher null or _unset clears a default, and a pointer field then reads nil.
func TestDefaultsCleared(t *testing.T) {
	t.Parallel()
	one := 1
	type flat struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
		Ptr  *int   `koanf:"ptr"`
	}
	res := loadOK(t, map[string]string{projFile: `{"ptr": null, "_unset": ["host"]}`},
		cc.WithDefaults(flat{Host: "h", Port: 8080, Ptr: &one}))
	var v flat
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if v.Host != "" || v.Port != 8080 || v.Ptr != nil {
		t.Errorf("decoded %+v, want the host and pointer cleared", v)
	}
}

// api.md Defaults: OriginOf reports "set by defaults", with TrustSystem, and keeps it in a key's history.
func TestDefaultsOrigin(t *testing.T) {
	t.Parallel()
	res := loadOK(t, map[string]string{projFile: `{"port": 2}`}, cc.WithDefaults(map[string]any{"port": 1, "host": "h"}))
	o, ok := cc.OriginOf(res, "host")
	if !ok || o.Layer.Scope != cc.ScopeDefaults || o.Layer.Trust != cc.TrustSystem || o.Kind != cc.OriginSet {
		t.Errorf("OriginOf(host) = %+v, %v; want set by defaults", o, ok)
	}
	o, ok = cc.OriginOf(res, "port")
	if !ok || o.Layer.Path != projFile || len(o.History) != 1 || o.History[0].Layer.Scope != cc.ScopeDefaults {
		t.Errorf("OriginOf(port) = %+v, %v; want the file over the defaults", o, ok)
	}
}

// DL-153: the defaults scope is the lowest and always selected, so a later WithScopes keeps it; WithoutScopes removes
// it; a second WithDefaults replaces the first.
func TestDefaultsScope(t *testing.T) {
	t.Parallel()
	defaults := cc.WithDefaults(map[string]any{"a": 1})

	res := loadOK(t, nil, defaults, cc.WithScopes("proj"))
	if got := res.Tree(); got["a"] != int64(1) {
		t.Errorf("after WithScopes, Tree = %v, want the defaults", got)
	}
	rep := res.Describe()
	if len(rep.Scopes) == 0 {
		t.Fatalf("the report lists no scopes")
	}
	if s := rep.Scopes[0]; s.Name != cc.ScopeDefaults || s.Kind != "Defaults" || s.Trust != cc.TrustSystem || !s.Selected {
		t.Errorf("lowest scope = %+v, want the selected Defaults scope", s)
	}

	if got := loadOK(t, nil, defaults, cc.WithoutScopes(cc.ScopeDefaults)).Tree(); len(got) != 0 {
		t.Errorf("after WithoutScopes, Tree = %v, want it empty", got)
	}
	if got := loadOK(t, nil, defaults, cc.WithDefaults(map[string]any{"b": 2})).Tree(); !reflect.DeepEqual(got, cc.Tree{"b": int64(2)}) {
		t.Errorf("after a second WithDefaults, Tree = %v, want only b", got)
	}
}

// DL-76: changing a result's tree doesn't change the defaults of later loads.
func TestDefaultsAreCopied(t *testing.T) {
	t.Parallel()
	l := newLoader(t, nil, nil, twoScopes, cc.WithDefaults(map[string]any{"a": map[string]any{"b": 1}}))
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a, ok := res.Tree()["a"].(map[string]any)
	if !ok {
		t.Fatalf("Tree = %v, want the defaults", res.Tree())
	}
	a["b"] = "changed"
	again, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a, _ := again.Tree()["a"].(map[string]any); a["b"] != int64(1) {
		t.Errorf("a = %v after changing an earlier tree, want b = 1", a)
	}
}
