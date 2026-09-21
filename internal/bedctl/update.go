package bedctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupSQLite snapshots the sqlite database while the server is stopped,
// keeping the newest 3 copies in dir/backups.
func BackupSQLite(dir string) {
	rel, err := ReadDBPath(dir)
	if err != nil || rel == "" {
		return
	}
	db := rel
	if !filepath.IsAbs(db) {
		db = filepath.Join(dir, rel)
	}
	if !fileExists(db) {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o755); err != nil {
		Warn("无法创建备份目录: %v", err)
		return
	}
	backup := filepath.Join(dir, "backups", "bedrock-"+time.Now().Format("20060102-150405")+".sqlite")
	data, err := os.ReadFile(db)
	if err == nil {
		err = os.WriteFile(backup, data, 0o600)
	}
	if err != nil {
		Warn("备份 SQLite 失败: %v", err)
		return
	}
	Info("已备份 SQLite: %s", backup)
	pruneBackups(filepath.Join(dir, "backups"), 3)
}

func pruneBackups(backupDir string, keep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sqlite" {
			backups = append(backups, e.Name())
		}
	}
	// ReadDir returns sorted names; timestamped names sort oldest first.
	for i := 0; i < len(backups)-keep; i++ {
		_ = os.Remove(filepath.Join(backupDir, backups[i]))
	}
}

// UpdateComponent downloads the target release, really stops the service,
// swaps the binary, refreshes the service unit, restarts and health-checks,
// rolling back to .bak on failure. Applies --port/--addr when requested.
func UpdateComponent(ctx context.Context, a *App, comp Component, dir string) error {
	bin := BinaryPath(comp, dir)
	if !fileExists(bin) {
		Info("%s: 未安装（跳过）", comp.ServiceName())
		return nil
	}
	cur := BinaryVersion(bin)
	d := a.Downloader(a.Mirror.Selected())
	tag, err := a.ResolveTag(ctx, d)
	if err != nil {
		return err
	}
	if skip, note := NeedsUpdate(cur, tag); skip {
		Info("%s: %s", comp.ServiceName(), note)
		return nil
	}
	Info("%s: 当前 %s → 目标 %s", comp.ServiceName(), cur, tag)

	newBin := filepath.Join(dir, ".update", comp.BinaryName()+".new")
	Info("下载新版本...")
	if err := d.DownloadVerified(ctx, comp.AssetName(a.Suffix), tag, a.Suffix, newBin); err != nil {
		return err
	}

	// Resolve the health-check port before stopping: for server, --port is
	// applied below and must win; otherwise parse the existing config.
	var portChange, addrChange string
	var ignored []string
	if comp == Server && a.PortFlag != 0 {
		portChange = fmt.Sprintf("%d", a.PortFlag)
	} else if a.PortFlag != 0 {
		ignored = append(ignored, "--port")
	}
	if comp == Agent && a.Addr != "" {
		addrChange = a.Addr
	} else if a.Addr != "" {
		ignored = append(ignored, "--addr")
	}
	if len(ignored) > 0 {
		Warn("以下选项不适用于 %s，已忽略: %s", comp, strings.Join(ignored, " "))
	}

	// Stop-time port verification must check the port the OLD process is
	// bound to (current config); the post-update health check re-parses the
	// config after the change below.
	Info("优雅停机...")
	mgr := a.Manager()
	if err := mgr.Stop(ctx, comp, dir, currentPort(comp, dir)); err != nil {
		return err
	}
	if comp == Server {
		BackupSQLite(dir)
	}

	if portChange != "" {
		if err := SetServerPort(dir, a.PortFlag); err != nil {
			return fmt.Errorf("修改 config.yaml 端口失败: %w", err)
		}
		Info("已将 server.port 修改为 %s", portChange)
	}
	if addrChange != "" {
		if err := SetAgentAddr(dir, addrChange); err != nil {
			return fmt.Errorf("修改 bedrock-agent.yaml addr 失败: %w", err)
		}
		Info("已将监听地址修改为 %s", addrChange)
	}

	Info("替换二进制...")
	if err := swapBin(bin, newBin); err != nil {
		return err
	}
	if err := a.refreshAndStart(comp, dir); err != nil {
		return err
	}

	url, bearer := HealthParts(comp, dir)
	tries := comp.HealthTries()
	Info("等待服务就绪（最长 %ds，进程退出会提前报告）...", tries)
	if code, ok := WaitHealthy(mgr, comp, dir, url, bearer, tries); !ok {
		Err("%s: 新版本健康检查失败（%s），自动回滚到 %s", comp.ServiceName(), ReadinessHint(code), cur)
		printRecentLogs(a, comp, dir, 30)
		back, err := a.rollbackAndRecheck(ctx, comp, dir, url, bearer, tries)
		if err != nil {
			return err
		}
		if back {
			Warn("已回滚到旧版本 %s 并恢复运行；失败原因见上方日志，请反馈至项目仓库", cur)
		} else {
			Err("回滚后启动仍失败，请手动检查: %s / %s.bak", bin, bin)
			printRecentLogs(a, comp, dir, 30)
		}
		return fmt.Errorf("%s 更新失败", comp.ServiceName())
	}
	Info("%s 更新成功: %s → %s", comp.ServiceName(), cur, orVersion(BinaryVersion(bin), tag))
	return nil
}

// currentPort reads the port the running component is (or would be) bound
// to from its existing config. 0 means unparsable for the server, which
// skips stop-time port verification; the agent falls back to its default.
func currentPort(comp Component, dir string) uint16 {
	switch comp {
	case Agent:
		addr, _ := ReadAgentAddr(dir)
		if p, err := AgentPort(addr); err == nil {
			return p
		}
		return 9091
	default:
		if p, err := ReadServerPort(dir); err == nil && p > 0 {
			return uint16(p)
		}
		return 0
	}
}

// UpdateAll updates the components selected on the command line: an explicit
// server|agent selector updates just that component (in --dir when set, else
// its remembered/default dir); with only --dir the first binary found in it
// wins; with neither, every installed component in its own dir.
func UpdateAll(ctx context.Context, a *App) error {
	if a.Component != "" {
		comp := Component(a.Component)
		dir := a.DefaultDir(comp)
		if !fileExists(filepath.Join(dir, comp.BinaryName())) {
			return fmt.Errorf("目录 %s 下未发现 %s 二进制", dir, comp.BinaryName())
		}
		return UpdateComponent(ctx, a, comp, dir)
	}
	if a.DirFlag != "" {
		dir := a.DirFlag
		switch {
		case fileExists(filepath.Join(dir, Server.BinaryName())):
			return UpdateComponent(ctx, a, Server, dir)
		case fileExists(filepath.Join(dir, Agent.BinaryName())):
			return UpdateComponent(ctx, a, Agent, dir)
		default:
			return fmt.Errorf("目录 %s 下未发现 bedrock / bedrock-agent 二进制", dir)
		}
	}
	rc := error(nil)
	if fileExists(filepath.Join(a.DefaultDir(Server), Server.BinaryName())) {
		if err := UpdateComponent(ctx, a, Server, a.DefaultDir(Server)); err != nil {
			rc = err
		}
	}
	if fileExists(filepath.Join(a.DefaultDir(Agent), Agent.BinaryName())) {
		if err := UpdateComponent(ctx, a, Agent, a.DefaultDir(Agent)); err != nil {
			rc = err
		}
	}
	if rc == nil &&
		!fileExists(filepath.Join(a.DefaultDir(Server), Server.BinaryName())) &&
		!fileExists(filepath.Join(a.DefaultDir(Agent), Agent.BinaryName())) {
		Warn("默认目录下未发现已安装组件；自定义目录请用 --dir <DIR> 指定")
	}
	return rc
}
