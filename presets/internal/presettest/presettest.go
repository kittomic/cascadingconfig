// Package presettest has what the presets' tests share (DL-251): a rich fixture built from docs/precedence.md's
// tables, and helpers to load and compare.
package presettest

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// Home and Work are the fixture host's home and working directories; XDG is its XDG_CONFIG_HOME, which keeps mise's
// user directory out of the walk.
const (
	Home = "/home/me"
	Work = Home + "/work"
	XDG  = "/xdg"
)

// Host is the fixture host: Linux x64, with env.
func Host(env map[string]string) cctest.Host {
	return cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: Home, Cwd: Work, Env: env}
}

// Content is a file holding top = p, both at the top level and under the key myapp, in the format its extension
// names; a file with no extension is YAML.
func Content(p string) string {
	switch path.Ext(p) {
	case ".toml":
		return fmt.Sprintf("top = %q\n[myapp]\ntop = %q\n", p, p)
	case ".json":
		return fmt.Sprintf(`{"top": %q, "myapp": {"top": %q}}`, p, p)
	}
	return fmt.Sprintf("top: %q\nmyapp:\n  top: %q\n", p, p)
}

// Fixture builds a file for every pattern in precedence.md's tables for layout, in each directory its scope searches
// on the fixture host, with the environments dev and linux, the extensions exts, the stem "a" and the directory "f".
func Fixture(t *testing.T, layout string, exts []string) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../testdata/precedence.txt") // a copy of docs/precedence.md, which apicheck checks (DL-274)
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string][]string{
		"MiseLayout scope system":         {"/etc/myapp"},
		"MiseLayout scope user":           {XDG + "/myapp"},
		"MiseLayout scope project":        {Home, Work},
		"MiseLayout scope override":       {XDG + "/myapp"},
		"CosmiconfigLayout scope user":    {Home + "/.config/myapp"},
		"CosmiconfigLayout scope project": {Work},
		"CosmiconfigLayout scope meta":    {Work},
	}
	heading := regexp.MustCompile("^#### `(\\w+)` scope `(\\w+)`")
	row := regexp.MustCompile("^\\| *\\d+ *\\| *`([^`]+)` *\\|")
	files := map[string]string{}
	key := ""
	var templates = map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			key = m[1] + " scope " + m[2]
			continue
		}
		if strings.HasPrefix(line, "#") {
			key = ""
		}
		if m := row.FindStringSubmatch(line); m != nil && strings.HasPrefix(key, layout+" ") {
			templates[key] = append(templates[key], m[1])
		}
	}
	if layout == "MiseLayout" {
		templates["MiseLayout scope user"] = templates["MiseLayout scope system"]
	}
	for scope, tpls := range templates {
		for _, dir := range dirs[scope] {
			for _, tpl := range tpls {
				for _, env := range []string{"dev", "linux"} {
					for _, ext := range exts {
						name := strings.NewReplacer("{name}", "myapp", "{env}", env, "{ext}", ext).Replace(tpl)
						segs := strings.Split(name, "/")
						for i := range segs[:len(segs)-1] {
							segs[i] = strings.ReplaceAll(segs[i], "*", "f")
						}
						segs[len(segs)-1] = strings.ReplaceAll(segs[len(segs)-1], "*", "a")
						p := path.Join(dir, strings.Join(segs, "/"))
						files[p] = Content(p)
					}
				}
			}
		}
	}
	rows := 0
	for _, tpls := range templates {
		rows += len(tpls)
	}
	if rows == 0 || len(files) < rows {
		t.Fatalf("the fixture for %s has %d files for %d table rows; precedence.md's tables didn't parse", layout, len(files), rows)
	}
	return files
}

// Load loads files with preset, on the fixture host with env, and opts.
func Load(t *testing.T, preset cascadingconfig.Preset, files map[string]string, env map[string]string, opts ...cascadingconfig.Option) (*cascadingconfig.Result, error) {
	t.Helper()
	all := append([]cascadingconfig.Option{cascadingconfig.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(Host(env))}, opts...)
	l, err := cascadingconfig.NewWithPreset("myapp", preset, all...)
	if err != nil {
		t.Fatalf("NewWithPreset(%s): %v", preset.Name(), err)
	}
	return l.Load(context.Background())
}

// Order is the report's layers as "path scope status" lines, lowest first.
func Order(res *cascadingconfig.Result) []string {
	var out []string
	for _, l := range res.Describe().Layers {
		out = append(out, fmt.Sprintf("%s %s %s", l.Path, l.Scope, l.Status))
	}
	return out
}

// SameOrder loads files with preset and with layout plus layoutOpts, and checks that their reports list the same
// layers in the same order, with at least minLoaded of them loaded (DL-251).
func SameOrder(t *testing.T, preset, layout cascadingconfig.Preset, layoutOpts []cascadingconfig.Option, files map[string]string, env map[string]string, minLoaded int) {
	t.Helper()
	got, err := Load(t, preset, files, env)
	if err != nil {
		t.Fatalf("Load with %s: %v", preset.Name(), err)
	}
	want, err := Load(t, layout, files, env, layoutOpts...)
	if err != nil {
		t.Fatalf("Load with %s: %v", layout.Name(), err)
	}
	g, w := Order(got), Order(want)
	if !slices.Equal(g, w) {
		t.Fatalf("%s orders the fixture differently from %s:\n%s", preset.Name(), layout.Name(), diff(g, w))
	}
	loaded := 0
	for _, l := range got.Describe().Layers {
		if l.Status == cascadingconfig.LayerLoaded {
			loaded++
		}
	}
	if loaded < minLoaded {
		t.Errorf("only %d layers loaded, want at least %d, so the fixture tests little", loaded, minLoaded)
	}
}

func diff(got, want []string) string {
	var b strings.Builder
	for i := range max(len(got), len(want)) {
		g, w := "", ""
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		mark := "  "
		if g != w {
			mark = "! "
		}
		fmt.Fprintf(&b, "%s%-70s | %s\n", mark, g, w)
	}
	return b.String()
}

// WatchReal watches a real temporary directory with preset and its default notifier, starting from file holding
// before, and reports whether rewriting it to after reaches the handler as a change in key top within 10 s. home and
// cwd are set on the host, with the current user's UID, so permission checks pass.
func WatchReal(t *testing.T, preset cascadingconfig.Preset, rel, before, after string, cwdRel string) {
	t.Helper()
	home := t.TempDir()
	file := path.Join(home, rel)
	if err := os.MkdirAll(path.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	h := cctest.Host{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Home: home, Cwd: path.Join(home, cwdRel), UID: os.Getuid()}
	l, err := cascadingconfig.NewWithPreset("myapp", preset, cctest.WithHost(h))
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	changes := make(chan cascadingconfig.Change[cascadingconfig.Tree], 8)
	w, err := l.Watch(t.Context(), func(c cascadingconfig.Change[cascadingconfig.Tree]) { changes <- c },
		cascadingconfig.WithDebounce(20*time.Millisecond, 200*time.Millisecond))
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	if err := os.WriteFile(file, []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case c := <-changes:
			if c.Err == nil && c.New["top"] == "after" {
				return
			}
		case <-timeout:
			t.Fatalf("%s: no reload within 10 s after editing %s", preset.Name(), file)
		}
	}
}
