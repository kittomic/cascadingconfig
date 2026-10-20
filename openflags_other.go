//go:build !unix

package cascadingconfig

// openNonBlock is zero where the OS has no non-blocking open.
const openNonBlock = 0
