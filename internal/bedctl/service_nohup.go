package bedctl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bedrock/internal/pkg"
)

type nohupManager struct{}

func (*nohupManager) Kind() string { return "nohup" }

// Install is a no-op: without root there is no service manager to register
// with; processes are supervised via pidfile only.
func (*nohupManager) Install(Component, string) error { return nil }

// readPidfile returns the recorded PID; 0 when absent or unparsable.
func readPidfile(comp Component, dir string) int {
	data, err := os.ReadFile(PIDFile(comp, dir))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// pidOwnsBinary checks /proc/<pid>/cmdline so a recycled PID is never
// mistaken for our service (the shell version trusted kill -0 alone).
func pidOwnsBinary(pid int, binPath string) bool {
	cmdline := PIDCmdLine(pid)
	if cmdline == "" {
		// Non-Linux (no /proc): fall back to existence only.
		return ProcessAlive(pid)
	}
	args := strings.Fields(cmdline)
	want := filepath.Clean(binPath)
	return len(args) > 0 && (args[0] == binPath || filepath.Clean(args[0]) == want)
}

func (m *nohupManager) IsActive(comp Component, dir string) bool {
	pid := readPidfile(comp, dir)
	bin := BinaryPath(comp, dir)
	if pid != 0 && ProcessAlive(pid) && pidOwnsBinary(pid, bin) {
		return true
	}
	// pidfile lost or stale: the real process may still be alive.
	return len(PIDsRunningBinary(bin)) > 0
}

func (m *nohupManager) IsDead(comp Component, dir string) bool {
	return !m.IsActive(comp, dir)
}

// Stop terminates the component wherever it actually is: recorded pidfile
// PID (identity-checked) plus any process running the binary, graceful
// SIGTERM first, SIGKILL after the stop budget, then the port-free check.
func (m *nohupManager) Stop(ctx context.Context, comp Component, dir string, port uint16) error {
	name := comp.ServiceName()
	bin := BinaryPath(comp, dir)
	seen := map[int]bool{}
	var pids []int
	add := func(list []int) {
		for _, pid := range list {
			if !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	if pid := readPidfile(comp, dir); pid != 0 && ProcessAlive(pid) {
		add([]int{pid})
	}
	add(PIDsRunningBinary(bin))

	if len(pids) == 0 {
		Info("%s: 未在运行", name)
		_ = os.Remove(PIDFile(comp, dir))
		return ensurePortFree(comp, dir, port)
	}

	Info("停止 %s（优雅停机，最长等待 %ds）...", name, comp.StopTimeoutSec())
	grace := time.Duration(comp.StopTimeoutSec()) * time.Second
	terminatePIDs(pids, grace)
	_ = os.Remove(PIDFile(comp, dir))
	if anyAlive(pids) {
		return fmt.Errorf("%s 停止失败：进程 %v 仍存活，请手动检查", name, pids)
	}
	Info("%s: 已停止", name)
	return ensurePortFree(comp, dir, port)
}

// Start launches the binary detached (own session, like nohup), logging to
// the install dir and recording the PID. PATH is extended with the known
// tool dirs so build scripts find bun/cargo/mise without a login shell.
func (m *nohupManager) Start(comp Component, dir string) error {
	bin := BinaryPath(comp, dir)
	args := ExecArgs(comp, dir)[1:]
	log := LogFile(comp, dir)
	lf, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("无法打开日志文件 %s: %w", log, err)
	}
	defer lf.Close()

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	home := pkg.ResolveHomeDir()
	env := map[string]string{"PATH": os.Getenv("PATH")}
	pkg.EnsurePATH(env, home)
	cmd.Env = append(os.Environ(), "PATH="+env["PATH"])
	if os.Getenv("HOME") == "" && home != "" {
		cmd.Env = append(cmd.Env, "HOME="+home)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", bin, err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(PIDFile(comp, dir), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		Warn("无法写入 pidfile %s: %v", PIDFile(comp, dir), err)
	}
	go func() { _ = cmd.Wait() }() // reap so the child does not stay a zombie
	return nil
}
