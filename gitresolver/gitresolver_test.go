package gitresolver_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/gitresolver"
)

// DL-211: the resolver handles git.
func TestSchemes(t *testing.T) {
	t.Parallel()
	if got := gitresolver.New().Schemes(); !slices.Equal(got, []string{"git"}) {
		t.Errorf("Schemes() = %q, want git", got)
	}
}

// history is a repo where a.json is {"v": 1} at commit first, tagged v1 (annotated) and light (lightweight), and
// {"v": 2} at commit second, the tip of main.
func history(t *testing.T, format string) (r *repo, first, second string) {
	r = newRepo(t, format)
	first = r.write("a.json", `{"v": 1}`).commit("one")
	r.git("tag", "-a", "-m", "v1", "v1")
	r.git("tag", "light")
	second = r.write("a.json", `{"v": 2}`).commit("two")
	return r, first, second
}

// DL-211: a branch, an annotated tag, a lightweight tag, a full commit id and no ref each fetch the file at that
// commit, and the identity is the commit id.
func TestFetchRefs(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			r, first, second := history(t, format)
			for _, tc := range []struct {
				ref, commit string
				v           int64
			}{
				{"main", second, 2},
				{"v1", first, 1},
				{"light", first, 1},
				{first, first, 1},
				{second, second, 2},
				{"", second, 2},
			} {
				t.Run(tc.ref, func(t *testing.T) {
					ref := gitRef(r.url(), "a.json", tc.ref)
					res := loadOK(t, fileGit(), []string{ref})
					if got := res.Tree()["v"]; got != tc.v {
						t.Errorf("v = %v, want %d", got, tc.v)
					}
					if l := layerAt(t, res, ref); l.Remote == nil || l.Remote.Identity != tc.commit {
						t.Errorf("Remote = %+v, want identity %s", l.Remote, tc.commit)
					}
				})
			}
		})
	}
}

// DL-211, design.md git, "Regular files only": a symlink, a submodule or a directory is a *ReferenceError, and an
// executable file loads.
func TestModes(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("dir/a.json", `{"a": 1}`).write("exec.json", `{"x": 1}`)
	if err := os.Chmod(filepath.Join(r.dir, "exec.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir/a.json", filepath.Join(r.dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	sub := r.commit("files")
	r.git("update-index", "--add", "--cacheinfo", "160000,"+sub+",sub.json")
	r.git("commit", "-q", "-m", "submodule") // not commit(), whose add -A would drop the gitlink
	loadOK(t, fileGit(), []string{gitRef(r.url(), "exec.json", "main")})
	for _, path := range []string{"link.json", "sub.json", "dir"} {
		t.Run(path, func(t *testing.T) {
			_, err := load(t, fileGit(), []string{gitRef(r.url(), path, "main")}, cc.WithFileFormat(cc.FileName("dir"), "json"))
			wantCode(t, err, cc.CodeBadRequest)
		})
	}
}

// DL-214: a ref the remote doesn't have, or a path the commit doesn't have, is a *NotFoundError; a file over the size
// limit is a *TooLargeError.
func TestFetchErrors(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{"a": "`+strings.Repeat("x", 100)+`"}`).commit("one")
	t.Run("missing ref", func(t *testing.T) {
		_, err := load(t, fileGit(), []string{gitRef(r.url(), "a.json", "nope")})
		wantCode(t, err, cc.CodeNotFound)
	})
	t.Run("missing commit", func(t *testing.T) {
		_, err := load(t, fileGit(), []string{gitRef(r.url(), "a.json", strings.Repeat("0", 40))})
		wantCode(t, err, cc.CodeNotFound)
	})
	t.Run("missing path", func(t *testing.T) {
		_, err := load(t, fileGit(), []string{gitRef(r.url(), "b.json", "main")})
		wantCode(t, err, cc.CodeNotFound)
	})
	t.Run("too large", func(t *testing.T) {
		_, err := load(t, fileGit(), []string{gitRef(r.url(), "a.json", "main")}, cc.WithMaxIncludeSize(50))
		wantCode(t, err, cc.CodeTooLarge)
	})
}

// DL-212, design.md git, "Transports": by default only https and ssh are allowed; a file:// or http repo is a
// *PolicyError, and git never runs.
func TestTransports(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{}`).commit("one")
	ran := marker(t, "ran")
	git := gitresolver.WithGit(wrapper(t, "touch "+ran))
	_, err := load(t, gitresolver.New(git), []string{gitRef(r.url(), "a.json", "main")})
	wantCode(t, err, cc.CodeForbidden)
	b := newBackend(t, filepath.Dir(r.dir), nil)
	_, err = load(t, gitresolver.New(git), []string{gitRef(b.URL+"/repo", "a.json", "main")})
	wantCode(t, err, cc.CodeForbidden)
	wantNoFile(t, ran, "git")
	if got := b.requested(); len(got) != 0 {
		t.Errorf("the http server got %q", got)
	}
}

// DL-212: a ref that starts with - never reaches git.
func TestNoOptionInjection(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{}`).commit("one")
	ran := marker(t, "ran")
	_, err := load(t, fileGit(gitresolver.WithGit(wrapper(t, "touch "+ran))),
		[]string{gitRef(r.url(), "a.json", "--upload-pack=touch%20"+ran)})
	wantCode(t, err, cc.CodeBadRequest)
	wantNoFile(t, ran, "git")
}

// DL-59, DL-212, design.md git, "Isolated environment" and "No side effects": the user's git config and GIT_*
// variables have no effect: a url.insteadOf rule, a hooks path, a smudge filter, injected config, GIT_DIR. Not
// parallel, because it sets the process's environment.
func TestIsolation(t *testing.T) {
	r := newRepo(t, "sha1")
	r.write(".gitattributes", "*.json filter=evil\n").write("a.json", `{"from": "repo"}`).commit("one")
	other := newRepo(t, "sha1")
	other.write("a.json", `{"from": "other"}`).commit("one")
	hookRan, filterRan := marker(t, "hook"), marker(t, "filter")
	home := t.TempDir()
	hooks := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"reference-transaction", "post-checkout", "post-merge"} {
		if err := os.WriteFile(filepath.Join(hooks, h), []byte("#!/bin/sh\ntouch "+hookRan+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n[core]\n\thooksPath = %s\n[filter \"evil\"]\n\tsmudge = \"touch %s; cat\"\n\tclean = cat\n",
		other.url(), r.url(), hooks, filterRan)
	gc := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gc, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+other.url()+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", r.url())
	t.Setenv("GIT_DIR", filepath.Join(home, "nowhere"))
	res := loadOK(t, fileGit(), []string{gitRef(r.url(), "a.json", "main")})
	if got := res.Tree()["from"]; got != "repo" {
		t.Errorf("from = %v, want repo: a URL rewrite took effect", got)
	}
	wantNoFile(t, hookRan, "a hook")
	wantNoFile(t, filterRan, "a filter")
}

// DL-214: an old or missing git is a *FetchError that says so.
func TestGitProgram(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{}`).commit("one")
	ref := gitRef(r.url(), "a.json", "main")
	_, err := load(t, fileGit(gitresolver.WithGit(filepath.Join(t.TempDir(), "no-git"))), []string{ref})
	wantCode(t, err, cc.CodeBadGateway)
	old := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho git version 2.20.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = load(t, fileGit(gitresolver.WithGit(old)), []string{ref})
	wantCode(t, err, cc.CodeBadGateway)
	if err != nil && !strings.Contains(err.Error(), "2.24") {
		t.Errorf("error %q doesn't name the version git needs", err)
	}
}

// DL-199, DL-214: the include timeout stops git.
func TestTimeout(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{}`).commit("one")
	slow := wrapper(t, `[ "$1" = version ] || sleep 30`)
	start := time.Now()
	_, err := load(t, fileGit(gitresolver.WithGit(slow)), []string{gitRef(r.url(), "a.json", "main")},
		cc.WithIncludeTimeout(300*time.Millisecond))
	wantCode(t, err, cc.CodeGatewayTimeout)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the load took %v", d)
	}
}

// DL-212, DL-214, design.md git, "Isolated environment": without a credential helper, a repo that asks for
// credentials fails at once, with no prompt; WithCredentialHelper supplies them.
func TestCredentialHelper(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{"a": 1}`).commit("one")
	b := newBackend(t, filepath.Dir(r.dir), func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if u, p, ok := req.BasicAuth(); !ok || u != "u" || p != "p" {
				w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, req)
		})
	})
	ref := gitRef(b.URL+"/repo", "a.json", "main")
	httpGit := gitresolver.WithProtocols("http")
	_, err := load(t, gitresolver.New(httpGit), []string{ref}, cc.WithIncludeTimeout(10*time.Second))
	wantCode(t, err, cc.CodeBadGateway)
	helper := gitresolver.WithCredentialHelper("!f() { echo username=u; echo password=p; }; f")
	res := loadOK(t, gitresolver.New(httpGit, helper), []string{ref})
	if got := res.Tree()["a"]; got != int64(1) {
		t.Errorf("a = %v, want 1", got)
	}
}

// redirector redirects /r/N to /r/N-1 and /r/0/... to target/..., keeping the rest of the path and the query.
func redirector(t *testing.T, tls bool, target string) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/r/")
		n, tail, _ := strings.Cut(rest, "/")
		hops, err := strconv.Atoi(n)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		next := fmt.Sprintf("/r/%d/%s", hops-1, tail)
		if hops == 0 {
			next = target + "/" + tail
		}
		if r.URL.RawQuery != "" {
			next += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, next, http.StatusMovedPermanently)
	})
	var s *httptest.Server
	if tls {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

// DL-213, design.md git, "Redirects": the resolver follows an http repo's redirects itself, each checked against the
// allow-lists, and git fetches from the final URL; a redirect the allow-list rejects, one from https to http, and more
// than 5 hops never reach the target.
func TestRedirects(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "sha1")
	r.write("a.json", `{"a": 1}`).commit("one")
	b := newBackend(t, filepath.Dir(r.dir), nil)
	redir := redirector(t, false, b.URL)
	httpGit := gitresolver.New(gitresolver.WithProtocols("http"))
	allowBoth := cc.WithDefaultPolicy(cc.AllowResolvers("file", "git"),
		cc.AllowFor("git", cc.Prefix("git::"+redir.URL+"/"), cc.Prefix("git::"+b.URL+"/")))
	allowRedir := cc.WithDefaultPolicy(cc.AllowResolvers("file", "git"), cc.AllowFor("git", cc.Prefix("git::"+redir.URL+"/")))

	t.Run("direct", func(t *testing.T) {
		loadOK(t, httpGit, []string{gitRef(b.URL+"/repo", "a.json", "main")})
	})
	t.Run("followed", func(t *testing.T) {
		ref := gitRef(redir.URL+"/r/2/repo", "a.json", "main")
		res := loadOK(t, httpGit, []string{ref}, allowBoth)
		if l := layerAt(t, res, ref); l.Remote == nil || l.Remote.Ref != ref {
			t.Errorf("Remote = %+v, want the original reference %s", l.Remote, ref)
		}
	})
	t.Run("rejected target", func(t *testing.T) {
		before := len(b.requested())
		_, err := load(t, httpGit, []string{gitRef(redir.URL+"/r/0/repo", "a.json", "main")}, allowRedir)
		wantCode(t, err, cc.CodeForbidden)
		if got := b.requested()[before:]; len(got) != 0 {
			t.Errorf("the rejected target got %q", got)
		}
	})
	t.Run("https to http", func(t *testing.T) {
		secure := redirector(t, true, b.URL)
		before := len(b.requested())
		tlsGit := gitresolver.New(gitresolver.WithProtocols("https", "http"), gitresolver.WithClient(secure.Client()))
		_, err := load(t, tlsGit, []string{gitRef(secure.URL+"/r/0/repo", "a.json", "main")})
		wantCode(t, err, cc.CodeForbidden)
		if got := b.requested()[before:]; len(got) != 0 {
			t.Errorf("the http target got %q", got)
		}
	})
	t.Run("too many hops", func(t *testing.T) {
		_, err := load(t, httpGit, []string{gitRef(redir.URL+"/r/5/repo", "a.json", "main")}, allowBoth)
		wantCode(t, err, cc.CodeBadGateway)
	})
	t.Run("client unchanged", func(t *testing.T) {
		client := &http.Client{Timeout: time.Minute}
		before := *client
		loadOK(t, gitresolver.New(gitresolver.WithProtocols("http"), gitresolver.WithClient(client)),
			[]string{gitRef(redir.URL+"/r/0/repo", "a.json", "main")}, allowBoth)
		if !reflect.DeepEqual(before.Timeout, client.Timeout) || client.CheckRedirect != nil {
			t.Errorf("the app's client was changed")
		}
	})
}
