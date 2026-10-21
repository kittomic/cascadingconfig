package cascadingconfig_test

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// The precedence oracle (DL-244) computes the order of layers from docs/precedence.md alone. It has its own copy of
// the built-in layouts' tables, written from the doc, and its own implementation of every rule; it never reads
// layouts.go or calls the loader. Paths are slash paths on a Unix host.

// oVariant is a pattern's group (PR-3), in rank order.
type oVariant int

const (
	oShared oVariant = iota
	oEnv
	oLocal
	oEnvLocal
)

var oVariantNames = map[string]oVariant{"shared": oShared, "env": oEnv, "local": oLocal, "env-local": oEnvLocal}

// oPattern is one row of a layout table.
type oPattern struct {
	tpl      string
	variant  oVariant
	fileRoot bool
}

// oScope is one scope: a FixedDir with its directory filled in, or the walk.
type oScope struct {
	name     string
	walk     bool
	dir      string
	patterns []oPattern
	trust    int    // for PR-11: higher is more trusted
	key      string // a pattern Key, such as the meta file's {name}; its files hold {key: config}
}

// oFormat is a format, with its extensions highest first.
type oFormat struct {
	name string
	exts []string
}

// oWorld is everything the order depends on (precedence.md: Inputs), as the fuzz test sets it up.
type oWorld struct {
	name, goos, goarch string
	home, start        string
	stop               string // "home", "start" or "root"
	envs               []string
	formats            []oFormat // highest first
	ruleFormat         string    // the format of a template without {ext}, from a WithFileFormat rule
	conflictFirst      bool
	firstMatch         bool
	scopes             []oScope // lowest first
	explicit           []string // explicit files, at the place of the scope named explicitAt, or above every scope
	explicitAt         string
	explicitOnly       bool              // WithConfigFilesOnly: explicit files replace every file-searching scope, at the highest one\'s place (the top here)
	files              map[string]bool   // regular files; true for an empty one
	links              map[string]string // symlinks: path → target
}

// oLayer is one entry of the expected report.
type oLayer struct {
	path, scope, status string
	identity            string // the resolved path, for PR-11
	trust               int
	top                 bool // not included; every oracle layer is top-level
}

// --- the built-in layouts' tables, written from precedence.md ---

func oTable(rows ...string) []oPattern {
	var out []oPattern
	for _, r := range rows {
		f := strings.Fields(r)
		out = append(out, oPattern{tpl: f[0], variant: oVariantNames[f[1]], fileRoot: f[2] == "file"})
	}
	return out
}

var oMiseGlobal = oTable(
	"conf.d/*.{ext} shared search",
	"conf.d/*/{name}.{ext} shared file",
	"config.{ext} shared search",
	"{name}.{ext} shared search",
	"conf.d/*.{env}.{ext} env search",
	"conf.d/*/{name}.{env}.{ext} env file",
	"config.{env}.{ext} env search",
	"{name}.{env}.{ext} env search",
	"conf.d/*/{name}.local.{ext} local file",
	"config.local.{ext} local search",
	"{name}.local.{ext} local search",
	"conf.d/*.{env}.local.{ext} env-local search",
	"conf.d/*/{name}.{env}.local.{ext} env-local file",
	"config.{env}.local.{ext} env-local search",
	"{name}.{env}.local.{ext} env-local search",
)

var oMiseProject = oTable(
	".config/{name}/conf.d/*.{ext} shared search",
	".config/{name}/conf.d/*/{name}.{ext} shared file",
	".config/{name}/config.{ext} shared search",
	".config/{name}/{name}.{ext} shared file",
	".config/{name}.{ext} shared search",
	".{name}/conf.d/*.{ext} shared search",
	".{name}/conf.d/*/{name}.{ext} shared file",
	".{name}/config.{ext} shared search",
	"{name}/conf.d/*.{ext} shared search",
	"{name}/conf.d/*/{name}.{ext} shared file",
	"{name}/config.{ext} shared search",
	"{name}.{ext} shared search",
	".{name}.{ext} shared search",
	".config/{name}/conf.d/*.{env}.{ext} env search",
	".config/{name}/conf.d/*/{name}.{env}.{ext} env file",
	".config/{name}/config.{env}.{ext} env search",
	".config/{name}.{env}.{ext} env search",
	"{name}/conf.d/*.{env}.{ext} env search",
	"{name}/conf.d/*/{name}.{env}.{ext} env file",
	"{name}/config.{env}.{ext} env search",
	"{name}.{env}.{ext} env search",
	".{name}/conf.d/*.{env}.{ext} env search",
	".{name}/conf.d/*/{name}.{env}.{ext} env file",
	".{name}/config.{env}.{ext} env search",
	".{name}.{env}.{ext} env search",
	".config/{name}/conf.d/*/{name}.local.{ext} local file",
	".config/{name}/config.local.{ext} local search",
	".config/{name}/{name}.local.{ext} local file",
	".config/{name}.local.{ext} local search",
	".{name}/conf.d/*/{name}.local.{ext} local file",
	".{name}/config.local.{ext} local search",
	"{name}/conf.d/*/{name}.local.{ext} local file",
	"{name}/config.local.{ext} local search",
	"{name}.local.{ext} local search",
	".{name}.local.{ext} local search",
	".config/{name}/conf.d/*.{env}.local.{ext} env-local search",
	".config/{name}/conf.d/*/{name}.{env}.local.{ext} env-local file",
	".config/{name}/config.{env}.local.{ext} env-local search",
	".config/{name}.{env}.local.{ext} env-local search",
	"{name}/conf.d/*.{env}.local.{ext} env-local search",
	"{name}/conf.d/*/{name}.{env}.local.{ext} env-local file",
	"{name}/config.{env}.local.{ext} env-local search",
	"{name}.{env}.local.{ext} env-local search",
	".{name}/conf.d/*.{env}.local.{ext} env-local search",
	".{name}/conf.d/*/{name}.{env}.local.{ext} env-local file",
	".{name}/config.{env}.local.{ext} env-local search",
	".{name}.{env}.local.{ext} env-local search",
)

var oMiseOverride = oTable(
	"config.override.{ext} shared search",
	"config.{env}.override.{ext} env search",
)

var oCosmiUser = oTable("config.{ext} shared search", "config shared search")

var oCosmiProject = oTable(
	".config/{name}rc.{ext} shared search",
	".config/{name}rc shared search",
	".{name}rc.{ext} shared search",
	".{name}rc shared search",
)

var oCosmiMeta = oTable(".config/config.{ext} shared search")

// oLayoutTables are the tables precedence.md must hold, by heading: "Layout scope name".
var oLayoutTables = map[string][]oPattern{
	"MiseLayout scope system":         oMiseGlobal,
	"MiseLayout scope project":        oMiseProject,
	"MiseLayout scope override":       oMiseOverride,
	"CosmiconfigLayout scope user":    oCosmiUser,
	"CosmiconfigLayout scope project": oCosmiProject,
	"CosmiconfigLayout scope meta":    oCosmiMeta,
}

// --- the filesystem the world describes ---

func (w *oWorld) isDir(p string) bool {
	prefix := strings.TrimSuffix(p, "/") + "/"
	for f := range w.files {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	for l := range w.links {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// entries lists the names in dir, in byte order.
func (w *oWorld) entries(dir string) []string {
	set := map[string]bool{}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	add := func(p string) {
		if rest, ok := strings.CutPrefix(p, prefix); ok && rest != "" {
			set[strings.SplitN(rest, "/", 2)[0]] = true
		}
	}
	for f := range w.files {
		add(f)
	}
	for l := range w.links {
		add(l)
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// resolve follows a symlink: the target, or "" when it's broken.
func (w *oWorld) resolve(p string) string {
	if t, ok := w.links[p]; ok {
		if _, ok := w.files[t]; ok {
			return t
		}
		return ""
	}
	return p
}

// --- PR-4: environments ---

func (w *oWorld) envOrder() []string {
	var selected []string
	for i, e := range w.envs {
		if !slices.Contains(w.envs[i+1:], e) {
			selected = append(selected, e)
		}
	}
	var platform []string
	switch w.goos {
	case "windows", "plan9", "js", "wasip1":
	default:
		platform = append(platform, "unix")
	}
	os, arch := w.goos, w.goarch
	if os == "darwin" {
		os = "macos"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	platform = append(platform, os, os+"-"+arch)
	var out []string
	for _, p := range platform {
		if !slices.Contains(selected, p) {
			out = append(out, p)
		}
	}
	return append(out, selected...)
}

// --- PR-2: the walk ---

func (w *oWorld) walkDirs() []string {
	var dirs []string
	for d := path.Clean(w.start); ; {
		dirs = append(dirs, d)
		if w.stop == "start" || w.stop == "home" && d == w.home {
			break
		}
		parent := path.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	slices.Reverse(dirs)
	return dirs
}

// --- PR-6 and PR-7: matching one pattern in one directory ---

// oCand is one candidate file, with what ranks it within its slot.
type oCand struct {
	path   string
	format int // index in the format order: lower ranks higher
	ext    int // index in the format's extensions: lower ranks higher
}

// globRE turns a segment with * into a regexp: in a file name * matches a non-empty run with no dots.
func globRE(seg string, file bool) *regexp.Regexp {
	star := ".+"
	if file {
		star = "[^.]+"
	}
	parts := strings.Split(seg, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, "("+star+")") + "$")
}

func (w *oWorld) fill(s, env string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "{name}", w.name), "{env}", env)
}

// match returns the slots one pattern makes in dir, in rank order, each slot's files lowest first (PR-6, PR-7).
func (w *oWorld) match(dir string, p oPattern, env string) [][]oCand {
	segs := strings.Split(p.tpl, "/")
	subs := []string{dir}
	for _, seg := range segs[:len(segs)-1] {
		seg = w.fill(seg, env)
		var next []string
		for _, sub := range subs {
			if !strings.Contains(seg, "*") {
				next = append(next, path.Join(sub, seg))
				continue
			}
			re := globRE(seg, false)
			for _, n := range w.entries(sub) {
				if !strings.HasPrefix(n, ".") && re.MatchString(n) && w.isDir(path.Join(sub, n)) {
					next = append(next, path.Join(sub, n))
				}
			}
		}
		subs = next
	}
	last := w.fill(segs[len(segs)-1], env)
	var out [][]oCand
	for _, sub := range subs {
		names := w.entries(sub)
		slots := map[string][]oCand{}
		if !strings.Contains(last, "{ext}") {
			re := globRE(last, true)
			for _, n := range names {
				if n == last || strings.Contains(last, "*") && re.MatchString(n) {
					slots[n] = append(slots[n], oCand{path: path.Join(sub, n)})
				}
			}
		} else {
			for fi, f := range w.formats {
				for ei, e := range f.exts {
					name := strings.ReplaceAll(last, "{ext}", e)
					if !strings.Contains(name, "*") {
						if slices.Contains(names, name) {
							slots[""] = append(slots[""], oCand{path: path.Join(sub, name), format: fi, ext: ei})
						}
						continue
					}
					re := globRE(name, true)
					for _, n := range names {
						if m := re.FindStringSubmatch(n); m != nil {
							stem := strings.Join(m[1:], "\x00")
							slots[stem] = append(slots[stem], oCand{path: path.Join(sub, n), format: fi, ext: ei})
						}
					}
				}
			}
		}
		keys := make([]string, 0, len(slots))
		for k := range slots {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			slot := slots[k]
			// highest rank first, then reversed, so the slot applies lowest first
			slices.SortStableFunc(slot, func(a, b oCand) int {
				if a.format != b.format {
					return a.format - b.format
				}
				return a.ext - b.ext
			})
			slices.Reverse(slot)
			out = append(out, slot)
		}
	}
	return out
}

// --- the whole order ---

// order returns the expected report, lowest first.
func (w *oWorld) order() []oLayer {
	envs := w.envOrder()
	var found []oLayer
	add := func(p, scope string, trust int, status string) {
		found = append(found, oLayer{path: p, scope: scope, trust: trust, status: status, identity: w.resolve(p), top: true})
	}
	read := func(p, scope string, trust int) {
		switch {
		case w.resolve(p) == "":
			add(p, scope, trust, "ignored")
		case w.files[w.resolve(p)]:
			add(p, scope, trust, "empty")
		default:
			add(p, scope, trust, "loaded")
		}
	}
	scopes := w.scopes
	if w.explicit != nil && w.explicitOnly {
		scopes = nil
	}
	for _, s := range scopes {
		if w.explicit != nil && s.name == w.explicitAt {
			for _, e := range w.explicit {
				read(e, s.name, 1)
			}
			continue
		}
		dirs := []string{s.dir}
		if s.walk {
			dirs = w.walkDirs()
		}
		for _, dir := range dirs {
			for g := oShared; g <= oEnvLocal; g++ {
				groupEnvs := []string{""}
				if g == oEnv || g == oEnvLocal {
					groupEnvs = envs
				}
				for _, env := range groupEnvs {
					for _, p := range s.patterns {
						if p.variant != g {
							continue
						}
						for _, slot := range w.match(dir, p, env) {
							for i, c := range slot {
								if w.conflictFirst && i < len(slot)-1 {
									add(c.path, s.name, s.trust, "ignored")
									continue
								}
								read(c.path, s.name, s.trust)
							}
						}
					}
				}
			}
		}
	}
	if w.explicit != nil && w.explicitAt == "" {
		for _, e := range w.explicit {
			read(e, "", 1)
		}
	}
	// PR-11: a file reached twice keeps its highest place, with its most trusted route's scope and trust
	for i := range found {
		if found[i].status != "loaded" && found[i].status != "empty" {
			continue
		}
		for j := range found[:i] {
			prev := &found[j]
			if (prev.status != "loaded" && prev.status != "empty") || prev.identity != found[i].identity {
				continue
			}
			if prev.trust > found[i].trust {
				found[i].scope, found[i].trust = prev.scope, prev.trust
			}
			prev.status = "duplicate"
			break
		}
	}
	// PR-13: the highest loaded, non-empty file is chosen; files below it are ignored, undecoded
	if w.firstMatch {
		chosen := false
		for i := len(found) - 1; i >= 0; i-- {
			l := &found[i]
			switch {
			case l.status != "loaded" && l.status != "empty":
			case chosen:
				l.status = "ignored"
			case l.status == "loaded":
				chosen = true
			}
		}
	}
	return found
}

// oTop returns the file whose content the highest merging layer holds: its resolved path, since a symlink is read
// through its target (PR-8), or "" when nothing merges.
func oTop(layers []oLayer) string {
	for i := len(layers) - 1; i >= 0; i-- {
		if layers[i].status == "loaded" {
			return layers[i].identity
		}
	}
	return ""
}
