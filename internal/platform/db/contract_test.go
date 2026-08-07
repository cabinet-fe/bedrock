//go:build contract

package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dashboardmodel "bedrock/internal/dashboard/model"
	dashboardrepo "bedrock/internal/dashboard/repository"
	opsmodel "bedrock/internal/ops/model"
	opsrepo "bedrock/internal/ops/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"
	storagemodel "bedrock/internal/storage/model"
	storagerepo "bedrock/internal/storage/repository"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Three-driver migration + thin CI/CD CRUD contract (see .agents/be.md).
//
//	go test ./internal/platform/db/... -tags=contract
//
// Postgres/MySQL skip with a clear message when DSN env is unset:
//
//	BEDROCK_CONTRACT_POSTGRES_DSN
//	BEDROCK_CONTRACT_MYSQL_DSN

func TestContract_MigrationsAndCICDTables(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			gdb := openDriver(t, driver)
			if err := migration.Up(context.Background(), gdb, migration.Driver(driver)); err != nil {
				t.Fatalf("migration.Up: %v", err)
			}
			for _, table := range []string{
				"repositories", "build_jobs", "build_runs", "credentials",
				"deploy_targets", "build_deploy_attempts", "dashboard_layouts",
				"dev_environments", "dev_env_install_sources", "dev_env_jobs", "schema_migrations",
				"storage_objects", "product_projects", "project_members", "requirements",
				"requirement_comments", "requirement_attachments", "api_doc_nodes", "dev_doc_nodes",
				"menu_groups", "rbac_resources", "roles",
			} {
				if !gdb.Migrator().HasTable(table) {
					t.Fatalf("missing table %s on %s", table, driver)
				}
			}

			suffix := fmt.Sprintf("%s-%d", driver, time.Now().UnixNano())
			credRepo := resourcerepo.NewCredentialRepository(gdb)
			repoRepo := resourcerepo.NewRepositoryRepository(gdb)
			dashboardRepo := dashboardrepo.NewDashboardRepository(gdb)
			opsRepo := opsrepo.NewOpsRepository(gdb)
			projectRepo := projectrepo.NewProjectRepository(gdb)
			storageRepo := storagerepo.NewStorageRepository(gdb)
			cred := &resourcemodel.Credential{Name: "db-contract-" + suffix, Type: "token", SecretCipher: "x", CreatedBy: 99}
			if err := credRepo.Create(cred); err != nil {
				t.Fatalf("create credential: %v", err)
			}
			repo := &resourcemodel.Repository{
				Name: "db-contract-repo-" + suffix, RepoURL: "https://example.com/" + suffix + ".git",
				AuthType: "none", CreatedBy: 99,
			}
			if err := repoRepo.Create(repo); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			layout := &dashboardmodel.Layout{UserID: uint(time.Now().UnixNano() & 0x7fffffff), CardsJSON: `[]`}
			if err := dashboardRepo.CreateLayout(layout); err != nil {
				t.Fatalf("create dashboard layout: %v", err)
			}
			env := &opsmodel.DevEnvironment{
				Name: "db-contract-env-" + suffix, Kind: "custom", Executable: "true", CreatedBy: 99,
			}
			if err := opsRepo.CreateEnvironment(env); err != nil {
				t.Fatalf("create dev environment: %v", err)
			}
			source := &opsmodel.DevEnvInstallSource{
				EnvironmentID: env.ID, Name: "db-contract-source-" + suffix,
				BaseURL: "https://example.com", Priority: 999, Enabled: true,
			}
			if err := opsRepo.CreateSource(source); err != nil {
				t.Fatalf("create install source: %v", err)
			}
			job := &opsmodel.DevEnvJob{
				EnvironmentID: env.ID, Operation: "install", Status: "queued", CreatedBy: 99,
			}
			if err := opsRepo.CreateJob(job); err != nil {
				t.Fatalf("create install job: %v", err)
			}
			object := &storagemodel.StorageObject{
				Kind: "attachment", SHA256: fmt.Sprintf("%064x", time.Now().UnixNano()),
				Size: 1, ContentType: "text/plain", Path: "objects/aa/contract", RefCount: 1, CreatedBy: 99,
			}
			if err := storageRepo.Create(object); err != nil {
				t.Fatalf("create storage object: %v", err)
			}
			project := &projectmodel.ProductProject{
				Name: "db-contract-project-" + suffix, Slug: "db-contract-" + strings.ReplaceAll(suffix, "-", ""),
				Status: "active", OwnerID: 99, CreatedBy: 99,
			}
			if err := projectRepo.CreateProjectWithOwner(project); err != nil {
				t.Fatalf("create product project: %v", err)
			}
			requirement := &projectmodel.Requirement{
				ProjectID: project.ID, Title: "contract", Status: "backlog", Priority: "normal", CreatedBy: 99, UpdatedBy: 99,
			}
			if err := projectRepo.CreateRequirement(requirement); err != nil {
				t.Fatalf("create requirement: %v", err)
			}
			doc := &projectmodel.ApiDocNode{ProjectID: project.ID, Kind: "doc", Name: "contract.md", CreatedBy: 99, UpdatedBy: 99}
			if err := projectRepo.CreateDocNode(doc); err != nil {
				t.Fatalf("create api doc node: %v", err)
			}
			devDoc := &projectmodel.DevDocNode{ProjectID: project.ID, Kind: "doc", Name: "dev.md", CreatedBy: 99, UpdatedBy: 99}
			if err := projectRepo.CreateDevDocNode(devDoc); err != nil {
				t.Fatalf("create dev doc node: %v", err)
			}
			for _, table := range []string{"build_jobs", "script_jobs", "build_pipelines"} {
				if !gdb.Migrator().HasColumn(table, "project_id") {
					t.Fatalf("%s.project_id missing on %s", table, driver)
				}
			}
			if gdb.Migrator().HasColumn("ai_agents", "project_id") {
				t.Fatalf("ai_agents.project_id should be removed on %s", driver)
			}
			for _, check := range []struct {
				model any
				index string
			}{
				{projectIDIndexBuildJobs{}, "idx_build_jobs_project_id"},
				{projectIDIndexScriptJobs{}, "idx_script_jobs_project_id"},
				{projectIDIndexBuildPipelines{}, "idx_build_pipelines_project_id"},
			} {
				if !gdb.Migrator().HasIndex(check.model, check.index) {
					t.Fatalf("%s missing on %s", check.index, driver)
				}
			}
			if gdb.Migrator().HasIndex(projectIDIndexAIAgents{}, "idx_ai_agents_project_id") {
				t.Fatalf("idx_ai_agents_project_id should be removed on %s", driver)
			}
			_ = repoRepo.Delete(repo.ID)
			_ = credRepo.Delete(cred.ID)
		})
	}
}

func openDriver(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	switch driver {
	case "sqlite":
		gdb, err := db.Open(&config.DatabaseConfig{
			Driver: "sqlite",
			Path:   filepath.Join(t.TempDir(), "contract.sqlite"),
		})
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
		t.Fatalf("unknown driver %s", driver)
		return nil
	}
}

type projectIDIndexBuildJobs struct {
	ProjectID *uint `gorm:"index:idx_build_jobs_project_id"`
}

func (projectIDIndexBuildJobs) TableName() string { return "build_jobs" }

type projectIDIndexScriptJobs struct {
	ProjectID *uint `gorm:"index:idx_script_jobs_project_id"`
}

func (projectIDIndexScriptJobs) TableName() string { return "script_jobs" }

type projectIDIndexBuildPipelines struct {
	ProjectID *uint `gorm:"index:idx_build_pipelines_project_id"`
}

func (projectIDIndexBuildPipelines) TableName() string { return "build_pipelines" }

type projectIDIndexAIAgents struct {
	ProjectID *uint `gorm:"index:idx_ai_agents_project_id"`
}

func (projectIDIndexAIAgents) TableName() string { return "ai_agents" }
