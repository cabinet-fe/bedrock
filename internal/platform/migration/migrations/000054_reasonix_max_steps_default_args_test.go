package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
)

func reasonixDefaultArgs(t *testing.T, gdb *gorm.DB) string {
	t.Helper()
	var args string
	if err := gdb.Table("cli_runtime_definitions").
		Where("`key` = ?", "reasonix").
		Select("default_args").Scan(&args).Error; err != nil {
		t.Fatal(err)
	}
	return args
}

func TestUpReasonixMaxStepsDefaultArgs_RewritesSeededValue(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m054.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}
	if got := reasonixDefaultArgs(t, gdb); got != "run --max-steps 400" {
		t.Fatalf("reasonix default_args=%q want=%q", got, "run --max-steps 400")
	}

	// Other CLIs seeded by 000009 must keep their non-interactive subcommand only.
	var opencodeArgs string
	if err := gdb.Table("cli_runtime_definitions").
		Where("`key` = ?", "opencode").
		Select("default_args").Scan(&opencodeArgs).Error; err != nil {
		t.Fatal(err)
	}
	if opencodeArgs != "run" {
		t.Fatalf("opencode default_args=%q want=%q", opencodeArgs, "run")
	}
}

func TestUpReasonixMaxStepsDefaultArgs_PreservesCustomValue(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m054-custom.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}
	if err := gdb.Exec(`
		UPDATE cli_runtime_definitions SET default_args = 'run --max-steps 999'
		WHERE ` + "`key`" + ` = 'reasonix'
	`).Error; err != nil {
		t.Fatal(err)
	}

	if err := upReasonixMaxStepsDefaultArgs(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("upReasonixMaxStepsDefaultArgs: %v", err)
	}
	if got := reasonixDefaultArgs(t, gdb); got != "run --max-steps 999" {
		t.Fatalf("reasonix default_args=%q want customized value preserved", got)
	}
}
