package deployer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalDeployer copies the build output tree into a directory on the machine
// running Bedrock. Does not remove files that exist under the destination but
// not in the source (only creates dirs and overwrites same relative paths).
type LocalDeployer struct{}

func (d *LocalDeployer) Deploy(ctx context.Context, opts DeployOptions) error {
	src := filepath.Clean(opts.SourceDir)
	dst := filepath.Clean(strings.TrimSpace(opts.RemotePath))
	if dst == "" || dst == "." {
		return fmt.Errorf("本机部署目标路径为空")
	}
	if !filepath.IsAbs(dst) {
		return fmt.Errorf("本机部署目标路径须为绝对路径: %s", opts.RemotePath)
	}
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("读取产物目录失败: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("产物路径不是目录: %s", src)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建部署目录失败: %w", err)
	}
	if err := copyTreeMerge(ctx, src, dst); err != nil {
		return fmt.Errorf("本机复制失败: %w", err)
	}
	if opts.Logger != nil {
		opts.Logger(fmt.Sprintf("Local deploy: %s -> %s", src, dst))
	}
	return nil
}

// copyTreeMerge copies files from src dir into dst: creates subdirs, overwrites same relative paths.
// Files present under dst but not in src are left unchanged.
func copyTreeMerge(ctx context.Context, srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFileOverwrite(path, target, d)
	})
}

func copyFileOverwrite(src, dst string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode()&0o777)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ExecuteLocalScriptInDir runs script on the local host with workDir as cwd (shell on Unix, cmd on Windows).
func ExecuteLocalScriptInDir(ctx context.Context, workDir, script string, logFn func(string)) error {
	if logFn == nil {
		logFn = func(string) {}
	}
	if strings.TrimSpace(script) == "" {
		return nil
	}
	wd := filepath.Clean(strings.TrimSpace(workDir))
	if wd == "" || wd == "." {
		return fmt.Errorf("本机部署后脚本工作目录无效")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", script)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", script)
	}
	cmd.Dir = wd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stdout.Len() > 0 {
			for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
				logFn(line)
			}
		}
		if stderr.Len() > 0 {
			for line := range strings.SplitSeq(strings.TrimSpace(stderr.String()), "\n") {
				logFn("stderr: " + line)
			}
		}
		return fmt.Errorf("script execution: %w", err)
	}
	if stdout.Len() > 0 {
		for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
			logFn(line)
		}
	}
	if stderr.Len() > 0 {
		for line := range strings.SplitSeq(strings.TrimSpace(stderr.String()), "\n") {
			logFn("stderr: " + line)
		}
	}
	return nil
}
