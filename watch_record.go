package cascadingconfig

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// watchRecord is what a watched load records, for planning the watch set (DL-224) and polling remotes (DL-230).
type watchRecord struct {
	targets  []watchTarget
	symlinks map[string]string // a symlinked file's path → the path it resolved to
	loaded   map[string]bool   // the files the load read, by path and by resolved path
	remotes  []remoteUse       // the remote includes the load used, in walk order
}

func newWatchRecord() *watchRecord {
	return &watchRecord{symlinks: map[string]string{}, loaded: map[string]bool{}}
}

// watchTarget is a place a candidate could appear: base, then one directory per segment but the last, then a file
// the last segment matches.
type watchTarget struct {
	base string
	segs []nameMatcher
}

// nameMatcher matches one path segment: exactly, or with a * glob, in any of several forms.
type nameMatcher struct {
	exact []string
	globs []*regexp.Regexp
}

func (m nameMatcher) match(name string) bool {
	return slices.Contains(m.exact, name) || slices.ContainsFunc(m.globs, func(re *regexp.Regexp) bool { return re.MatchString(name) })
}

// add adds a form of the segment; file says whether it names a file, for * matching.
func (m *nameMatcher) add(seg string, file bool) {
	if strings.Contains(seg, "*") {
		m.globs = append(m.globs, globPattern(seg, file))
		return
	}
	if !slices.Contains(m.exact, seg) {
		m.exact = append(m.exact, seg)
	}
}

func exactName(name string) nameMatcher { return nameMatcher{exact: []string{name}} }

// watchSearch records where search looks for patterns in dir, whether or not dir exists.
func (ld *loading) watchSearch(dir string, patterns []Pattern) {
	if ld.rec == nil || dir == "" {
		return
	}
	skip := ld.walking && ld.isHomeOrRoot(dir)
	for _, x := range ld.expansions(patterns) {
		if skip && x.p.SkipHomeAndRoot {
			continue
		}
		parts := strings.Split(x.p.Template, "/")
		segs := make([]nameMatcher, len(parts))
		for i, seg := range parts[:len(parts)-1] {
			segs[i].add(expand(seg, ld.c.name, x.env, ""), false)
		}
		last := &segs[len(segs)-1]
		tpl := parts[len(parts)-1]
		if !slices.Contains(placeholders(tpl), "{ext}") {
			last.add(expand(tpl, ld.c.name, x.env, ""), true)
		} else {
			for _, d := range ld.order {
				for _, ext := range d.Extensions() {
					last.add(expand(tpl, ld.c.name, x.env, ext), true)
				}
			}
		}
		ld.rec.targets = append(ld.rec.targets, watchTarget{base: dir, segs: segs})
	}
}

// watchFile records a file named directly, an explicit file or a local include, and its symlink target if it has
// one.
func (ld *loading) watchFile(p string) {
	if ld.rec == nil {
		return
	}
	ld.rec.targets = append(ld.rec.targets, watchTarget{base: filepath.Dir(p), segs: []nameMatcher{exactName(filepath.Base(p))}})
	ld.watchSymlink(p)
}

// watchSymlink records p's target, if p is a symlink: its directory is watched for its name, and events beside p
// resolve p again.
func (ld *loading) watchSymlink(p string) {
	if ld.rec == nil {
		return
	}
	resolved, err := ld.c.fsys.EvalSymlinks(p)
	if err != nil || resolved == p {
		return
	}
	ld.rec.symlinks[p] = resolved
	ld.rec.targets = append(ld.rec.targets, watchTarget{base: filepath.Dir(resolved), segs: []nameMatcher{exactName(filepath.Base(resolved))}})
}

// watchLoaded records a file the load read, so polling can hash it.
func (ld *loading) watchLoaded(paths ...string) {
	if ld.rec == nil {
		return
	}
	for _, p := range paths {
		if p != "" {
			ld.rec.loaded[p] = true
		}
	}
}
