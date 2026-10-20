package cascadingconfig

import (
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

var variantNames = map[Variant]string{
	VariantShared: "shared", VariantEnv: "env", VariantLocal: "local", VariantEnvLocal: "env-local",
}

// detail writes the layers a load considered, one per line: scope, path, variant, env, config root and status, then any
// warning codes.
func detail(res *Result) string {
	var b strings.Builder
	for _, l := range res.found {
		env := l.Env
		if env == "" {
			env = "-"
		}
		fields := []string{string(l.Scope), l.Path, variantNames[l.Variant], env, l.ConfigRoot, statusNames[l.Status]}
		for _, w := range l.Warnings {
			fields = append(fields, string(w.Code))
		}
		b.WriteString(strings.Join(fields, " ") + "\n")
	}
	return b.String()
}

// fileSet returns files with the same JSON content naming each file, so the merged tree shows which loaded last.
func fileSet(paths ...string) map[string]string {
	files := map[string]string{}
	for _, p := range paths {
		files[p] = `{"last": "` + p + `"}`
	}
	return files
}

// miseLoad loads MiseLayout over fsys, with TOML registered as JSON so file names can use .toml, on the test host
// changed by change.
func miseLoad(t *testing.T, fsys *memfs.FS, change func(h *hooks.Host), opts ...Option) *Result {
	t.Helper()
	h := testHost()
	if change != nil {
		change(&h)
	}
	l, err := NewWithPreset("myapp", MiseLayout,
		append([]Option{WithFileSystem(memFS{fsys}), withHost(fakeHost{h}), WithDecoder(tomlAsJSON)}, opts...)...)
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

func setEnv(kv ...string) func(h *hooks.Host) {
	return func(h *hooks.Host) {
		for i := 0; i+1 < len(kv); i += 2 {
			h.Env[kv[i]] = kv[i+1]
		}
	}
}

func inDir(dir string, change func(h *hooks.Host)) func(h *hooks.Host) {
	return func(h *hooks.Host) {
		h.Cwd = dir
		if change != nil {
			change(h)
		}
	}
}
