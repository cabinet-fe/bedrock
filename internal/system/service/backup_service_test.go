package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	authmodel "bedrock/internal/auth/model"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

type mockUserFinder struct {
	users map[uint]*authmodel.User
}

func (m *mockUserFinder) FindByID(id uint) (*authmodel.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrBackupNotFound
	}
	return u, nil
}

type serviceTestEnv struct {
	rootDir    string
	dbPath     string
	configPath string
	storageDir string
	backupDir  string
	cfg        *config.Config
	svc        *BackupService
	repo       *repository.BackupRepository
	engine     *BackupEngine
	userFinder *mockUserFinder
}

func setupServiceTestEnv(t *testing.T) *serviceTestEnv {
	t.Helper()
	root := t.TempDir()

	dbPath := filepath.Join(root, "data", "app.sqlite")
	configPath := filepath.Join(root, "config.yaml")
	storageDir := filepath.Join(root, "data", "storage")
	backupDir := filepath.Join(storageDir, "backups")

	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	_ = os.MkdirAll(storageDir, 0o755)
	_ = os.MkdirAll(backupDir, 0o755)

	if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
		t.Fatalf("writing mock config: %v", err)
	}

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			Path:   dbPath,
		},
		Storage: config.StorageConfig{
			Root: storageDir,
		},
	}

	gdb, err := db.Open(&cfg.Database)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close(gdb)
	})

	if err := migration.Up(context.Background(), gdb, "sqlite"); err != nil {
		t.Fatalf("migration: %v", err)
	}

	repo := repository.NewBackupRepository(gdb)
	engine := NewBackupEngine(BackupEngineOptions{
		DB:         gdb,
		Config:     cfg,
		AppVersion: "1.0.0-test",
		ConfigPath: configPath,
		BackupDir:  backupDir,
	})

	pwdHash, err := pkg.HashPassword("correct-admin-password")
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}

	userFinder := &mockUserFinder{
		users: map[uint]*authmodel.User{
			1: {
				ID:           1,
				Username:     "admin",
				PasswordHash: pwdHash,
				IsActive:     true,
				IsSuperAdmin: true,
			},
		},
	}

	svc := NewBackupService(repo, engine, userFinder)

	return &serviceTestEnv{
		rootDir:    root,
		dbPath:     dbPath,
		configPath: configPath,
		storageDir: storageDir,
		backupDir:  backupDir,
		cfg:        cfg,
		svc:        svc,
		repo:       repo,
		engine:     engine,
		userFinder: userFinder,
	}
}

func TestBackupService_CreateBackup(t *testing.T) {
	env := setupServiceTestEnv(t)

	// 1. Validation errors
	_, err := env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{},
	})
	if err == nil {
		t.Fatalf("expected error for empty modules")
	}

	_, err = env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{"invalid_mod"},
	})
	if err == nil {
		t.Fatalf("expected error for invalid module")
	}

	// 2. Successful creation
	created, err := env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{model.BackupModuleDatabase, model.BackupModuleConfig},
		Note:    "test backup note",
	})
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected valid backup id, got 0")
	}
	if created.Status != model.BackupStatusSuccess {
		t.Fatalf("expected status %q, got %q", model.BackupStatusSuccess, created.Status)
	}
	if created.FileSize <= 0 {
		t.Fatalf("expected positive file size, got %d", created.FileSize)
	}
	if _, err := os.Stat(created.FilePath); err != nil {
		t.Fatalf("backup zip not created on disk: %v", err)
	}

	// 3. List
	items, total, err := env.svc.List(pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 backup item, got total=%d len=%d", total, len(items))
	}
	if len(items[0].Modules) != 2 {
		t.Fatalf("expected 2 modules, got %v", items[0].Modules)
	}
}

func TestBackupService_DownloadAndDelete(t *testing.T) {
	env := setupServiceTestEnv(t)

	created, err := env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{model.BackupModuleConfig},
		Note:    "download and delete test",
	})
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}

	// Download path
	filePath, filename, err := env.svc.GetDownloadPath(created.ID)
	if err != nil {
		t.Fatalf("get download path failed: %v", err)
	}
	if filePath != created.FilePath || filename != created.Filename {
		t.Fatalf("mismatch in download path or filename")
	}

	// Non-existent
	_, _, err = env.svc.GetDownloadPath(99999)
	if err != ErrBackupNotFound {
		t.Fatalf("expected ErrBackupNotFound, got %v", err)
	}

	// Delete
	if err := env.svc.DeleteBackup(created.ID); err != nil {
		t.Fatalf("delete backup failed: %v", err)
	}

	// Check file is removed
	if _, err := os.Stat(created.FilePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted from disk, but stat returned %v", err)
	}

	// Check record is removed
	_, err = env.repo.FindByID(created.ID)
	if err == nil {
		t.Fatalf("expected record to be deleted from database")
	}
}

func TestBackupService_Restore_PasswordAndAutoSnapshot(t *testing.T) {
	env := setupServiceTestEnv(t)

	// Create a backup first
	created, err := env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{model.BackupModuleConfig},
		Note:    "pre restore test",
	})
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}

	// 1. Password verification failure
	_, err = env.svc.Restore(1, model.SystemBackupRestoreRequest{
		BackupID:      &created.ID,
		AdminPassword: "wrong-password",
	})
	if err != ErrInvalidAdminPassword {
		t.Fatalf("expected ErrInvalidAdminPassword, got %v", err)
	}

	// 2. Missing targets
	_, err = env.svc.Restore(1, model.SystemBackupRestoreRequest{
		AdminPassword: "correct-admin-password",
	})
	if err == nil {
		t.Fatalf("expected error for missing backup_id and upload_token")
	}

	// 3. Successful restore with auto snapshot
	resp, err := env.svc.Restore(1, model.SystemBackupRestoreRequest{
		BackupID:      &created.ID,
		AdminPassword: "correct-admin-password",
	})
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true, got false")
	}

	// Verify auto snapshot was created and recorded
	items, total, err := env.svc.List(pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 backups (original + auto snapshot), got %d", total)
	}

	var foundSnapshot bool
	for _, it := range items {
		if it.Note == "还原前自动快照" {
			foundSnapshot = true
			if _, err := os.Stat(it.FilePath); err != nil {
				t.Fatalf("snapshot file does not exist on disk: %v", err)
			}
		}
	}
	if !foundSnapshot {
		t.Fatalf("pre-restore auto snapshot record not found in repository")
	}
}

func TestBackupService_InspectAndRestoreUpload(t *testing.T) {
	env := setupServiceTestEnv(t)

	// Create a backup to get a valid zip
	created, err := env.svc.CreateBackup(1, model.SystemBackupCreateRequest{
		Modules: []string{model.BackupModuleConfig},
	})
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}

	file, err := os.Open(created.FilePath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer file.Close()

	stat, _ := file.Stat()
	inspectRes, err := env.svc.InspectUploadedBackup(created.Filename, file, stat.Size())
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}

	if inspectRes.UploadToken == "" {
		t.Fatalf("expected non-empty upload token")
	}
	if !inspectRes.Compatible {
		t.Fatalf("expected compatible true, message=%s", inspectRes.Message)
	}

	// Restore using uploaded token
	autoSnap := false
	resp, err := env.svc.Restore(1, model.SystemBackupRestoreRequest{
		UploadToken:   inspectRes.UploadToken,
		AdminPassword: "correct-admin-password",
		AutoSnapshot:  &autoSnap,
	})
	if err != nil {
		t.Fatalf("restore from upload failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true")
	}
}
