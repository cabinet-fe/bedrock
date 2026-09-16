//go:build windows

package harness

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// stopProcess terminates a leftover listener. Kill is the only cross-process
// signal; on a process that already exited it fails, which is fine. The
// caller waits for the port to free instead of watching the pid.
func stopProcess(proc *os.Process) {
	_ = proc.Kill()
}

// listenersOnPort reports the PIDs of processes listening on port via
// netstat (`TCP 127.0.0.1:4096 ... LISTENING <pid>`).
func listenersOnPort(port int) ([]int, error) {
	var buf strings.Builder
	cmd := exec.Command("netstat", "-ano", "-p", "TCP")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	want := ":" + strconv.Itoa(port) + " "
	seen := make(map[int]bool)
	var pids []int
	for _, line := range strings.Split(buf.String(), "\n") {
		// listening lines end with the pid; local address carries the port
		if !strings.Contains(strings.ToUpper(line), "LISTENING") || !strings.Contains(line, want) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if pid, err := strconv.Atoi(fields[len(fields)-1]); err == nil && !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}
	if len(pids) == 0 {
		return nil, errors.New("netstat reported no listener")
	}
	return pids, nil
}
