package bedctl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bedrock/internal/pkg"
)

const systemdDir = "/etc/systemd/system"

func systemdAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}

type systemdManager struct{}

func (*systemdManager) Kind() string { return "systemd" }

// loginPath captures the PATH an interactive login shell would have so
// user-level tools (bun, cargo, mise...) stay visible to build scripts. The
// probe is time-boxed and detached from the caller's terminal; on any
// failure the current PATH (plus known tool dirs) is used instead.
func loginPath() string {
	probes := []string{os.Getenv("SHELL"), "/bin/bash"}
	for _, shell := range probes {
		if shell == "" {
			continue
		}
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "echo $PATH")
		cmd.Stdin = nil
		out, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		lines := splitLines(string(out))
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			return strings.TrimSpace(lines[len(lines)-1])
		}
	}
	return ""
}

// servicePathEnv composes the PATH for the unit: login-shell PATH when the
// probe succeeds, always extended with the known per-user tool dirs.
func servicePathEnv() string {
	home := pkg.ResolveHomeDir()
	path := loginPath()
	if path == "" {
		path = os.Getenv("PATH")
	}
	env := map[string]string{"PATH": path}
	pkg.EnsurePATH(env, home)
	return env["PATH"]
}

func (s *systemdManager) unitPath(comp Component) string {
	return filepath.Join(systemdDir, comp.ServiceName()+".service")
}

// Install renders the unit file, mirroring install.sh's template, and
// reloads/enables systemd with bounded calls.
func (s *systemdManager) Install(comp Component, dir string) error {
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.unitPath(comp), []byte(UnitContent(comp, dir)), 0o644); err != nil {
		return err
	}
	ctx := context.Background()
	if out, err := runCmd(ctx, 15*time.Second, "systemctl", "daemon-reload"); err != nil {
		Warn("systemd daemon-reload 超时或失败（%v），重启服务前请手动执行: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCmd(ctx, 15*time.Second, "systemctl", "enable", comp.ServiceName()+".service"); err != nil {
		Warn("设置开机自启失败: %s（%s）", comp.ServiceName(), strings.TrimSpace(out))
	}
	return nil
}

// UnitContent renders the systemd unit for a component.
func UnitContent(comp Component, dir string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Bedrock %s (installed by bedctl)\n", comp)
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", dir)
	if home := pkg.ResolveHomeDir(); home != "" {
		fmt.Fprintf(&b, "Environment=\"HOME=%s\"\n", home)
	}
	fmt.Fprintf(&b, "Environment=\"PATH=%s\"\n", servicePathEnv())
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(ExecArgs(comp, dir), " "))
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=3\n")
	fmt.Fprintf(&b, "TimeoutStopSec=%d\n\n", comp.StopTimeoutSec())
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	return b.String()
}

func (s *systemdManager) IsActive(comp Component, dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", comp.ServiceName()+".service").Run() == nil
}

// IsDead reads ActiveState/SubState: failed/inactive means exited, and
// activating/auto-restart means Restart=on-failure is crash-looping.
func (s *systemdManager) IsDead(comp Component, dir string) bool {
	unit := comp.ServiceName() + ".service"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state := strings.TrimSpace(systemctlShow(ctx, "ActiveState", unit))
	sub := strings.TrimSpace(systemctlShow(ctx, "SubState", unit))
	switch {
	case state == "failed" || state == "inactive":
		return true
	case state == "activating" && sub == "auto-restart":
		return true
	}
	return false
}

func systemctlShow(ctx context.Context, prop, unit string) string {
	cmd := exec.CommandContext(ctx, "systemctl", "show", "-p", prop, "--value", unit)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// Stop implements the real graceful shutdown the shell version promised:
// unit stop with a deadline, wait for inactive, SIGKILL stragglers, then a
// port-free verification that also reaps orphan processes.
func (s *systemdManager) Stop(ctx context.Context, comp Component, dir string, port uint16) error {
	name := comp.ServiceName()
	unit := name + ".service"
	budget := time.Duration(comp.StopTimeoutSec()) * time.Second
	if !s.IsActive(comp, dir) {
		Info("%s: 未在运行", name)
	} else {
		Info("停止 %s（优雅停机，最长等待 %ds）...", name, int(budget.Seconds()))
		// systemd itself escalates to SIGKILL at TimeoutStopSec; the context
		// deadline only guards against a wedged systemd.
		if _, err := runCmd(ctx, budget+10*time.Second, "systemctl", "stop", unit); err != nil {
			Warn("systemctl stop 未按预期返回: %v", err)
		}
		deadline := time.Now().Add(budget + 5*time.Second)
		for s.IsActive(comp, dir) && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
		if s.IsActive(comp, dir) {
			_, _ = runCmd(ctx, 10*time.Second, "systemctl", "kill", "--signal=SIGKILL", unit)
			time.Sleep(2 * time.Second)
		}
		if s.IsActive(comp, dir) {
			return fmt.Errorf("%s 停止失败：unit 在超时后仍为 active，请检查 systemctl status %s", name, unit)
		}
		Info("%s: 已停止", name)
	}
	return ensurePortFree(comp, dir, port)
}

func (s *systemdManager) Start(comp Component, dir string) error {
	ctx := context.Background()
	if _, err := runCmd(ctx, time.Duration(comp.StopTimeoutSec())*time.Second+10*time.Second,
		"systemctl", "start", comp.ServiceName()+".service"); err != nil {
		return fmt.Errorf("systemctl start %s 失败: %w", comp.ServiceName(), err)
	}
	return nil
}
