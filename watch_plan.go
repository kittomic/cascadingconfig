package cascadingconfig

import (
	"errors"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"time"
)

// watchPlan is the watch set (DL-224): the directories to watch, each with the names that matter in it.
type watchPlan struct {
	dirs     map[string]*nameMatcher
	symlinks map[string]string // a symlinked file's path → the path it resolved to
	loaded   map[string]bool   // files the load read, which polling hashes
}

// planWatch works out the watch set from what a load recorded, looking at the filesystem as it is now.
func planWatch(fsys FileSystem, rec *watchRecord) *watchPlan {
	pl := &watchPlan{dirs: map[string]*nameMatcher{}, symlinks: maps.Clone(rec.symlinks), loaded: maps.Clone(rec.loaded)}
	for _, t := range rec.targets {
		pl.target(fsys, t.base, t.segs)
	}
	return pl
}

// union adds other's directories, names, symlinks and loaded files to pl, for a failed reload (DL-224).
func (pl *watchPlan) union(other *watchPlan) *watchPlan {
	if other == nil {
		return pl
	}
	for dir, m := range other.dirs {
		pl.add(dir, *m)
	}
	for k, v := range other.symlinks {
		if _, ok := pl.symlinks[k]; !ok {
			pl.symlinks[k] = v
		}
	}
	for k := range other.loaded {
		pl.loaded[k] = true
	}
	return pl
}

func (pl *watchPlan) add(dir string, m nameMatcher) {
	cur, ok := pl.dirs[dir]
	if !ok {
		cur = &nameMatcher{}
		pl.dirs[dir] = cur
	}
	for _, e := range m.exact {
		cur.add(e, false)
	}
	cur.globs = append(cur.globs, m.globs...)
	cur.any = cur.any || m.any
}

// dirState says whether dir exists. A directory that can't be opened for any reason but not existing counts as
// existing, so it's watched or polled rather than skipped.
func dirState(fsys FileSystem, dir string) bool {
	r, err := fsys.OpenRoot(dir)
	if err == nil {
		_ = r.Close()
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// target plans one recorded target: base watched for its first segment, then each existing directory the segment
// leads to for the next. A missing base is watched through its nearest existing ancestor, for the next path segment.
func (pl *watchPlan) target(fsys FileSystem, base string, segs []nameMatcher) {
	if !dirState(fsys, base) {
		for cur := base; ; {
			parent := filepath.Dir(cur)
			if parent == cur {
				return
			}
			if dirState(fsys, parent) {
				pl.add(parent, exactName(filepath.Base(cur)))
				return
			}
			cur = parent
		}
	}
	pl.add(base, segs[0])
	if len(segs) == 1 {
		return
	}
	r, err := fsys.OpenRoot(base)
	if err != nil {
		return
	}
	entries, err := r.ReadDir(".")
	var dirs []string
	if err == nil {
		for _, e := range entries {
			if !segs[0].match(e.Name()) || (len(segs[0].globs) > 0 && !segs[0].matchExact(e.Name()) && strings.HasPrefix(e.Name(), ".")) {
				continue
			}
			if info, err := r.Stat(e.Name()); err == nil && info.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
	}
	_ = r.Close()
	for _, d := range dirs {
		pl.target(fsys, filepath.Join(base, d), segs[1:])
	}
}

func (m nameMatcher) matchExact(name string) bool {
	for _, e := range m.exact {
		if e == name {
			return true
		}
	}
	return false
}

// relevant reports whether an event for p matters: its name passes its directory's filter, it's a watched directory
// itself, or it's beside a symlinked file that now resolves somewhere else.
func (pl *watchPlan) relevant(fsys FileSystem, p string) bool {
	dir := filepath.Dir(p)
	if m, ok := pl.dirs[dir]; ok && m.match(filepath.Base(p)) {
		return true
	}
	if _, ok := pl.dirs[p]; ok {
		return true
	}
	return pl.linkMoved(fsys, dir)
}

// linkMoved reports whether a symlinked file in dir now resolves somewhere else, or not at all.
func (pl *watchPlan) linkMoved(fsys FileSystem, dir string) bool {
	for link, was := range pl.symlinks {
		if filepath.Dir(link) != dir {
			continue
		}
		if now, err := fsys.EvalSymlinks(link); err != nil || now != was {
			return true
		}
	}
	return false
}

// holdsLink reports whether dir holds a symlinked file.
func (pl *watchPlan) holdsLink(dir string) bool {
	for link := range pl.symlinks {
		if filepath.Dir(link) == dir {
			return true
		}
	}
	return false
}

// dirScan is one poll of a directory (DL-226).
type dirScan struct {
	state   int // 0 listed, 1 missing, 2 unreadable
	entries map[string]entryScan
}

type entryScan struct {
	typ, mode fs.FileMode
	size      int64
	mtime     time.Time
	info      fs.FileInfo
	target    string // where a symlink resolves to
	sum       string // the content's SHA-256, for a loaded file
}

// scan polls dir: the entries its filter passes, or every entry beside a symlinked file, compared by type, mode,
// size, modification time and identity, and loaded files by content too.
func (pl *watchPlan) scan(fsys FileSystem, dir string, maxSize int64) dirScan {
	r, err := fsys.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dirScan{state: 1}
		}
		return dirScan{state: 2}
	}
	defer r.Close()
	entries, err := r.ReadDir(".")
	if err != nil {
		return dirScan{state: 2}
	}
	m := pl.dirs[dir]
	all := pl.holdsLink(dir)
	out := dirScan{entries: map[string]entryScan{}}
	for _, e := range entries {
		name := e.Name()
		if !all && (m == nil || !m.match(name)) {
			continue
		}
		s := entryScan{typ: e.Type()}
		info, err := r.Stat(name)
		if err != nil {
			info, err = e.Info()
		}
		if err == nil {
			s.mode, s.size, s.mtime, s.info = info.Mode(), info.Size(), info.ModTime(), info
		}
		p := filepath.Join(dir, name)
		if e.Type()&fs.ModeSymlink != 0 {
			s.target, _ = fsys.EvalSymlinks(p)
		}
		if pl.loaded[p] && s.mode.IsRegular() {
			if f, err := r.Open(name); err == nil {
				data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
				_ = f.Close()
				if err == nil {
					s.sum = sha256Hex(data)
				}
			}
		}
		out.entries[name] = s
	}
	return out
}

// same reports whether two scans of a directory match.
func (a dirScan) same(fsys FileSystem, b dirScan) bool {
	if a.state != b.state || len(a.entries) != len(b.entries) {
		return false
	}
	for name, x := range a.entries {
		y, ok := b.entries[name]
		if !ok || x.typ != y.typ || x.mode != y.mode || x.size != y.size || !x.mtime.Equal(y.mtime) || x.target != y.target || x.sum != y.sum {
			return false
		}
		if x.info != nil && y.info != nil && !fsys.SameFile(x.info, y.info) {
			return false
		}
	}
	return true
}
