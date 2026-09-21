package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bedrock/internal/pkg"
)

// A bare harness.bin must be resolved against the completed PATH: the server
// runs under systemd with a minimal PATH, while opencode is typically
// installed per-user (e.g. ~/.bun/bin).
func TestServeEnv_ResolvesBareBinViaCompletedPath(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := pkg.ToolPathDirs
	pkg.ToolPathDirs = []string{".bun/bin"}
	t.Cleanup(func() { pkg.ToolPathDirs = orig })
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin")

	env, resolved := serveEnv("opencode", "pw")
	if resolved != fake {
		t.Fatalf("resolved=%q want %q", resolved, fake)
	}
	if !slices.Contains(env, "OPENCODE_SERVER_PASSWORD=pw") {
		t.Fatalf("password missing from env: %v", env)
	}
	path := envValue(env, "PATH")
	if path == "" || !slices.Contains(filepath.SplitList(path), binDir) {
		t.Fatalf("completed PATH %q missing %q", path, binDir)
	}
}

func TestServeEnv_AbsoluteBinUntouched(t *testing.T) {
	_, resolved := serveEnv("/opt/oc/bin/opencode", "pw")
	if resolved != "/opt/oc/bin/opencode" {
		t.Fatalf("absolute bin must pass through, got %q", resolved)
	}
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			return v
		}
	}
	return ""
}
