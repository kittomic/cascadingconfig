// Package memfs is an in-memory filesystem with directories, files, symlinks, hard links, FIFOs and permissions. It
// backs cctest.FS and the core's own tests. Paths are absolute, in the resolved OS's form.
package memfs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// FS is an in-memory filesystem. It is safe for concurrent use.
type FS struct {
	mu   sync.RWMutex
	root *node
}

// node is a file, directory, symlink or FIFO. Hard links share one node.
type node struct {
	mode     fs.FileMode // type bits and permissions
	data     []byte      // a regular file's contents
	target   string      // a symlink's target, as written
	children map[string]*node
	modTime  time.Time
	owner    int  // the owner's uid, when hasOwner is set
	hasOwner bool // Chown gave it an owner
}

func newDir(perm fs.FileMode) *node {
	return &node{mode: fs.ModeDir | perm, children: map[string]*node{}, modTime: time.Now()}
}

// New returns a filesystem holding files, keyed by absolute path, creating their directories as needed.
func New(files map[string]string) *FS {
	f := &FS{root: newDir(0o755)}
	for p, data := range files {
		f.WriteFile(p, []byte(data), 0o644)
	}
	return f
}

// segments splits an absolute path into its names. It panics on a relative path: the mutating methods are for tests.
func segments(p string) []string {
	if !filepath.IsAbs(p) {
		panic("memfs: not an absolute path: " + p)
	}
	return split(filepath.ToSlash(filepath.Clean(p)))
}

func split(slashPath string) []string {
	var out []string
	for _, s := range strings.Split(slashPath, "/") {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	return out
}

// parent returns the directory that holds the last of segs, creating directories as needed, without following
// symlinks.
func (f *FS) parent(segs []string) *node {
	dir := f.root
	for _, s := range segs[:len(segs)-1] {
		next, ok := dir.children[s]
		if !ok || !next.mode.IsDir() {
			next = newDir(0o755)
			dir.children[s] = next
		}
		dir = next
	}
	return dir
}

func (f *FS) put(p string, n *node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segs := segments(p)
	if len(segs) == 0 {
		panic("memfs: can't replace the root")
	}
	f.parent(segs).children[segs[len(segs)-1]] = n
}

// WriteFile creates or replaces a regular file, creating its directories as needed. Writing to a hard-linked file
// changes every name for it.
func (f *FS) WriteFile(p string, data []byte, perm fs.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segs := segments(p)
	dir := f.parent(segs)
	name := segs[len(segs)-1]
	if n, ok := dir.children[name]; ok && n.mode.IsRegular() {
		n.data = slices.Clone(data)
		n.mode = perm
		n.modTime = time.Now()
		return
	}
	dir.children[name] = &node{mode: perm, data: slices.Clone(data), modTime: time.Now()}
}

// Mkdir creates a directory and its parents.
func (f *FS) Mkdir(p string, perm fs.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segs := segments(p)
	if len(segs) == 0 {
		return
	}
	dir := f.parent(segs)
	if n, ok := dir.children[segs[len(segs)-1]]; ok && n.mode.IsDir() {
		n.mode = fs.ModeDir | perm
		return
	}
	dir.children[segs[len(segs)-1]] = newDir(perm)
}

// Symlink creates a symlink at link pointing to target, which may be relative to link's directory.
func (f *FS) Symlink(target, link string) {
	f.put(link, &node{mode: fs.ModeSymlink | 0o777, target: target, modTime: time.Now()})
}

// Link creates a hard link: newPath becomes another name for the file at oldPath.
func (f *FS) Link(oldPath, newPath string) {
	f.mu.RLock()
	n, _, err := f.resolve(segments(oldPath), false)
	f.mu.RUnlock()
	if err != nil {
		panic("memfs: " + err.Error())
	}
	f.put(newPath, n)
}

// Mkfifo creates a named pipe.
func (f *FS) Mkfifo(p string) {
	f.put(p, &node{mode: fs.ModeNamedPipe | 0o644, modTime: time.Now()})
}

// Chmod changes a file's or directory's permission bits, and its sticky, setuid and setgid bits, without following a
// symlink. A file without owner read permission can't be opened, and a directory without it can't be listed.
func (f *FS) Chmod(p string, perm fs.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _, err := f.resolve(segments(p), false)
	if err != nil {
		panic("memfs: " + err.Error())
	}
	n.mode = n.mode.Type() | perm&(fs.ModePerm|fs.ModeSticky|fs.ModeSetuid|fs.ModeSetgid)
}

// Chown gives a file or directory an owner, without following a symlink.
func (f *FS) Chown(p string, uid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _, err := f.resolve(segments(p), false)
	if err != nil {
		panic("memfs: " + err.Error())
	}
	n.owner, n.hasOwner = uid, true
}

// Remove removes a file, symlink or directory and everything below it. Removing nothing is a no-op.
func (f *FS) Remove(p string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segs := segments(p)
	if len(segs) == 0 {
		return
	}
	dir := f.root
	for _, s := range segs[:len(segs)-1] {
		next, ok := dir.children[s]
		if !ok || !next.mode.IsDir() {
			return
		}
		dir = next
	}
	delete(dir.children, segs[len(segs)-1])
}

var errLoop = errors.New("too many levels of symbolic links")

// resolve finds the node at segs from the root, following symlinks, including the last one when followLast is set.
// It returns the node and its resolved path.
func (f *FS) resolve(segs []string, followLast bool) (*node, []string, error) {
	return f.resolveFrom(nil, segs, followLast, 0)
}

func (f *FS) resolveFrom(done, rest []string, followLast bool, links int) (*node, []string, error) {
	n := f.root
	resolved := []string{}
	walk := append(slices.Clone(done), rest...)
	for i, s := range walk {
		if s == ".." {
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
			n = f.lookup(resolved)
			continue
		}
		if !n.mode.IsDir() {
			return nil, nil, fs.ErrNotExist
		}
		child, ok := n.children[s]
		if !ok {
			return nil, nil, fs.ErrNotExist
		}
		last := i == len(walk)-1
		if child.mode&fs.ModeSymlink != 0 && (!last || followLast) {
			if links >= 40 {
				return nil, nil, errLoop
			}
			target := filepath.ToSlash(child.target)
			var next []string
			if strings.HasPrefix(target, "/") {
				next = split(target)
			} else {
				next = append(slices.Clone(resolved), split(target)...)
			}
			return f.resolveFrom(nil, append(next, walk[i+1:]...), followLast, links+1)
		}
		n = child
		resolved = append(resolved, s)
	}
	return n, resolved, nil
}

// lookup returns the node at resolved, a path already free of symlinks.
func (f *FS) lookup(resolved []string) *node {
	n := f.root
	for _, s := range resolved {
		n = n.children[s]
	}
	return n
}

func toPath(resolved []string) string {
	return filepath.FromSlash("/" + strings.Join(resolved, "/"))
}

// OpenRoot opens dir, following symlinks, as a root. Names inside it are slash-separated and relative, as io/fs
// requires, and a symlink that leaves the root can't be followed through it, as with os.Root.
func (f *FS) OpenRoot(dir string) (*Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n, resolved, err := f.resolve(segments(dir), true)
	if err != nil {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: err}
	}
	if !n.mode.IsDir() {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: errors.New("not a directory")}
	}
	return &Root{fsys: f, resolved: resolved}, nil
}

// EvalSymlinks returns path with every symlink resolved.
func (f *FS) EvalSymlinks(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", &fs.PathError{Op: "evalsymlinks", Path: p, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, resolved, err := f.resolve(segments(p), true)
	if err != nil {
		return "", &fs.PathError{Op: "evalsymlinks", Path: p, Err: err}
	}
	return toPath(resolved), nil
}

// SameFile reports whether a and b, from this filesystem, describe the same file.
func (f *FS) SameFile(a, b fs.FileInfo) bool {
	na, ok1 := a.Sys().(*node)
	nb, ok2 := b.Sys().(*node)
	return ok1 && ok2 && na == nb
}

// Root is a directory opened with OpenRoot.
type Root struct {
	fsys     *FS
	resolved []string
}

// find resolves name inside the root, following symlinks only while they stay inside it.
func (r *Root) find(op, name string, followLast bool) (*node, string, error) {
	if !fs.ValidPath(name) {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	r.fsys.mu.RLock()
	defer r.fsys.mu.RUnlock()
	n, resolved, err := r.fsys.resolveFrom(r.resolved, split(name), followLast, 0)
	if err != nil {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: err}
	}
	if len(resolved) < len(r.resolved) || !slices.Equal(resolved[:len(r.resolved)], r.resolved) {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: errors.New("path escapes from parent")}
	}
	return n, path.Base("/" + strings.Join(resolved, "/")), nil
}

// Open opens a file or directory in the root. Opening a FIFO doesn't block: it reads as empty.
func (r *Root) Open(name string) (fs.File, error) {
	n, base, err := r.find("open", name, true)
	if err != nil {
		return nil, err
	}
	r.fsys.mu.RLock()
	defer r.fsys.mu.RUnlock()
	if n.mode.Perm()&0o400 == 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	f := &file{info: info(base, n)}
	if n.mode.IsDir() {
		f.entries = entries(n)
	} else {
		f.r = bytes.NewReader(slices.Clone(n.data))
	}
	return f, nil
}

// Stat returns a file's information, following symlinks inside the root.
func (r *Root) Stat(name string) (fs.FileInfo, error) {
	n, base, err := r.find("stat", name, true)
	if err != nil {
		return nil, err
	}
	r.fsys.mu.RLock()
	defer r.fsys.mu.RUnlock()
	return info(base, n), nil
}

// ReadDir lists a directory, sorted by name. Entries report their own type: a symlink isn't followed.
func (r *Root) ReadDir(name string) ([]fs.DirEntry, error) {
	n, _, err := r.find("readdir", name, true)
	if err != nil {
		return nil, err
	}
	r.fsys.mu.RLock()
	defer r.fsys.mu.RUnlock()
	if !n.mode.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}
	if n.mode.Perm()&0o400 == 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrPermission}
	}
	return entries(n), nil
}

// Close closes the root.
func (r *Root) Close() error { return nil }

func entries(dir *node) []fs.DirEntry {
	out := make([]fs.DirEntry, 0, len(dir.children))
	for name, c := range dir.children {
		out = append(out, fs.FileInfoToDirEntry(info(name, c)))
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

// fileInfo is a snapshot of a node's metadata. Sys returns the node, which SameFile compares.
type fileInfo struct {
	name     string
	size     int64
	mode     fs.FileMode
	modTime  time.Time
	owner    int
	hasOwner bool
	n        *node
}

func info(name string, n *node) fileInfo {
	return fileInfo{
		name: name, size: int64(len(n.data)), mode: n.mode, modTime: n.modTime, owner: n.owner, hasOwner: n.hasOwner, n: n,
	}
}

// Owner returns the uid Chown gave the node, or false when it has none, as cascadingconfig.OwnerInfo asks.
func (i fileInfo) Owner() (uid int, ok bool) { return i.owner, i.hasOwner }

func (i fileInfo) Name() string       { return i.name }
func (i fileInfo) Size() int64        { return i.size }
func (i fileInfo) Mode() fs.FileMode  { return i.mode }
func (i fileInfo) ModTime() time.Time { return i.modTime }
func (i fileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fileInfo) Sys() any           { return i.n }

// file is an open file or directory.
type file struct {
	info    fileInfo
	r       *bytes.Reader
	entries []fs.DirEntry
}

func (f *file) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *file) Close() error               { return nil }

func (f *file) Read(b []byte) (int, error) {
	if f.r == nil {
		return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: errors.New("is a directory")}
	}
	return f.r.Read(b)
}

func (f *file) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.r != nil {
		return nil, &fs.PathError{Op: "readdir", Path: f.info.name, Err: errors.New("not a directory")}
	}
	if n <= 0 {
		out := f.entries
		f.entries = nil
		return out, nil
	}
	if len(f.entries) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(f.entries))
	out := f.entries[:n]
	f.entries = f.entries[n:]
	return out, nil
}
