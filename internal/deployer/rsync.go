package deployer

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type RsyncDeployer struct{}

func (d *RsyncDeployer) Deploy(ctx context.Context, opts DeployOptions) error {
	if isWindowsServer(opts.Server) {
		if opts.Logger != nil {
			opts.Logger("Windows 目标不支持 rsync，自动降级为 SFTP")
		}
		return (&SFTPDeployer{}).Deploy(ctx, opts)
	}

	sshOpts, cleanup := buildSSHOptions(opts.Server)
	defer cleanup()

	source := strings.TrimSuffix(opts.SourceDir, string(filepath.Separator)) + string(filepath.Separator)
	remote := fmt.Sprintf("%s@%s:%s", opts.Server.Username, opts.Server.Host, normalizeRemotePath(opts.Server, opts.RemotePath))

	var sshCmd string
	if opts.Server.Password != "" && !IsSSHKeyAuth(opts.Server.AuthType) {
		sshCmd = fmt.Sprintf("sshpass -p %q ssh %s", opts.Server.Password, sshOpts)
	} else {
		sshCmd = "ssh " + sshOpts
	}
	args := []string{
		"-avz",
		"--delete",
		"-e", sshCmd,
		source,
		remote,
	}

	cmd := exec.CommandContext(ctx, "rsync", args...)
	return runAndLog(cmd, opts.Logger)
}
