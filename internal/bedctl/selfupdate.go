package bedctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SelfUpdate downloads the newest bedctl release asset (or --version tag)
// with the same mirror logic as component updates and replaces this binary
// in place — safe on Linux even while running, via rename.
func SelfUpdate(ctx context.Context, a *App, currentVersion string) error {
	if a.Suffix == "" {
		_, _, sfx, err := PlatformDeps()
		if err != nil {
			return err
		}
		a.Suffix = sfx
	}
	d := a.Downloader(a.Mirror.Selected())
	tag, err := a.ResolveTag(ctx, d)
	if err != nil {
		return err
	}
	if skip, note := NeedsUpdate(currentVersion, tag); skip && a.VersionTag == "" {
		Info("bedctl: %s", note)
		return nil
	}
	Info("bedctl: 当前 %s → 目标 %s", currentVersion, tag)

	dest := CLIPath(a.State, a.IsRoot, a.Home)
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			dest = resolved
		}
	}
	tmp := dest + ".update"
	if err := d.DownloadVerified(ctx, "bedctl-"+a.Suffix, tag, a.Suffix, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", dest, err)
	}
	_ = a.State.Set(KeyCLIPath, dest)
	Info("bedctl 已更新到 %s（%s）", tag, dest)
	return nil
}
