//go:build unix

package harness

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroup makes cancellation kill the subprocess's whole PROCESS GROUP
// (E4; audit P1: exec.CommandContext kills only the immediate child, so
// a compiler, shell, or subject that spawned children leaked them past
// the deadline). Setpgid isolates the child in its own group; Cancel
// signals the group; WaitDelay guarantees Wait returns even if the
// child holds its pipes open.
// KillGroup is the exported form for sibling adapters.
func KillGroup(cmd *exec.Cmd) { killGroup(cmd) }

func killGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
}
