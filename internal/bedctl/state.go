package bedctl

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// State file keys, byte-compatible with the shell install.sh (v1.x) so an
// existing install keeps its directories and mirror after migrating.
const (
	KeyCLIPath   = "CLI_PATH"
	KeyServerDir = "SERVER_DIR"
	KeyAgentDir  = "AGENT_DIR"
	KeyMirror    = "MIRROR"
)

// StateDir returns the state directory: /etc/bedrock for root, ~/.bedrock otherwise.
func StateDir(isRoot bool, home string) string {
	if isRoot {
		return "/etc/bedrock"
	}
	return filepath.Join(home, ".bedrock")
}

// State is the bedctl.env key=value store.
type State struct {
	Path string
}

// LoadState reads the state file; a missing file yields an empty State.
func LoadState(isRoot bool, home string) *State {
	return &State{Path: filepath.Join(StateDir(isRoot, home), "bedctl.env")}
}

// Get returns the first value for key; present reports whether the key line
// exists (an existing-but-empty value is distinct from an absent key).
func (s *State) Get(key string) (val string, present bool) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Set upserts key=value atomically, preserving unrelated lines.
func (s *State) Set(key, val string) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	var lines []string
	if data, err := os.ReadFile(s.Path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line != "" && !strings.HasPrefix(line, key+"=") {
				lines = append(lines, line)
			}
		}
	}
	lines = append(lines, key+"="+val)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// HomeDir resolves the current user's home, falling back to the passwd database
// like the shell installer did when HOME is unset (cron/systemd contexts).
func HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return ""
}

// IsRoot reports whether the process runs as uid 0.
func IsRoot() bool { return os.Geteuid() == 0 }

// DefaultDir returns the remembered install dir for a component or the
// platform default: /opt/bedrock[-agent] for root, ~/bedrock[-agent] otherwise.
func DefaultDir(state *State, comp Component, isRoot bool, home string) string {
	key := KeyServerDir
	suffix := ""
	if comp == Agent {
		key = KeyAgentDir
		suffix = "-agent"
	}
	if val, ok := state.Get(key); ok && val != "" {
		return val
	}
	if isRoot {
		return "/opt/bedrock" + suffix
	}
	return filepath.Join(home, "bedrock"+suffix)
}

// CLIPath returns where the bedctl binary itself is installed.
func CLIPath(state *State, isRoot bool, home string) string {
	if dest, ok := state.Get(KeyCLIPath); ok && dest != "" {
		return dest
	}
	if isRoot {
		return "/usr/local/bin/bedctl"
	}
	return filepath.Join(home, ".local", "bin", "bedctl")
}
