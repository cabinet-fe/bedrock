package bedctl

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// App carries one bedctl invocation's resolved environment and options.
type App struct {
	IsRoot      bool
	Home        string
	Interactive bool
	Prompts     *Prompts

	State  *State
	Mirror *MirrorSource

	// Options from the command line.
	MirrorFlag string
	NoMirror   bool
	DirFlag    string
	VersionTag string
	PortFlag   int
	AdminUser  string
	AdminPass  string
	Token      string
	Addr       string
	LogsN      int
	Component  string // optional server|agent selector
	Suffix     string // release asset suffix, e.g. linux-amd64

	mgr Manager
}

// Manager returns the service backend, creating it on first use.
func (a *App) Manager() Manager {
	if a.mgr == nil {
		a.mgr = NewManager()
	}
	return a.mgr
}

// DefaultDir resolves the install dir for a component: --dir when given,
// then the remembered dir, then the platform default.
func (a *App) DefaultDir(comp Component) string {
	if a.DirFlag != "" {
		return a.DirFlag
	}
	return DefaultDir(a.State, comp, a.IsRoot, a.Home)
}

// Downloader builds a downloader bound to the picked mirror state.
func (a *App) Downloader(mirror string) *Downloader {
	d := &Downloader{Base: ReleaseBase(), Mirror: mirror}
	d.OnMirrorHit = func(hit string) { a.Mirror.Remember(hit) }
	return d
}

// ResolveTag returns --version when set, otherwise the latest release tag.
func (a *App) ResolveTag(ctx context.Context, d *Downloader) (string, error) {
	if a.VersionTag != "" {
		return a.VersionTag, nil
	}
	return d.LatestTag(ctx)
}

// BinaryVersion runs `bin --version` and returns the first output line;
// "unknown" when the binary cannot be executed or does not answer within
// 5s (a hung or non-CLI binary must never wedge the updater).
func BinaryVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return "unknown"
}

// ServerPort resolves the effective server port from config, defaulting to
// 8080 with a warning when the config does not parse.
func ServerPort(dir string) uint16 {
	port, err := ReadServerPort(dir)
	if err != nil || port == 0 {
		Warn("未能从 %s/config.yaml 解析 server.port，按默认端口 8080 进行", dir)
		return 8080
	}
	return uint16(port)
}

// ServerHealthURL builds the server health endpoint for an install dir.
func ServerHealthURL(dir string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", ServerPort(dir))
}

// AgentHealthParts returns the agent health URL and bearer token.
func AgentHealthParts(dir string) (url, bearer string) {
	addr, _ := ReadAgentAddr(dir)
	port := 9091
	if p, err := AgentPort(addr); err == nil {
		port = int(p)
	}
	token, _ := ReadAgentToken(dir)
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", port), token
}

// HealthParts returns url+bearer for a component, used by status/doctor/manage.
func HealthParts(comp Component, dir string) (string, string) {
	if comp == Agent {
		return AgentHealthParts(dir)
	}
	return ServerHealthURL(dir), ""
}

// printRecentLogs prints the last n log lines for a component.
func printRecentLogs(a *App, comp Component, dir string, n int) {
	fmt.Println()
	name := comp.ServiceName()
	if a.Manager().Kind() == "systemd" {
		Info("最近 %d 行日志（journalctl -u %s）:", n, name)
		out, err := runCmd(context.Background(), 10*time.Second, "journalctl",
			"-u", name+".service", "-n", strconv.Itoa(n), "--no-pager", "--quiet")
		if err != nil {
			Warn("无法读取 journal，请手动执行: journalctl -u %s -n %d --no-pager", name, n)
			return
		}
		fmt.Print(out)
		return
	}
	f := LogFile(comp, dir)
	lines := tailLines(f, n)
	if lines == nil {
		Warn("未找到日志文件: %s", f)
		return
	}
	Info("最近 %d 行日志（%s）:", n, f)
	for _, line := range lines {
		fmt.Println(line)
	}
}

// ProbeURL is the HTTP status returned by a GET with optional bearer (0 =
// unreachable).
func ProbeURL(url, bearer string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
