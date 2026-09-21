package engine

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// toolPathDirs lists common per-user tool install dirs (home-relative) plus
// absolute dirs that minimal service environments miss. The server is usually
// started by systemd or nohup with a bare PATH, so build scripts would miss
// commands like bun that login shells pick up from rc files.
var toolPathDirs = []string{
	".bun/bin",
	".local/bin",
	".cargo/bin",
	"go/bin",
	".local/share/mise/shims",
	"/opt/homebrew/bin",
	"/usr/local/bin",
}

// resolveHomeDir returns the process user's home directory. systemd services
// often run without HOME set, so fall back to the passwd database.
func resolveHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

// ensurePATH appends missing tool dirs to env["PATH"] in place. Existing
// entries keep priority; only dirs that exist on disk are added. Home-relative
// entries are skipped when home is empty.
func ensurePATH(env map[string]string, home string) {
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(env["PATH"]) {
		seen[dir] = true
	}
	var missing []string
	for _, entry := range toolPathDirs {
		dir := entry
		if !filepath.IsAbs(dir) {
			if home == "" {
				continue
			}
			dir = filepath.Join(home, entry)
		}
		if seen[dir] {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		seen[dir] = true
		missing = append(missing, dir)
	}
	if len(missing) == 0 {
		return
	}
	joined := strings.Join(missing, string(os.PathListSeparator))
	if env["PATH"] == "" {
		env["PATH"] = joined
		return
	}
	env["PATH"] += string(os.PathListSeparator) + joined
}
