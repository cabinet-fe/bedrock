package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/pkg"
)

func setupHarnessConfig(t *testing.T) (*HarnessConfigService, *ProviderService, string) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "hcfg.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	providers := NewProviderService(repository.NewProviderRepository(gdb))
	root := t.TempDir()
	return NewHarnessConfigService(providers, root, nil), providers, root
}

func seedHarnessProvider(t *testing.T, providers *ProviderService, key string) (uint, string) {
	t.Helper()
	p, err := providers.CreateProvider(1, model.ProviderInput{
		Name: "DeepSeek", APIURL: "https://api.deepseek.com/v1", APIKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := providers.CreateModel(p.ID, model.ModelInput{
		Name: "Chat", ModelID: "deepseek-chat",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := providers.CreateModel(p.ID, model.ModelInput{
		Name: "Reasoner", ModelID: "deepseek-reasoner",
		ReasoningEfforts: []model.ReasoningEffortOption{
			{Value: "low", Label: "低"}, {Value: "high", Label: "高"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return p.ID, "bedrock-p" + strconv.FormatUint(uint64(p.ID), 10)
}

func TestHarnessConfigRefreshAndEnsure(t *testing.T) {
	svc, providers, root := setupHarnessConfig(t)

	// No providers: refresh removes stale configs.
	stale := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(); err != nil {
		t.Fatalf("refresh empty: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale config must be removed when no providers exist")
	}
	if p, m := svc.DefaultModelRef(); p != "" || m != "" {
		t.Fatalf("default model ref = %q/%q, want empty", p, m)
	}

	id, key := seedHarnessProvider(t, providers, "sk-test")
	_ = id

	if err := svc.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if p, m := svc.DefaultModelRef(); p != key || m != "deepseek-chat" {
		t.Fatalf("default model ref = %q/%q", p, m)
	}
	got := readHarnessConfig(t, filepath.Join(root, "opencode.json"))
	for _, want := range []string{
		`"` + key + `"`,
		`"@ai-sdk/openai-compatible"`,
		`"baseURL": "https://api.deepseek.com/v1"`,
		`"apiKey": "sk-test"`,
		`"model": "` + key + `/deepseek-chat"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %s:\n%s", want, got)
		}
	}
	info, err := os.Stat(filepath.Join(root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}

	// Chat-style directories get the same config (per-location load).
	dir := t.TempDir()
	if err := svc.EnsureDirectoryConfig(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if again := readHarnessConfig(t, filepath.Join(dir, "opencode.json")); again != got {
		t.Fatalf("directory config differs from root config:\n%s\n---\n%s", again, got)
	}

	// Agent workspaces apply the agent's effort to its model via request options.
	agentDir := t.TempDir()
	if err := svc.EnsureAgentDirectoryConfig(agentDir, key, "deepseek-reasoner", "high"); err != nil {
		t.Fatalf("ensure agent dir: %v", err)
	}
	agentCfg := readHarnessConfig(t, filepath.Join(agentDir, "opencode.json"))
	if !strings.Contains(agentCfg, `"reasoningEffort": "high"`) {
		t.Fatalf("agent config missing model reasoning effort:\n%s", agentCfg)
	}
	if got := readHarnessConfig(t, filepath.Join(root, "opencode.json")); got != agentCfg && strings.Contains(got, "reasoningEffort") {
		t.Fatal("root config must stay effort-free")
	}

	// Deleting the provider prunes the config file.
	if err := providers.DeleteProvider(id); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("config must be removed after provider deletion")
	}
}

// TestHarnessConfigProviderWithoutKey omits apiKey but keeps the provider.
func TestHarnessConfigProviderWithoutKey(t *testing.T) {
	svc, providers, root := setupHarnessConfig(t)
	p, err := providers.CreateProvider(1, model.ProviderInput{
		Name: "本地网关", APIURL: "http://127.0.0.1:8000/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := providers.CreateModel(p.ID, model.ModelInput{Name: "M", ModelID: "m"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(); err != nil {
		t.Fatal(err)
	}
	got := readHarnessConfig(t, filepath.Join(root, "opencode.json"))
	if strings.Contains(got, "apiKey") {
		t.Fatalf("keyless provider must omit apiKey:\n%s", got)
	}
	if !strings.Contains(got, `"baseURL": "http://127.0.0.1:8000/v1"`) {
		t.Fatalf("baseURL missing:\n%s", got)
	}
}

func readHarnessConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}
