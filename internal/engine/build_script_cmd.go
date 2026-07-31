package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// newBuildScriptCommand constructs the build script command for the given type.
// cleanup removes any temporary script file; call it after cmd.Wait returns.
func newBuildScriptCommand(ctx context.Context, workDir, scriptType, script string) (cmd *exec.Cmd, cleanup func(), err error) {
	cleanup = func() {}
	st := strings.ToLower(strings.TrimSpace(scriptType))
	if st == "" {
		st = "bash"
	}

	switch st {
	case "node":
		return exec.CommandContext(ctx, "node", "-e", script), cleanup, nil
	case "python":
		return exec.CommandContext(ctx, "python3", "-c", script), cleanup, nil
	case "powershell", "pwsh":
		return newPowerShellBuildCommand(ctx, workDir, st, script)
	case "cmd", "batch":
		return newCmdBuildCommand(ctx, workDir, script)
	default:
		return newPOSIXShellBuildCommand(ctx, script)
	}
}

func newPOSIXShellBuildCommand(ctx context.Context, script string) (*exec.Cmd, func(), error) {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"bash", "sh"} {
			path, err := exec.LookPath(name)
			if err == nil {
				return exec.CommandContext(ctx, path, "-c", script), func() {}, nil
			}
		}
		return nil, func() {}, fmt.Errorf("未找到 bash 或 sh。请在 Windows 上安装 Git for Windows，或将脚本类型改为 PowerShell / CMD")
	}
	sh, err := resolvePOSIXShell()
	if err != nil {
		return nil, func() {}, err
	}
	return exec.CommandContext(ctx, sh, "-c", script), func() {}, nil
}

// resolvePOSIXShell finds sh via PATH, then falls back to common absolute paths.
func resolvePOSIXShell() (string, error) {
	if path, err := exec.LookPath("sh"); err == nil {
		return path, nil
	}
	for _, candidate := range []string{"/bin/sh", "/usr/bin/sh"} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("未找到 sh（已尝试 PATH、/bin/sh、/usr/bin/sh）")
}

func findPowerShellByType(scriptType string) (string, error) {
	var name string
	switch scriptType {
	case "powershell":
		name = "powershell"
	case "pwsh":
		name = "pwsh"
	default:
		return "", fmt.Errorf("未知 PowerShell 脚本类型 %q", scriptType)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("未找到 %s", name)
	}
	return path, nil
}

func isLegacyWindowsPowerShell(psPath string) bool {
	normalized := strings.ReplaceAll(psPath, `\`, `/`)
	base := strings.ToLower(filepath.Base(normalized))
	return base == "powershell.exe" || base == "powershell"
}

const (
	psUTF8Preamble  = "$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)\r\n"
	cmdUTF8Preamble = "@echo off\r\nchcp 65001 >nul\r\n"
)

func newPowerShellBuildCommand(ctx context.Context, workDir, scriptType, script string) (*exec.Cmd, func(), error) {
	ps, err := findPowerShellByType(scriptType)
	if err != nil {
		return nil, func() {}, err
	}
	if strings.Contains(script, "&&") && isLegacyWindowsPowerShell(ps) {
		return nil, func() {}, fmt.Errorf(
			"当前 Windows PowerShell 5.x 不支持 && 链式命令。请将脚本类型改为 CMD、安装 PowerShell 7 (pwsh)，或拆成多行命令",
		)
	}
	f, err := os.CreateTemp(workDir, ".bedrock-*.ps1")
	if err != nil {
		return nil, func() {}, err
	}
	path := f.Name()
	body := psUTF8Preamble + script
	if err := writeUTF8BOMFile(f, body); err != nil {
		f.Close()
		os.Remove(path)
		return nil, func() {}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, func() {}, err
	}
	cleanup := func() { os.Remove(path) }
	cmd := exec.CommandContext(ctx, ps,
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
	return cmd, cleanup, nil
}

func newCmdBuildCommand(ctx context.Context, workDir, script string) (*exec.Cmd, func(), error) {
	cmdexe := os.Getenv("ComSpec")
	if cmdexe == "" {
		if windir := os.Getenv("SystemRoot"); windir != "" {
			cmdexe = filepath.Join(windir, "System32", "cmd.exe")
		}
	}
	if cmdexe == "" {
		if p, err := exec.LookPath("cmd"); err == nil {
			cmdexe = p
		} else {
			return nil, func() {}, fmt.Errorf("未找到 cmd.exe")
		}
	} else if _, err := os.Stat(cmdexe); err != nil {
		if p, err2 := exec.LookPath("cmd"); err2 == nil {
			cmdexe = p
		} else {
			return nil, func() {}, fmt.Errorf("未找到 cmd.exe")
		}
	}

	f, err := os.CreateTemp(workDir, ".bedrock-*.cmd")
	if err != nil {
		return nil, func() {}, err
	}
	path := f.Name()
	if _, err := f.WriteString(cmdUTF8Preamble + script); err != nil {
		f.Close()
		os.Remove(path)
		return nil, func() {}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, func() {}, err
	}
	cleanup := func() { os.Remove(path) }
	cmd := exec.CommandContext(ctx, cmdexe, "/C", path)
	return cmd, cleanup, nil
}

func writeUTF8BOMFile(f *os.File, content string) error {
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	_, err := f.WriteString(content)
	return err
}
