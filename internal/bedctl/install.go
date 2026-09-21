package bedctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// PlatformDeps resolves the download platform; overridable via env like the
// shell installer (BEDROCK_OS / BEDROCK_ARCH, useful under emulation).
func PlatformDeps() (osName, arch, suffix string, err error) {
	osName = os.Getenv("BEDROCK_OS")
	if osName == "" {
		osName = runtime.GOOS
	}
	arch = os.Getenv("BEDROCK_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	if osName != "linux" {
		return "", "", "", fmt.Errorf("bedctl 面向 Linux 部署（当前: %s）。macOS/Windows 请参考 README 从源码构建或手动下载发布包", osName)
	}
	switch arch {
	case "amd64", "arm64":
	default:
		return "", "", "", fmt.Errorf("不支持的架构: %s（发布包仅提供 linux amd64/arm64）", arch)
	}
	return osName, arch, "linux-" + arch, nil
}

func parsePortAnswer(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("无效端口: %s", s)
	}
	return n, nil
}

// askInstallDir prompts for the install dir (platform default in
// non-interactive runs) and rejects paths with spaces.
func askInstallDir(a *App, comp Component) (string, error) {
	dir := a.DefaultDir(comp)
	if a.Interactive {
		dir = a.Prompts.Ask("安装目录", dir)
	}
	if strings.Contains(dir, " ") {
		return "", fmt.Errorf("安装目录不能包含空格: %s", dir)
	}
	return dir, nil
}

// downloadNew resolves the target release and stages the component binary in
// dir/.update, announcing the target version and any existing install.
func downloadNew(ctx context.Context, a *App, comp Component, dir string) (newBin, tag string, err error) {
	d := a.Downloader(a.Mirror.Selected())
	tag, err = a.ResolveTag(ctx, d)
	if err != nil {
		return "", "", err
	}
	Info("目标版本: %s", tag)
	bin := BinaryPath(comp, dir)
	if fileExists(bin) {
		Info("发现已有安装（%s），将保留为 %s", BinaryVersion(bin), filepath.Base(bin)+".bak")
	}
	newBin = filepath.Join(dir, ".update", comp.BinaryName()+".new")
	if err := d.DownloadVerified(ctx, comp.AssetName(a.Suffix), tag, a.Suffix, newBin); err != nil {
		return "", "", err
	}
	return newBin, tag, nil
}

// swapBin moves the staged binary over the live one, keeping the previous
// binary as <bin>.bak for rollback.
func swapBin(bin, newBin string) error {
	if fileExists(bin) {
		if err := os.Rename(bin, bin+".bak"); err != nil {
			return err
		}
	}
	if err := os.Rename(newBin, bin); err != nil {
		return err
	}
	return os.Chmod(bin, 0o755)
}

// refreshAndStart rewrites the service definition (best-effort, so the
// service env follows the current installer rather than the first-ever
// install) and starts the service.
func (a *App) refreshAndStart(comp Component, dir string) error {
	if err := a.Manager().Install(comp, dir); err != nil {
		Warn("写入服务定义失败: %v", err)
	}
	return a.Manager().Start(comp, dir)
}

// rollbackAndRecheck stops the service, restores the <bin>.bak copy,
// restarts and re-runs the health check, reporting whether the old version
// came back healthy. The stop is best-effort: the service may already be
// down after a failed health check.
func (a *App) rollbackAndRecheck(ctx context.Context, comp Component, dir, url, bearer string, tries int) (bool, error) {
	bin := BinaryPath(comp, dir)
	_ = a.Manager().Stop(ctx, comp, dir, 0)
	if err := os.Rename(bin+".bak", bin); err != nil {
		return false, err
	}
	if err := a.Manager().Start(comp, dir); err != nil {
		return false, err
	}
	_, ok := WaitHealthy(a.Manager(), comp, dir, url, bearer, tries)
	return ok, nil
}

// InstallServer installs or reinstalls Bedrock Server into dir.
func InstallServer(ctx context.Context, a *App) error {
	dir, err := askInstallDir(a, Server)
	if err != nil {
		return err
	}
	for _, sub := range []string{"data", ".update", "backups"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("无法创建目录 %s（安装到系统路径请用 sudo 运行，或用 --dir 指定可写目录）: %w", filepath.Join(dir, sub), err)
		}
	}

	// An existing config.yaml is never overwritten; its values win.
	keptConfig := fileExists(filepath.Join(dir, "config.yaml"))
	var port int
	var user, pass string
	generatedPass := false
	if keptConfig {
		var ignored []string
		if a.PortFlag != 0 {
			ignored = append(ignored, "--port")
		}
		if a.AdminUser != "" {
			ignored = append(ignored, "--admin-user")
		}
		if a.AdminPass != "" {
			ignored = append(ignored, "--admin-pass")
		}
		if len(ignored) > 0 {
			Warn("已有 config.yaml，以下选项不生效（不覆盖现有配置）: %s", strings.Join(ignored, " "))
			Warn("如需修改端口，可运行: bedctl update server --port <N> 或 bedctl port <N>")
		}
	} else {
		if a.Interactive {
			port = a.PortFlag
			if port == 0 {
				port = 8080
			}
			port, err = parsePortAnswer(a.Prompts.Ask("监听端口", strconv.Itoa(port)))
			if err != nil {
				return err
			}
			user = a.AdminUser
			if user == "" {
				user = "admin"
			}
			user = a.Prompts.Ask("超级管理员用户名", user)
		} else {
			port = a.PortFlag
			if port == 0 {
				port = 8080
			}
			user = a.AdminUser
			if user == "" {
				user = "admin"
			}
		}
		if a.AdminPass != "" {
			pass = a.AdminPass
		} else {
			pass, err = RandHex(8)
			if err != nil {
				return err
			}
			generatedPass = true
		}
	}

	newBin, tag, err := downloadNew(ctx, a, Server, dir)
	if err != nil {
		return err
	}

	if keptConfig {
		Warn("config.yaml 已存在，保留现有配置（不覆盖）")
	} else if err := GenServerConfig(dir, port, user, pass); err != nil {
		return err
	}

	var effPort uint16
	if keptConfig {
		p, err := ReadServerPort(dir)
		switch {
		case err == nil && p > 0:
			effPort = uint16(p)
			Info("监听端口（来自现有配置）: %d", effPort)
		default:
			Warn("未能从现有 config.yaml 解析 server.port，健康检查按默认端口 8080 进行")
			effPort = 8080
		}
		if host, _ := ReadServerHost(dir); isLoopback(host) {
			Warn("现有配置 server.host=%s 仅监听本机回环，外部将无法访问（改为 0.0.0.0 可对外开放）", host)
		}
	} else {
		effPort = uint16(port)
	}

	mgr := a.Manager()
	if err := mgr.Stop(ctx, Server, dir, effPort); err != nil {
		return err
	}

	Info("放置二进制...")
	bin := BinaryPath(Server, dir)
	if err := swapBin(bin, newBin); err != nil {
		return err
	}
	if err := a.refreshAndStart(Server, dir); err != nil {
		return err
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", effPort)
	Info("等待服务就绪（最长 60s，进程退出会提前报告）...")
	if code, ok := WaitHealthy(mgr, Server, dir, healthURL, "", 60); !ok {
		Err("健康检查未通过：%s", ReadinessHint(code))
		printRecentLogs(a, Server, dir, 30)
		if fileExists(bin + ".bak") {
			Warn("自动回滚到旧版本...")
			back, err := a.rollbackAndRecheck(ctx, Server, dir, healthURL, "", 30)
			if err != nil {
				return err
			}
			if back {
				Warn("已回滚到旧版本并恢复运行；失败原因见上方日志，修复后重新运行即可")
			} else {
				Err("回滚后仍无法启动，请手动检查 %s 与上方日志", bin)
			}
		} else {
			_ = mgr.Stop(ctx, Server, dir, 0)
			Err("首次安装无可回滚的旧版本。常见原因：config.yaml 缺少必填项（jwt.secret / encryption.key）、数据库初始化失败、端口冲突；修复后重新运行即可")
		}
		return fmt.Errorf("Bedrock Server 安装失败")
	}

	fmt.Println()
	Info("%s  版本: %s", Highlight("Bedrock Server 安装成功"), orVersion(BinaryVersion(bin), tag))
	fmt.Printf("  访问地址:     http://<本机IP>:%d\n", effPort)
	fmt.Printf("  安装目录:     %s\n", dir)
	fmt.Printf("  数据目录:     %s/data\n", dir)
	if keptConfig {
		fmt.Printf("  配置文件:     %s/config.yaml（沿用旧配置）\n", dir)
	} else {
		fmt.Printf("  超级管理员:   %s\n", user)
		if generatedPass {
			fmt.Printf("  管理员密码:   %s（随机生成，仅本次显示，请妥善保存）\n", pass)
		}
	}
	if mgr.Kind() == "systemd" {
		fmt.Printf("  服务管理:     systemctl {status|restart|stop} bedrock 或 bedctl {start|stop|restart} server\n")
	} else {
		fmt.Printf("  服务管理:     bedctl {start|stop|restart} server；日志 bedctl logs server\n")
	}
	fmt.Printf("  故障排查:     bedctl doctor server（外部访问不通时先跑这个）\n")
	if err := a.State.Set(KeyServerDir, dir); err != nil {
		Warn("无法记录安装目录: %v", err)
	}
	return SelfInstall(a)
}

// InstallAgent installs or reinstalls the Deploy Agent into dir.
func InstallAgent(ctx context.Context, a *App) error {
	dir, err := askInstallDir(a, Agent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".update"), 0o755); err != nil {
		return fmt.Errorf("无法创建目录 %s: %w", dir, err)
	}

	keptConfig := fileExists(filepath.Join(dir, "bedrock-agent.yaml"))
	var addr, token string
	generatedToken := false
	if keptConfig {
		var ignored []string
		if a.Addr != "" {
			ignored = append(ignored, "--addr")
		}
		if a.Token != "" {
			ignored = append(ignored, "--token")
		}
		if len(ignored) > 0 {
			Warn("已有 bedrock-agent.yaml，以下选项不生效（不覆盖现有配置）: %s", strings.Join(ignored, " "))
		}
	} else {
		addr = a.Addr
		if addr == "" {
			if a.Interactive {
				addr = a.Prompts.Ask("监听地址", ":9091")
			} else {
				addr = ":9091"
			}
		}
		if a.Token != "" {
			token = a.Token
		} else {
			token, err = RandHex(16)
			if err != nil {
				return err
			}
			generatedToken = true
		}
	}

	newBin, tag, err := downloadNew(ctx, a, Agent, dir)
	if err != nil {
		return err
	}

	if keptConfig {
		Warn("bedrock-agent.yaml 已存在，保留现有配置（不覆盖）")
	} else if err := GenAgentConfig(dir, addr, token); err != nil {
		return err
	}

	var effPort uint16
	shownAddr := addr
	if keptConfig {
		shownAddr, _ = ReadAgentAddr(dir)
		if shownAddr == "" {
			shownAddr = ":9091"
			Warn("未能从现有 bedrock-agent.yaml 解析 addr，健康检查按默认端口 9091 进行")
		}
		effPort, err = AgentPort(shownAddr)
		if err != nil {
			Warn("%v，健康检查按默认端口 9091 进行", err)
			effPort = 9091
		}
		Info("监听地址（来自现有配置）: %s", shownAddr)
	} else {
		effPort, err = AgentPort(addr)
		if err != nil {
			return err
		}
	}

	mgr := a.Manager()
	if err := mgr.Stop(ctx, Agent, dir, effPort); err != nil {
		return err
	}

	Info("放置二进制...")
	bin := BinaryPath(Agent, dir)
	if err := swapBin(bin, newBin); err != nil {
		return err
	}
	if err := a.refreshAndStart(Agent, dir); err != nil {
		return err
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", effPort)
	bearer, _ := ReadAgentToken(dir)
	Info("等待服务就绪（最长 30s，进程退出会提前报告）...")
	if code, ok := WaitHealthy(mgr, Agent, dir, healthURL, bearer, 30); !ok {
		Err("健康检查未通过：%s", ReadinessHint(code))
		printRecentLogs(a, Agent, dir, 30)
		if fileExists(bin + ".bak") {
			Warn("自动回滚到旧版本...")
			back, err := a.rollbackAndRecheck(ctx, Agent, dir, healthURL, bearer, 15)
			if err != nil {
				return err
			}
			if back {
				Warn("已回滚到旧版本并恢复运行；失败原因见上方日志，修复后重新运行即可")
			} else {
				Err("回滚后仍无法启动，请手动检查 %s 与上方日志", bin)
			}
		} else {
			_ = mgr.Stop(ctx, Agent, dir, 0)
			Err("首次安装无可回滚的旧版本。常见原因：bedrock-agent.yaml 缺少 token、端口冲突；修复后重新运行即可")
		}
		return fmt.Errorf("Deploy Agent 安装失败")
	}

	fmt.Println()
	Info("%s  版本: %s", Highlight("Deploy Agent 安装成功"), orVersion(BinaryVersion(bin), tag))
	fmt.Printf("  安装目录:     %s\n", dir)
	fmt.Printf("  监听地址:     %s\n", shownAddr)
	if !keptConfig {
		fmt.Printf("  认证 token:   %s\n", token)
		if generatedToken {
			fmt.Printf("                （随机生成，仅本次显示；请填入平台「资源管理 → 服务器」对应 Agent 的 token）\n")
		}
	}
	if mgr.Kind() == "systemd" {
		fmt.Printf("  服务管理:     systemctl {status|restart|stop} bedrock-agent 或 bedctl {start|stop|restart} agent\n")
	} else {
		fmt.Printf("  服务管理:     bedctl {start|stop|restart} agent；日志 bedctl logs agent\n")
	}
	if err := a.State.Set(KeyAgentDir, dir); err != nil {
		Warn("无法记录安装目录: %v", err)
	}
	return SelfInstall(a)
}

// SelfInstall copies the running bedctl binary to the CLI install location,
// replacing the shell-script bedctl of pre-migration installs.
func SelfInstall(a *App) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dest := CLIPath(a.State, a.IsRoot, a.Home)
	if sameFile(self, dest) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		Warn("无法创建 %s，跳过 bedctl 安装: %v", filepath.Dir(dest), err)
		return nil
	}
	data, err := os.ReadFile(self)
	if err != nil {
		Warn("读取自身二进制失败，跳过 bedctl 安装: %v", err)
		return nil
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		Warn("写入 %s 失败，跳过 bedctl 安装: %v", tmp, err)
		return nil
	}
	if err := os.Rename(tmp, dest); err != nil {
		Warn("替换 %s 失败，跳过 bedctl 安装: %v", dest, err)
		return nil
	}
	_ = a.State.Set(KeyCLIPath, dest)
	Info("已安装命令行工具: %s", dest)
	if dir := filepath.Dir(dest); !inPATH(dir) {
		Warn("提示: %s 不在 PATH 中，请将其加入 PATH 后使用 bedctl", dir)
	}
	return nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func orVersion(got, tag string) string {
	if got == "" || got == "unknown" {
		return tag
	}
	return got
}

func inPATH(dir string) bool {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == dir {
			return true
		}
	}
	return false
}

func sameFile(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
