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
