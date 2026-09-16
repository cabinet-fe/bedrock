//go:build !windows

package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// stopProcess terminates a leftover listener, escalating to SIGKILL after
// the grace period. The process may be a foreign one we did not spawn; it
// could be dead already (its pid then belongs to someone else, so the
// signals are best-effort and errors ignored).
func stopProcess(proc *os.Process) {
	if err := proc.Signal(syscall.SIGTERM); err == nil {
		waitProcessExit(proc, 5*time.Second)
	}
	_ = proc.Kill()
	waitProcessExit(proc, 5*time.Second)
}

// listenersOnPort reports the PIDs of processes listening on port. A port
// may carry several pids when a new server duplicated the socket fd before
// the old one exited (SO_REUSEPORT-style handoff). lsof lives in /usr/sbin
// on macOS, which is not always on PATH.
func listenersOnPort(port int) ([]int, error) {
	for _, bin := range []string{"/usr/sbin/lsof", "lsof", "/usr/bin/netstat", "netstat"} {
		switch {
		case strings.Contains(bin, "lsof"):
			if pids, err := lsofPids(bin, port); err == nil {
				return pids, nil
			}
		default:
			if pids, err := netstatPids(bin, port); err == nil {
				return pids, nil
			}
		}
	}
	return nil, errors.New("no lsof or netstat available")
}

func lsofPids(bin string, port int) ([]int, error) {
	var buf strings.Builder
	cmd := exec.Command(bin, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", bin, err)
	}
	// -Fp prints one `p<pid>` field per process, no header.
	seen := make(map[int]bool)
	var pids []int
	for _, f := range strings.Fields(buf.String()) {
		if len(f) > 1 && f[0] == 'p' {
			if pid, err := strconv.Atoi(f[1:]); err == nil && !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	return pids, nil
}

// netstatPids is the fallback where lsof is absent (some Linux images):
// `netstat -tlnp` prints `tcp 0 0 127.0.0.1:4096 ... LISTEN <pid>/<name>`.
func netstatPids(bin string, port int) ([]int, error) {
	var buf strings.Builder
	cmd := exec.Command(bin, "-tlnp")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", bin, err)
	}
	want := ":" + strconv.Itoa(port) + " "
	seen := make(map[int]bool)
	var pids []int
	for _, line := range strings.Split(buf.String(), "\n") {
		// pid is the token right after LISTEN, formatted <pid>/<name>.
		if !strings.Contains(line, want) || !strings.Contains(line, "LISTEN") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "LISTEN" && i+1 < len(fields) {
				pidStr, _, _ := strings.Cut(fields[i+1], "/")
				if pid, err := strconv.Atoi(pidStr); err == nil && !seen[pid] {
					seen[pid] = true
					pids = append(pids, pid)
				}
				break
			}
		}
	}
	return pids, nil
}

// waitProcessExit polls the process until it is gone. It observes only
// processes we did not spawn: signal 0 succeeds while the pid exists (the
// exit status itself is owned by the parent or init).
func waitProcessExit(proc *os.Process, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return // process gone (or reaped by init)
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
