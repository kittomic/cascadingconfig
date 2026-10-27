package cascadingconfig_test

import (
	"log/slog"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// DL-262: a file where a directory is searched is skipped like a missing directory: no warning, and a Debug record
// naming the directory that was searched. A directory that can't be read still warns.
func TestNotADirectory(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		files   map[string]string
		scope   cc.Option
		path    string // the not-a-dir record's path; empty when a warning is expected instead
		warning cc.WarningCode
	}{
		{
			name:  "a file inside a pattern's directory",
			files: map[string]string{work + "/myapp": "#!binary"},
			scope: cc.WithScope("s", cc.FixedDir(work, cc.Pattern{Template: "myapp/config.json"})),
			path:  work + "/myapp",
		},
		{
			name:  "a file partway along a pattern's path",
			files: map[string]string{work + "/myapp": "#!binary"},
			scope: cc.WithScope("s", cc.FixedDir(work, cc.Pattern{Template: "myapp/conf.d/*.json"})),
			path:  work + "/myapp/conf.d",
		},
		{
			name:  "a file at a fixed directory",
			files: map[string]string{sysDir: "a legacy single-file config"},
			scope: cc.WithScope("s", cc.FixedDir(sysDir, cc.Pattern{Template: "config.json"})),
			path:  sysDir,
		},
		{
			name:    "a directory that can't be read",
			files:   map[string]string{work + "/myapp/config.json": `{"a": 1}`},
			scope:   cc.WithScope("s", cc.FixedDir(work, cc.Pattern{Template: "myapp/config.json"})),
			warning: cc.WarnUnreadable,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fsys := cctest.NewFS(c.files)
			if c.warning != "" {
				fsys.Chmod(work+"/myapp", 0o000)
			}
			r := &records{}
			res, err := newLoaderFS(t, fsys, nil, c.scope, cc.WithScopes("s"), cc.WithLogger(slog.New(r))).Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			warnings := res.Describe().Warnings
			if c.warning != "" {
				if len(warnings) != 1 || warnings[0].Code != c.warning {
					t.Errorf("warnings %v, want one %s", warnings, c.warning)
				}
				return
			}
			if len(warnings) != 0 {
				t.Errorf("warnings %v, want none", warnings)
			}
			got := ops(r, "not-a-dir")
			if len(got) != 1 || got[0]["path"] != c.path {
				t.Errorf("not-a-dir records %v, want one for %s", got, c.path)
			}
		})
	}
}
