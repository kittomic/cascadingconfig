package cascadingconfig

import (
	"path/filepath"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// hostWith returns the test host with env vars and changes applied.
func hostWith(env map[string]string, change func(h *hooks.Host)) Option {
	h := testHost()
	for k, v := range env {
		h.Env[k] = v
	}
	if change != nil {
		change(&h)
	}
	return withHost(fakeHost{h})
}

// DL-112, design.md Platform directories: XDGDirs' user and system directories, and the bindings that override them.
func TestPlatformDirs(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/xdg/myapp/config.json":             `{"user": "xdg"}`,
		"/home/me/.config/myapp/config.json": `{"user": "home"}`,
		"/custom/config.json":                `{"user": "custom"}`,
		"/home/me/work/rel/config.json":      `{"user": "relative"}`,
		"/home/me/tilde/config.json":         `{"user": "tilde"}`,
		"/etc/myapp/config.json":             `{"system": "etc"}`,
		"/opt/system/config.json":            `{"system": "custom"}`,
		"/pd/myapp/config.json":              `{"system": "programdata"}`,
	}
	scopes := []Option{
		WithPlatformDirs(XDGDirs),
		fixed("system", "{system}", "config.json"),
		fixed("user", "{user}", "config.json"),
		WithScopes("system", "user"),
	}
	bindings := []Option{WithBinding(BindConfigDir, ""), WithBinding(BindSystemConfigDir, "")}
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"defaults", nil, `{"user": "home", "system": "etc"}`},
		{"XDG_CONFIG_HOME", []Option{hostWith(map[string]string{"XDG_CONFIG_HOME": "/xdg"}, nil)}, `{"user": "xdg", "system": "etc"}`},
		{
			"bindings", append(bindings, hostWith(map[string]string{
				"MYAPP_CONFIG_DIR": "/custom", "MYAPP_SYSTEM_CONFIG_DIR": "/opt/system", "XDG_CONFIG_HOME": "/xdg",
			}, nil)),
			`{"user": "custom", "system": "custom"}`,
		},
		{"a relative binding", append(bindings, hostWith(map[string]string{"MYAPP_CONFIG_DIR": "rel"}, nil)), `{"user": "relative", "system": "etc"}`},
		{"a ~ binding", append(bindings, hostWith(map[string]string{"MYAPP_CONFIG_DIR": "~/tilde"}, nil)), `{"user": "tilde", "system": "etc"}`},
		{"an empty binding counts as unset", append(bindings, hostWith(map[string]string{"MYAPP_CONFIG_DIR": ""}, nil)), `{"user": "home", "system": "etc"}`},
		{"undeclared bindings are ignored", []Option{hostWith(map[string]string{"MYAPP_CONFIG_DIR": "/custom"}, nil)}, `{"user": "home", "system": "etc"}`},
		{
			"Windows", []Option{hostWith(map[string]string{"ProgramData": "/pd"}, func(h *hooks.Host) { h.GOOS = "windows" })},
			`{"user": "home", "system": "programdata"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mustLoad(t, memfs.New(files), append(append([]Option{}, scopes...), tc.opts...)...)
			wantTree(t, res, tc.want)
		})
	}
}

// DL-70, DL-108: on Windows, the system directory is %ProgramData%\{name}, falling back to C:\ProgramData. Paths are
// joined with the real OS's rules.
func TestWindowsSystemDir(t *testing.T) {
	t.Parallel()
	h := testHost()
	h.GOOS = "windows"
	if got, want := XDGDirs.(xdgDirs).systemDir(fakeHost{h}, "myapp"), filepath.Join(`C:\ProgramData`, "myapp"); got != want {
		t.Errorf("systemDir without ProgramData = %q, want %q", got, want)
	}
	h.Env = map[string]string{"ProgramData": `D:\Data`}
	if got, want := XDGDirs.(xdgDirs).systemDir(fakeHost{h}, "myapp"), filepath.Join(`D:\Data`, "myapp"); got != want {
		t.Errorf("systemDir = %q, want %q", got, want)
	}
	h.GOOS = "linux"
	if got := XDGDirs.(xdgDirs).systemDir(fakeHost{h}, "myapp"); got != "/etc/myapp" {
		t.Errorf("systemDir on Linux = %q, want /etc/myapp", got)
	}
}

// DL-112: without a home directory, scopes that use the user directory are skipped with a WarnNoHome warning. The
// others still load.
func TestNoHome(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(map[string]string{"/etc/myapp/config.json": `{"system": 1}`})
	res := mustLoad(t, fsys,
		hostWith(nil, func(h *hooks.Host) { h.Home = "" }),
		WithPlatformDirs(XDGDirs),
		fixed("system", "{system}", "config.json"),
		fixed("user", "{user}", "config.json"),
		fixed("tilde", "~/.myapp", "config.json"),
		WithScopes("system", "user", "tilde"),
	)
	wantTree(t, res, `{"system": 1}`)
	n := 0
	for _, w := range res.warnings {
		if w.Code == WarnNoHome {
			n++
		}
	}
	if n != 2 {
		t.Errorf("warnings = %#v, want a WarnNoHome for each of the two scopes", res.warnings)
	}
}
