//go:build !unix

package cascadingconfig

import "io/fs"

// ownerOf reports no owner where the OS has none the loader reads.
func ownerOf(fs.FileInfo) (int, bool) { return 0, false }
