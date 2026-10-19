// Package memfs is an in-memory filesystem with directories, files, symlinks, hard links, FIFOs and permissions. It
// backs cctest.FS and the core's own tests. Paths are absolute, in the real OS's form.
package memfs

import (
	"io/fs"
)

// FS is an in-memory filesystem. It is safe for concurrent use.
type FS struct{}

// New returns a filesystem holding files, keyed by absolute path, creating their directories as needed.
func New(files map[string]string) *FS {
	_ = files // stub
	return &FS{}
}

// WriteFile creates or replaces a regular file, creating its directories as needed.
func (f *FS) WriteFile(path string, data []byte, perm fs.FileMode) { _, _, _ = path, data, perm }

// Mkdir creates a directory and its parents.
func (f *FS) Mkdir(path string, perm fs.FileMode) { _, _ = path, perm }

// Symlink creates a symlink at link pointing to target, which may be relative to link's directory.
func (f *FS) Symlink(target, link string) { _, _ = target, link }

// Link creates a hard link: newPath becomes another name for the file at oldPath.
func (f *FS) Link(oldPath, newPath string) { _, _ = oldPath, newPath }

// Mkfifo creates a named pipe.
func (f *FS) Mkfifo(path string) { _ = path }

// Chmod changes a file's or directory's permission bits.
func (f *FS) Chmod(path string, perm fs.FileMode) { _, _ = path, perm }

// Remove removes a file, symlink or directory and everything below it.
func (f *FS) Remove(path string) { _ = path }

// OpenRoot opens dir as a root. Names inside it are slash-separated and relative, as io/fs requires, and a symlink
// that leaves the root can't be followed through it, as with os.Root.
func (f *FS) OpenRoot(dir string) (*Root, error) {
	_ = dir // stub
	return nil, fs.ErrNotExist
}

// EvalSymlinks returns path with every symlink resolved.
func (f *FS) EvalSymlinks(path string) (string, error) {
	_ = path // stub
	return "", fs.ErrNotExist
}

// SameFile reports whether a and b, from this filesystem, describe the same file.
func (f *FS) SameFile(a, b fs.FileInfo) bool {
	_, _ = a, b // stub
	return false
}

// Root is a directory opened with OpenRoot.
type Root struct{}

// Open opens a file in the root. Opening a FIFO doesn't block.
func (r *Root) Open(name string) (fs.File, error) {
	_ = name // stub
	return nil, fs.ErrNotExist
}

// Stat returns a file's information, following symlinks inside the root.
func (r *Root) Stat(name string) (fs.FileInfo, error) {
	_ = name // stub
	return nil, fs.ErrNotExist
}

// ReadDir lists a directory, without following symlinks, sorted by name.
func (r *Root) ReadDir(name string) ([]fs.DirEntry, error) {
	_ = name // stub
	return nil, fs.ErrNotExist
}

// Close closes the root.
func (r *Root) Close() error { return nil }
