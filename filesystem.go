package cascadingconfig

import (
	"io/fs"
	"log/slog"
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

// Root is a directory opened by a FileSystem. Names are slash-separated and relative, as io/fs requires.
type Root interface {
	fs.StatFS
	fs.ReadDirFS
	Close() error
}

// WithFileSystem makes the loader read through fsys instead of the OS filesystem.
func WithFileSystem(fsys FileSystem) Option {
	_ = fsys // stub
	return optionFunc(func(*config) {})
}

// MountFS returns a FileSystem in which fsys appears at dir, an absolute path, and nothing else exists. It lets a test
// run the whole search against an fs.FS such as fstest.MapFS or embed.FS.
func MountFS(dir string, fsys fs.FS) FileSystem {
	_, _ = dir, fsys // stub
	return nil
}

// WithMaxFileSize sets the largest local file the loader reads, in bytes. A larger file is a *TooLargeError. The
// default is 10 MiB.
func WithMaxFileSize(n int64) Option {
	_ = n // stub
	return optionFunc(func(*config) {})
}

// WithLogger sets the logger that warnings go to, at Warn level. The default is slog.Default().
func WithLogger(l *slog.Logger) Option {
	_ = l // stub
	return optionFunc(func(*config) {})
}
