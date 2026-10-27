package gitresolver_test

import (
	"encoding/json"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
	"github.com/kittomic/cascadingconfig/gitresolver"
)

// The tests load /home/me/work/myapp.json, a project file whose include key names git references to test repos. The
// repos are made with the git binary, with an environment of their own, so neither the user's git config nor a test's
// t.Setenv changes them.

const rootCfg = "/home/me/work/myapp.json"

// requireGit skips the test without git 2.29 or newer (for SHA-256 repos), and on Windows, like the rest of the suite.
func requireGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git isn't installed")
	}
	out, err := exec.Command(path, "version").Output()
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if err != nil || m == nil {
		t.Skipf("can't tell git's version: %q", out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 2 || major == 2 && minor < 29 {
		t.Skipf("git %s.%s is older than 2.29", m[1], m[2])
	}
	return path
}

// fixtureEnv is the environment of the tests' own git commands.
func fixtureEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	}
}

// gitIn runs git in dir for a fixture, and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, fixtureEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a test repository with a work tree, on branch main.
type repo struct {
	t   *testing.T
	dir string
}

// newRepo makes a repository; format is "sha1" or "sha256".
func newRepo(t *testing.T, format string) *repo {
	t.Helper()
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "repo")
	gitIn(t, filepath.Dir(dir), "init", "-q", "-b", "main", "--object-format="+format, dir)
	return &repo{t: t, dir: dir}
}

// write writes a file in the work tree.
func (r *repo) write(path, content string) *repo {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
	return r
}

// commit commits every change and returns the commit id.
func (r *repo) commit(msg string) string {
	r.t.Helper()
	gitIn(r.t, r.dir, "add", "-A")
	gitIn(r.t, r.dir, "commit", "-q", "--allow-empty", "-m", msg)
	return gitIn(r.t, r.dir, "rev-parse", "HEAD")
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	return gitIn(r.t, r.dir, args...)
}

// url is the repository's file URL.
func (r *repo) url() string { return "file://" + filepath.ToSlash(r.dir) }

// gitRef is the include reference for path at ref in the repository at repoURL; an empty ref means none.
func gitRef(repoURL, path, ref string) string {
	s := "git::" + repoURL + "//" + path
	if ref != "" {
		s += "?ref=" + ref
	}
	return s
}

// load loads rootCfg including refs, with r registered and no include cache.
func load(t *testing.T, r cc.IncludeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	b, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"root": 1, "include": ` + string(b) + `}`})
	base := []cc.Option{
		cc.WithFileSystem(fsys), cc.WithDecoder(ccjson.Decoder()),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}),
		cc.WithScope("proj", cc.FixedDir("/home/me/work", cc.Pattern{Template: "myapp.json"})), cc.WithScopes("proj"),
		cc.WithIncludeKey("include", cc.IncludeFlat), cc.WithIncludeResolver(r), cc.WithoutIncludeCache(),
	}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l.Load(t.Context())
}

func loadOK(t *testing.T, r cc.IncludeResolver, refs []string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := load(t, r, refs, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

func wantCode(t *testing.T, err error, code cc.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("Load succeeded, want code %d", code)
	}
	if got := cc.CodeOf(err); got != code {
		t.Errorf("code = %d, want %d: %v", got, code, err)
	}
}

func layerAt(t *testing.T, res *cc.Result, path string) cc.Layer {
	t.Helper()
	for _, l := range res.Describe().Layers {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no layer %s", path)
	return cc.Layer{}
}

// fileGit is a resolver that may use file:// repositories.
func fileGit(opts ...gitresolver.Option) cc.IncludeResolver {
	return gitresolver.New(append([]gitresolver.Option{gitresolver.WithProtocols("file")}, opts...)...)
}

// wrapper writes a script that runs body before the real git, and returns its path.
func wrapper(t *testing.T, body string) string {
	t.Helper()
	gitPath := requireGit(t)
	p := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\n" + body + "\nexec " + gitPath + ` "$@"` + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// marker is a file path that a hook, filter or wrapper creates when it runs.
func marker(t *testing.T, name string) string { return filepath.Join(t.TempDir(), name) }

func wantNoFile(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s ran: %s exists", what, path)
	}
}

// backend is an httptest server running git http-backend over the repositories under root, recording the paths it was
// asked for.
type backend struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newBackend(t *testing.T, root string, wrap func(http.Handler) http.Handler) *backend {
	t.Helper()
	b := &backend{}
	var h http.Handler = &cgi.Handler{
		Path: requireGit(t), Args: []string{"http-backend"},
		Env: append(fixtureEnv(), "GIT_PROJECT_ROOT="+root, "GIT_HTTP_EXPORT_ALL=1"),
	}
	if wrap != nil {
		h = wrap(h)
	}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.paths = append(b.paths, r.URL.Path)
		b.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(b.Close)
	return b
}

func (b *backend) requested() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.paths)
}
