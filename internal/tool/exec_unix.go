//go:build unix

package tool

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// killGrace is how long Wait blocks after the context is cancelled and the
// process group has been signalled, before it gives up on draining the output
// pipe. Without this a child that outlived the kill and still holds the pipe
// open would wedge Wait -- and the whole agent turn -- forever.
const killGrace = 3 * time.Second

// hardenCmd makes a context-cancelled command actually die. It runs the command
// in its own process group so a single kill reaches the shell AND everything it
// spawned (a `cd x && npm test` that starts a watcher, a pipeline, a background
// server), and it bounds the post-kill wait so a lingering grandchild cannot
// hang the caller. Call it after exec.CommandContext, before Run/Start/Output.
func hardenCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid targets the whole process group led by the child.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.WaitDelay = killGrace
}
