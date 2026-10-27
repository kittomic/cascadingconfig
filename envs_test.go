package cascadingconfig

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// envFiles are a file for each environment name the tests select, plus the shared file.
var envFiles = fileSet(
	"/d/myapp.json", "/d/myapp.dev.json", "/d/myapp.ci.json", "/d/myapp.linux.json", "/d/myapp.unix.json",
	"/d/myapp.linux-x64.json", "/d/myapp.dev.linux.json",
)

// envsLoaded returns the env of each env layer, in order.
func envsLoaded(t *testing.T, change func(*hooks.Host), opts ...Option) []string {
	t.Helper()
	res := mustLoad(t, memfs.New(envFiles), append([]Option{
		hostWith(nil, change), WithBinding(BindEnv, ""),
		WithScope("s", FixedDir("/d", Pattern{Template: "{name}.json"}, Pattern{Template: "{name}.{env}.json", Variant: VariantEnv})),
		WithScopes("s"),
	}, opts...)...)
	var out []string
	for _, l := range res.found {
		if l.Variant == VariantEnv {
			out = append(out, l.Env)
		}
	}
	return out
}

// DL-121, design.md Environments: WithEnvs beats {NAME}_ENV, a comma list with spaces trimmed and empty entries
// dropped; platform envs rank below the selected ones; a name selected twice keeps its later position.
func TestEnvironmentSelection(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	tests := []struct {
		name   string
		change func(*hooks.Host)
		opts   []Option
		want   []string
	}{
		{"platform only", nil, nil, []string{"unix", "linux", "linux-x64"}},
		{"from the variable", setEnv("MYAPP_ENV", " ci , ,dev"), nil, []string{"unix", "linux", "linux-x64", "ci", "dev"}},
		{"an empty variable", setEnv("MYAPP_ENV", ""), nil, []string{"unix", "linux", "linux-x64"}},
		{"WithEnvs wins", setEnv("MYAPP_ENV", "ci"), []Option{WithEnvs("dev")}, []string{"unix", "linux", "linux-x64", "dev"}},
		{"a repeated name", setEnv("MYAPP_ENV", "dev,ci,dev"), nil, []string{"unix", "linux", "linux-x64", "ci", "dev"}},
		{"a selected platform name", setEnv("MYAPP_ENV", "linux,dev"), nil, []string{"unix", "linux-x64", "linux", "dev"}},
		{"dotted names are literal", setEnv("MYAPP_ENV", "dev.linux"), nil, []string{"unix", "linux", "linux-x64", "dev.linux"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := envsLoaded(t, tc.change, tc.opts...); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("env layers = %q, want %q", got, tc.want)
			}
		})
	}
}

// DL-31: names are never combined: myapp.dev.linux.json loads only when dev.linux itself is selected.
func TestEnvironmentsAreNeverCombined(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	got := envsLoaded(t, setEnv("MYAPP_ENV", "dev"))
	for _, env := range got {
		if strings.Contains(env, ".") {
			t.Errorf("env layers = %q: a combined name loaded", got)
		}
	}
}

// design.md Environments: platform environment names follow GOOS and GOARCH, with mise's renames.
func TestPlatformEnvs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos, goarch string
		want         []string
	}{
		{"linux", "amd64", []string{"unix", "linux", "linux-x64"}},
		{"darwin", "arm64", []string{"unix", "macos", "macos-arm64"}},
		{"windows", "amd64", []string{"windows", "windows-x64"}},
		{"freebsd", "riscv64", []string{"unix", "freebsd", "freebsd-riscv64"}},
		{"android", "arm64", []string{"unix", "android", "android-arm64"}},
		{"plan9", "386", []string{"plan9", "plan9-386"}},
		{"js", "wasm", []string{"js", "js-wasm"}},
		{"wasip1", "wasm", []string{"wasip1", "wasip1-wasm"}},
	}
	for _, tc := range tests {
		if got := platformEnvs(tc.goos, tc.goarch); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("platformEnvs(%s, %s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

// design.md Environments: when a name makes one file match two patterns, it loads once, at its higher position. With
// env local, myapp.local.json is both the local file and the env file of local.
func TestAmbiguousEnvironmentName(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := memfs.New(fileSet("/d/myapp.local.json"))
	res := mustLoad(t, fsys, WithEnvs("local"),
		WithScope("s", FixedDir("/d",
			Pattern{Template: "{name}.{env}.json", Variant: VariantEnv},
			Pattern{Template: "{name}.local.json", Variant: VariantLocal},
		)),
		WithScopes("s"),
	)
	if len(res.found) != 1 || res.found[0].Variant != VariantLocal {
		t.Errorf("found:\n%s\nwant the file once, as the local file", detail(res))
	}
}

// DL-32, DL-121: an unsafe name in {NAME}_ENV is an *OptionError at Load whose Op is the variable's name, and so is a
// filename longer than 255 bytes.
func TestUnsafeEnvironmentNames(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	for _, value := range []string{"../x", "a/b", `a\b`, "dev,..", "a:b", strings.Repeat("e", 250)} {
		t.Run(value, func(t *testing.T) {
			l := loader(t, memfs.New(envFiles), hostWith(map[string]string{"MYAPP_ENV": value}, nil), WithBinding(BindEnv, ""),
				WithScope("s", FixedDir("/d", Pattern{Template: "{name}.{env}.json", Variant: VariantEnv})), WithScopes("s"))
			_, err := l.Load(t.Context())
			var oe *OptionError
			if !errors.As(err, &oe) || oe.Op != "MYAPP_ENV" {
				t.Errorf("error = %v, want an *OptionError with Op MYAPP_ENV", err)
			}
		})
	}
}
