package deployer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// IsSSHKeyAuth reports whether auth_type uses host SSH keys.
func IsSSHKeyAuth(authType string) bool {
	return strings.ToLower(strings.TrimSpace(authType)) == "ssh_key"
}

// SSHAuthMethods builds auth methods for SSH: PEM private key, password, and/or SSH agent / default ~/.ssh keys when key auth has no embedded key.
func SSHAuthMethods(server ServerInfo) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	if IsSSHKeyAuth(server.AuthType) && server.PrivateKey != "" {
		signer, err := parsePrivateKey(server.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if server.Password != "" {
		authMethods = append(authMethods, ssh.Password(server.Password))
	}

	if IsSSHKeyAuth(server.AuthType) && server.PrivateKey == "" {
		authMethods = append(authMethods, sshAgentAuthMethods()...)
		authMethods = append(authMethods, defaultUserKeyAuthMethods()...)
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no auth method available (password, private key, SSH agent, or ~/.ssh default keys required)")
	}
	return authMethods, nil
}

func defaultUserKeyAuthMethods() []ssh.AuthMethod {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")
	keyFiles := []string{
		"id_ed25519",
		"id_rsa",
		"id_ecdsa",
		"id_dsa",
	}
	var signers []ssh.Signer
	for _, kf := range keyFiles {
		keyPath := filepath.Join(sshDir, kf)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		signer, err := parsePrivateKey(string(data))
		if err == nil {
			signers = append(signers, signer)
		}
	}
	if len(signers) == 0 {
		return nil
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
}

func sshAgentAuthMethods() []ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	aclient := agent.NewClient(conn)
	return []ssh.AuthMethod{ssh.PublicKeysCallback(aclient.Signers)}
}

// CreateSSHClientConfig creates ssh.ClientConfig from ServerInfo (password or key auth)
func CreateSSHClientConfig(server ServerInfo) (*ssh.ClientConfig, error) {
	authMethods, err := SSHAuthMethods(server)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	return config, nil
}

func parsePrivateKey(pem string) (ssh.Signer, error) {
	// Try without passphrase first
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err == nil {
		return signer, nil
	}
	if !strings.Contains(err.Error(), "passphrase") {
		return nil, err
	}
	// With passphrase - not supported for now, return original error
	return nil, err
}

func ExecuteRemoteScriptInDir(ctx context.Context, server ServerInfo, workDir, script string, logFn func(string)) error {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	if server.AuthType == "agent" {
		return executeAgentScript(ctx, server, workDir, script, logFn)
	}

	command := wrapRemoteScript(server, workDir, script)
	name, args, cleanup := buildRemoteSSHCLI(server, command)
	defer cleanup()

	cmd := exec.CommandContext(ctx, name, args...)
	if err := runAndLog(cmd, logFn); err != nil {
		return fmt.Errorf("script execution: %w", err)
	}
	return nil
}

// buildRemoteSSHCLI assembles system ssh/sshpass argv for a remote command.
// Matches rsync's auth path: password (non-key) uses sshpass; ssh_key uses bare ssh
// (host ~/.ssh / agent / IdentityFile from buildSSHOptionsSlice).
func buildRemoteSSHCLI(server ServerInfo, remoteCommand string) (name string, args []string, cleanup func()) {
	sshOpts, cleanup := buildSSHOptionsSlice(server)
	target := fmt.Sprintf("%s@%s", server.Username, server.Host)
	if server.Password != "" && !IsSSHKeyAuth(server.AuthType) {
		args = []string{"-p", server.Password, "ssh"}
		args = append(args, sshOpts...)
		args = append(args, target, "--", remoteCommand)
		return "sshpass", args, cleanup
	}
	args = append(args, sshOpts...)
	args = append(args, target, "--", remoteCommand)
	return "ssh", args, cleanup
}

// buildSSHOptions returns SSH option string and a cleanup function for temp key files.
func buildSSHOptions(server ServerInfo) (string, func()) {
	opts, cleanup := buildSSHOptionsSlice(server)
	return strings.Join(opts, " "), cleanup
}

// buildSSHOptionsSlice returns []string{"-o", "Opt1", "-o", "Opt2"} and a cleanup function
// that removes any temporary key files created during the call.
func buildSSHOptionsSlice(server ServerInfo) ([]string, func()) {
	var result []string
	var tmpFiles []string
	result = append(result, "-o", "StrictHostKeyChecking=no")
	if server.Port > 0 && server.Port != 22 {
		result = append(result, "-o", fmt.Sprintf("Port=%d", server.Port))
	}
	if IsSSHKeyAuth(server.AuthType) && server.PrivateKey != "" {
		tmpFile, err := os.CreateTemp("", "bedrock-deploy-key-*")
		if err == nil {
			tmpFile.WriteString(server.PrivateKey)
			tmpFile.Close()
			os.Chmod(tmpFile.Name(), 0600)
			result = append(result, "-o", "IdentityFile="+tmpFile.Name())
			tmpFiles = append(tmpFiles, tmpFile.Name())
		}
	}
	cleanup := func() {
		for _, f := range tmpFiles {
			os.Remove(f)
		}
	}
	return result, cleanup
}

// runAndLog executes cmd and streams output line by line to logFn
func runAndLog(cmd *exec.Cmd, logFn func(string)) error {
	if logFn == nil {
		logFn = func(string) {}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logFn(scanner.Text())
		}
	}()
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		logFn(scanner.Text())
	}
	return cmd.Wait()
}

func wrapRemoteScript(server ServerInfo, workDir, script string) string {
	trimmedScript := strings.TrimSpace(script)
	if trimmedScript == "" {
		return ""
	}

	remoteDir := normalizeRemotePath(server, workDir)
	if isWindowsServer(server) {
		if remoteDir == "" {
			return fmt.Sprintf("powershell -NoProfile -NonInteractive -Command %s", quoteForPowershell(trimmedScript))
		}
		psScript := fmt.Sprintf("Set-Location -Path %s; %s", quoteForPowershell(remoteDir), trimmedScript)
		return fmt.Sprintf("powershell -NoProfile -NonInteractive -Command %s", quoteForPowershell(psScript))
	}

	if remoteDir == "" {
		return fmt.Sprintf("sh -lc %s", quoteForShell(trimmedScript))
	}
	shScript := fmt.Sprintf("cd %s && %s", quoteForShell(remoteDir), trimmedScript)
	return fmt.Sprintf("sh -lc %s", quoteForShell(shScript))
}

func quoteForShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func quoteForPowershell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func executeAgentScript(ctx context.Context, server ServerInfo, workDir, script string, logFn func(string)) error {
	execURL, err := joinAgentURL(server.AgentURL, "exec")
	if err != nil {
		return err
	}

	payload := map[string]string{
		"script":   script,
		"work_dir": normalizeRemotePath(server, workDir),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, execURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+server.AgentToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("agent exec failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if len(respBody) > 0 && logFn != nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(respBody)), "\n") {
			if line != "" {
				logFn(line)
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agent exec failed: %s", strings.TrimSpace(string(respBody)))
	}
	return nil
}

func joinAgentURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse agent url: %w", err)
	}
	joined, err := url.JoinPath(parsed.String(), path)
	if err != nil {
		return "", fmt.Errorf("join agent url: %w", err)
	}
	return joined, nil
}
