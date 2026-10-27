package cascadingconfig

import (
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// walkFiles has a myapp.json in every directory the walk tests pass through.
var walkFiles = fileSet(
	"/myapp.json", "/home/myapp.json", "/home/me/myapp.json", "/home/me/work/myapp.json", "/home/me/work/app/myapp.json",
	"/srv/myapp.json", "/srv/app/myapp.json",
)

func walkLoad(t *testing.T, change func(*hooks.Host), opts ...Option) *Result {
	t.Helper()
	return mustLoad(t, memfs.New(walkFiles), append([]Option{
		hostWith(nil, change), WithScope("project", Walk(Pattern{Template: "{name}.json"})), WithScopes("project"),
	}, opts...)...)
}

// DL-5, DL-122, design.md Directory walk: from inside ~, the walk searches up to and including ~; from outside, it
// goes to /. Directories apply root-most first.
func TestWalkGolden(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	inside := walkLoad(t, func(h *hooks.Host) { h.Cwd = "/home/me/work/app" })
	golden.Check(t, "discovery/walk-inside-home.golden", []byte(detail(inside)))
	outside := walkLoad(t, func(h *hooks.Host) { h.Cwd = "/srv/app" })
	golden.Check(t, "discovery/walk-outside-home.golden", []byte(detail(outside)))
}

// DL-122: StopAtRoot always walks to /; WithStartDir sets the start, resolving a relative dir against the current
// directory; with no home directory, the walk goes to /.
func TestWalkOptions(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	cwd := func(h *hooks.Host) { h.Cwd = "/home/me/work/app" }
	tests := []struct {
		name   string
		change func(*hooks.Host)
		opts   []Option
		want   []string
	}{
		{
			"StopAtRoot", cwd, []Option{WithWalkStop(StopAtRoot)},
			[]string{"/myapp.json", "/home/myapp.json", "/home/me/myapp.json", "/home/me/work/myapp.json", "/home/me/work/app/myapp.json"},
		},
		{"an absolute start dir", cwd, []Option{WithStartDir("/srv/app")}, []string{"/myapp.json", "/srv/myapp.json", "/srv/app/myapp.json"}},
		{
			"a relative start dir", func(h *hooks.Host) { h.Cwd = "/home/me" }, []Option{WithStartDir("work")},
			[]string{"/home/me/myapp.json", "/home/me/work/myapp.json"},
		},
		{
			"no home", func(h *hooks.Host) { h.Cwd = "/home/me/work"; h.Home = "" }, nil,
			[]string{"/myapp.json", "/home/myapp.json", "/home/me/myapp.json", "/home/me/work/myapp.json"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantPaths(t, walkLoad(t, tc.change, tc.opts...), tc.want...)
		})
	}
}

// DL-5, DL-122, DL-123: ceilings are exclusive, from WithCeilingPaths and {NAME}_CEILING_PATHS together. The variable
// is a path list with the host's separator; empty entries are dropped, relative ones resolve against the current
// directory, and ~ is expanded.
func TestCeilings(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	cwd := "/home/me/work/app"
	tests := []struct {
		name   string
		change func(*hooks.Host)
		opts   []Option
		want   []string
	}{
		{"an option", nil, []Option{WithCeilingPaths("/home/me")}, []string{"/home/me/work/myapp.json", "/home/me/work/app/myapp.json"}},
		{"the variable", setEnv("MYAPP_CEILING_PATHS", "/nowhere::/home/me/work"), nil, []string{"/home/me/work/app/myapp.json"}},
		{"both", setEnv("MYAPP_CEILING_PATHS", "/home/me/work"), []Option{WithCeilingPaths("/elsewhere")}, []string{"/home/me/work/app/myapp.json"}},
		{"relative", setEnv("MYAPP_CEILING_PATHS", ".."), nil, []string{"/home/me/work/app/myapp.json"}},
		{"~", setEnv("MYAPP_CEILING_PATHS", "~"), nil, []string{"/home/me/work/myapp.json", "/home/me/work/app/myapp.json"}},
		{
			"the start dir itself", setEnv("MYAPP_CEILING_PATHS", "/home/me/work/app"), nil, nil,
		},
		{
			"Windows separators", func(h *hooks.Host) { h.GOOS = "windows"; h.Env["MYAPP_CEILING_PATHS"] = "/x;/home/me/work" }, nil,
			[]string{"/home/me/work/app/myapp.json"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			change := func(h *hooks.Host) {
				h.Cwd = cwd
				if tc.change != nil {
					tc.change(h)
				}
			}
			res := walkLoad(t, change, append([]Option{WithBinding(BindCeilingPaths, "")}, tc.opts...)...)
			wantPaths(t, res, tc.want...)
		})
	}
}

func wantPaths(t *testing.T, res *Result, want ...string) {
	t.Helper()
	var got []string
	for _, l := range res.found {
		got = append(got, l.Path)
	}
	if len(got) != len(want) {
		t.Errorf("paths = %q, want %q", got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("paths = %q, want %q", got, want)
			return
		}
	}
}
