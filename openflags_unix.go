//go:build unix

package cascadingconfig

import "syscall"

// openNonBlock keeps opening a FIFO from blocking.
const openNonBlock = syscall.O_NONBLOCK
