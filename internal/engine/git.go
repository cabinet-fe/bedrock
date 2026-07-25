package engine

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitCloneOrPull(ctx context.Context, workDir, repoURL, authType, username, password, branch string, logFn func(string)) error {
	authURL := buildAuthURL(repoURL, authType, username, password)

	if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
		logFn("Cloning repository...")
		os.MkdirAll(filepath.Dir(workDir), 0755)
		return runGit(ctx, "", logFn, "clone", "--branch", branch, authURL, workDir)
	}

	// Remove stale lock files that may remain from a previous crashed build
	cleanGitLockFiles(workDir, logFn)

	// 凭证或仓库地址变更后，工作区 .git/config 里仍可能缓存旧的 origin URL
	if err := syncGitRemoteOrigin(ctx, workDir, authURL, logFn); err != nil {
		return err
	}

	logFn("Fetching updates...")
	if err := runGit(ctx, workDir, logFn, "fetch", "origin"); err != nil {
		return err
	}
	logFn("Checking out branch: " + branch)
	if err := runGit(ctx, workDir, logFn, "checkout", branch); err != nil {
		// Branch might not exist locally
		runGit(ctx, workDir, logFn, "checkout", "-b", branch, "origin/"+branch)
	}
	if err := runGit(ctx, workDir, logFn, "reset", "--hard", "origin/"+branch); err != nil {
		return err
	}
	logFn("Cleaning workspace (preserving dependency caches)...")
	runGit(ctx, workDir, logFn, "clean", "-fd",
		"-e", "node_modules", "-e", "vendor", "-e", ".gradle",
		"-e", "target", "-e", "__pycache__", "-e", ".venv",
		"-e", "venv", "-e", ".tox")
	return nil
}

func syncGitRemoteOrigin(ctx context.Context, workDir, remoteURL string, logFn func(string)) error {
	logFn("Syncing remote credentials...")
	return runGit(ctx, workDir, logFn, "remote", "set-url", "origin", remoteURL)
}

var gitLockFiles = []string{
	".git/index.lock",
	".git/shallow.lock",
	".git/refs/heads/*.lock",
	".git/HEAD.lock",
}

func cleanGitLockFiles(workDir string, logFn func(string)) {
	for _, pattern := range gitLockFiles {
		matches, _ := filepath.Glob(filepath.Join(workDir, pattern))
		for _, f := range matches {
			if err := os.Remove(f); err == nil {
				logFn("[git] Removed stale lock file: " + filepath.Base(f))
			}
		}
	}
}

func runGit(ctx context.Context, dir string, logFn func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
			logFn("[git] " + line)
		}
	}
	return err
}

func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func buildAuthURL(repoURL, authType, username, password string) string {
	if authType == "none" || (username == "" && password == "") {
		return repoURL
	}
	u, err := url.Parse(repoURL)
	if err != nil {
		return repoURL
	}
	if authType == "token" {
		platform := DetectPlatform(repoURL)
		u = platform.BuildTokenAuthURL(u, username, password)
	} else {
		u.User = url.UserPassword(username, password)
	}
	return u.String()
}

// GitListBranches returns remote branch names via git ls-remote --heads.
func GitListBranches(repoURL, authType, username, password string) ([]string, error) {
	authURL := buildAuthURL(repoURL, authType, username, password)
	cmd := exec.Command("git", "ls-remote", "--heads", authURL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote failed: %w", err)
	}
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ref := parts[1]
			branch := strings.TrimPrefix(ref, "refs/heads/")
			branches = append(branches, branch)
		}
	}
	return branches, nil
}
