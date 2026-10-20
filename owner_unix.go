//go:build unix

package cascadingconfig

import (
	"io/fs"
	"syscall"
)

// ownerOf returns the uid that owns the file info describes.
func ownerOf(info fs.FileInfo) (int, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), true
	}
	return 0, false
}
