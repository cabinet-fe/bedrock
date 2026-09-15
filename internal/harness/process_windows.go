//go:build windows

package harness

import (
	"os/exec"
)

// configureServeProc: Windows has no POSIX process groups; signals go to the
// root process only.
func configureServeProc(cmd *exec.Cmd) {}

// signalServeProc: Windows has no graceful cross-process signal; fall
// through to a hard kill.
func signalServeProc(cmd *exec.Cmd) {
	killServeProc(cmd)
}

// killServeProc terminates the root process.
func killServeProc(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
