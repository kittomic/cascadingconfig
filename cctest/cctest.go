package cctest

import (
	"io/fs"
	"testing"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/internal/golden"
	"github.com/kittomic/cascadingconfig/internal/hooks"
	"github.com/kittomic/cascadingconfig/internal/memfs"
)

// FS is an in-memory filesystem implementing cascadingconfig.FileSystem, with directories, files, symlinks, hard
// links, FIFOs and permissions. Paths are absolute. It is safe for concurrent use. Pass it to a loader with
// cascadingconfig.WithFileSystem.
type FS struct {
	m *memfs.FS
}

var _ cascadingconfig.FileSystem = (*FS)(nil)

// NewFS returns a filesystem holding files, keyed by absolute path, creating their directories as needed.
func NewFS(files map[string]string) *FS { return &FS{m: memfs.New(files)} }

// WriteFile creates or replaces a regular file, creating its directories as needed.
func (f *FS) WriteFile(path string, data []byte, perm fs.FileMode) { f.m.WriteFile(path, data, perm) }

// Mkdir creates a directory and its parents.
func (f *FS) Mkdir(path string, perm fs.FileMode) { f.m.Mkdir(path, perm) }

// Symlink creates a symlink at link pointing to target, which may be relative to link's directory.
func (f *FS) Symlink(target, link string) { f.m.Symlink(target, link) }

// Link creates a hard link: newPath becomes another name for the file at oldPath.
func (f *FS) Link(oldPath, newPath string) { f.m.Link(oldPath, newPath) }

// Mkfifo creates a named pipe.
func (f *FS) Mkfifo(path string) { f.m.Mkfifo(path) }

// Chmod changes a file's or directory's permission bits. A file without read permission can't be read.
func (f *FS) Chmod(path string, perm fs.FileMode) { f.m.Chmod(path, perm) }

// Remove removes a file, symlink or directory and everything below it.
func (f *FS) Remove(path string) { f.m.Remove(path) }

// OpenRoot implements cascadingconfig.FileSystem.
func (f *FS) OpenRoot(dir string) (cascadingconfig.Root, error) {
	r, err := f.m.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// EvalSymlinks implements cascadingconfig.FileSystem.
func (f *FS) EvalSymlinks(path string) (string, error) { return f.m.EvalSymlinks(path) }

// SameFile implements cascadingconfig.FileSystem.
func (f *FS) SameFile(a, b fs.FileInfo) bool { return f.m.SameFile(a, b) }

// Host is a fake host: env vars, home dir, current dir, GOOS, GOARCH and the current user's UID. An empty Home means
// the host has no home directory. GOOS picks the defaults that depend on the OS; paths are still joined with the real
// OS's rules.
type Host struct {
	GOOS, GOARCH string
	Env          map[string]string
	Home, Cwd    string
	UID          int
}

// WithHost returns an option that makes a loader read everything about the host from h, never from the process.
func WithHost(h Host) cascadingconfig.Option {
	return hooks.WithHost(hooks.Host(h)).(cascadingconfig.Option)
}

// Golden compares got with the file testdata/file, and rewrites the file instead when the test runs with -update.
func Golden(t testing.TB, file string, got []byte) {
	t.Helper()
	golden.Check(t, file, got)
}
