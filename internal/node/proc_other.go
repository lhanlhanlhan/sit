//go:build !unix

package node

import "os/exec"

// configureProcGroup is a no-op on non-Unix platforms.
func configureProcGroup(cmd *exec.Cmd) {}

// killGroup falls back to killing just the process.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
