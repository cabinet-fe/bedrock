//go:build contract

package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContract_ResourceCliPat_CRUD(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			gdb := openResourceContractDB(t, driver)
			if err := migration.Up(context.Background(), gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("migration.Up(%s): %v", driver, err)
			}
			if err := migration.Up(context.Background(), gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("idempotent migration.Up(%s): %v", driver, err)
			}

			cliRepo := repository.NewCLIRepository(gdb)
			clis, err := cliRepo.List()
			if err != nil || len(clis) != 4 {
				t.Fatalf("seeded CLIs: %v len=%d", err, len(clis))
			}
			cli, err := cliRepo.FindByKey("claude_code")
			if err != nil {
				t.Fatal(err)
			}
			cli.InstalledVersion = "1.0.0-" + driver
			if err := cliRepo.Update(cli); err != nil {
				t.Fatal(err)
			}
			sources, err := cliRepo.ListEnabledSources("claude_code")
			if err != nil || len(sources) == 0 {
				t.Fatalf("seeded sources: %v len=%d", err, len(sources))
			}

			patRepo := repository.NewPATRepository(gdb)
			pat := &model.PersonalAccessToken{
				UserID: 1, Name: "p", TokenPrefix: "br_xxxxxxxx", TokenHash: "hash-" + driver,
				ScopesJSON: `["skills:read"]`,
			}
			if err := patRepo.Create(pat); err != nil {
				t.Fatal(err)
			}
			if _, err := patRepo.FindByHash(pat.TokenHash); err != nil {
				t.Fatal(err)
			}
			if err := patRepo.Delete(pat.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func openResourceContractDB(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	switch driver {
	case "sqlite":
		gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "c.sqlite")})
		if err != nil {
			t.Fatal(err)
		}
		return gdb
	case "postgres":
		dsn := os.Getenv("BEDROCK_CONTRACT_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("BEDROCK_CONTRACT_POSTGRES_DSN not set")
		}
		gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		return gdb
	case "mysql":
		dsn := os.Getenv("BEDROCK_CONTRACT_MYSQL_DSN")
		if dsn == "" {
			t.Skip("BEDROCK_CONTRACT_MYSQL_DSN not set")
		}
		gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		return gdb
	default:
		t.Fatalf("unknown driver %s", driver)
		return nil
	}
}
