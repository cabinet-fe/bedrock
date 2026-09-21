package bedctl

import (
	"context"
	"fmt"
	"path/filepath"
)

// PickComponent resolves which components a command applies to.
func PickComponent(a *App) []Component {
	if a.Component == "server" {
		return []Component{Server}
	}
	if a.Component == "agent" {
		return []Component{Agent}
	}
	var comps []Component
	if fileExists(filepath.Join(a.DefaultDir(Server), Server.BinaryName())) {
		comps = append(comps, Server)
	}
	if fileExists(filepath.Join(a.DefaultDir(Agent), Agent.BinaryName())) {
		comps = append(comps, Agent)
	}
	return comps
}

// CmdSvc implements start/stop/restart across the selected components.
func CmdSvc(ctx context.Context, a *App, action string) error {
	comps := PickComponent(a)
	if len(comps) == 0 {
		return fmt.Errorf("未发现已安装组件（bedctl status 查看，或用参数 server|agent 指定）")
	}
	mgr := a.Manager()
	for _, comp := range comps {
		dir := a.DefaultDir(comp)
		bin := BinaryPath(comp, dir)
		if !fileExists(bin) {
			Warn("%s: 未安装（%s 不存在），跳过", comp.ServiceName(), bin)
			continue
		}
		url, bearer := HealthParts(comp, dir)
		switch action {
		case "stop":
			if err := mgr.Stop(ctx, comp, dir, currentPort(comp, dir)); err != nil {
				return err
			}
		case "start":
			if mgr.IsActive(comp, dir) {
				Info("%s: 已在运行", comp.ServiceName())
				continue
			}
			if err := mgr.Start(comp, dir); err != nil {
				return err
			}
			if _, ok := WaitHealthy(mgr, comp, dir, url, bearer, 15); ok {
				Info("%s: 已启动（健康检查通过）", comp.ServiceName())
			} else {
				Warn("%s: 已执行启动但未就绪，运行 bedctl logs %s 查看日志", comp.ServiceName(), comp)
			}
		case "restart":
			if err := mgr.Stop(ctx, comp, dir, currentPort(comp, dir)); err != nil {
				return err
			}
			if err := mgr.Start(comp, dir); err != nil {
				return err
			}
			if _, ok := WaitHealthy(mgr, comp, dir, url, bearer, 15); ok {
				Info("%s: 已重启（健康检查通过）", comp.ServiceName())
			} else {
				Warn("%s: 已执行重启但未就绪，运行 bedctl logs %s 查看日志", comp.ServiceName(), comp)
			}
		}
	}
	return nil
}

// CmdStatus prints install state per component.
func CmdStatus(a *App) error {
	comps := []Component{Server, Agent}
	if a.Component != "" {
		comps = []Component{Component(a.Component)}
	}
	if a.DirFlag != "" {
		var found []Component
		for _, comp := range comps {
			if fileExists(filepath.Join(a.DirFlag, comp.BinaryName())) {
				found = append(found, comp)
			}
		}
		if len(found) == 0 {
			return fmt.Errorf("目录 %s 下未发现 bedrock / bedrock-agent 二进制", a.DirFlag)
		}
		comps = found
	}
	mgr := a.Manager()
	for _, comp := range comps {
		dir := a.DefaultDir(comp)
		fmt.Println(Bold(fmt.Sprintf("=== Bedrock %s ===", comp.ServiceName())))
		bin := BinaryPath(comp, dir)
		if !fileExists(bin) {
			fmt.Println("  状态: 未安装")
			continue
		}
		ver := BinaryVersion(bin)
		active := "未运行"
		health := "-"
		if mgr.IsActive(comp, dir) {
			active = "运行中"
			url, bearer := HealthParts(comp, dir)
			if ProbeURL(url, bearer) == 200 {
				health = "健康"
			} else {
				health = "异常（健康检查未通过）"
			}
		}
		fmt.Printf("  目录:   %s\n  版本:   %s\n  服务:   %s\n  健康:   %s\n", dir, ver, active, health)
		if mgr.Kind() != "systemd" {
			if f := LogFile(comp, dir); fileExists(f) {
				fmt.Printf("  日志:   %s\n", f)
			}
		}
	}
	return nil
}

// CmdLogs prints recent logs for the selected component.
func CmdLogs(a *App) error {
	comps := PickComponent(a)
	if len(comps) == 0 {
		return fmt.Errorf("未发现已安装组件（bedctl logs server|agent）")
	}
	comp := comps[0]
	n := a.LogsN
	if n <= 0 {
		n = 100
	}
	printRecentLogs(a, comp, a.DefaultDir(comp), n)
	return nil
}

// CmdPort changes the server listen port in config.yaml and restarts the
// service when it is running — the operation the shell version never had.
func CmdPort(ctx context.Context, a *App, port int) error {
	dir := a.DefaultDir(Server)
	cfg := filepath.Join(dir, "config.yaml")
	if !fileExists(cfg) {
		return fmt.Errorf("未找到 %s，请先安装 server", cfg)
	}
	// Stop-time port verification must check the port the OLD process is
	// bound to, so capture it before the config change below.
	oldPort := currentPort(Server, dir)
	if err := SetServerPort(dir, port); err != nil {
		return err
	}
	Info("已将 %s 的 server.port 修改为 %d", cfg, port)
	mgr := a.Manager()
	if mgr.IsActive(Server, dir) {
		Info("重启 bedrock 使新端口生效...")
		if err := mgr.Stop(ctx, Server, dir, oldPort); err != nil {
			return err
		}
		if err := mgr.Start(Server, dir); err != nil {
			return err
		}
		url := ServerHealthURL(dir)
		if _, ok := WaitHealthy(mgr, Server, dir, url, "", 30); ok {
			Info("服务已在端口 %d 就绪", port)
		} else {
			Warn("服务已重启但未就绪，运行 bedctl logs server 查看日志")
			return fmt.Errorf("健康检查未通过")
		}
	} else {
		Info("服务当前未运行；下次启动生效")
	}
	return nil
}
