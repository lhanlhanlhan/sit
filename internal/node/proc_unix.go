//go:build unix

package node

import (
	"os/exec"
	"syscall"
)

// configureProcGroup puts the child in its own process group so the whole tree
// can be signalled on timeout (protects embedded-device memory).
func configureProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup sends SIGKILL to the child's entire process group.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid targets the process group led by the child.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
