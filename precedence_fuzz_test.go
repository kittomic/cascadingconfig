package cascadingconfig_test

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// FuzzPrecedence (DL-246) builds a random world: a built-in or random layout, a host, a start dir, environments, a
// format order, a conflict policy, and files, with decoys, empty files and symlinks. It loads it, and checks the
// report's order and statuses, and the merged "top" key, against the oracle, which computes them from
// docs/precedence.md alone.

// fuzzBytes reads choices from the fuzzer's input; past the end, every choice is 0.
type fuzzBytes struct {
	b []byte
	i int
}

func (f *fuzzBytes) n(k int) int {
	if f.i >= len(f.b) || k <= 1 {
		f.i++
		return 0
	}
	f.i++
	return int(f.b[f.i-1]) % k
}

func pick[T any](f *fuzzBytes, xs ...T) T { return xs[f.n(len(xs))] }

// The formats every world registers: JSON's decoder stands in for TOML and YAML (DL-246).
var (
	fzToml = oFormat{"toml", []string{"toml"}}
	fzYaml = oFormat{"yaml", []string{"yaml", "yml"}}
	fzJSON = oFormat{"json", []string{"json"}}
)

func fzDecoders() cc.Option {
	return cc.Options(cc.WithDecoder(jsonNamed{"toml", []string{"toml"}}), cc.WithDecoder(jsonNamed{"yaml", []string{"yaml", "yml"}}))
}

// fzFormats returns a permutation of formats.
func fzFormats(f *fuzzBytes, formats ...oFormat) []oFormat {
	out := slices.Clone(formats)
	for i := len(out) - 1; i > 0; i-- {
		j := f.n(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func formatNames(fs []oFormat) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.name)
	}
	return out
}

// fzWorld is a world and the options that set it up for the loader.
type fzWorld struct {
	w      *oWorld
	preset *cc.Preset // a built-in layout to start from, or nil
	opts   []cc.Option
}

const fzHome = "/home/me"

func fzHost(f *fuzzBytes) (goos, goarch, start string) {
	return pick(f, "linux", "darwin", "freebsd"), pick(f, "amd64", "arm64"),
		pick(f, fzHome+"/work/app", fzHome+"/work", fzHome, "/srv/app/deep", "/srv")
}

func fzEnvs(f *fuzzBytes) []string {
	var envs []string
	for range f.n(4) {
		envs = append(envs, pick(f, "dev", "prod", "local", "dev.local", "linux", "macos", "unix"))
	}
	return envs
}

// fzMise is MiseLayout's world.
func fzMise(f *fuzzBytes) fzWorld {
	goos, goarch, start := fzHost(f)
	w := &oWorld{
		name: "myapp", goos: goos, goarch: goarch, home: fzHome, start: start, stop: "home", envs: fzEnvs(f),
		formats: fzFormats(f, fzToml, fzYaml, fzJSON), conflictFirst: f.n(4) == 0,
		scopes: []oScope{
			{name: "system", dir: "/etc/myapp", patterns: oMiseGlobal, trust: 3},
			{name: "user", dir: fzHome + "/.config/myapp", patterns: oMiseGlobal, trust: 2},
			{name: "project", walk: true, patterns: oMiseProject, trust: 1},
			{name: "override", dir: fzHome + "/.config/myapp", patterns: oMiseOverride, trust: 2},
		},
		explicitAt: "project",
	}
	opts := []cc.Option{fzDecoders(), cc.WithFormats(formatNames(w.formats)...)}
	if w.envs != nil {
		opts = append(opts, cc.WithEnvs(w.envs...))
	}
	if w.conflictFirst {
		opts = append(opts, cc.WithFormatConflict(cc.ConflictFirst))
	}
	return fzWorld{w: w, preset: &cc.MiseLayout, opts: opts}
}

// fzCosmi is CosmiconfigLayout's world, searching the start dir only, or walking to ~ with the user dir too.
func fzCosmi(f *fuzzBytes) fzWorld {
	goos, goarch, start := fzHost(f)
	global := f.n(2) == 0
	user := fzHome + "/.config/myapp"
	if goos == "darwin" {
		user = fzHome + "/Library/Preferences/myapp"
	}
	w := &oWorld{
		name: "myapp", goos: goos, goarch: goarch, home: fzHome, start: start, stop: "start",
		formats: []oFormat{fzJSON, fzYaml}, ruleFormat: "yaml", conflictFirst: true, firstMatch: true, explicitOnly: true,
		scopes: []oScope{
			{name: "project", walk: true, patterns: oCosmiProject, trust: 1},
			{name: "meta", dir: start, patterns: oCosmiMeta, trust: 1, key: "myapp"},
		},
	}
	opts := []cc.Option{fzDecoders()}
	if global {
		w.stop = "home"
		w.scopes = append([]oScope{{name: "user", dir: user, patterns: oCosmiUser, trust: 2}}, w.scopes...)
		opts = append(opts, cc.WithSearchStrategy(cc.SearchGlobal))
	}
	return fzWorld{w: w, preset: &cc.CosmiconfigLayout, opts: opts}
}

// fzVocab is the random layouts' template vocabulary, with each template's group.
var fzVocab = oTable(
	"{name}.{ext} shared search",
	"config.{ext} shared search",
	"conf.d/*.{ext} shared search",
	"conf.d/*/{name}.{ext} shared file",
	"{name}/config.{ext} shared search",
	".{name}.{ext} shared search",
	"*.{ext} shared search",
	"{name}.{env}.{ext} env search",
	"conf.d/*.{env}.{ext} env search",
	"config.{env}.{ext} env search",
	"{name}.local.{ext} local search",
	"config.local.{ext} local search",
	"{name}.{env}.local.{ext} env-local search",
	"*.{env}.local.{ext} env-local search",
)

var fzVariants = []cc.Variant{cc.VariantShared, cc.VariantEnv, cc.VariantLocal, cc.VariantEnvLocal}

// fzRandom is a random layout: one to three scopes, each a FixedDir or a walk, with random patterns and trust.
func fzRandom(f *fuzzBytes) fzWorld {
	goos, goarch, start := fzHost(f)
	w := &oWorld{
		name: "myapp", goos: goos, goarch: goarch, home: fzHome, start: start, stop: pick(f, "home", "start", "root"),
		envs: fzEnvs(f), formats: fzFormats(f, fzToml, fzYaml, fzJSON), conflictFirst: f.n(3) == 0, firstMatch: f.n(4) == 0,
	}
	opts := []cc.Option{cc.WithDecoder(jsonDecoder()), fzDecoders(), cc.WithFormats(formatNames(w.formats)...),
		cc.WithWalkStop(map[string]cc.WalkStop{"home": cc.StopAtHome, "start": cc.StopAtStart, "root": cc.StopAtRoot}[w.stop])}
	if w.envs != nil {
		opts = append(opts, cc.WithEnvs(w.envs...))
	}
	if w.conflictFirst {
		opts = append(opts, cc.WithFormatConflict(cc.ConflictFirst))
	}
	if w.firstMatch {
		opts = append(opts, cc.WithResolution(cc.FirstMatch))
	}
	trusts := []cc.TrustLevel{cc.TrustProject, cc.TrustUser, cc.TrustSystem}
	var names []cc.ScopeName
	for i := range 1 + f.n(3) {
		s := oScope{name: fmt.Sprintf("s%d", i), trust: 1 + f.n(3)}
		var patterns []cc.Pattern
		for range 1 + f.n(6) {
			p := fzVocab[f.n(len(fzVocab))]
			s.patterns = append(s.patterns, p)
			root := cc.RootSearchDir
			if p.fileRoot {
				root = cc.RootFileDir
			}
			patterns = append(patterns, cc.Pattern{Template: p.tpl, Variant: fzVariants[p.variant], Root: root})
		}
		var kind cc.ScopeKind
		if f.n(2) == 0 {
			s.walk = true
			kind = cc.Walk(patterns...)
		} else {
			s.dir = pick(f, "/etc/r", "/opt/r/conf", fzHome+"/.r")
			kind = cc.FixedDir(s.dir, patterns...)
		}
		w.scopes = append(w.scopes, s)
		opts = append(opts, cc.WithScope(cc.ScopeName(s.name), kind, cc.Trust(trusts[s.trust-1])))
		names = append(names, cc.ScopeName(s.name))
	}
	opts = append(opts, cc.WithScopes(names...))
	return fzWorld{w: w, opts: opts}
}

// fzFiles adds files to the world: each built from one of its patterns, in one of its directories, with random
// environments, stems, glob directories and extensions, or a decoy; some are empty, symlinks or broken symlinks.
func fzFiles(f *fuzzBytes, w *oWorld) map[string]string {
	w.files, w.links = map[string]bool{}, map[string]string{}
	content := map[string]string{}
	var regular []string
	clash := func(p string) bool {
		for q := range content {
			if strings.HasPrefix(q, p+"/") || strings.HasPrefix(p, q+"/") || q == p {
				return true
			}
		}
		return false
	}
	envs := append(w.envOrder(), "qa")
	exts := []string{"toml", "yaml", "yml", "json", "txt"}
	for range 1 + f.n(14) {
		s := w.scopes[f.n(len(w.scopes))]
		dir := s.dir
		if s.walk {
			dirs := append(w.walkDirs(), "/home", path.Join(w.start, "sub"))
			dir = dirs[f.n(len(dirs))]
		}
		p := s.patterns[f.n(len(s.patterns))]
		name := w.name
		if f.n(12) == 0 {
			name = "MyApp" // a decoy: matching is case-sensitive
		}
		segs := strings.Split(p.tpl, "/")
		for i, seg := range segs {
			seg = strings.ReplaceAll(seg, "{name}", name)
			seg = strings.ReplaceAll(seg, "{env}", envs[f.n(len(envs))])
			seg = strings.ReplaceAll(seg, "{ext}", exts[f.n(len(exts))])
			if i < len(segs)-1 {
				seg = strings.ReplaceAll(seg, "*", pick(f, "w", "x", ".h"))
			} else {
				seg = strings.ReplaceAll(seg, "*", pick(f, "a", "b", "10-x", "a.b"))
			}
			segs[i] = seg
		}
		file := path.Join(dir, strings.Join(segs, "/"))
		if clash(file) {
			continue
		}
		switch k := f.n(10); {
		case k == 0 && len(regular) > 0:
			w.links[file] = regular[f.n(len(regular))]
			content[file] = "->"
		case k == 1:
			w.links[file] = file + ".missing"
			content[file] = "->"
		case k == 2:
			w.files[file] = true
			content[file] = ""
		default:
			w.files[file] = false
			// both shapes, so a symlink from a scope with a pattern Key to one without, or back, reads the same top
			content[file] = fmt.Sprintf(`{"top": %q, %q: {"top": %q}}`, file, w.name, file)
			regular = append(regular, file)
		}
	}
	for i := range f.n(3) {
		e := fmt.Sprintf("/x/e%d.json", i)
		w.files[e] = false
		content[e] = fmt.Sprintf(`{"top": %q}`, e)
		w.explicit = append(w.explicit, e)
	}
	return content
}

func FuzzPrecedence(f *testing.F) {
	skipUnixPaths(f)
	f.Add([]byte{0, 0, 0, 0, 2, 3, 0, 1, 0, 9, 0, 3, 5, 1, 2, 3})
	f.Add([]byte{1, 1, 1, 1, 0, 1, 2, 2, 3, 1, 0, 0, 4, 7, 2, 1, 1, 0})
	f.Add([]byte{2, 0, 1, 3, 1, 2, 0, 1, 3, 2, 5, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{0, 1, 0, 2, 3, 4, 1, 0, 0, 5, 6, 0, 1, 0, 0, 1, 2, 0, 0, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		fb := &fuzzBytes{b: b}
		var fw fzWorld
		switch fb.n(3) {
		case 0:
			fw = fzMise(fb)
		case 1:
			fw = fzCosmi(fb)
		default:
			fw = fzRandom(fb)
		}
		w := fw.w
		files := fzFiles(fb, w)
		fsys := cctest.NewFS(nil)
		for p, body := range files {
			if body != "->" {
				fsys.WriteFile(p, []byte(body), 0o644)
			}
		}
		for p, target := range w.links {
			fsys.Symlink(target, p)
		}
		opts := slices.Clone(fw.opts)
		if w.explicit != nil {
			opts = append(opts, cc.WithConfigFiles(w.explicit...))
		}
		host := cctest.Host{GOOS: w.goos, GOARCH: w.goarch, Home: w.home, Cwd: w.start}
		opts = append([]cc.Option{cc.WithFileSystem(fsys), cctest.WithHost(host)}, opts...)
		var l *cc.Loader
		var err error
		if fw.preset != nil {
			l, err = cc.NewWithPreset("myapp", *fw.preset, opts...)
		} else {
			l, err = cc.New("myapp", opts...)
		}
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		res, err := l.Load(t.Context())
		if err != nil {
			t.Fatalf("Load: %v\nfiles: %v\nlinks: %v", err, files, w.links)
		}
		want := w.order()
		var got []string
		for _, lr := range res.Describe().Layers {
			got = append(got, fmt.Sprintf("%s %s %s", lr.Path, lr.Scope, lr.Status))
		}
		var exp []string
		for _, ol := range want {
			exp = append(exp, fmt.Sprintf("%s %s %s", ol.path, ol.scope, ol.status))
		}
		if !slices.Equal(got, exp) {
			t.Fatalf("the report's layers differ from the oracle's (precedence.md)\nworld: %+v\nlinks: %v\ngot:\n  %s\nwant:\n  %s",
				*w, w.links, strings.Join(got, "\n  "), strings.Join(exp, "\n  "))
		}
		top, _ := res.Tree()["top"].(string)
		if wantTop := oTop(want); top != wantTop {
			t.Fatalf("top = %q, want %q from the oracle's highest layer", top, wantTop)
		}
	})
}
