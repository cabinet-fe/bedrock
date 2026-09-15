package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_sqliteDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	content := `
server:
  port: 8080
database:
  driver: sqlite
  path: "./data/db.sqlite"
jwt:
  secret: "test-secret"
encryption:
  key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
admin:
  username: admin
  password: admin123
build:
  workspace_dir: "./data/workspaces"
  artifact_dir: "./data/artifacts"
  log_dir: "./data/logs"
  cache_dir: "./data/caches"
`
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("driver=%q", cfg.Database.Driver)
	}
	if cfg.Database.Path != filepath.Join(tmpDir, "data", "db.sqlite") {
		t.Fatalf("path=%q", cfg.Database.Path)
	}
	if cfg.Dsh.Enabled {
		t.Fatal("dsh.enabled should be false when dsh section is omitted")
	}
	if cfg.Dsh.Bin != "dsh" || cfg.Dsh.Port != 17800 || cfg.Dsh.MaxSessions != 64 {
		t.Fatalf("dsh defaults: bin=%q port=%d max_sessions=%d", cfg.Dsh.Bin, cfg.Dsh.Port, cfg.Dsh.MaxSessions)
	}
	if cfg.Dsh.StartupTimeout != "60s" || cfg.Dsh.HealthInterval != "10s" || cfg.Dsh.PendingTTL != "10m" || cfg.Dsh.SessionIdleTTL != "72h" {
		t.Fatalf("dsh duration defaults: %+v", cfg.Dsh)
	}
	if !cfg.Dsh.AutoRestart || cfg.Dsh.ApprovalMode != "manual" || cfg.Dsh.LogDir != "" {
		t.Fatalf("dsh other defaults: %+v", cfg.Dsh)
	}
	if cfg.Harness.Enabled {
		t.Fatal("harness.enabled should be false when harness section is omitted")
	}
	if cfg.Harness.Backend != "opencode" || cfg.Harness.Bin != "opencode" || cfg.Harness.Port != 4096 || cfg.Harness.ApprovalMode != "manual" {
		t.Fatalf("harness defaults: %+v", cfg.Harness)
	}
}

func TestLoad_rejectsBadDriver(t *testing.T) {
	tmpDir := t.TempDir()
	content := `
server:
  port: 8080
database:
  driver: oracle
  path: "./data/db.sqlite"
jwt:
  secret: "test-secret"
encryption:
  key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
`
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_postgresRequiresHost(t *testing.T) {
	tmpDir := t.TempDir()
	content := `
database:
  driver: postgres
  name: bedrock
  user: bedrock
jwt:
  secret: "test-secret"
encryption:
  key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
`
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestLoad_dshEnabledReadsKeys(t *testing.T) {
	dir, path := writeConfig(t, `
dsh:
  enabled: true
  bin: "/usr/local/bin/dsh"
  home: "./data/dsh"
  workspace_root: "./data/dsh-workspaces"
  port: 17800
  startup_timeout: "60s"
  health_interval: "10s"
  auto_restart: true
  approval_mode: "manual"
  pending_ttl: "10m"
  session_idle_ttl: "72h"
  log_dir: ""
  max_sessions: 64
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Dsh.Enabled {
		t.Fatal("enabled")
	}
	if cfg.Dsh.Bin != "/usr/local/bin/dsh" {
		t.Fatalf("bin=%q", cfg.Dsh.Bin)
	}
	if cfg.Dsh.Home != filepath.Join(dir, "data", "dsh") {
		t.Fatalf("home=%q", cfg.Dsh.Home)
	}
	if cfg.Dsh.WorkspaceRoot != filepath.Join(dir, "data", "dsh-workspaces") {
		t.Fatalf("workspace_root=%q", cfg.Dsh.WorkspaceRoot)
	}
	if cfg.Dsh.Port != 17800 || cfg.Dsh.MaxSessions != 64 {
		t.Fatalf("port=%d max_sessions=%d", cfg.Dsh.Port, cfg.Dsh.MaxSessions)
	}
	if cfg.Dsh.StartupTimeout != "60s" || cfg.Dsh.HealthInterval != "10s" || cfg.Dsh.PendingTTL != "10m" || cfg.Dsh.SessionIdleTTL != "72h" {
		t.Fatalf("durations: %+v", cfg.Dsh)
	}
	if !cfg.Dsh.AutoRestart || cfg.Dsh.ApprovalMode != "manual" || cfg.Dsh.LogDir != "" {
		t.Fatalf("other: %+v", cfg.Dsh)
	}
}

func TestLoad_dshEnvOverrides(t *testing.T) {
	_, path := writeConfig(t, `
dsh:
  enabled: false
  port: 17800
  max_sessions: 64
`)
	t.Setenv("BEDROCK_DSH_ENABLED", "true")
	t.Setenv("BEDROCK_DSH_PORT", "17900")
	t.Setenv("BEDROCK_DSH_MAX_SESSIONS", "32")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Dsh.Enabled {
		t.Fatal("BEDROCK_DSH_ENABLED should override enabled")
	}
	if cfg.Dsh.Port != 17900 {
		t.Fatalf("port=%d", cfg.Dsh.Port)
	}
	if cfg.Dsh.MaxSessions != 32 {
		t.Fatalf("max_sessions=%d", cfg.Dsh.MaxSessions)
	}
}

func TestLoad_dshInvalidDuration(t *testing.T) {
	_, path := writeConfig(t, `
dsh:
  enabled: false
  startup_timeout: "not-a-duration"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestLoad_harnessEnabledReadsKeys(t *testing.T) {
	_, path := writeConfig(t, `
harness:
  enabled: true
  backend: "opencode"
  bin: "/usr/local/bin/opencode"
  port: 14096
  approval_mode: "auto"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Harness.Enabled || cfg.Harness.Backend != "opencode" || cfg.Harness.Bin != "/usr/local/bin/opencode" ||
		cfg.Harness.Port != 14096 || cfg.Harness.ApprovalMode != "auto" {
		t.Fatalf("harness: %+v", cfg.Harness)
	}
}

func TestLoad_harnessEnvOverrides(t *testing.T) {
	_, path := writeConfig(t, `
harness:
  enabled: false
  port: 4096
`)
	t.Setenv("BEDROCK_HARNESS_ENABLED", "true")
	t.Setenv("BEDROCK_HARNESS_PORT", "14097")
	t.Setenv("BEDROCK_HARNESS_BIN", "/opt/opencode/bin/opencode")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Harness.Enabled || cfg.Harness.Port != 14097 || cfg.Harness.Bin != "/opt/opencode/bin/opencode" {
		t.Fatalf("harness env overrides: %+v", cfg.Harness)
	}
}

func TestLoad_harnessInvalidSettings(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"backend", "harness:\n  enabled: true\n  backend: \"dsh\"\n"},
		{"approval_mode", "harness:\n  enabled: true\n  approval_mode: \"always\"\n"},
		{"port", "harness:\n  enabled: true\n  port: 70000\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, path := writeConfig(t, tt.yaml)
			if _, err := Load(path); err == nil {
				t.Fatal("expected invalid harness setting error")
			}
		})
	}
}

func writeConfig(t *testing.T, extra string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "config.yaml")
	content := `
database:
  driver: sqlite
  path: "./data/db.sqlite"
jwt:
  secret: "test-secret"
encryption:
  key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
` + extra
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}
