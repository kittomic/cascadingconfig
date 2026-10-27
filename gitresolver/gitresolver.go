package gitresolver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kittomic/cascadingconfig"
)

// protocolVersion is the git protocol version every fetch forces. Only the internal test of the fallback for servers
// that refuse a commit id changes it, to 0, since protocol v2 servers serve any reachable commit.
var protocolVersion = "2"

// maxRedirects is how many redirects the resolver follows for an http or https repo, as httpresolver does (DL-213).
const maxRedirects = 5

// Option configures the resolver. It's sealed: only this package's With functions make one.
type Option interface{ apply(r *resolver) }

type optionFunc func(r *resolver)

func (f optionFunc) apply(r *resolver) { f(r) }

// WithProtocols sets the transports git may use, such as "https", "ssh", "http" or "file". The default is https and
// ssh. A repo URL with any other scheme is a *PolicyError, before git runs.
func WithProtocols(protocols ...string) Option {
	return optionFunc(func(r *resolver) {
		r.protocols = nil
		for _, p := range protocols {
			r.protocols = append(r.protocols, strings.ToLower(p))
		}
	})
}

// WithCredentialHelper sets git's credential.helper. By default there is none, and git never prompts.
func WithCredentialHelper(helper string) Option {
	return optionFunc(func(r *resolver) { r.helper = helper })
}

// WithGit sets the git program. By default it's found on PATH.
func WithGit(path string) Option {
	return optionFunc(func(r *resolver) { r.git = path })
}

// WithClient sets the HTTP client that follows an http or https repo URL's redirects before git runs. The resolver
// copies it, so c isn't changed.
func WithClient(c *http.Client) Option {
	return optionFunc(func(r *resolver) { r.client = c })
}

// New returns a resolver for git includes. It reads the file from the object database of a fresh bare repo, never a
// work tree, with git isolated from the user's and the system's config and environment. It's safe for concurrent use.
func New(opts ...Option) cascadingconfig.IncludeResolver {
	r := &resolver{protocols: []string{"https", "ssh"}}
	for _, o := range opts {
		o.apply(r)
	}
	return r
}

type resolver struct {
	protocols []string
	helper    string
	git       string
	client    *http.Client

	once    sync.Once
	gitPath string
	version [2]int
	gitErr  error
}

// Schemes implements cascadingconfig.IncludeResolver.
func (r *resolver) Schemes() []string { return []string{"git"} }

// Resolve implements cascadingconfig.IncludeResolver.
func (r *resolver) Resolve(ctx context.Context, ref cascadingconfig.IncludeRef) (*cascadingconfig.Fetched, error) {
	f := &fetch{r: r, ctx: ctx, ref: ref}
	return f.run()
}

// fetch is the state of one Resolve.
type fetch struct {
	r    *resolver
	ctx  context.Context
	ref  cascadingconfig.IncludeRef
	dir  string // the temp dir: the repo, an empty hooks dir and an empty global config
	repo string
	env  []string
}

func (f *fetch) base(err error) cascadingconfig.BaseError {
	return cascadingconfig.BaseError{Op: "fetch", Path: f.ref.Raw, Err: err}
}

func (f *fetch) failed(err error) error {
	return &cascadingconfig.FetchError{BaseError: f.base(err)}
}

func (f *fetch) run() (*cascadingconfig.Fetched, error) {
	r, ref := f.r, f.ref
	u, err := url.Parse(ref.Location)
	if err != nil {
		return nil, &cascadingconfig.ReferenceError{BaseError: f.base(err)}
	}
	proto := strings.ToLower(u.Scheme)
	if !slices.Contains(r.protocols, proto) {
		return nil, &cascadingconfig.PolicyError{BaseError: f.base(fmt.Errorf(
			"the protocol %q isn't allowed; gitresolver.WithProtocols allows %s", proto, strings.Join(r.protocols, ", ")))}
	}
	repoURL := ref.Location
	if proto == "http" || proto == "https" {
		if repoURL, err = f.followRedirects(); err != nil {
			return nil, err
		}
	}
	if err := r.program(); err != nil {
		return nil, f.failed(err)
	}
	f.dir, err = os.MkdirTemp("", "cascadingconfig-git-")
	if err != nil {
		return nil, f.failed(err)
	}
	defer func() { _ = os.RemoveAll(f.dir) }()
	global := filepath.Join(f.dir, "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		return nil, f.failed(err)
	}
	if err := os.Mkdir(filepath.Join(f.dir, "hooks"), 0o700); err != nil {
		return nil, f.failed(err)
	}
	f.env = gitEnv(global, r.protocols)
	if ref.Validator != "" && !commitID.MatchString(ref.Version) {
		same, err := f.unchanged(repoURL)
		if err != nil {
			return nil, err
		}
		if same {
			return nil, cascadingconfig.ErrNotModified
		}
	}
	format := "sha1"
	if len(ref.Version) == 64 && isHex(ref.Version) {
		format = "sha256"
	}
	content, commit, err := f.fetchAs(repoURL, format)
	var mm *mismatch
	if errors.As(err, &mm) {
		content, commit, err = f.fetchAs(repoURL, mm.server)
	}
	if err != nil {
		return nil, err
	}
	return &cascadingconfig.Fetched{Content: content, Identity: commit, Validator: commit}, nil
}

// unchanged asks the remote, with ls-remote, which commit the reference names now, and reports whether it's still
// the validator's (DL-229). The ref is matched as fetch matches it: as given, then under refs/, refs/tags/ and
// refs/heads/, with an annotated tag's commit taken from its peeled line. A ref the remote doesn't have isn't
// unchanged, so the fetch reports it.
func (f *fetch) unchanged(repoURL string) (bool, error) {
	want := f.ref.Version
	if want == "" {
		want = "HEAD"
	}
	out, err := f.git(f.dir, "ls-remote", "--end-of-options", repoURL, want)
	if err != nil {
		return false, err
	}
	refs := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		oid, name, ok := strings.Cut(line, "\t")
		if ok && commitID.MatchString(oid) {
			refs[name] = oid
		}
	}
	for _, name := range []string{want, "refs/" + want, "refs/tags/" + want, "refs/heads/" + want} {
		oid, ok := refs[name+"^{}"]
		if !ok {
			oid, ok = refs[name]
		}
		if ok {
			return strings.EqualFold(oid, f.ref.Validator), nil
		}
	}
	return false, nil
}

// mismatch is git's report that the repository uses another object format.
type mismatch struct{ server string }

func (m *mismatch) Error() string { return "the repository uses " + m.server }

var mismatchRE = regexp.MustCompile(`mismatched algorithms: client \w+; server (sha1|sha256)`)

// fetchAs fetches the file into a fresh bare repo with object format format, and returns its content and commit id.
func (f *fetch) fetchAs(repoURL, format string) ([]byte, string, error) {
	ref := f.ref
	if format == "sha256" && f.r.older(2, 29) {
		return nil, "", f.failed(fmt.Errorf("git %d.%d is older than 2.29, which SHA-256 repositories need", f.r.version[0], f.r.version[1]))
	}
	f.repo = filepath.Join(f.dir, "repo-"+format)
	if _, err := f.git(f.dir, "init", "-q", "--bare", "--object-format="+format, f.repo); err != nil {
		return nil, "", err
	}
	isCommit := commitID.MatchString(ref.Version)
	cmds := commands(repoURL, ref.Version, ref.Path, "", "")
	target := "FETCH_HEAD^{commit}"
	if _, err := f.git(f.repo, cmds[0]...); err != nil {
		var ge *gitError
		switch {
		case !errors.As(err, &ge):
			return nil, "", err
		case mismatchRE.MatchString(ge.stderr):
			return nil, "", &mismatch{server: mismatchRE.FindStringSubmatch(ge.stderr)[1]}
		case strings.Contains(ge.stderr, "couldn't find remote ref"):
			return nil, "", &cascadingconfig.NotFoundError{BaseError: f.base(fmt.Errorf("the repository has no ref %q", ref.Version))}
		case isCommit && (strings.Contains(ge.stderr, "not our ref") || strings.Contains(ge.stderr, "unadvertised object")):
			if err := f.fetchEverything(repoURL); err != nil {
				return nil, "", err
			}
			target = ref.Version + "^{commit}"
		default:
			return nil, "", err
		}
	}
	out, err := f.git(f.repo, "rev-parse", "--verify", "--quiet", "--end-of-options", target)
	if err != nil {
		if f.ctx.Err() != nil {
			return nil, "", f.ctx.Err()
		}
		return nil, "", &cascadingconfig.NotFoundError{BaseError: f.base(fmt.Errorf("the repository has no commit %s", ref.Version))}
	}
	commit := strings.TrimSpace(string(out))
	if isCommit && !strings.EqualFold(commit, ref.Version) {
		return nil, "", f.failed(fmt.Errorf("asked for commit %s, git fetched %s", ref.Version, commit))
	}
	cmds = commands(repoURL, ref.Version, ref.Path, commit, "")
	out, err = f.git(f.repo, cmds[2]...)
	if err != nil {
		return nil, "", err
	}
	line := strings.TrimSuffix(string(out), "\n")
	if line == "" {
		return nil, "", &cascadingconfig.NotFoundError{BaseError: f.base(fmt.Errorf("commit %s has no %s", commit, ref.Path))}
	}
	mode, kind, oid, path, ok := parseLsTree(line)
	switch {
	case !ok || path != ref.Path:
		return nil, "", f.failed(fmt.Errorf("unexpected ls-tree output for %s", ref.Path))
	case kind != "blob":
		return nil, "", &cascadingconfig.ReferenceError{BaseError: f.base(fmt.Errorf("%s is a %s, not a regular file", ref.Path, describeKind(kind)))}
	case mode != "100644" && mode != "100755":
		return nil, "", &cascadingconfig.ReferenceError{BaseError: f.base(fmt.Errorf("%s is %s, not a regular file", ref.Path, describeMode(mode)))}
	}
	cmds = commands(repoURL, ref.Version, ref.Path, commit, oid)
	out, err = f.git(f.repo, cmds[3]...)
	if err != nil {
		return nil, "", err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return nil, "", f.failed(fmt.Errorf("unexpected cat-file -s output %q", out))
	}
	if ref.MaxSize > 0 && size > ref.MaxSize {
		return nil, "", &cascadingconfig.TooLargeError{BaseError: f.base(fmt.Errorf("the file is %d bytes, and the limit is %d", size, ref.MaxSize))}
	}
	content, err := f.git(f.repo, cmds[4]...)
	if err != nil {
		return nil, "", err
	}
	return content, commit, nil
}

// fetchEverything fetches every branch and tag without blobs, for a server that refuses to fetch a commit id; the
// blob is fetched when it's read (DL-211). A server without filter support sends everything.
func (f *fetch) fetchEverything(repoURL string) error {
	if _, err := f.git(f.repo, "remote", "add", "--end-of-options", "origin", repoURL); err != nil {
		return err
	}
	_, err := f.git(f.repo, "fetch", "--filter=blob:none", "--no-tags", "--end-of-options", "origin",
		"+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")
	return err
}

// followRedirects follows an http or https repo URL's redirects with Go's client, each hop checked as httpresolver
// checks one, and returns the URL git should fetch from (DL-213). An answer that isn't a redirect leaves the URL.
func (f *fetch) followRedirects() (string, error) {
	ref := f.ref
	client := &http.Client{}
	if f.r.client != nil {
		c := *f.r.client
		client = &c
	}
	appCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return f.failed(fmt.Errorf("more than %d redirects", maxRedirects))
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return &cascadingconfig.PolicyError{BaseError: f.base(fmt.Errorf("a redirect from https to %s", req.URL.Redacted()))}
		}
		if repo, ok := repoOf(req.URL); ok && ref.Check != nil {
			target := "git::" + repo + "//" + ref.Path
			if ref.Version != "" {
				target += "?ref=" + ref.Version
			}
			if err := ref.Check(target); err != nil {
				return err
			}
		}
		if appCheck != nil {
			return appCheck(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, ref.Location+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return "", &cascadingconfig.ReferenceError{BaseError: f.base(err)}
	}
	resp, err := client.Do(req)
	if err != nil {
		if cascadingconfig.CodeOf(err) != 0 || f.ctx.Err() != nil {
			return "", err
		}
		return ref.Location, nil // git reports the network failure itself
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.Request != req {
		if repo, ok := repoOf(resp.Request.URL); ok {
			return repo, nil
		}
	}
	return ref.Location, nil
}

// repoOf returns the repo URL that an info/refs request URL asks for.
func repoOf(u *url.URL) (string, bool) {
	p, ok := strings.CutSuffix(u.Path, "/info/refs")
	if !ok {
		return "", false
	}
	out := url.URL{Scheme: u.Scheme, Host: u.Host, Path: p, RawPath: strings.TrimSuffix(u.RawPath, "/info/refs")}
	return out.String(), true
}

// gitError is a git command that failed, with the first line of its error output.
type gitError struct {
	args   []string
	stderr string
	err    error
}

func (e *gitError) Error() string {
	line, _, _ := strings.Cut(strings.TrimSpace(e.stderr), "\n")
	if len(line) > 200 {
		line = line[:200]
	}
	if line == "" {
		line = e.err.Error()
	}
	return "git " + e.args[0] + ": " + line
}

func (e *gitError) Unwrap() error { return e.err }

// git runs git with the isolating config in dir, and returns its output. A failure is the context's error after a
// timeout or cancellation, and otherwise a *FetchError holding a *gitError.
func (f *fetch) git(dir string, args ...string) ([]byte, error) {
	r := f.r
	full := append(r.config(filepath.Join(f.dir, "hooks")), args...)
	// The reference's values follow --end-of-options, and git runs isolated (DL-212).
	cmd := exec.CommandContext(f.ctx, r.gitPath, full...) //nolint:gosec // see above
	cmd.Dir, cmd.Env = dir, f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	killGroup(cmd)
	if err := cmd.Run(); err != nil {
		if f.ctx.Err() != nil {
			return nil, f.ctx.Err()
		}
		return nil, f.failed(&gitError{args: args, stderr: stderr.String(), err: err})
	}
	return stdout.Bytes(), nil
}

// config is the -c settings every git command gets (DL-212).
func (r *resolver) config(hooks string) []string {
	settings := []string{
		"protocol.version=" + protocolVersion,
		"protocol.allow=never",
	}
	for _, p := range r.protocols {
		settings = append(settings, "protocol."+p+".allow=always")
	}
	settings = append(settings,
		"core.hooksPath="+hooks,
		"http.followRedirects=false",
		"credential.helper=",
	)
	if r.helper != "" {
		settings = append(settings, "credential.helper="+r.helper)
	}
	settings = append(settings,
		"core.sshCommand=ssh -o BatchMode=yes",
		"submodule.recurse=false",
		"fetch.recurseSubmodules=false",
		"transfer.fsckObjects=true",
		"core.fsmonitor=false",
		"gc.auto=0",
		"maintenance.auto=false",
		"init.defaultBranch=main",
	)
	var out []string
	for _, s := range settings {
		out = append(out, "-c", s)
	}
	return out
}

// keptEnv are the variables git keeps from the process's environment; every GIT_* variable goes, because
// GIT_CONFIG_COUNT and GIT_CONFIG_KEY_<n> set config even with GIT_CONFIG_NOSYSTEM (DL-212).
var keptEnv = []string{
	"PATH", "HOME", "SSH_AUTH_SOCK",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SYSTEMROOT", "SystemRoot", "WINDIR", "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE",
	"HOMEPATH", "PATHEXT", "COMSPEC", "ProgramData",
}

// gitEnv is the environment git runs with.
func gitEnv(global string, protocols []string) []string {
	var env []string
	for _, k := range keptEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+global,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL="+strings.Join(protocols, ":"),
		"GCM_INTERACTIVE=never",
		"LC_ALL=C",
	)
}

var versionRE = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// program finds git and checks its version, once.
func (r *resolver) program() error {
	r.once.Do(func() {
		path := r.git
		if path == "" {
			var err error
			if path, err = exec.LookPath("git"); err != nil {
				r.gitErr = errors.New("git isn't installed, or isn't on PATH")
				return
			}
		}
		cmd := exec.Command(path, "version")
		cmd.Env = gitEnv(os.DevNull, nil)
		out, err := cmd.Output()
		if err != nil {
			r.gitErr = fmt.Errorf("git can't be run: %w", err)
			return
		}
		m := versionRE.FindStringSubmatch(string(out))
		if m == nil {
			r.gitErr = fmt.Errorf("can't tell git's version from %q", strings.TrimSpace(string(out)))
			return
		}
		r.version[0], _ = strconv.Atoi(m[1])
		r.version[1], _ = strconv.Atoi(m[2])
		if r.older(2, 24) {
			r.gitErr = fmt.Errorf("git %d.%d is older than 2.24, which gitresolver needs for --end-of-options",
				r.version[0], r.version[1])
			return
		}
		r.gitPath = path
	})
	return r.gitErr
}

func (r *resolver) older(major, minor int) bool {
	return r.version[0] < major || r.version[0] == major && r.version[1] < minor
}

// commands returns the argument lists, after any -c settings, of the git commands that fetch path at ref from repo:
// fetch, rev-parse, ls-tree, cat-file -s and cat-file blob. commit and blob stand for the ids earlier commands print.
// Every value from the reference follows --end-of-options, and the path follows -- too.
func commands(repo, ref, path, commit, blob string) [][]string {
	want := ref
	if want == "" {
		want = "HEAD"
	}
	return [][]string{
		{"fetch", "--depth=1", "--no-tags", "--end-of-options", repo, want},
		{"rev-parse", "--verify", "--quiet", "--end-of-options", "FETCH_HEAD^{commit}"},
		{"ls-tree", "--end-of-options", commit, "--", path},
		{"cat-file", "-s", "--end-of-options", blob},
		{"cat-file", "--end-of-options", "blob", blob},
	}
}

var (
	lsTreeRE = regexp.MustCompile(`^([0-7]{6}) (blob|tree|commit) ([0-9a-f]{40}|[0-9a-f]{64})\t([^\t\n\x00]+)$`)
	commitID = regexp.MustCompile(`^(?i:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// parseLsTree parses one line of git ls-tree output, "<mode> SP <type> SP <oid> TAB <path>", and reports whether it
// was well formed.
func parseLsTree(line string) (mode, kind, oid, path string, ok bool) {
	m := lsTreeRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", "", false
	}
	return m[1], m[2], m[3], m[4], true
}

func describeKind(kind string) string {
	if kind == "commit" {
		return "submodule"
	}
	return "directory"
}

func describeMode(mode string) string {
	switch mode {
	case "120000":
		return "a symlink"
	case "160000":
		return "a submodule"
	}
	return "mode " + mode
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
