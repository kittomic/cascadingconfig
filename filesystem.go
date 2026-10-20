package cascadingconfig

import (
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// FileSystem carries every read: the main search, the bootstrap search and the file resolver. The default reads the
// OS filesystem; WithFileSystem replaces it.
type FileSystem interface {
	// OpenRoot opens dir, an absolute path, as a root confined to it, in the style of os.Root.
	OpenRoot(dir string) (Root, error)
	// EvalSymlinks returns path with every symlink resolved.
	EvalSymlinks(path string) (string, error)
	// SameFile reports whether a and b, returned by this filesystem, describe the same file.
	SameFile(a, b fs.FileInfo) bool
}

// OwnerInfo is implemented by the fs.FileInfo values a FileSystem returns, where the platform has owners, for
// FilePermissions checks (DL-191).
type OwnerInfo interface {
	// Owner returns the owner's uid, or false when it isn't known.
	Owner() (uid int, ok bool)
}

// Root is a directory opened by a FileSystem. Names are slash-separated and relative, as io/fs requires.
type Root interface {
	fs.StatFS
	fs.ReadDirFS
	Close() error
}

// WithFileSystem makes the loader read through fsys instead of the OS filesystem.
func WithFileSystem(fsys FileSystem) Option {
	return optionFunc(func(c *config) {
		if fsys == nil {
			c.fail("WithFileSystem", "a nil filesystem")
			return
		}
		c.fsys = fsys
	})
}

// WithMaxFileSize sets the largest local file the loader reads, in bytes. A larger file is a *TooLargeError. The
// default is 10 MiB.
func WithMaxFileSize(n int64) Option {
	return optionFunc(func(c *config) {
		if n <= 0 {
			c.fail("WithMaxFileSize", "the limit must be positive, not %d", n)
			return
		}
		c.maxFileSize = n
	})
}

// WithLogger sets the logger that warnings go to, at Warn level. The default is slog.Default().
func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(c *config) {
		if l == nil {
			c.fail("WithLogger", "a nil logger")
			return
		}
		c.logger = l
	})
}

const defaultMaxFileSize = 10 << 20

// osFileSystem reads the OS filesystem through os.Root.
type osFileSystem struct{}

func (osFileSystem) OpenRoot(dir string) (Root, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return osRoot{r}, nil
}

func (osFileSystem) EvalSymlinks(p string) (string, error) { return filepath.EvalSymlinks(p) }

func (osFileSystem) SameFile(a, b fs.FileInfo) bool { return os.SameFile(a, b) }

type osRoot struct{ r *os.Root }

// Open opens name without blocking, so a file swapped for a FIFO after it was checked can't hang the loader.
func (r osRoot) Open(name string) (fs.File, error) {
	return r.r.OpenFile(name, os.O_RDONLY|openNonBlock, 0)
}

func (r osRoot) Stat(name string) (fs.FileInfo, error) { return r.r.Stat(name) }

func (r osRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	f, err := r.r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, err
}

func (r osRoot) Close() error { return r.r.Close() }

// MountFS returns a FileSystem in which fsys appears at dir, an absolute path, and nothing else exists. It lets a test
// run the whole search against an fs.FS such as fstest.MapFS or embed.FS.
func MountFS(dir string, fsys fs.FS) FileSystem {
	return mountFS{dir: filepath.Clean(dir), fsys: fsys}
}

type mountFS struct {
	dir  string
	fsys fs.FS
}

// inside returns p's slash path inside the mounted fs.FS.
func (m mountFS) inside(p string) (string, bool) {
	rel, err := filepath.Rel(m.dir, filepath.Clean(p))
	if err != nil || !filepath.IsLocal(rel) && rel != "." {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (m mountFS) OpenRoot(dir string) (Root, error) {
	sub, ok := m.inside(dir)
	if !ok {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: fs.ErrNotExist}
	}
	info, err := fs.Stat(m.fsys, sub)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: fs.ErrInvalid}
	}
	return mountRoot{fsys: m.fsys, sub: sub, dir: filepath.Clean(dir)}, nil
}

func (m mountFS) EvalSymlinks(p string) (string, error) {
	sub, ok := m.inside(p)
	if !ok {
		return "", &fs.PathError{Op: "evalsymlinks", Path: p, Err: fs.ErrNotExist}
	}
	if _, err := fs.Stat(m.fsys, sub); err != nil {
		return "", err
	}
	return filepath.Clean(p), nil
}

func (m mountFS) SameFile(a, b fs.FileInfo) bool {
	ma, ok1 := a.(mountInfo)
	mb, ok2 := b.(mountInfo)
	return ok1 && ok2 && ma.full == mb.full
}

type mountRoot struct {
	fsys fs.FS
	sub  string
	dir  string
}

func (r mountRoot) path(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return path.Join(r.sub, name), nil
}

func (r mountRoot) Open(name string) (fs.File, error) {
	p, err := r.path(name)
	if err != nil {
		return nil, err
	}
	return r.fsys.Open(p)
}

func (r mountRoot) Stat(name string) (fs.FileInfo, error) {
	p, err := r.path(name)
	if err != nil {
		return nil, err
	}
	info, err := fs.Stat(r.fsys, p)
	if err != nil {
		return nil, err
	}
	return mountInfo{FileInfo: info, full: path.Join(r.sub, name)}, nil
}

func (r mountRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	p, err := r.path(name)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(r.fsys, p)
}

func (r mountRoot) Close() error { return nil }

// mountInfo carries a file's path inside the fs.FS, which is its identity there.
type mountInfo struct {
	fs.FileInfo
	full string
}
