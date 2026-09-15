//go:build contract

package migrations_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Three-driver contract for migration 000055 (harness session columns):
//
//	go test ./internal/platform/migration/migrations/... -tags=contract
//
// Postgres/MySQL skip with a clear message when DSN env is unset:
//
//	BEDROCK_CONTRACT_POSTGRES_DSN
//	BEDROCK_CONTRACT_MYSQL_DSN
func TestContract_Migration000055HarnessSessions(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			gdb := openContractDB(t, driver)
			if err := migration.Up(context.Background(), gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("migration.Up(%s): %v", driver, err)
			}
			if err := migration.Up(context.Background(), gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("idempotent migration.Up(%s): %v", driver, err)
			}

			for _, column := range []string{"harness_session_id", "harness_session_status", "final_output"} {
				if !gdb.Migrator().HasColumn("agent_runs", column) {
					t.Fatalf("agent_runs.%s missing on %s", column, driver)
				}
			}
			if !gdb.Migrator().HasIndex("agent_runs", "uidx_agent_runs_harness_session_id") {
				t.Fatalf("uidx_agent_runs_harness_session_id missing on %s", driver)
			}
			for _, column := range []string{"model_provider", "model_id", "approval_mode"} {
				if !gdb.Migrator().HasColumn("ai_agents", column) {
					t.Fatalf("ai_agents.%s missing on %s", column, driver)
				}
			}
			// Legacy columns must survive: cli_key retained (deprecated), old runs unread.
			for _, legacy := range []struct{ table, column string }{
				{"ai_agents", "cli_key"},
				{"agent_runs", "output_text"},
			} {
				if !gdb.Migrator().HasColumn(legacy.table, legacy.column) {
					t.Fatalf("%s.%s legacy column missing on %s", legacy.table, legacy.column, driver)
				}
			}

			insertRun := func(sessionID any) error {
				return gdb.Exec(
					"INSERT INTO agent_runs (agent_id, trigger_type, status, harness_session_id) VALUES (1, 'manual', 'queued', ?)",
					sessionID,
				).Error
			}
			if err := insertRun(nil); err != nil {
				t.Fatalf("legacy run with NULL session id on %s: %v", driver, err)
			}
			if err := insertRun(nil); err != nil {
				t.Fatalf("second NULL session id must be allowed on %s: %v", driver, err)
			}
			if err := insertRun("ses_contract_dup"); err != nil {
				t.Fatalf("insert run with session id on %s: %v", driver, err)
			}
			if err := insertRun("ses_contract_dup"); err == nil {
				t.Fatalf("duplicate harness_session_id must be rejected on %s", driver)
			}
		})
	}
}

func openContractDB(t *testing.T, driver string) *gorm.DB {
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
