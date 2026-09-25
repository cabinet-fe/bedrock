package bedctl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CmdDoctor runs the health diagnosis for the selected components:
// service state, configured port/host, local health endpoint, enablement
// and — when everything is locally healthy — a firewall/security-group
// checklist for "unreachable from outside" cases.
func CmdDoctor(ctx context.Context, a *App) error {
	comps := PickComponent(a)
	if len(comps) == 0 {
		return fmt.Errorf("未发现已安装组件（bedctl doctor server|agent）")
	}
	mgr := a.Manager()
	rc := 0
	for _, comp := range comps {
		fmt.Println()
		Info("诊断 Bedrock %s", comp.ServiceName())
		dir := a.DefaultDir(comp)
		bin := BinaryPath(comp, dir)
		issues := 0
		if !fileExists(bin) {
			Err("  未安装: %s 不存在", bin)
			rc = 1
			continue
		}
		fmt.Printf("  安装目录: %s\n", dir)
		fmt.Printf("  版本:     %s\n", BinaryVersion(bin))

		if mgr.IsActive(comp, dir) {
			fmt.Println("  服务:     运行中")
		} else {
			fmt.Printf("  %s服务:     未运行（bedctl start %s 启动）%s\n", Code("33"), comp, Code("0"))
			issues = 1
		}

		var port uint16
		if comp == Server {
			port = ServerPort(dir)
			fmt.Printf("  配置端口: %d\n", port)
			host, _ := ReadServerHost(dir)
			if host == "" {
				host = "0.0.0.0"
			}
			if isLoopback(host) {
				fmt.Printf("  %s监听地址: %s —— 仅本机可访问，外部连不上优先改这里（改为 0.0.0.0 后 bedctl restart server）%s\n", Code("33"), host, Code("0"))
				issues = 1
			} else {
				fmt.Printf("  监听地址: %s（对所有网卡开放）\n", host)
			}
		} else {
			addr, _ := ReadAgentAddr(dir)
			if addr == "" {
				addr = ":9091"
			}
			port, _ = AgentPort(addr)
			if head := strings.SplitN(addr, ":", 2)[0]; isLoopback(head) {
				fmt.Printf("  %s监听地址: %s —— 仅本机可访问，外部连不上优先改这里（如 :9091）%s\n", Code("33"), addr, Code("0"))
				issues = 1
			} else {
				fmt.Printf("  监听地址: %s\n", addr)
			}
		}

		if FreePort(port) {
			fmt.Printf("  %s端口:     %d 无监听 —— 服务大概率没起来，运行 bedctl logs %s 查看原因%s\n", Code("33"), port, comp, Code("0"))
			issues = 1
		} else {
			fmt.Printf("  端口:     %d 有进程监听\n", port)
		}

		if mgr.IsActive(comp, dir) {
			url, bearer := HealthParts(comp, dir)
			if code := ProbeURL(url, bearer); code == 200 {
				fmt.Printf("  本机健康: 通过（%s）\n", url)
			} else {
				fmt.Printf("  %s本机健康: 未通过（%s）—— 运行 bedctl logs %s 查看日志%s\n", Code("33"), ReadinessHint(code), comp, Code("0"))
				issues = 1
			}
		}

		if mgr.Kind() == "systemd" {
			if _, err := runCmd(ctx, 5*time.Second, "systemctl", "is-enabled", "--quiet", comp.ServiceName()+".service"); err == nil {
				fmt.Println("  开机自启: 已启用")
			} else {
				fmt.Printf("  %s开机自启: 未启用（systemctl enable %s）%s\n", Code("33"), comp.ServiceName(), Code("0"))
				issues = 1
			}
		}

		if issues == 0 {
			fmt.Printf("  %s本机一切正常。外部仍连不上时按序检查：%s\n", Code("1"), Code("0"))
			fmt.Printf("    1. 云安全组: 入方向放行 TCP %d（源 0.0.0.0/0）\n", port)
			if a.IsRoot {
				fmt.Printf("    %s\n", firewallAdvice(ctx, port))
			} else {
				fmt.Printf("    2. 宿主机防火墙: firewalld/ufw 是否放行 %d/tcp（root 运行 bedctl doctor 会自动检查）\n", port)
			}
			fmt.Println("    3. 用公网 IP/EIP 访问（不是内网 IP），并确认 EIP 已绑定该实例")
			fmt.Println("    4. 配了 Nginx 等反代/HTTPS 时，检查反代监听端口与 upstream 转发")
			fmt.Printf("    5. 外部机器上执行 curl -v http://<公网IP>:%d/ —— 超时=安全组/防火墙，连接拒绝=服务未监听\n", port)
		}
		if issues == 1 {
			rc = 1
		}
	}
	if rc == 1 {
		return ErrSilent
	}
	return nil
}

// firewallAdvice inspects firewalld/ufw when running as root and returns a
// concrete advisory line for the port.
func firewallAdvice(ctx context.Context, port uint16) string {
	p := fmt.Sprintf("%d/tcp", port)
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if _, err := runCmd(ctx, 5*time.Second, "firewall-cmd", "-q", "--state"); err == nil {
			out, err := runCmd(ctx, 5*time.Second, "firewall-cmd", "--list-ports")
			if err == nil {
				for _, f := range strings.Fields(out) {
					if f == p {
						return "    2. firewalld: 已放行 " + p
					}
				}
			}
			return fmt.Sprintf("    2. firewalld 运行中且未放行 %s，执行: firewall-cmd --permanent --add-port=%s && firewall-cmd --reload", p, p)
		}
	}
	if _, err := exec.LookPath("ufw"); err == nil {
		out, err := runCmd(ctx, 5*time.Second, "ufw", "status")
		if err == nil && strings.Contains(out, "Status: active") {
			for _, line := range splitLines(out) {
				if strings.Contains(line, p) {
					return "    2. ufw: 已放行 " + p
				}
			}
			return fmt.Sprintf("    2. ufw 运行中且未放行 %s，执行: ufw allow %s", p, p)
		}
	}
	return fmt.Sprintf("    2. 宿主机防火墙: 未检测到 firewalld/ufw，如有 iptables/nftables 规则请自查 %s", p)
}
