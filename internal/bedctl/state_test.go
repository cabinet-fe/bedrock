package bedctl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateSetGet(t *testing.T) {
	s := &State{Path: filepath.Join(t.TempDir(), "bedctl.env")}
	if _, ok := s.Get(KeyServerDir); ok {
		t.Fatal("empty state should not report SERVER_DIR present")
	}
	if err := s.Set(KeyServerDir, "/opt/bedrock"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(KeyMirror, "https://gh-proxy.com/"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get(KeyServerDir); !ok || v != "/opt/bedrock" {
		t.Fatalf("Get SERVER_DIR = %q,%v", v, ok)
	}
	// Overwrite keeps the other key.
	if err := s.Set(KeyServerDir, "/opt/bedrock2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(KeyServerDir); v != "/opt/bedrock2" {
		t.Fatalf("overwrite failed: %q", v)
	}
	if v, _ := s.Get(KeyMirror); v != "https://gh-proxy.com/" {
		t.Fatalf("other key lost: %q", v)
	}
	// Empty value is a recorded decision (present, distinct from absent).
	if err := s.Set(KeyMirror, ""); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get(KeyMirror); !ok || v != "" {
		t.Fatalf("empty-value persistence broken: %q,%v", v, ok)
	}
}

func TestShellCompatStateFile(t *testing.T) {
	// A file written by install.sh v1.x (KEY=VALUE lines) must be readable.
	dir := t.TempDir()
	content := "CLI_PATH=/usr/local/bin/bedctl\nSERVER_DIR=/opt/bedrock\nMIRROR=https://ghfast.top/\n"
	if err := os.WriteFile(filepath.Join(dir, "bedctl.env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &State{Path: filepath.Join(dir, "bedctl.env")}
	if v, _ := s.Get(KeyCLIPath); v != "/usr/local/bin/bedctl" {
		t.Fatalf("CLI_PATH = %q", v)
	}
	if v, _ := s.Get(KeyMirror); v != "https://ghfast.top/" {
		t.Fatalf("MIRROR = %q", v)
	}
}

func TestDefaultDirAndCLIPath(t *testing.T) {
	s := &State{Path: filepath.Join(t.TempDir(), "bedctl.env")}
	if got := DefaultDir(s, Server, true, "/root"); got != "/opt/bedrock" {
		t.Fatalf("root server default = %q", got)
	}
	if got := DefaultDir(s, Agent, false, "/home/u"); got != "/home/u/bedrock-agent" {
		t.Fatalf("user agent default = %q", got)
	}
	_ = s.Set(KeyServerDir, "/custom")
	if got := DefaultDir(s, Server, true, "/root"); got != "/custom" {
		t.Fatalf("remembered dir = %q", got)
	}
	if got := CLIPath(s, false, "/home/u"); got != "/home/u/.local/bin/bedctl" {
		t.Fatalf("user CLI path = %q", got)
	}
}
