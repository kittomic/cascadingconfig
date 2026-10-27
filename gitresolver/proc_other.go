//go:build !unix

package gitresolver

import "os/exec"

// killGroup leaves cmd alone: a timeout kills git, and WaitDelay stops waiting for its children.
func killGroup(*exec.Cmd) {}
