package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func setupProviderTestDB(t *testing.T) (*gorm.DB, *service.ProviderService) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ef", 32)); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "provider_test.sqlite")
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration up: %v", err)
	}

	repo := repository.NewProviderRepository(gdb)
	svc := service.NewProviderService(repo)
	return gdb, svc
}

func TestProviderService_CreateAndGetProvider(t *testing.T) {
	_, svc := setupProviderTestDB(t)

	// Create with API Key
	p, err := svc.CreateProvider(1, model.ProviderInput{
		Name:   "OpenAI",
		APIURL: "https://api.openai.com/v1",
		APIKey: "sk-secret-token-12345",
		Notes:  "official openai",
	})
	if err != nil {
		t.Fatalf("create provider failed: %v", err)
	}
	if p.ID == 0 {
		t.Fatalf("expected non-zero ID")
	}
	if !p.HasAPIKey {
		t.Fatalf("expected has_api_key=true")
	}
	if p.APIKeyCipher != "" {
		t.Fatalf("APIKeyCipher should be hidden/masked")
	}

	// Internal decrypt
	decrypted, err := svc.DecryptAPIKey(p.ID)
	if err != nil {
		t.Fatalf("decrypt apiKey failed: %v", err)
	}
	if decrypted != "sk-secret-token-12345" {
		t.Fatalf("got decrypted %q, want %q", decrypted, "sk-secret-token-12345")
	}

	// Get provider
	got, err := svc.GetProvider(p.ID)
	if err != nil {
		t.Fatalf("get provider failed: %v", err)
	}
	if got.Name != "OpenAI" || got.APIURL != "https://api.openai.com/v1" || !got.HasAPIKey {
		t.Fatalf("unexpected provider data: %+v", got)
	}

	// Duplicate name rejected
	_, err = svc.CreateProvider(1, model.ProviderInput{
		Name:   "OpenAI",
		APIURL: "https://other.com/v1",
	})
	if err == nil {
		t.Fatalf("expected error on duplicate provider name")
	}
}

func TestProviderService_UpdateProvider(t *testing.T) {
	_, svc := setupProviderTestDB(t)

	p, err := svc.CreateProvider(1, model.ProviderInput{
		Name:   "DeepSeek",
		APIURL: "https://api.deepseek.com/v1",
		APIKey: "sk-original-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Update with empty APIKey -> retains old key
	newName := "DeepSeek-Official"
	updated, err := svc.UpdateProvider(p.ID, model.ProviderInput{
		Name:   newName,
		APIKey: "", // empty means retain
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName || !updated.HasAPIKey {
		t.Fatalf("update failed: %+v", updated)
	}
	dec, err := svc.DecryptAPIKey(p.ID)
	if err != nil || dec != "sk-original-key" {
		t.Fatalf("expected retained key, got %q, err: %v", dec, err)
	}

	// Update with new APIKey -> replaces old key
	_, err = svc.UpdateProvider(p.ID, model.ProviderInput{
		APIKey: "sk-new-key-67890",
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err = svc.DecryptAPIKey(p.ID)
	if err != nil || dec != "sk-new-key-67890" {
		t.Fatalf("expected updated key, got %q, err: %v", dec, err)
	}
}

func TestProviderService_ModelCRUDAndCascadeDelete(t *testing.T) {
	_, svc := setupProviderTestDB(t)

	// Create provider
	p, err := svc.CreateProvider(1, model.ProviderInput{
		Name:   "Anthropic-Compatible",
		APIURL: "https://api.anthropic.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Model under non-existent provider fails
	_, err = svc.CreateModel(9999, model.ModelInput{
		Name:    "Model X",
		ModelID: "x",
	})
	if err == nil {
		t.Fatal("expected error for non-existent provider")
	}

	// Model with invalid default_params JSON fails
	_, err = svc.CreateModel(p.ID, model.ModelInput{
		Name:          "Model Invalid",
		ModelID:       "invalid-json",
		DefaultParams: "{not-valid-json",
	})
	if err == nil {
		t.Fatal("expected error for invalid default_params")
	}

	// Model with duplicate reasoning effort value fails
	_, err = svc.CreateModel(p.ID, model.ModelInput{
		Name:    "Model Dup Efforts",
		ModelID: "dup-efforts",
		ReasoningEfforts: []model.ReasoningEffortOption{
			{Value: "low", Label: "低"},
			{Value: "low", Label: "重复低"},
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate reasoning effort")
	}

	// Model creation success
	m1, err := svc.CreateModel(p.ID, model.ModelInput{
		Name:    "DeepSeek-R1",
		ModelID: "deepseek-reasoner",
		ReasoningEfforts: []model.ReasoningEffortOption{
			{Value: "low", Label: "低"},
			{Value: "medium", Label: "中"},
			{Value: "high", Label: "高"},
		},
		DefaultParams: map[string]any{"temperature": 0.6},
		Notes:         "reasoning model",
	})
	if err != nil {
		t.Fatalf("create model failed: %v", err)
	}
	if m1.ID == 0 || m1.Name != "DeepSeek-R1" || len(m1.ReasoningEfforts) != 3 {
		t.Fatalf("unexpected model data: %+v", m1)
	}

	// Duplicate model_id under same provider fails
	_, err = svc.CreateModel(p.ID, model.ModelInput{
		Name:    "DeepSeek-R1 Duplicate",
		ModelID: "deepseek-reasoner",
	})
	if err == nil {
		t.Fatal("expected error on duplicate model_id under same provider")
	}

	// Update model
	updatedM1, err := svc.UpdateModel(m1.ID, model.ModelInput{
		Name:          "DeepSeek-R1 Updated",
		DefaultParams: `{"temperature": 0.7, "max_tokens": 8192}`,
	})
	if err != nil {
		t.Fatalf("update model failed: %v", err)
	}
	if updatedM1.Name != "DeepSeek-R1 Updated" || updatedM1.DefaultParams["max_tokens"] != float64(8192) {
		t.Fatalf("unexpected updated model: %+v", updatedM1)
	}

	// List models by provider
	models, total, err := svc.ListModelsByProvider(p.ID, 1, 10)
	if err != nil {
		t.Fatalf("list models failed: %v", err)
	}
	if total != 1 || len(models) != 1 {
		t.Fatalf("expected 1 model, got %d (total %d)", len(models), total)
	}

	// Delete provider cascades models
	if err := svc.DeleteProvider(p.ID); err != nil {
		t.Fatalf("delete provider failed: %v", err)
	}

	// Verify provider gone
	if _, err := svc.GetProvider(p.ID); err == nil {
		t.Fatal("expected provider to be deleted")
	}
	// Verify model gone
	if _, err := svc.GetModel(m1.ID); err == nil {
		t.Fatal("expected model to be cascade deleted")
	}
}
