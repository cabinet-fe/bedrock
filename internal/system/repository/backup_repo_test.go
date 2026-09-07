package repository_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "test_repo.sqlite"),
	})
	if err != nil {
		t.Fatalf("Open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close(gdb)
	})

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}
	return gdb
}

func TestBackupRepository_CRUD(t *testing.T) {
	gdb := setupTestDB(t)
	repo := repository.NewBackupRepository(gdb)

	now := time.Now().UTC().Truncate(time.Second)
	backup := &model.SystemBackup{
		Filename:  "backup_20260906_120000.zip",
		FilePath:  "/data/backups/backup_20260906_120000.zip",
		FileSize:  1024,
		Modules:   []string{model.BackupModuleDatabase, model.BackupModuleConfig},
		Note:      "initial test backup",
		Status:    model.BackupStatusProcessing,
		CreatedBy: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// 1. Create
	if err := repo.Create(backup); err != nil {
		t.Fatalf("Create backup failed: %v", err)
	}
	if backup.ID == 0 {
		t.Fatalf("expected non-zero ID after create")
	}

	// 2. FindByID
	found, err := repo.FindByID(backup.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if found.Filename != backup.Filename {
		t.Errorf("expected filename %s, got %s", backup.Filename, found.Filename)
	}
	if len(found.Modules) != 2 || found.Modules[0] != model.BackupModuleDatabase || found.Modules[1] != model.BackupModuleConfig {
		t.Errorf("expected decoded modules [database config], got %#v", found.Modules)
	}

	// 3. FindByFilename
	foundByName, err := repo.FindByFilename("backup_20260906_120000.zip")
	if err != nil {
		t.Fatalf("FindByFilename failed: %v", err)
	}
	if foundByName.ID != backup.ID {
		t.Errorf("expected ID %d, got %d", backup.ID, foundByName.ID)
	}

	// 4. UpdateStatus
	if err := repo.UpdateStatus(backup.ID, model.BackupStatusFailed, "disk full"); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	found, _ = repo.FindByID(backup.ID)
	if found.Status != model.BackupStatusFailed || found.ErrorMessage != "disk full" {
		t.Errorf("UpdateStatus not reflected: status=%s, err=%s", found.Status, found.ErrorMessage)
	}

	// 5. UpdateSuccess
	if err := repo.UpdateSuccess(backup.ID, 2048); err != nil {
		t.Fatalf("UpdateSuccess failed: %v", err)
	}
	found, _ = repo.FindByID(backup.ID)
	if found.Status != model.BackupStatusSuccess || found.FileSize != 2048 || found.ErrorMessage != "" {
		t.Errorf("UpdateSuccess not reflected: status=%s, size=%d, err=%s", found.Status, found.FileSize, found.ErrorMessage)
	}

	// 6. UpdateFailed
	if err := repo.UpdateFailed(backup.ID, "some failure"); err != nil {
		t.Fatalf("UpdateFailed failed: %v", err)
	}
	found, _ = repo.FindByID(backup.ID)
	if found.Status != model.BackupStatusFailed || found.ErrorMessage != "some failure" {
		t.Errorf("UpdateFailed not reflected: status=%s, err=%s", found.Status, found.ErrorMessage)
	}

	// 7. UpdateFileSize
	if err := repo.UpdateFileSize(backup.ID, 4096); err != nil {
		t.Fatalf("UpdateFileSize failed: %v", err)
	}
	found, _ = repo.FindByID(backup.ID)
	if found.FileSize != 4096 {
		t.Errorf("UpdateFileSize not reflected: size=%d", found.FileSize)
	}

	// 8. Update (full model)
	found.Note = "updated note"
	found.Modules = []string{model.BackupModuleStorage}
	if err := repo.Update(found); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	found, _ = repo.FindByID(backup.ID)
	if found.Note != "updated note" || len(found.Modules) != 1 || found.Modules[0] != model.BackupModuleStorage {
		t.Errorf("Update not reflected: note=%s, modules=%#v", found.Note, found.Modules)
	}

	// 9. Delete
	if err := repo.Delete(backup.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = repo.FindByID(backup.ID)
	if err == nil {
		t.Errorf("expected error after delete, got nil")
	}
}

func TestBackupRepository_List(t *testing.T) {
	gdb := setupTestDB(t)
	repo := repository.NewBackupRepository(gdb)

	baseTime := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		b := &model.SystemBackup{
			Filename:  "backup_" + string(rune('0'+i)) + ".zip",
			FilePath:  "/data/backup.zip",
			FileSize:  int64(i * 100),
			Modules:   []string{model.BackupModuleDatabase},
			Status:    model.BackupStatusSuccess,
			CreatedBy: 1,
			CreatedAt: baseTime.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(b); err != nil {
			t.Fatalf("Create backup %d failed: %v", i, err)
		}
	}

	// Page 1, page size 2
	items, total, err := repo.List(pkg.ListQuery{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 1 failed: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// By default ordered by created_at DESC, so items[0] should be backup 5
	if items[0].Filename != "backup_5.zip" {
		t.Errorf("expected first item to be backup_5.zip, got %s", items[0].Filename)
	}
	if items[1].Filename != "backup_4.zip" {
		t.Errorf("expected second item to be backup_4.zip, got %s", items[1].Filename)
	}

	// Page 3, page size 2 -> should have 1 item (backup 1)
	items3, total3, err := repo.List(pkg.ListQuery{Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 3 failed: %v", err)
	}
	if total3 != 5 {
		t.Errorf("expected total 5, got %d", total3)
	}
	if len(items3) != 1 || items3[0].Filename != "backup_1.zip" {
		t.Errorf("expected items3 to have backup_1.zip, got %#v", items3)
	}
}
