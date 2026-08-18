package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyMisePath 把 mise shims 与 ~/.local/bin 放到 PATH 前面，供开发语言环境发现工具。
func ApplyMisePath(cmd *exec.Cmd) {
	home, err := os.UserHomeDir()
	path := os.Getenv("PATH")
	if err == nil {
		dataDir := os.Getenv("MISE_DATA_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(home, ".local", "share", "mise")
		}
		path = filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) +
			filepath.Join(dataDir, "shims") + string(os.PathListSeparator) + path
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PATH=") {
			continue
		}
		env = append(env, item)
	}
	cmd.Env = append(env, "PATH="+path, "MISE_YES=1")
}

// ParseVersionLines 从 mise ls-remote / 同类输出中取出版本号，最新的在前。
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
