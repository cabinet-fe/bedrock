//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

// configureServeProc puts serve in its own process group so a group signal
// also reaches children spawned by it.
func configureServeProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalServeProc asks the process group to terminate gracefully.
func signalServeProc(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// killServeProc force-kills the process group.
func killServeProc(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
