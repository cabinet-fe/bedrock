package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
)

func TestUpAgentInjectDefaultPrompt(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "058.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	if !gdb.Migrator().HasColumn(&aiAgentInjectDefaultPromptModel{}, "InjectDefaultPrompt") {
		t.Fatal("expected inject_default_prompt column")
	}
	if err := upAgentInjectDefaultPrompt(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
