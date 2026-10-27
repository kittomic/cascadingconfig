package cascadingconfig_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// The decoding tests use only the public API: cctest for the filesystem and the host, and the report for what a load
// found.

const (
	sysFile  = "/etc/myapp/config.json"
	projFile = "/home/me/work/myapp.json"
)

// twoScopes defines and selects a system scope with sysFile below a project scope with projFile.
var twoScopes = cc.Options(
	cc.WithScope("sys", cc.FixedDir("/etc/myapp", cc.Pattern{Template: "config.json"}), cc.Trust(cc.TrustSystem)),
	cc.WithScope("proj", cc.FixedDir("/home/me/work", cc.Pattern{Template: "myapp.json"})),
	cc.WithScopes("sys", "proj"),
)

// testHost is a Linux host with a home directory and env.
func testHost(env map[string]string) cctest.Host {
	if env == nil {
		env = map[string]string{}
	}
	return cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work", Env: env}
}

func jsonDecoder() cc.Decoder { return ccjson.Decoder() }

// newLoader builds a loader over files on the test host with env, with JSON registered. opts come last.
func newLoader(t *testing.T, files map[string]string, env map[string]string, opts ...cc.Option) *cc.Loader {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	base := []cc.Option{cc.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(testHost(env)), cc.WithDecoder(ccjson.Decoder())}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// loadOK loads files with twoScopes and opts, and fails the test on an error.
func loadOK(t *testing.T, files map[string]string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := newLoader(t, files, nil, append([]cc.Option{twoScopes}, opts...)...).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// valuesScope defines and selects a Values scope named v holding tree.
func valuesScope(tree cc.Tree) cc.Option {
	return cc.Options(cc.WithScope("v", cc.Values(tree)), cc.WithScopes("v"))
}

// decodeValues decodes tree, from a Values scope, into v.
func decodeValues(t *testing.T, tree cc.Tree, v any, opts ...cc.Option) error {
	t.Helper()
	res, err := newLoader(t, nil, nil, append([]cc.Option{valuesScope(tree)}, opts...)...).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res.Decode(v)
}

// decodeErrors returns the *DecodeErrors in err, failing the test if err holds anything else.
func decodeErrors(t *testing.T, err error) []*cc.DecodeError {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want *DecodeErrors")
	}
	var out []*cc.DecodeError
	for _, e := range cc.Errors(err) {
		var de *cc.DecodeError
		if !errors.As(e, &de) {
			t.Errorf("error %v isn't a *DecodeError", e)
			continue
		}
		out = append(out, de)
	}
	return out
}

// layerAt returns the report's layer with path.
func layerAt(t *testing.T, rep *cc.Report, path string) cc.Layer {
	t.Helper()
	for _, l := range rep.Layers {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no layer %s in the report:\n%s", path, rep)
	return cc.Layer{}
}

// warningsWith returns the report's warnings with code.
func warningsWith(rep *cc.Report, code cc.WarningCode) []cc.Warning {
	var out []cc.Warning
	for _, w := range rep.Warnings {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}

// noSecret fails the test if s contains secret.
func noSecret(t *testing.T, what, s, secret string) {
	t.Helper()
	if strings.Contains(s, secret) {
		t.Errorf("%s contains the secret value %q:\n%s", what, secret, s)
	}
}
