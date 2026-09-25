package bedctl

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// Manager controls the backing service of a component: a systemd unit for
// root installs, a nohup+pidfile process otherwise.
type Manager interface {
	// Kind names the backend for display ("systemd" / "nohup").
	Kind() string
	// Install (re)writes the service definition. It is a no-op for nohup.
	Install(comp Component, dir string) error
	IsActive(comp Component, dir string) bool
	// Stop terminates the component: graceful SIGTERM first, escalation to
	// SIGKILL after the component's stop budget, then a port-free
	// verification that also cleans up orphans the manager does not know
	// about. port==0 skips the port check.
	Stop(ctx context.Context, comp Component, dir string, port uint16) error
	Start(comp Component, dir string) error
	// IsDead reports whether the process has exited or is crash-looping, so
	// readiness loops can bail out early.
	IsDead(comp Component, dir string) bool
}

// NewManager picks the service backend from the environment: systemd when
// running as root with a functioning systemd, nohup otherwise.
func NewManager() Manager {
	if IsRoot() && systemdAvailable() {
		return &systemdManager{}
	}
	return &nohupManager{}
}

// BinaryPath is the component binary inside an install dir.
func BinaryPath(comp Component, dir string) string {
	return dir + "/" + comp.BinaryName()
}

// ExecArgs are the service start arguments for a component.
func ExecArgs(comp Component, dir string) []string {
	return []string{BinaryPath(comp, dir), "--config", dir + "/" + comp.ConfigName()}
}

// PIDFile / LogFile paths for the nohup backend, same layout as install.sh.
func PIDFile(comp Component, dir string) string { return dir + "/." + comp.ServiceName() + ".pid" }
func LogFile(comp Component, dir string) string { return dir + "/" + comp.ServiceName() + ".log" }

// ensurePortFree makes sure nothing listens on port anymore. Leftover
// bedrock processes that bypass the service manager are SIGTERM'd, waited
// out for the component's stop budget, then SIGKILL'd — what the shell
// version claimed but never verified. A port held by any other program is
// an error, never killed.
func ensurePortFree(comp Component, dir string, port uint16) error {
	if port == 0 {
		return nil
	}
	listeners := PIDsListeningOnPort(port)
	if len(listeners) == 0 {
		return nil
	}
	bin := BinaryPath(comp, dir)
	ours := map[int]bool{}
	for _, pid := range PIDsRunningBinary(bin) {
		ours[pid] = true
	}
	var mine, foreign []int
	for _, pid := range listeners {
		if ours[pid] {
			mine = append(mine, pid)
		} else {
			foreign = append(foreign, pid)
		}
	}
	if len(foreign) > 0 {
		return fmt.Errorf("端口 %d 被其他程序占用（PID %v，非 bedrock 进程，不做自动清理）：请用 ss -ltnp 'sport = :%d' 确认后处理", port, foreign, port)
	}
	if len(mine) == 0 {
		// /proc unavailable (non-Linux): cannot attribute the listener.
		return fmt.Errorf("端口 %d 仍被占用，请手动处理：ss -ltnp 'sport = :%d'", port, port)
	}
	Warn("端口 %d 仍被 bedrock 残留进程占用（PID %v），尝试清理", port, mine)
	grace := time.Duration(comp.StopTimeoutSec()) * time.Second
	terminatePIDs(mine, grace)
	if len(PIDsListeningOnPort(port)) == 0 {
		return nil
	}
	return fmt.Errorf("端口 %d 在清理后仍被占用，请手动处理：ss -ltnp 'sport = :%d'", port, port)
}

// terminatePIDs sends SIGTERM to every pid, escalates to SIGKILL for the
// survivors after grace, and returns once all are gone (or after the final
// wait deadline).
func terminatePIDs(pids []int, grace time.Duration) {
	termAt := time.Now()
	for _, pid := range pids {
		if pid != os.Getpid() {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	deadline := termAt.Add(grace)
	for {
		if !anyAlive(pids) {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	for _, pid := range pids {
		if ProcessAlive(pid) && pid != os.Getpid() {
			Warn("PID %d 优雅退出超时，发送 SIGKILL", pid)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	killDeadline := time.Now().Add(5 * time.Second)
	for anyAlive(pids) && time.Now().Before(killDeadline) {
		time.Sleep(200 * time.Millisecond)
	}
}

func anyAlive(pids []int) bool {
	for _, pid := range pids {
		if ProcessAlive(pid) {
			return true
		}
	}
	return false
}

// run timeouts for external commands; a wedged systemctl must never hang the
// updater (the shell version left stop/restart unbounded).
func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// WaitHealthy polls the health endpoint for tries seconds, bailing out early
// when the process is gone or crash-looping. Returns the last HTTP status
// code (000 = no response) for diagnostics.
func WaitHealthy(mgr Manager, comp Component, dir, url, bearer string, tries int) (int, bool) {
	last := 0
	for range tries {
		client := &http.Client{Timeout: 2 * time.Second}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err == nil {
			if bearer != "" {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			if resp, err := client.Do(req); err == nil {
				last = resp.StatusCode
				resp.Body.Close()
				if last == http.StatusOK {
					return last, true
				}
			}
		}
		if mgr.IsDead(comp, dir) {
			return last, false
		}
		time.Sleep(1 * time.Second)
	}
	return last, false
}

// ReadinessHint explains a failed health check like the shell installer did.
func ReadinessHint(code int) string {
	switch code {
	case 0:
		return "端口无响应，服务可能启动即退出或未监听"
	case 401, 403:
		return fmt.Sprintf("HTTP %d，健康检查鉴权不匹配（token/配置变更？）", code)
	default:
		return fmt.Sprintf("HTTP %d，端口有响应但不是预期的服务（可能被其他程序占用）", code)
	}
}

// FreePort reports whether a TCP connect to 127.0.0.1:port fails (nothing
// listening). Used for pre-install port checks.
func FreePort(port uint16) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), time.Second)
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

// tailLines returns the last n lines of a file for failure diagnostics.
func tailLines(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := splitLines(string(data))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
