// Command bedctl installs, updates and operates Bedrock Server and Deploy
// Agent. It replaces the shell install.sh of pre-1.x releases; that script
// now bootstraps this binary.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"bedrock/internal/bedctl"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `Bedrock 一键安装 / 更新工具（Go 版 bedctl）

用法:
  bedctl                           交互模式（推荐）
  bedctl install server|agent [选项]
                                   安装 Bedrock Server / Deploy Agent
  bedctl server [选项]             同 install server
  bedctl agent [选项]              同 install agent
  bedctl update [server|agent] [选项]
                                   更新已安装组件（下载 → 优雅停机 → 更新 → 重启）
  bedctl port <N>                  修改 Server 监听端口并重启
  bedctl status [server|agent]     查看安装状态
  bedctl start|stop|restart [server|agent]
                                   服务管理（缺省作用于全部已安装组件）
  bedctl logs [server|agent] [-n N]
                                   最近日志（默认 100 行）
  bedctl doctor [server|agent]     体检: 服务/端口/监听地址/本机健康/防火墙
  bedctl self-update               bedctl 自我升级
  bedctl version                   查看 bedctl 版本

bedctl 会记住安装目录与下载源（root: /etc/bedrock/bedctl.env，非 root: ~/.bedrock/bedctl.env），
后续命令无需重复传 --dir / --mirror。

选项:
  --mirror <URL>     GitHub 镜像前缀，如 https://gh-proxy.com/
  --no-mirror        强制直连 GitHub
  --dir <DIR>        安装目录（默认 /opt/bedrock[-agent]，非 root 为 ~/bedrock[-agent]；已记住时用记住的目录）
  --version <TAG>    指定版本（默认最新 release，如 v2.0.0）
  --port <N>         Server 监听端口（默认 8080；update 时修改现有端口）
  --admin-user <U>   超级管理员用户名（默认 admin）
  --admin-pass <P>   超级管理员密码（默认随机生成并打印）
  --token <T>        Agent 认证 token（默认随机生成并打印，需填回平台「服务器」配置）
  --addr <A>         Agent 监听地址（默认 :9091；update 时修改现有地址）
  -n <N>             logs 输出行数（配合 bedctl logs）
  --yes, -y          非交互模式，未提供的参数取默认值

环境变量:
  BEDROCK_MIRROR         等效 --mirror
  BEDROCK_RELEASE_BASE   覆盖下载源（默认 GitHub Releases，测试用）
  BEDROCK_OS / BEDROCK_ARCH  覆盖平台探测（默认 linux + 本机架构）
`

// options is the flat flag set parsed from anywhere in argv, matching the
// shell installer's "--opt value" / "--opt=value" style.
type options struct {
	mirror     string
	noMirror   bool
	dir        string
	versionTag string
	port       int
	portSet    bool
	adminUser  string
	adminPass  string
	token      string
	addr       string
	logsN      int
	yes        bool
	help       bool
}

func parseArgs(args []string) (*options, []string, error) {
	opt := &options{port: 0}
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		take := func() string {
			i++
			if i < len(args) {
				return args[i]
			}
			return ""
		}
		val := ""
		hasVal := false
		if eq := strings.Index(arg, "="); eq >= 0 && strings.HasPrefix(arg, "--") {
			val = arg[eq+1:]
			arg = arg[:eq]
			hasVal = true
		}
		need := func(name string) (string, error) {
			if hasVal {
				return val, nil
			}
			v := take()
			if v == "" {
				return "", fmt.Errorf("%s 需要一个值（--help 查看用法）", name)
			}
			return v, nil
		}
		switch arg {
		case "--mirror":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.mirror = v
		case "--no-mirror":
			opt.noMirror = true
		case "--dir":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.dir = v
		case "--version":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.versionTag = v
		case "--port":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			n := 0
			if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 || n > 65535 {
				return nil, nil, fmt.Errorf("无效端口: %s", v)
			}
			opt.port, opt.portSet = n, true
		case "--admin-user":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.adminUser = v
		case "--admin-pass":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.adminPass = v
		case "--token":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.token = v
		case "--addr":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			opt.addr = v
		case "-n":
			v, err := need(arg)
			if err != nil {
				return nil, nil, err
			}
			n := 0
			if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
				return nil, nil, fmt.Errorf("无效行数: %s", v)
			}
			opt.logsN = n
		case "--yes", "-y":
			opt.yes = true
		case "-h", "--help":
			opt.help = true
		default:
			pos = append(pos, arg)
		}
	}
	return opt, pos, nil
}

// newApp builds the invocation context from parsed options.
func newApp(opt *options) (*bedctl.App, error) {
	a := &bedctl.App{
		Home:   bedctl.HomeDir(),
		IsRoot: bedctl.IsRoot(),
	}
	a.State = bedctl.LoadState(a.IsRoot, a.Home)
	a.Prompts, a.Interactive = bedctl.NewPrompts()
	if opt != nil {
		if opt.yes {
			a.Interactive = false
		}
		a.MirrorFlag = os.Getenv("BEDROCK_MIRROR")
		if opt.mirror != "" {
			a.MirrorFlag = opt.mirror
		}
		a.NoMirror = opt.noMirror
		a.DirFlag = opt.dir
		a.VersionTag = opt.versionTag
		if opt.portSet {
			a.PortFlag = opt.port
		}
		a.AdminUser = opt.adminUser
		a.AdminPass = opt.adminPass
		a.Token = opt.token
		a.Addr = opt.addr
		a.LogsN = opt.logsN
	}
	return a, nil
}

// prepareMirror runs platform detection and mirror selection for the
// download-driven commands, and records the asset suffix on the app.
func prepareMirror(ctx context.Context, a *bedctl.App) error {
	_, _, suffix, err := bedctl.PlatformDeps()
	if err != nil {
		return err
	}
	a.Suffix = suffix
	a.Mirror = &bedctl.MirrorSource{
		Base:         bedctl.ReleaseBase(),
		State:        a.State,
		FlagMirror:   a.MirrorFlag,
		FlagNoMirror: a.NoMirror,
		Interactive:  a.Interactive,
	}
	if a.Interactive {
		a.Mirror.PromptMirrors = func(builtins []string, def int) (string, error) {
			fmt.Println()
			bedctl.Info("下载源选择（GitHub 直连不可达时建议使用镜像）")
			fmt.Println("  [1] 直连 GitHub")
			for i, b := range builtins {
				fmt.Printf("  [%d] %s\n", i+2, b)
			}
			fmt.Printf("  [%d] 自定义镜像 URL\n", len(builtins)+2)
			fmt.Printf("  当前推荐: %s\n", mirrorDefaultLabel(builtins, def))
			defChoice := fmt.Sprintf("%d", def)
			sel := a.Prompts.AskChoice("请选择 [1-"+fmt.Sprintf("%d", len(builtins)+2)+"]", `^\d+$`, defChoice)
			n := 0
			for _, r := range sel {
				if r < '0' || r > '9' {
					continue
				}
				n = n*10 + int(r-'0')
			}
			if n >= 2 && n-2 < len(builtins) {
				return builtins[n-2], nil
			}
			if n == len(builtins)+2 {
				custom := a.Prompts.Ask("镜像前缀 URL（如 https://gh-proxy.com）", "")
				if strings.TrimSpace(custom) == "" {
					return "", fmt.Errorf("未输入镜像 URL")
				}
				return bedctl.NormalizeMirror(custom), nil
			}
			return "", nil
		}
	}
	return a.Mirror.Pick(ctx)
}

func mirrorDefaultLabel(builtins []string, def int) string {
	if def == 2 && len(builtins) > 0 {
		return builtins[0]
	}
	return "直连 GitHub"
}

func die(err error) {
	bedctl.Err("%v", err)
	os.Exit(1)
}

func main() {
	opt, pos, err := parseArgs(os.Args[1:])
	if err != nil {
		die(err)
	}
	if opt.help {
		fmt.Print(usage)
		return
	}

	ctx := context.Background()
	a, err := newApp(opt)
	if err != nil {
		die(err)
	}

	command := ""
	if len(pos) > 0 {
		command = pos[0]
	}

	// port <N> consumes a numeric positional; check it before the generic
	// component validation below (a port is not server|agent).
	if command == "port" {
		if len(pos) != 2 {
			die(fmt.Errorf("用法: bedctl port <N>（修改 Server 监听端口并重启）"))
		}
		port := 0
		if _, err := fmt.Sscanf(pos[1], "%d", &port); err != nil || port <= 0 || port > 65535 {
			die(fmt.Errorf("无效端口: %s", pos[1]))
		}
		if err := bedctl.CmdPort(ctx, a, port); err != nil {
			die(err)
		}
		return
	}

	if len(pos) > 1 {
		if !bedctl.Valid(pos[1]) {
			die(fmt.Errorf("未知组件: %s（可选 server|agent）", pos[1]))
		}
		if command == "server" || command == "agent" {
			die(fmt.Errorf("命令 %s 后不需要再指定组件", command))
		}
		a.Component = pos[1]
	}
	if len(pos) > 2 {
		die(fmt.Errorf("多余参数: %s（--help 查看用法）", pos[2]))
	}

	switch command {
	case "":
		// No subcommand: interactive menu (non-interactive runs print usage).
		if !a.Interactive {
			fmt.Fprint(os.Stderr, usage)
			die(fmt.Errorf("非交互模式（--yes）需要指定子命令: install | server | agent | update | status | start | stop | restart | logs | doctor | port | self-update | version"))
		}
		if err := prepareMirror(ctx, a); err != nil {
			die(err)
		}
		runMenu(ctx, a)
	case "server", "agent":
		a.Component = command
		if err := prepareMirror(ctx, a); err != nil {
			die(err)
		}
		installOne(ctx, a)
	case "install":
		if a.Component == "" {
			if !a.Interactive {
				fmt.Fprint(os.Stderr, usage)
				die(fmt.Errorf("非交互模式需要: install server|agent"))
			}
			if err := prepareMirror(ctx, a); err != nil {
				die(err)
			}
			runMenu(ctx, a)
			return
		}
		if err := prepareMirror(ctx, a); err != nil {
			die(err)
		}
		installOne(ctx, a)
	case "update":
		if err := prepareMirror(ctx, a); err != nil {
			die(err)
		}
		if err := bedctl.UpdateAll(ctx, a); err != nil {
			die(err)
		}
	case "status":
		if err := bedctl.CmdStatus(a); err != nil {
			die(err)
		}
	case "start", "stop", "restart":
		if err := bedctl.CmdSvc(ctx, a, command); err != nil {
			die(err)
		}
	case "logs":
		if err := bedctl.CmdLogs(a); err != nil {
			die(err)
		}
	case "doctor":
		if err := bedctl.CmdDoctor(ctx, a); err != nil {
			die(err)
		}
	case "self-update":
		if err := prepareMirror(ctx, a); err != nil {
			die(err)
		}
		if err := bedctl.SelfUpdate(ctx, a, version); err != nil {
			die(err)
		}
	case "version":
		fmt.Printf("bedctl %s\n", version)
	default:
		die(fmt.Errorf("未知命令: %s（--help 查看用法）", command))
	}
}

func installOne(ctx context.Context, a *bedctl.App) {
	if a.Component == "agent" {
		if err := bedctl.InstallAgent(ctx, a); err != nil {
			die(err)
		}
		return
	}
	if err := bedctl.InstallServer(ctx, a); err != nil {
		die(err)
	}
}

func runMenu(ctx context.Context, a *bedctl.App) {
	fmt.Println(bedctl.Bold("=== Bedrock 一键安装 ==="))
	fmt.Println("  [1] 安装 Bedrock Server（主体）")
	fmt.Println("  [2] 安装 Deploy Agent（代理分发工具）")
	fmt.Println("  [3] 更新已安装组件")
	fmt.Println("  [4] 查看状态")
	fmt.Println("  [q] 退出")
	sel := a.Prompts.AskChoice("请选择", `^[1-4qQ]$`, "1")
	switch sel {
	case "1":
		a.Component = "server"
		installOne(ctx, a)
	case "2":
		a.Component = "agent"
		installOne(ctx, a)
	case "3":
		_ = bedctl.UpdateAll(ctx, a)
	case "4":
		_ = bedctl.CmdStatus(a)
	case "q", "Q":
		os.Exit(0)
	}
}
