//go:build windows

package service

import "os/exec"

func configureAgentCmdProc(cmd *exec.Cmd) {
	// Windows has no POSIX process groups; terminate the root process.
}

func killAgentCmdProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
