package pkg

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// homeDir prefers $HOME; service processes (e.g. systemd units) often have
// no HOME set, so fall back to the current user's home from /etc/passwd.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return home
	}
	if u, userErr := user.Current(); userErr == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return ""
}

// ApplyMisePath prepends mise shims and ~/.local/bin to PATH so dev language
// environments and tools can find their commands.
func ApplyMisePath(cmd *exec.Cmd) {
	home := homeDir()
	path := os.Getenv("PATH")
	if home != "" {
		dataDir := os.Getenv("MISE_DATA_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(home, ".local", "share", "mise")
		}
		path = filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) +
			filepath.Join(dataDir, "shims") + string(os.PathListSeparator) + path
	}
	env := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PATH=") || strings.HasPrefix(item, "HOME=") {
			continue
		}
		env = append(env, item)
	}
	// Inject HOME explicitly: the shell prelude's $HOME (profile and mise paths) relies on it
	if home != "" && runtime.GOOS != "windows" {
		env = append(env, "HOME="+home)
	}
	cmd.Env = append(env, "PATH="+path, "MISE_YES=1")
}

// WrapShellWithProfile makes non-login, non-interactive shells pick up the
// user's configured tools before running a command: it loads the login profile
// (.bash_profile / .profile, the right entry for non-interactive shells), then
// discovers mise and exports a static PATH via activate --shims (no hooks,
// safe for non-interactive use). It does not source .bashrc: distros ship
// `case $- in *i*) ;; *) return;; esac` guards that return before mise
// activate and may pollute stdout.
func WrapShellWithProfile(command string) string {
	if runtime.GOOS == "windows" {
		return command
	}
	prelude := `[ -f "$HOME/.bash_profile" ] && . "$HOME/.bash_profile" >/dev/null 2>&1; ` +
		`[ -f "$HOME/.profile" ] && . "$HOME/.profile" >/dev/null 2>&1; ` +
		`command -v mise >/dev/null 2>&1 || { [ -x "$HOME/.local/bin/mise" ] && export PATH="$HOME/.local/bin:$PATH"; }; ` +
		`command -v mise >/dev/null 2>&1 || { [ -x /usr/local/bin/mise ] && export PATH="/usr/local/bin:$PATH"; }; ` +
		`command -v mise >/dev/null 2>&1 && eval "$(mise activate bash --shims 2>/dev/null)" || export PATH="${MISE_DATA_DIR:-$HOME/.local/share/mise}/shims:$PATH"; `
	return prelude + command
}

// ParseVersionLines extracts version numbers from mise ls-remote / similar
// output, newest first.
func ParseVersionLines(output string, limit int) []string {
	seen := make(map[string]struct{})
	items := make([]string, 0, 32)
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "mise ") || strings.Contains(lower, "error") ||
			strings.Contains(lower, "warn") || strings.Contains(lower, "required") {
			continue
		}
		if field, _, ok := strings.Cut(line, " "); ok {
			line = field
		}
		if line == "" {
			continue
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		items = append(items, line)
	}
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}
