//go:build !unix

package tool

import (
	"os/exec"
	"time"
)

// killGrace bounds the post-cancel wait (see the unix build for the rationale).
const killGrace = 3 * time.Second

// hardenCmd on non-unix platforms only bounds the wait after a cancel: killing a
// whole process tree needs platform-specific job objects that are out of scope
// here (and `bash -c` is a unix shape to begin with).
func hardenCmd(cmd *exec.Cmd) {
	cmd.WaitDelay = killGrace
}
