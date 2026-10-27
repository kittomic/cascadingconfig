package cascadingconfig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// DL-107, DL-111: a FixedDir's patterns apply lowest precedence first, and scopes apply in their order.
func TestLoadFixedDir(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{
		"/etc/myapp/base.json": `{"a": "base", "b": "base"}`,
		"/etc/myapp/top.json":  `{"b": "top"}`,
		"/low/config.json":     `{"c": "low", "d": "low"}`,
		"/high/config.json":    `{"d": "high"}`,
	})
	res := mustLoad(t, fsys,
		fixed("system", "/etc/{name}", "base.json", "top.json"),
		fixed("low", "/low", "config.json"),
		fixed("high", "/high", "config.json"),
		WithScopes("system", "low", "high"),
	)
	wantTree(t, res, `{"a": "base", "b": "top", "c": "low", "d": "high"}`)
	wantOrigin(t, res, "b", "set by /etc/myapp/top.json after /etc/myapp/base.json set")
	wantOrigin(t, res, "d", "set by /high/config.json after /low/config.json set")
}

// DL-110: a missing directory loads nothing, without a warning.
func TestLoadMissingDir(t *testing.T) {
	t.Parallel()
	res := mustLoad(t, memfs.New(nil), fixed("s", "/nope/{name}", "config.json"), WithScopes("s"))
	wantTree(t, res, `{}`)
	wantFound(t, res)
	if len(res.warnings) != 0 {
		t.Errorf("warnings = %#v, want none", res.warnings)
	}
}

// DL-3, DL-111, design.md File format precedence: {ext} expands to the extensions of the searched formats, and a
// slot's files load in format order, so the highest-ranked format wins. A file whose extension has no decoder, or
// isn't searched, is never found.
func TestLoadExtensions(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	files := map[string]string{
		"/d/config.json":  `{"a": "json", "j": true}`,
		"/d/config.yaml":  `{"a": "yaml", "y": true}`,
		"/d/config.toml":  `{"a": "toml"}`,
		"/d/config.txt":   `{"a": "txt"}`,
		"/d/settings.ini": `{"a": "ini"}`,
	}
	decoders := []Option{WithDecoder(tomlAsJSON), WithDecoder(yamlAsJSON)}

	res := mustLoad(t, memfs.New(files), append(decoders,
		fixed("s", "/d", "config.{ext}", "settings.ini"), WithScopes("s"), WithFormats("toml", "yaml", "json"))...)
	wantTree(t, res, `{"a": "toml", "j": true, "y": true}`)
	wantFound(t, res,
		"s /d/config.json loaded",
		"s /d/config.yaml loaded",
		"s /d/config.toml loaded",
	)

	res = mustLoad(t, memfs.New(files), append(decoders,
		fixed("s", "/d", "config.{ext}"), WithScopes("s"), WithFormats("json"))...)
	wantTree(t, res, `{"a": "json", "j": true}`)
}

// DL-93: the conflict policies apply to a slot's files.
func TestLoadSlotPolicies(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	files := map[string]string{"/d/config.json": `{"a": "json"}`, "/d/config.toml": `{"a": "toml"}`}
	opts := []Option{WithDecoder(tomlAsJSON), fixed("s", "/d", "config.{ext}"), WithScopes("s"), WithFormats("toml", "json")}

	res := mustLoad(t, memfs.New(files), append(opts, WithFormatConflict(ConflictFirst))...)
	wantTree(t, res, `{"a": "toml"}`)
	wantFound(t, res, "s /d/config.json ignored format-conflict", "s /d/config.toml loaded")

	err := loadFails(t, memfs.New(files), append(opts, WithFormatConflict(ConflictFail))...)
	if _, ok := asErr[*ConflictError](err); !ok {
		t.Errorf("error = %v, want a *ConflictError", err)
	}
	wantCode(t, err, CodeConflict)
}

// DL-116: the golden discovery order for FixedDir scopes, with every layer's status: format order within a slot,
// patterns in order, a subdirectory pattern, an empty file and a FIFO.
func TestGoldenFixedDirOrder(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{
		"/etc/myapp/config.json":             `{"a": "system-json"}`,
		"/etc/myapp/config.toml":             `{"a": "system-toml"}`,
		"/etc/myapp/conf/extra.json":         `{}`,
		"/home/me/.config/myapp/config.json": ``,
		"/home/me/.config/myapp/local.json":  `{"b": 1}`,
		"/home/me/.config/myapp/unused.json": `{"never": true}`,
	})
	fsys.Mkfifo("/home/me/.config/myapp/myapp.json")
	res := mustLoad(t, fsys,
		WithDecoder(tomlAsJSON), WithFormats("toml", "json"),
		fixed("system", "/etc/{name}", "config.{ext}", "conf/extra.{ext}"),
		fixed("user", "/home/me/.config/{name}", "config.{ext}", "{name}.{ext}", "local.{ext}"),
		WithScopes("system", "user"),
	)
	golden.Check(t, "discovery/fixeddir.golden", []byte(found(res)))
	wantTree(t, res, `{"a": "system-toml", "b": 1}`)
}

// DL-66, DL-110: a file reached twice, through two patterns by a symlink, or through a hard link, loads once, at its
// higher position.
func TestDedup(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	t.Run("symlink, real name higher", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/d/config.json": `{"a": 1}`, "/d/middle.json": `{"a": 2}`})
		fsys.Symlink("config.json", "/d/alias.json")
		res := mustLoad(t, fsys, fixed("s", "/d", "alias.json", "middle.json", "config.json"), WithScopes("s"))
		wantFound(t, res, "s /d/middle.json loaded", "s /d/config.json loaded")
		wantTree(t, res, `{"a": 1}`)
	})
	t.Run("symlink, alias higher", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/d/config.json": `{"a": 1}`, "/d/middle.json": `{"a": 2}`})
		fsys.Symlink("config.json", "/d/alias.json")
		res := mustLoad(t, fsys, fixed("s", "/d", "config.json", "middle.json", "alias.json"), WithScopes("s"))
		wantFound(t, res, "s /d/middle.json loaded", "s /d/alias.json loaded")
		wantTree(t, res, `{"a": 1}`)
	})
	t.Run("hard link", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/d/a.json": `{"a": 1}`})
		fsys.Link("/d/a.json", "/d/b.json")
		res := mustLoad(t, fsys, fixed("s", "/d", "a.json", "b.json"), WithScopes("s"))
		wantFound(t, res, "s /d/b.json loaded")
	})
}

// DL-110: a file reached through two scopes loads once at its higher position, with the scope and trust of its most
// trusted route.
func TestDedupTakesMostTrustedRoute(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/shared/config.json": `{"a": "shared"}`, "/mid/config.json": `{"a": "mid"}`})
	fsys.Symlink("/shared/config.json", "/link/config.json")
	res := mustLoad(t, fsys,
		WithScope("low", FixedDir("/shared", Pattern{Template: "config.json"}), Trust(TrustSystem)),
		fixed("mid", "/mid", "config.json"),
		fixed("high", "/link", "config.json"),
		WithScopes("low", "mid", "high"),
	)
	wantTree(t, res, `{"a": "shared"}`)
	if len(res.found) != 2 {
		t.Fatalf("found:\n%s\nwant mid, then the shared file once", found(res))
	}
	l := res.found[1]
	if l.Path != "/link/config.json" || l.Scope != "low" || l.Trust != TrustSystem {
		t.Errorf("shared layer = %s in scope %s with trust %v; want /link/config.json, scope low, TrustSystem",
			l.Path, l.Scope, l.Trust)
	}
}

// DL-62, DL-110, design.md Filesystem hook: symlinks are followed, even out of the searched directory, and broken
// symlinks and loops are skipped with a warning.
func TestSymlinks(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	t.Run("a file leaving the directory", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/srv/shared/real.json": `{"a": 1}`})
		fsys.Symlink("/srv/shared/real.json", "/etc/myapp/config.json")
		res := mustLoad(t, fsys, fixed("s", "/etc/{name}", "config.json"), WithScopes("s"))
		wantTree(t, res, `{"a": 1}`)
	})
	t.Run("a dotfiles directory", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/home/me/dotfiles/myapp/config.json": `{"a": 1}`})
		fsys.Symlink("/home/me/dotfiles/myapp", "/home/me/.config/myapp")
		res := mustLoad(t, fsys, fixed("s", "/home/me/.config/{name}", "config.json"), WithScopes("s"))
		wantTree(t, res, `{"a": 1}`)
	})
	t.Run("broken", func(t *testing.T) {
		fsys := memfs.New(map[string]string{"/d/other.json": `{"b": 1}`})
		fsys.Symlink("/nowhere.json", "/d/config.json")
		res := mustLoad(t, fsys, fixed("s", "/d", "config.json", "other.json"), WithScopes("s"))
		wantFound(t, res, "s /d/config.json ignored broken-symlink", "s /d/other.json loaded")
	})
	t.Run("a loop", func(t *testing.T) {
		fsys := memfs.New(nil)
		fsys.Symlink("/d/b.json", "/d/a.json")
		fsys.Symlink("/d/a.json", "/d/b.json")
		res := mustLoad(t, fsys, fixed("s", "/d", "a.json"), WithScopes("s"))
		wantFound(t, res, "s /d/a.json ignored broken-symlink")
	})
}

// DL-63, DL-110: a FIFO, a directory, or a symlink to either, named like a candidate, is skipped with a warning.
func TestSpecialFiles(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/d/ok.json": `{"a": 1}`})
	fsys.Mkfifo("/d/fifo.json")
	fsys.Mkdir("/d/dir.json", 0o755)
	fsys.Symlink("fifo.json", "/d/link.json")
	res := mustLoad(t, fsys, fixed("s", "/d", "fifo.json", "dir.json", "link.json", "ok.json"), WithScopes("s"))
	wantFound(t, res,
		"s /d/fifo.json ignored not-a-file",
		"s /d/dir.json ignored not-a-file",
		"s /d/link.json ignored not-a-file",
		"s /d/ok.json loaded",
	)
	if len(res.found) != 4 {
		t.FailNow()
	}
	for _, l := range res.found[:3] {
		if l.Reason == "" {
			t.Errorf("%s has no reason", l.Path)
		}
	}
}

// DL-64: a local file over the size limit is a *TooLargeError (413); one at the limit loads.
func TestFileSizeLimit(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/d/ok.json": `{"a": 12}`, "/d/big.json": `{"a": 123}`}) // 9 and 10 bytes
	res := mustLoad(t, fsys, fixed("s", "/d", "ok.json"), WithScopes("s"), WithMaxFileSize(9))
	wantTree(t, res, `{"a": 12}`)

	err := loadFails(t, fsys, fixed("s", "/d", "big.json"), WithScopes("s"), WithMaxFileSize(9))
	if e, ok := asErr[*TooLargeError](err); !ok || e.Path != "/d/big.json" {
		t.Errorf("error = %v, want a *TooLargeError for /d/big.json", err)
	}
	wantCode(t, err, CodeTooLarge)

	huge := memfs.New(map[string]string{"/d/huge.json": `{"a": "` + strings.Repeat("x", 10<<20) + `"}`})
	err = loadFails(t, huge, fixed("s", "/d", "huge.json"), WithScopes("s"))
	wantCode(t, err, CodeTooLarge)
}

// DL-66: names match the directory listing case-sensitively, so MyApp.JSON doesn't match myapp.json.
func TestNameMatchingIsCaseSensitive(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/d/MyApp.JSON": `{"a": 1}`, "/d/Config.json": `{"b": 1}`})
	res := mustLoad(t, fsys, fixed("s", "/d", "{name}.{ext}", "config.{ext}"), WithScopes("s"))
	wantTree(t, res, `{}`)
	wantFound(t, res)
}

// D16, DL-110: a searched file that can't be read is skipped with a warning, as LayerUnreadable.
func TestUnreadableFile(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/d/secret.json": `{"a": 1}`, "/d/ok.json": `{"b": 1}`})
	fsys.Chmod("/d/secret.json", 0)
	res := mustLoad(t, fsys, fixed("s", "/d", "secret.json", "ok.json"), WithScopes("s"))
	wantTree(t, res, `{"b": 1}`)
	wantFound(t, res, "s /d/secret.json unreadable unreadable", "s /d/ok.json loaded")
	if l := layerFor(t, res, "/d/secret.json"); l.Reason == "" {
		t.Errorf("the unreadable layer has no reason")
	}
}

// DL-110, api.md Errors: every broken file is reported, joined.
func TestEveryBrokenFileIsReported(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{
		"/a/config.json": `{"a": `,
		"/b/config.json": `{"b": 1}`,
		"/c/config.json": `[1]`,
		"/d/config.json": `{"d": "` + strings.Repeat("x", 100) + `"}`,
	})
	err := loadFails(t, fsys,
		fixed("a", "/a", "config.json"), fixed("b", "/b", "config.json"),
		fixed("c", "/c", "config.json"), fixed("d", "/d", "config.json"),
		WithScopes("a", "b", "c", "d"), WithMaxFileSize(50),
	)
	errs := Errors(err)
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(errs), err)
	}
	var paths []string
	for _, e := range errs {
		if pe, ok := asErr[*ParseError](e); ok {
			paths = append(paths, pe.Path)
		} else if te, ok := asErr[*TooLargeError](e); ok {
			paths = append(paths, te.Path)
		} else {
			t.Errorf("unexpected error %T: %v", e, e)
		}
	}
	if want := []string{"/a/config.json", "/c/config.json", "/d/config.json"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("errors for %q, want %q", paths, want)
	}
}

// DL-110: empty files are kept as LayerEmpty and merge nothing.
func TestEmptyFiles(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{
		"/d/a.json": ``, "/d/b.json": "  \n", "/d/c.json": `null`, "/d/d.json": `{"d": 1}`,
	})
	res := mustLoad(t, fsys, fixed("s", "/d", "a.json", "b.json", "c.json", "d.json"), WithScopes("s"))
	wantTree(t, res, `{"d": 1}`)
	wantFound(t, res, "s /d/a.json empty", "s /d/b.json empty", "s /d/c.json empty", "s /d/d.json loaded")
}

// DL-75: a cancelled load keeps errors.Is(err, context.Canceled), with code 499.
func TestCancelledLoad(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	l := loader(t, memfs.New(map[string]string{"/d/a.json": `{}`}), fixed("s", "/d", "a.json"), WithScopes("s"))
	_, err := l.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	wantCode(t, err, CodeCanceled)
}

// api.md Concurrency and ownership: a loader is safe for concurrent loads, which agree.
func TestParallelLoads(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(map[string]string{"/d/a.json": `{"a": [1, 2], "t": {"x": 1}}`, "/d/b.json": `{"b": 2}`})
	l := loader(t, fsys, fixed("s", "/d", "a.json", "b.json"), WithScopes("s"))
	var wg sync.WaitGroup
	trees := make([]Tree, 16)
	for i := range trees {
		wg.Go(func() {
			if res, err := l.Load(t.Context()); err == nil {
				trees[i] = res.Tree()
			}
		})
	}
	wg.Wait()
	want := tr(t, `{"a": [1, 2], "t": {"x": 1}, "b": 2}`)
	for i, got := range trees {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("load %d: Tree() = %#v, want %#v", i, got, want)
		}
	}
}
