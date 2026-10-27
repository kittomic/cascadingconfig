package compat_test

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// KoanfProvider satisfies koanf.Provider by method set alone (DL-160).
var _ koanf.Provider = (*cc.KoanfProvider)(nil)

// both decodes tree with cascadingconfig, through Result.Decode or DecodeAt, and with koanf, loading through
// KoanfProvider (DL-160) and decoding with UnmarshalWithConf. opts configure the loader the way conf configures koanf.
func both[T any](t *testing.T, tree cc.Tree, path string, conf koanf.UnmarshalConf, opts ...cc.Option) (ours, theirs T, ourErr, theirErr error) {
	t.Helper()
	base := []cc.Option{cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me"}),
		cc.WithScope("v", cc.Values(tree)), cc.WithScopes("v")}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if path == "" {
		ourErr = res.Decode(&ours)
	} else {
		ourErr = cc.DecodeAt(res, path, &ours)
	}

	k := koanf.New(".")
	if err := k.Load(l.KoanfProvider(), nil); err != nil {
		t.Fatalf("koanf Load: %v", err)
	}
	theirErr = k.UnmarshalWithConf(path, &theirs, conf)
	return ours, theirs, ourErr, theirErr
}

// same checks that both decoded without errors, to equal values.
func same[T any](t *testing.T, tree cc.Tree, path string, conf koanf.UnmarshalConf, opts ...cc.Option) T {
	t.Helper()
	ours, theirs, ourErr, theirErr := both[T](t, tree, path, conf, opts...)
	if ourErr != nil || theirErr != nil {
		t.Fatalf("errors: ours %v, koanf %v", ourErr, theirErr)
	}
	if !reflect.DeepEqual(ours, theirs) {
		t.Errorf("decoded differently:\nours  %#v\nkoanf %#v", ours, theirs)
	}
	return ours
}

type upper string

func (u *upper) UnmarshalText(b []byte) error {
	*u = upper(strings.ToUpper(string(b)))
	return nil
}

type Embedded struct {
	Name string `koanf:"name"`
}

type item struct {
	ID   int    `koanf:"id"`
	Kind string `koanf:"kind"`
}

type everything struct {
	Embedded `koanf:",squash"`
	Days     time.Duration     `koanf:"days"`
	Fraction time.Duration     `koanf:"fraction"`
	Mixed    time.Duration     `koanf:"mixed"`
	Plain    time.Duration     `koanf:"plain"`
	Addr     netip.Addr        `koanf:"addr"`
	Upper    upper             `koanf:"upper"`
	Int      int               `koanf:"int"`
	Bool     bool              `koanf:"bool"`
	Str      string            `koanf:"str"`
	Float    float64           `koanf:"float"`
	Single   []string          `koanf:"single"`
	Items    []item            `koanf:"items"`
	Labels   map[string]string `koanf:"labels"`
	Ptr      *int              `koanf:"ptr"`
	Null     *int              `koanf:"null"`
	Any      any               `koanf:"any"`
	At       time.Time         `koanf:"at"`
	AtText   string            `koanf:"at_text"`
	Untagged string
}

var everythingTree = cc.Tree{
	"name": "n", "days": "7d", "fraction": "1.5d", "mixed": "1d12h", "plain": "90s",
	"addr": "127.0.0.1", "upper": "abc",
	"int": "8080", "bool": "true", "str": int64(5), "float": "1.5", "single": "x",
	"items":  []any{map[string]any{"id": "1", "kind": "a"}, map[string]any{"id": int64(2)}},
	"labels": map[string]any{"a": "b", "n": int64(1)},
	"ptr":    int64(3), "null": nil, "any": map[string]any{"k": []any{true}},
	"at": "2026-01-02T03:04:05+02:00", "at_text": "2026-01-02T03:04:05Z",
	"untagged": "u",
}

// DL-148: the same struct and tree decode identically with koanf and with cascadingconfig, including day durations,
// TextUnmarshaler, weak typing and RFC 3339 dates.
func TestParity(t *testing.T) {
	t.Parallel()
	got := same[everything](t, everythingTree, "", koanf.UnmarshalConf{})
	if got.Days != 7*24*time.Hour || got.Addr.String() != "127.0.0.1" || got.At.IsZero() || got.Untagged != "u" {
		t.Errorf("decoded %+v, want every field set", got)
	}
}

// DecodeAt matches koanf's Unmarshal with a path.
func TestParityAtPath(t *testing.T) {
	t.Parallel()
	tree := cc.Tree{"server": map[string]any{"port": "80", "timeout": "2d"}}
	type srv struct {
		Port    int           `koanf:"port"`
		Timeout time.Duration `koanf:"timeout"`
	}
	if got := same[srv](t, tree, "server", koanf.UnmarshalConf{}); got.Port != 80 || got.Timeout != 48*time.Hour {
		t.Errorf("decoded %+v, want the server table", got)
	}
	same[srv](t, tree, "missing", koanf.UnmarshalConf{})
}

// WithTag and WithFlatPaths match koanf's Tag and FlatPaths.
func TestParityTagAndFlatPaths(t *testing.T) {
	t.Parallel()
	type tagged struct {
		Port int `config:"port"`
	}
	if got := same[tagged](t, cc.Tree{"port": int64(1)}, "", koanf.UnmarshalConf{Tag: "config"}, cc.WithTag("config")); got.Port != 1 {
		t.Errorf("decoded %+v, want port 1", got)
	}

	type flat struct {
		Port int    `koanf:"server.port"`
		Host string `koanf:"server.host"`
	}
	tree := cc.Tree{"server": map[string]any{"port": int64(2), "host": "h"}}
	if got := same[flat](t, tree, "", koanf.UnmarshalConf{FlatPaths: true}, cc.WithFlatPaths()); got.Port != 2 || got.Host != "h" {
		t.Errorf("decoded %+v, want the flattened keys", got)
	}
}

// Both reject the same mismatches.
func TestParityErrors(t *testing.T) {
	t.Parallel()
	type bad struct {
		Int  int           `koanf:"int"`
		Dur  time.Duration `koanf:"dur"`
		Date time.Time     `koanf:"date"`
	}
	for name, tree := range map[string]cc.Tree{
		"an int":      {"int": "x"},
		"a duration":  {"dur": "7days"},
		"a date only": {"date": "2026-01-02"},
	} {
		_, _, ourErr, theirErr := both[bad](t, tree, "", koanf.UnmarshalConf{})
		if ourErr == nil || theirErr == nil {
			t.Errorf("%s: errors ours %v, koanf %v; want both", name, ourErr, theirErr)
		}
	}
}
