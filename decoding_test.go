package cascadingconfig_test

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

type server struct {
	Host    string        `koanf:"host"`
	Port    int           `koanf:"port"`
	Timeout time.Duration `koanf:"timeout"`
}

type appConfig struct {
	Server server   `koanf:"server"`
	Tags   []string `koanf:"tags"`
	Debug  bool     `koanf:"debug"`
}

// api.md Decoding, DL-148: Load decodes the merged tree into a fresh T, with koanf's weak typing and hooks.
func TestLoadDecodes(t *testing.T) {
	files := map[string]string{
		sysFile:  `{"server": {"host": "a", "port": 80}, "tags": ["x"]}`,
		projFile: `{"server": {"port": "8080", "timeout": "7d"}, "debug": "true"}`,
	}
	l := newLoader(t, files, nil, twoScopes)
	want := appConfig{Server: server{Host: "a", Port: 8080, Timeout: 7 * 24 * time.Hour}, Tags: []string{"x"}, Debug: true}

	got, err := cc.Load[appConfig](t.Context(), l)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server != want.Server || len(got.Tags) != 1 || got.Tags[0] != "x" || !got.Debug {
		t.Errorf("Load = %+v, want %+v", got, want)
	}

	ptr, err := cc.Load[*appConfig](t.Context(), l)
	if err != nil || ptr == nil || ptr.Server != want.Server {
		t.Errorf("Load[*appConfig] = %+v, %v; want %+v", ptr, err, want)
	}

	m, err := cc.Load[map[string]any](t.Context(), l)
	if err != nil || m["debug"] != "true" {
		t.Errorf("Load[map] = %v, %v; want the tree", m, err)
	}
}

// DL-72: every load decodes into a fresh value, so a key deleted from a file loses its value on the next load.
func TestLoadDecodesFresh(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{projFile: `{"server": {"host": "a", "port": 80}}`})
	l, err := cc.New("myapp", cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), cc.WithDecoder(jsonDecoder()), twoScopes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, err := cc.Load[appConfig](t.Context(), l); err != nil || got.Server.Host != "a" {
		t.Fatalf("first Load = %+v, %v", got, err)
	}
	fsys.WriteFile(projFile, []byte(`{"server": {"port": 80}}`), fs.FileMode(0o644))
	if got, err := cc.Load[appConfig](t.Context(), l); err != nil || got.Server.Host != "" {
		t.Errorf("second Load = %+v, %v; want an empty host", got, err)
	}
}

// api.md Errors: Load returns the zero T with any error, a load error or a decode error.
func TestLoadFailsWithZeroT(t *testing.T) {
	l := newLoader(t, map[string]string{projFile: `{"server": {"host": "a", "port": "x"}}`}, nil, twoScopes)
	got, err := cc.Load[appConfig](t.Context(), l)
	if err == nil || got.Server.Host != "" {
		t.Errorf("Load = %+v, %v; want the zero value and an error", got, err)
	}

	l = newLoader(t, map[string]string{projFile: `{`}, nil, twoScopes)
	if got, err := cc.Load[appConfig](t.Context(), l); !errors.Is(err, cc.ErrParse) || got.Server.Host != "" {
		t.Errorf("Load = %+v, %v; want the zero value and a parse error", got, err)
	}
}

// upper is a string type whose text form is upper-cased, as koanf's fixed TextUnmarshaler hook supports.
type upper string

func (u *upper) UnmarshalText(b []byte) error {
	*u = upper(strings.ToUpper(string(b)))
	return nil
}

// api.md Decoding, DL-148: koanf's three hooks, in its order, with weak typing.
func TestDecodeHooks(t *testing.T) {
	var v struct {
		Days     time.Duration `koanf:"days"`
		Fraction time.Duration `koanf:"fraction"`
		Mixed    time.Duration `koanf:"mixed"`
		Plain    time.Duration `koanf:"plain"`
		Addr     netip.Addr    `koanf:"addr"`
		Upper    upper         `koanf:"upper"`
		Int      int           `koanf:"int"`
		Bool     bool          `koanf:"bool"`
		Str      string        `koanf:"str"`
		Float    float64       `koanf:"float"`
	}
	tree := cc.Tree{
		"days": "7d", "fraction": "1.5d", "mixed": "1d12h", "plain": "90s",
		"addr": "127.0.0.1", "upper": "abc",
		"int": "8080", "bool": "true", "str": int64(5), "float": "1.5",
	}
	if err := decodeValues(t, tree, &v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"days", v.Days, 7 * 24 * time.Hour},
		{"fraction", v.Fraction, 36 * time.Hour},
		{"mixed", v.Mixed, 36 * time.Hour},
		{"plain", v.Plain, 90 * time.Second},
		{"addr", v.Addr, netip.MustParseAddr("127.0.0.1")},
		{"upper", v.Upper, upper("ABC")},
		{"int", v.Int, 8080},
		{"bool", v.Bool, true},
		{"str", v.Str, "5"},
		{"float", v.Float, 1.5},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// DL-48, DL-148: an RFC 3339 string decodes into time.Time and into string. A date-only or local date-time string
// into time.Time is a *DecodeError.
func TestDates(t *testing.T) {
	var v struct {
		At     time.Time `koanf:"at"`
		Offset time.Time `koanf:"offset"`
		Text   string    `koanf:"text"`
	}
	tree := cc.Tree{"at": "2026-01-02T03:04:05Z", "offset": "2026-01-02T03:04:05.5+02:00", "text": "2026-01-02T03:04:05Z"}
	if err := decodeValues(t, tree, &v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !v.At.Equal(want) {
		t.Errorf("at = %v, want %v", v.At, want)
	}
	if want := time.Date(2026, 1, 2, 1, 4, 5, 5e8, time.UTC); !v.Offset.Equal(want) {
		t.Errorf("offset = %v, want %v", v.Offset, want)
	}
	if v.Text != "2026-01-02T03:04:05Z" {
		t.Errorf("text = %q, want the RFC 3339 text", v.Text)
	}

	for _, bad := range []string{"2026-01-02", "2026-01-02T03:04:05"} {
		var d struct {
			At time.Time `koanf:"at"`
		}
		errs := decodeErrors(t, decodeValues(t, cc.Tree{"at": bad}, &d))
		if len(errs) != 1 || errs[0].Key.String() != "at" {
			t.Errorf("%q: errors = %v, want one *DecodeError for at", bad, errs)
		}
	}
}

// api.md Decoding: null and _unset leave Go-side defaults untouched, and a pointer stays nil.
func TestNullAndUnsetKeepGoDefaults(t *testing.T) {
	res := loadOK(t, map[string]string{
		sysFile:  `{"host": "sys", "port": 1, "ptr": 2}`,
		projFile: `{"host": null, "ptr": null, "_unset": ["port"], "name": "proj"}`,
	})
	v := struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
		Ptr  *int   `koanf:"ptr"`
		Name string `koanf:"name"`
	}{Host: "go", Port: 9}
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if v.Host != "go" || v.Port != 9 || v.Ptr != nil || v.Name != "proj" {
		t.Errorf("decoded %+v, want the Go defaults, a nil pointer and the file's name", v)
	}
}

// api.md Decoding: with ZeroFields, a null clears a Go-side default; _unset still keeps it.
func TestZeroFieldsClearsOnNull(t *testing.T) {
	res := loadOK(t, map[string]string{
		sysFile:  `{"host": "sys", "port": 1}`,
		projFile: `{"host": null, "_unset": ["port"]}`,
	}, cc.WithDecoderConfig(&mapstructure.DecoderConfig{ZeroFields: true, DecodeHook: cc.DecodeHook(), WeaklyTypedInput: true}))
	v := struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
	}{Host: "go", Port: 9}
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if v.Host != "" || v.Port != 9 {
		t.Errorf("decoded %+v, want an empty host and the Go port", v)
	}
}

// api.md Decoding: WithTag replaces the koanf tag, and WithFlatPaths decodes from flattened keys.
func TestTagAndFlatPaths(t *testing.T) {
	var tagged struct {
		Port int `config:"port"`
	}
	if err := decodeValues(t, cc.Tree{"port": int64(1)}, &tagged, cc.WithTag("config")); err != nil || tagged.Port != 1 {
		t.Errorf("WithTag: %+v, %v", tagged, err)
	}

	var flat struct {
		Port int `koanf:"server.port"`
	}
	tree := cc.Tree{"server": map[string]any{"port": int64(2)}}
	if err := decodeValues(t, tree, &flat, cc.WithFlatPaths()); err != nil || flat.Port != 2 {
		t.Errorf("WithFlatPaths: %+v, %v", flat, err)
	}
}

// DL-149: WithDecoderConfig replaces our hooks, as in koanf; DecodeHook composes them back. The config is copied, and
// an empty TagName falls back to the tag.
func TestDecoderConfig(t *testing.T) {
	type days struct {
		D time.Duration `koanf:"d"`
	}
	tree := cc.Tree{"d": "7d"}

	var v days
	if err := decodeValues(t, tree, &v, cc.WithDecoderConfig(&mapstructure.DecoderConfig{WeaklyTypedInput: true})); err == nil {
		t.Errorf("without our hooks, 7d decoded as %v; want an error", v.D)
	}

	v = days{}
	if err := decodeValues(t, tree, &v, cc.WithDecoderConfig(&mapstructure.DecoderConfig{DecodeHook: cc.DecodeHook()})); err != nil || v.D != 7*24*time.Hour {
		t.Errorf("with DecodeHook: %v, %v", v.D, err)
	}

	dc := &mapstructure.DecoderConfig{DecodeHook: cc.DecodeHook()}
	opt := cc.WithDecoderConfig(dc)
	dc.TagName = "other"
	var tagged struct {
		Port int `config:"port"`
	}
	if err := decodeValues(t, cc.Tree{"port": int64(3)}, &tagged, opt, cc.WithTag("config")); err != nil || tagged.Port != 3 {
		t.Errorf("a copied config with an empty TagName: %+v, %v; want the WithTag tag", tagged, err)
	}
}

// DL-150: each key that fails is its own *DecodeError, naming the key and the file that set it, never the value.
func TestDecodeErrors(t *testing.T) {
	res := loadOK(t, map[string]string{
		sysFile:  `{"server": {"port": "SECRET-80a"}}`,
		projFile: `{"debug": "SECRET-maybe"}`,
	})
	var v appConfig
	errs := decodeErrors(t, res.Decode(&v))
	if len(errs) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(errs), errs)
	}
	want := map[string]struct {
		file  string
		scope string
	}{"server.port": {sysFile, "sys"}, "debug": {projFile, "proj"}}
	for _, e := range errs {
		w, ok := want[e.Key.String()]
		if !ok {
			t.Errorf("unexpected key %v", e.Key)
			continue
		}
		if e.Path != w.file || e.Scope != w.scope || e.Unknown {
			t.Errorf("%v: path %q, scope %q, unknown %v; want %q, %q, false", e.Key, e.Path, e.Scope, e.Unknown, w.file, w.scope)
		}
		msg := e.Error()
		if !strings.Contains(msg, e.Key.String()) || !strings.Contains(msg, w.file) {
			t.Errorf("message %q doesn't name the key and the file", msg)
		}
		noSecret(t, "the error message", msg, "SECRET")
		if e.Unwrap() == nil {
			t.Errorf("%v: no underlying error", e.Key)
		}
		if cc.CodeOf(e) != cc.CodeUnprocessable || !errors.Is(e, cc.ErrDecode) {
			t.Errorf("%v: code %d, want 422 and ErrDecode", e.Key, cc.CodeOf(e))
		}
	}
	noSecret(t, "the joined error", errors.Join(cc.Errors(res.Decode(&v))...).Error(), "SECRET")
}

// DL-150: the layer blamed is the value's own: an array element's, a merged table's latest, the defaults, or the
// variable.
func TestDecodeErrorBlame(t *testing.T) {
	t.Run("an appended element", func(t *testing.T) {
		res := loadOK(t, map[string]string{sysFile: `{"ports": [1]}`, projFile: `{"ports": ["SECRET-x"]}`}, cc.WithArrayMerge(cc.Append))
		var v struct {
			Ports []int `koanf:"ports"`
		}
		errs := decodeErrors(t, res.Decode(&v))
		if len(errs) != 1 || errs[0].Key.String() != "ports[1]" || errs[0].Path != projFile {
			t.Errorf("errors = %v, want ports[1] from %s", errs, projFile)
		}
	})
	t.Run("a merged table", func(t *testing.T) {
		res := loadOK(t, map[string]string{sysFile: `{"server": {"a": 1}}`, projFile: `{"server": {"b": 2}}`})
		var v struct {
			Server int `koanf:"server"`
		}
		errs := decodeErrors(t, res.Decode(&v))
		if len(errs) != 1 || errs[0].Key.String() != "server" || errs[0].Path != projFile {
			t.Errorf("errors = %v, want server from %s", errs, projFile)
		}
	})
	t.Run("a default", func(t *testing.T) {
		res := loadOK(t, nil, cc.WithDefaults(map[string]any{"port": "SECRET-d"}))
		var v struct {
			Port int `koanf:"port"`
		}
		errs := decodeErrors(t, res.Decode(&v))
		if len(errs) != 1 || errs[0].Scope != string(cc.ScopeDefaults) || errs[0].Path != "" {
			t.Errorf("errors = %v, want port from the defaults", errs)
		}
	})
	t.Run("a variable", func(t *testing.T) {
		l := newLoader(t, nil, map[string]string{"MYAPP_PORT": "SECRET-e"},
			cc.WithScope("env", cc.EnvVars("MYAPP_")), cc.WithScopes("env"))
		res, err := l.Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		var v struct {
			Port int `koanf:"port"`
		}
		errs := decodeErrors(t, res.Decode(&v))
		if len(errs) != 1 || errs[0].Scope != "env" || !strings.Contains(errs[0].Error(), "MYAPP_PORT") {
			t.Errorf("errors = %v, want port from MYAPP_PORT", errs)
		}
		noSecret(t, "the error message", errs[0].Error(), "SECRET")
	})
}

// unknownTree has two unknown keys for appConfig: sever.port from projFile and servers[0].hots from sysFile.
var unknownTree = map[string]string{
	sysFile:  `{"servers": [{"hots": "SECRET-a"}]}`,
	projFile: `{"server": {"port": 1}, "sever": {"port": "SECRET-b"}}`,
}

type withServers struct {
	appConfig `koanf:",squash"`
	Servers   []server `koanf:"servers"`
}

// DL-151: unknown keys warn by default: each is logged and recorded in the report with its file, once each however
// many decodes find it.
func TestUnknownKeysWarn(t *testing.T) {
	logs := &records{}
	res := loadOK(t, unknownTree, cc.WithLogger(slogFor(logs)))
	var v withServers
	for range 2 {
		if err := res.Decode(&v); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	}
	got := map[string]string{}
	for _, w := range warningsWith(res.Describe(), cc.WarnUnknownKey) {
		if _, dup := got[w.Key.String()]; dup {
			t.Errorf("%v is recorded twice", w.Key)
		}
		got[w.Key.String()] = w.Path
		noSecret(t, "the warning", w.Message, "SECRET")
	}
	if got["sever.port"] != projFile || got["servers[0].hots"] != sysFile || len(got) != 2 {
		t.Errorf("unknown keys = %v, want sever.port from %s and servers[0].hots from %s", got, projFile, sysFile)
	}
	logged := 0
	for _, rec := range logs.got {
		if a := attrs(rec); a["code"] == string(cc.WarnUnknownKey) {
			logged++
			if rec.Level != slog.LevelWarn || a["path"] == "" {
				t.Errorf("log record %v: want Warn level with the file", rec)
			}
		}
	}
	if logged == 0 {
		t.Errorf("no unknown-key records were logged")
	}
}

// DL-151: in strict mode each unknown key is a *DecodeError with Unknown set; UnknownKeysIgnore is silent.
func TestUnknownKeysModes(t *testing.T) {
	res := loadOK(t, unknownTree, cc.WithUnknownKeys(cc.UnknownKeysError))
	var v withServers
	errs := decodeErrors(t, res.Decode(&v))
	keys := map[string]bool{}
	for _, e := range errs {
		if !e.Unknown {
			t.Errorf("%v: Unknown isn't set", e.Key)
		}
		keys[e.Key.String()] = true
	}
	if !keys["sever.port"] || !keys["servers[0].hots"] || len(errs) != 2 {
		t.Errorf("errors = %v, want one per unknown key", errs)
	}

	logs := &records{}
	res = loadOK(t, unknownTree, cc.WithUnknownKeys(cc.UnknownKeysIgnore), cc.WithLogger(slogFor(logs)))
	if err := res.Decode(&v); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if ws := warningsWith(res.Describe(), cc.WarnUnknownKey); len(ws) != 0 {
		t.Errorf("UnknownKeysIgnore recorded %v", ws)
	}
	for _, rec := range logs.got {
		if attrs(rec)["code"] == string(cc.WarnUnknownKey) {
			t.Errorf("UnknownKeysIgnore logged %v", rec)
		}
	}
}

// api.md Decoding: an app that sets ErrorUnused itself gets mapstructure's error wrapped in a *DecodeError.
func TestErrorUnusedIsWrapped(t *testing.T) {
	res := loadOK(t, unknownTree, cc.WithDecoderConfig(&mapstructure.DecoderConfig{ErrorUnused: true, DecodeHook: cc.DecodeHook()}))
	var v withServers
	err := res.Decode(&v)
	var de *cc.DecodeError
	if !errors.As(err, &de) {
		t.Errorf("Decode = %v, want a *DecodeError", err)
	}
}

// DL-160: DecodeAt decodes a subtree; a missing path leaves v untouched; a bad path or v is a 400. Unknown keys are
// named from the root.
func TestDecodeAt(t *testing.T) {
	res := loadOK(t, map[string]string{projFile: `{"server": {"host": "a", "sever": 1}}`})
	var s server
	if err := cc.DecodeAt(res, "server", &s); err != nil || s.Host != "a" {
		t.Errorf("DecodeAt(server) = %+v, %v", s, err)
	}
	if ws := warningsWith(res.Describe(), cc.WarnUnknownKey); len(ws) != 1 || ws[0].Key.String() != "server.sever" {
		t.Errorf("unknown keys = %v, want server.sever", ws)
	}
	if err := cc.DecodeAt(res, cc.Path{"server"}, &s); err != nil {
		t.Errorf("DecodeAt with a Path: %v", err)
	}

	untouched := server{Host: "keep"}
	if err := cc.DecodeAt(res, "nowhere.at.all", &untouched); err != nil || untouched.Host != "keep" {
		t.Errorf("DecodeAt(missing) = %+v, %v; want v untouched and no error", untouched, err)
	}

	var nilPtr *server
	for name, call := range map[string]func() error{
		"a bad path":                func() error { return cc.DecodeAt(res, "a..b", &s) },
		"a non-pointer":             func() error { return cc.DecodeAt(res, "server", s) },
		"a nil pointer":             func() error { return cc.DecodeAt(res, "server", nilPtr) },
		"Decode with a non-pointer": func() error { return res.Decode(s) },
	} {
		err := call()
		var be *cc.BaseError
		if !errors.As(err, &be) || cc.CodeOf(err) != cc.CodeBadRequest {
			t.Errorf("%s: %v, want a *BaseError with code 400", name, err)
		}
	}
}

// DL-156: decoding a failed result returns the load's error again.
func TestDecodeFailedResult(t *testing.T) {
	res, err := newLoader(t, map[string]string{projFile: `{`}, nil, twoScopes).Load(t.Context())
	if err == nil || res == nil {
		t.Fatalf("Load = %v, %v; want a result and an error", res, err)
	}
	var v appConfig
	if err := res.Decode(&v); !errors.Is(err, cc.ErrParse) {
		t.Errorf("Decode = %v, want the parse error", err)
	}
	if err := cc.DecodeAt(res, "server", &v.Server); !errors.Is(err, cc.ErrParse) {
		t.Errorf("DecodeAt = %v, want the parse error", err)
	}
}

// DL-160: KoanfProvider's Read runs a load and returns a copy of the tree, defaults included; ReadBytes is an error.
func TestKoanfProvider(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{projFile: `{"a": 1}`})
	l, err := cc.New("myapp", cc.WithFileSystem(fsys), cctest.WithHost(testHost(nil)), cc.WithDecoder(jsonDecoder()),
		twoScopes, cc.WithDefaults(map[string]any{"b": "d"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p := l.KoanfProvider()
	tree, err := p.Read()
	if err != nil || tree["a"] != int64(1) || tree["b"] != "d" {
		t.Fatalf("Read = %v, %v", tree, err)
	}
	tree["a"] = "changed"
	if again, _ := p.Read(); again["a"] != int64(1) {
		t.Errorf("changing Read's result changed the next Read: %v", again)
	}
	if b, err := p.ReadBytes(); err == nil {
		t.Errorf("ReadBytes = %q, want an error", b)
	}
	fsys.WriteFile(projFile, []byte(`{`), fs.FileMode(0o644))
	if _, err := p.Read(); !errors.Is(err, cc.ErrParse) {
		t.Errorf("Read of a broken file = %v, want the parse error", err)
	}
}
