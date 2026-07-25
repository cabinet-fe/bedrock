//go:build contract

package repository_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Contract suite: migrations + CRUD for key CI/CD tables on sqlite (always)
// and postgres/mysql when DSN env is set.
//
//	go test ./internal/cicd/repository/... -tags=contract
//	go test ./internal/platform/db/... -tags=contract
//
// Env:
//
//	BEDROCK_CONTRACT_POSTGRES_DSN — libpq/gorm postgres DSN
//	BEDROCK_CONTRACT_MYSQL_DSN    — go-sql-driver MySQL DSN

func TestContract_CICD_CRUD(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			gdb := openContractDB(t, driver)
			ctx := context.Background()
			if err := migration.Up(ctx, gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("migration.Up(%s): %v", driver, err)
			}
			runCICDCRUD(t, gdb, driver)
		})
	}
}

func openContractDB(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	switch driver {
	case "sqlite":
		cfg := &config.DatabaseConfig{
			Driver: "sqlite",
			Path:   filepath.Join(t.TempDir(), "contract.sqlite"),
		}
		gdb, err := db.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			sqlDB, _ := gdb.DB()
			if sqlDB != nil {
				_ = sqlDB.Close()
			}
		})
		return gdb
	case "postgres":
		dsn := os.Getenv("BEDROCK_CONTRACT_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("BEDROCK_CONTRACT_POSTGRES_DSN not set; skipping postgres contract test")
		}
		gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		sqlDB, err := gdb.DB()
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlDB.Ping(); err != nil {
			t.Skipf("postgres unreachable: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return gdb
	case "mysql":
		dsn := os.Getenv("BEDROCK_CONTRACT_MYSQL_DSN")
		if dsn == "" {
			t.Skip("BEDROCK_CONTRACT_MYSQL_DSN not set; skipping mysql contract test")
		}
		gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatalf("open mysql: %v", err)
		}
		sqlDB, err := gdb.DB()
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlDB.Ping(); err != nil {
			t.Skipf("mysql unreachable: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return gdb
	default:
		t.Fatalf("unknown driver %q", driver)
		return nil
	}
}

func runCICDCRUD(t *testing.T, gdb *gorm.DB, driver string) {
	t.Helper()
	suffix := fmt.Sprintf("%s-%d", driver, time.Now().UnixNano())

	credRepo := resourcerepo.NewCredentialRepository(gdb)
	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	jobRepo := repository.NewBuildJobRepository(gdb)
	runRepo := repository.NewBuildRunRepository(gdb)

	cred := &resourcemodel.Credential{
		Name:         "cred-" + suffix,
		Type:         "token",
		SecretCipher: "cipher-placeholder",
		CreatedBy:    1,
	}
	if err := credRepo.Create(cred); err != nil {
		t.Fatalf("credential create: %v", err)
	}
	gotCred, err := credRepo.FindByID(cred.ID)
	if err != nil || gotCred.Name != cred.Name {
		t.Fatalf("credential find: %+v %v", gotCred, err)
	}

	repo := &resourcemodel.Repository{
		Name:      "repo-" + suffix,
		RepoURL:   "https://example.com/" + suffix + ".git",
		AuthType:  "none",
		CreatedBy: 1,
	}
	if err := repoRepo.Create(repo); err != nil {
		t.Fatalf("repository create: %v", err)
	}
	gotRepo, err := repoRepo.FindByID(repo.ID)
	if err != nil || gotRepo.RepoURL != repo.RepoURL {
		t.Fatalf("repository find: %+v %v", gotRepo, err)
	}

	job := &model.BuildJob{
		RepositoryID:   repo.ID,
		Name:           "job-" + suffix,
		Enabled:        true,
		Branch:         "main",
		BuildScript:    "echo ok",
		TriggerManual:  true,
		ArtifactFormat: "gzip",
		CreatedBy:      1,
	}
	if err := jobRepo.Create(job); err != nil {
		t.Fatalf("build_job create: %v", err)
	}
	gotJob, err := jobRepo.FindByID(job.ID)
	if err != nil || gotJob.RepositoryID != repo.ID {
		t.Fatalf("build_job find: %+v %v", gotJob, err)
	}

	run := &model.BuildRun{
		BuildJobID:          job.ID,
		BuildNumber:         1,
		Status:              "queued",
		Stage:               "pending",
		TriggerType:         "manual",
		Branch:              "main",
		DistributionSummary: "none",
	}
	if err := runRepo.Create(run); err != nil {
		t.Fatalf("build_run create: %v", err)
	}
	gotRun, err := runRepo.FindByID(run.ID)
	if err != nil || gotRun.Status != "queued" {
		t.Fatalf("build_run find: %+v %v", gotRun, err)
	}
	if err := runRepo.UpdateFields(run.ID, map[string]interface{}{
		"status": "success",
		"stage":  "idle",
	}); err != nil {
		t.Fatalf("build_run update: %v", err)
	}
	gotRun, err = runRepo.FindByID(run.ID)
	if err != nil || gotRun.Status != "success" {
		t.Fatalf("build_run after update: %+v %v", gotRun, err)
	}

	// Cleanup in FK-safe order (best-effort for shared postgres/mysql DBs).
	_ = gdb.Delete(&model.BuildRun{}, run.ID).Error
	_ = jobRepo.Delete(job.ID)
	_ = repoRepo.Delete(repo.ID)
	_ = credRepo.Delete(cred.ID)
}
