package cascadingconfig_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/fstest"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

func p(templates ...string) []cc.Pattern {
	out := make([]cc.Pattern, len(templates))
	for i, tpl := range templates {
		out[i] = cc.Pattern{Template: tpl}
	}
	return out
}

func useKind(kind cc.ScopeKind) cc.Option {
	return cc.Options(cc.WithScope("k", kind), cc.WithScopes("k"))
}

// DL-111, DL-113, DL-115: options and kinds that can only be mistakes.
func TestLoadOptionErrors(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "a relative dir", opts: []cc.Option{useKind(cc.FixedDir("etc/{name}", p("config.json")...))}, ops: []string{"FixedDir"}},
		{name: "an unknown dir placeholder", opts: []cc.Option{useKind(cc.FixedDir("/x/{nope}", p("config.json")...))}, ops: []string{"FixedDir"}, contains: []string{"{nope}"}},
		{name: "an unknown template placeholder", opts: []cc.Option{useKind(cc.FixedDir("/x", p("{nope}.json")...))}, ops: []string{"FixedDir"}, contains: []string{"{nope}"}},
		{name: "a template leaving its dir", opts: []cc.Option{useKind(cc.FixedDir("/x", p("../config.json")...))}, ops: []string{"FixedDir"}},
		{name: "an absolute template", opts: []cc.Option{useKind(cc.FixedDir("/x", p("/etc/config.json")...))}, ops: []string{"FixedDir"}},
		{name: "an empty template", opts: []cc.Option{useKind(cc.FixedDir("/x", p("")...))}, ops: []string{"FixedDir"}},
		{name: "no patterns", opts: []cc.Option{useKind(cc.FixedDir("/x"))}, ops: []string{"FixedDir"}},
		{
			name: "{user} without platform dirs", opts: []cc.Option{useKind(cc.FixedDir("{user}", p("config.json")...))},
			ops: []string{"FixedDir"}, contains: []string{"WithPlatformDirs"},
		},
		{name: "a nil fs.FS", opts: []cc.Option{useKind(cc.EmbeddedFS(nil, p("config.json")...))}, ops: []string{"EmbeddedFS"}},
		{name: "EmbeddedFS without patterns", opts: []cc.Option{useKind(cc.EmbeddedFS(fstest.MapFS{}))}, ops: []string{"EmbeddedFS"}},
		{name: "a nil ValuesFunc", opts: []cc.Option{useKind(cc.ValuesFunc(nil))}, ops: []string{"ValuesFunc"}},
		{name: "an empty prefix", opts: []cc.Option{useKind(cc.EnvVars(""))}, ops: []string{"EnvVars"}},
		{name: "a nil MapEnv", opts: []cc.Option{useKind(cc.EnvVars("MYAPP_", cc.MapEnv(nil)))}, ops: []string{"MapEnv"}},
		{name: "a nil filesystem", opts: []cc.Option{scope, cc.WithFileSystem(nil)}, ops: []string{"WithFileSystem"}},
		{name: "a zero file size", opts: []cc.Option{scope, cc.WithMaxFileSize(0)}, ops: []string{"WithMaxFileSize"}},
		{name: "a negative file size", opts: []cc.Option{scope, cc.WithMaxFileSize(-1)}, ops: []string{"WithMaxFileSize"}},
		{name: "a nil logger", opts: []cc.Option{scope, cc.WithLogger(nil)}, ops: []string{"WithLogger"}},
		{name: "nil platform dirs", opts: []cc.Option{scope, cc.WithPlatformDirs(nil)}, ops: []string{"WithPlatformDirs"}},
	})
}

// DL-114: a selected file-reading scope with an empty format order is an *OptionError, the bare-loader check moved
// from M3. Value scopes need no decoder.
func TestFileScopeNeedsAFormat(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "no decoder", opts: []cc.Option{useKind(cc.FixedDir("/x", p("config.json")...))}, ops: []string{"New"}},
		{
			name: "a default order with no decoder",
			opts: []cc.Option{useKind(cc.EmbeddedFS(fstest.MapFS{}, p("config.{ext}")...)), cc.WithDefaultFormats("toml")},
			ops:  []string{"New"},
		},
	})
	if _, err := cc.New("myapp", useKind(cc.Values(cc.Tree{}))); err != nil {
		t.Errorf("a Values scope with no decoder: %v", err)
	}
	if _, err := cc.New("myapp", useKind(cc.FixedDir("/x", p("config.json")...)), cc.WithDecoder(jsonDec)); err != nil {
		t.Errorf("a FixedDir with a decoder: %v", err)
	}
}

// records collects log records.
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

// attrs returns a record's attributes as strings.
func attrs(rec slog.Record) map[string]string {
	out := map[string]string{}
	rec.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.String()
		return true
	})
	return out
}

// DL-115: warnings go to the logger at Warn level, with their code and file.
func TestWarningsReachTheLogger(t *testing.T) {
	fsys := cctest.NewFS(nil)
	fsys.Mkfifo("/d/config.json")
	logs := &records{}
	l, err := cc.New("myapp",
		cc.WithFileSystem(fsys), cctest.WithHost(cctest.Host{GOOS: "linux", Home: "/home/me", Cwd: "/"}),
		cc.WithLogger(slog.New(logs)), cc.WithDecoder(jsonDec),
		useKind(cc.FixedDir("/d", p("config.json")...)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := l.Load(t.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(logs.got) != 1 {
		t.Fatalf("got %d log records, want 1", len(logs.got))
	}
	rec := logs.got[0]
	if a := attrs(rec); rec.Level != slog.LevelWarn || a["code"] != string(cc.WarnNotAFile) || a["path"] != "/d/config.json" {
		t.Errorf("record = %v %q %v, want a Warn with code %s and path /d/config.json", rec.Level, rec.Message, a, cc.WarnNotAFile)
	}
}
