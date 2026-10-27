package yaml_test

import (
	"reflect"
	"runtime"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/format/yaml"
)

func load(t *testing.T, files, env map[string]string, opts ...cc.Option) *cc.Result {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests use Unix paths")
	}
	if env == nil {
		env = map[string]string{}
	}
	base := []cc.Option{
		cc.WithFileSystem(cctest.NewFS(files)),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me", Env: env}),
		cc.WithDecoder(yaml.Decoder()),
		cc.WithScope("s", cc.FixedDir("/etc/myapp", cc.Pattern{Template: "config.{ext}"})),
		cc.WithScopes("s"),
	}
	l, err := cc.New("myapp", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// DL-47: .yml is YAML, the yml format name means yaml, and .yaml ranks above .yml in a slot.
func TestYAMLExtensions(t *testing.T) {
	res := load(t, map[string]string{"/etc/myapp/config.yml": "a: yml\n"}, nil)
	if got := res.Tree(); !reflect.DeepEqual(got, cc.Tree{"a": "yml"}) {
		t.Errorf(".yml: Tree = %v", got)
	}

	res = load(t, map[string]string{"/etc/myapp/config.yml": "a: yml\n"}, map[string]string{"MYAPP_CONFIG_FORMATS": "yml"},
		cc.WithBinding(cc.BindConfigFormats, ""))
	if got := res.Tree(); !reflect.DeepEqual(got, cc.Tree{"a": "yml"}) {
		t.Errorf("MYAPP_CONFIG_FORMATS=yml: Tree = %v", got)
	}
	if f := res.Describe().Formats; !reflect.DeepEqual(f, []string{"yaml"}) {
		t.Errorf("MYAPP_CONFIG_FORMATS=yml: formats = %v, want yaml", f)
	}

	both := map[string]string{"/etc/myapp/config.yml": "a: yml\nb: yml\n", "/etc/myapp/config.yaml": "a: yaml\n"}
	if got := load(t, both, nil).Tree(); !reflect.DeepEqual(got, cc.Tree{"a": "yaml", "b": "yml"}) {
		t.Errorf("merged slot: Tree = %v, want .yaml applied over .yml", got)
	}
	res = load(t, both, nil, cc.WithFormatConflict(cc.ConflictFirst))
	if got := res.Tree(); !reflect.DeepEqual(got, cc.Tree{"a": "yaml"}) {
		t.Errorf("ConflictFirst: Tree = %v, want only .yaml", got)
	}
}
