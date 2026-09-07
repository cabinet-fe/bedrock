package service_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/system/model"
	"bedrock/internal/system/service"

	"gorm.io/gorm"
)

type engineTestEnv struct {
	rootDir     string
	dbPath      string
	configPath  string
	storageDir  string
	artifactDir string
	logDir      string
	backupDir   string
	cfg         *config.Config
	gdb         *gorm.DB
	engine      *service.BackupEngine
}

func setupEngineTestEnv(t *testing.T) *engineTestEnv {
	t.Helper()
	root := t.TempDir()

	dbPath := filepath.Join(root, "data", "test_app.sqlite")
	configPath := filepath.Join(root, "config.yaml")
	storageDir := filepath.Join(root, "data", "storage")
	artifactDir := filepath.Join(root, "data", "artifacts")
	logDir := filepath.Join(root, "data", "logs")
	backupDir := filepath.Join(storageDir, "backups")

	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	_ = os.MkdirAll(storageDir, 0o755)
	_ = os.MkdirAll(artifactDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)
	_ = os.MkdirAll(backupDir, 0o755)

	// Write mock config file
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
		Build: config.BuildConfig{
			ArtifactDir: artifactDir,
			LogDir:      logDir,
		},
	}

	gdb, err := db.Open(&cfg.Database)
	if err != nil {
		t.Fatalf("Open test db: %v", err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	engine := service.NewBackupEngine(service.BackupEngineOptions{
		DB:         gdb,
		Config:     cfg,
		AppVersion: "1.0.0-test",
		ConfigPath: configPath,
		BackupDir:  backupDir,
	})

	t.Cleanup(func() {
		_ = db.Close(gdb)
	})

	return &engineTestEnv{
		rootDir:     root,
		dbPath:      dbPath,
		configPath:  configPath,
		storageDir:  storageDir,
		artifactDir: artifactDir,
		logDir:      logDir,
		backupDir:   backupDir,
		cfg:         cfg,
		gdb:         gdb,
		engine:      engine,
	}
}

func TestBackupEngine_PackAndInspect(t *testing.T) {
	env := setupEngineTestEnv(t)

	// Add a sample file to storage, artifact, and log
	_ = os.WriteFile(filepath.Join(env.storageDir, "hello.txt"), []byte("storage hello"), 0o644)
	_ = os.WriteFile(filepath.Join(env.artifactDir, "build.tar.gz"), []byte("fake artifact"), 0o644)
	_ = os.WriteFile(filepath.Join(env.logDir, "build.log"), []byte("build step 1 completed"), 0o644)

	// Pack all modules
	modules := []string{
		model.BackupModuleDatabase,
		model.BackupModuleConfig,
		model.BackupModuleStorage,
		model.BackupModuleArtifacts,
		model.BackupModuleLogs,
	}

	archivePath, filename, manifest, fileSize, err := env.engine.CreateBackupArchive(modules, "unit test full backup")
	if err != nil {
		t.Fatalf("CreateBackupArchive failed: %v", err)
	}
	if filename == "" || fileSize <= 0 {
		t.Fatalf("invalid archive meta: filename=%s, size=%d", filename, fileSize)
	}
	if manifest == nil || manifest.Version != model.BackupManifestVersion {
		t.Fatalf("invalid manifest: %#v", manifest)
	}

	// Inspect the created archive
	inspected, err := env.engine.Inspect(archivePath)
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if inspected.DBDriver != "sqlite" {
		t.Errorf("expected db_driver sqlite, got %s", inspected.DBDriver)
	}
	if len(inspected.Modules) != len(modules) {
		t.Errorf("expected %d modules, got %d", len(modules), len(inspected.Modules))
	}
	if inspected.Note != "unit test full backup" {
		t.Errorf("expected note 'unit test full backup', got %s", inspected.Note)
	}

	// InspectPackage
	pkgResult, err := env.engine.InspectPackage(archivePath, "test-token-123")
	if err != nil {
		t.Fatalf("InspectPackage failed: %v", err)
	}
	if !pkgResult.Compatible || pkgResult.UploadToken != "test-token-123" {
		t.Errorf("InspectPackage unexpected: compatible=%v, token=%s, msg=%s",
			pkgResult.Compatible, pkgResult.UploadToken, pkgResult.Message)
	}
}

func TestBackupEngine_InspectRejectsInvalidArchives(t *testing.T) {
	env := setupEngineTestEnv(t)

	// 1. Non-existent file
	_, err := env.engine.Inspect(filepath.Join(env.rootDir, "non_existent.zip"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}

	// 2. Corrupt non-zip file
	badZip := filepath.Join(env.rootDir, "corrupt.zip")
	_ = os.WriteFile(badZip, []byte("this is not a zip"), 0o644)
	_, err = env.engine.Inspect(badZip)
	if err == nil {
		t.Error("expected error for corrupt zip file")
	}

	// 3. Zip without manifest.json
	noManifestZip := filepath.Join(env.rootDir, "no_manifest.zip")
	f, err := os.Create(noManifestZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("data.txt")
	_, _ = w.Write([]byte("some data"))
	_ = zw.Close()
	_ = f.Close()

	_, err = env.engine.Inspect(noManifestZip)
	if err == nil || !strings.Contains(err.Error(), "missing manifest.json") {
		t.Errorf("expected missing manifest error, got %v", err)
	}
}

func TestBackupEngine_DriverMismatchInterception(t *testing.T) {
	env := setupEngineTestEnv(t)

	// Construct manifest with mysql driver
	mysqlManifest := &model.BackupManifest{
		Version:   model.BackupManifestVersion,
		DBDriver:  "mysql",
		Modules:   []string{model.BackupModuleDatabase},
		CreatedAt: time.Now().UTC(),
	}

	compatible, reason := env.engine.CheckCompatibility(mysqlManifest)
	if compatible {
		t.Error("expected incompatible for mysql on sqlite environment")
	}
	if !strings.Contains(reason, "mismatch") {
		t.Errorf("expected mismatch in reason, got %s", reason)
	}
}

func TestBackupEngine_SQLiteSnapshotAndRestore(t *testing.T) {
	env := setupEngineTestEnv(t)

	// Create test table and record before backup
	if err := env.gdb.Exec("CREATE TABLE backup_test_records (id INT PRIMARY KEY, val TEXT)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := env.gdb.Exec("INSERT INTO backup_test_records VALUES (1, 'original')").Error; err != nil {
		t.Fatalf("insert record: %v", err)
	}

	// Write config and storage file
	_ = os.WriteFile(env.configPath, []byte("version: 1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(env.storageDir, "file1.txt"), []byte("version 1 content"), 0o644)

	// Backup database, config, and storage
	archivePath, _, _, _, err := env.engine.CreateBackupArchive([]string{
		model.BackupModuleDatabase,
		model.BackupModuleConfig,
		model.BackupModuleStorage,
	}, "snapshot test")
	if err != nil {
		t.Fatalf("CreateBackupArchive failed: %v", err)
	}

	// Now modify the live state:
	// 1. Insert extra record and mutate existing record in database
	if err := env.gdb.Exec("INSERT INTO backup_test_records VALUES (2, 'mutated')").Error; err != nil {
		t.Fatalf("insert mutated: %v", err)
	}
	if err := env.gdb.Exec("UPDATE backup_test_records SET val = 'changed' WHERE id = 1").Error; err != nil {
		t.Fatalf("update record: %v", err)
	}
	// 2. Overwrite config and storage file
	_ = os.WriteFile(env.configPath, []byte("version: 2_modified\n"), 0o644)
	_ = os.WriteFile(filepath.Join(env.storageDir, "file1.txt"), []byte("modified content"), 0o644)
	_ = os.WriteFile(filepath.Join(env.storageDir, "extra.txt"), []byte("unwanted file"), 0o644)

	// Execute full restore
	if err := env.engine.Restore(archivePath); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify database was restored to original snapshot
	var count int64
	if err := env.gdb.Table("backup_test_records").Count(&count).Error; err != nil {
		t.Fatalf("count after restore: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 record after restore, got %d", count)
	}

	var val string
	if err := env.gdb.Table("backup_test_records").Where("id = ?", 1).Pluck("val", &val).Error; err != nil {
		t.Fatalf("query val: %v", err)
	}
	if val != "original" {
		t.Errorf("expected val 'original', got %s", val)
	}

	// Verify database connection pool continues to accept writes after restore
	if err := env.gdb.Exec("INSERT INTO backup_test_records VALUES (3, 'post_restore')").Error; err != nil {
		t.Fatalf("post restore insert failed: %v", err)
	}

	// Verify config and storage restored
	cfgBytes, _ := os.ReadFile(env.configPath)
	if string(cfgBytes) != "version: 1\n" {
		t.Errorf("expected config restored to version: 1, got %s", string(cfgBytes))
	}

	storageBytes, _ := os.ReadFile(filepath.Join(env.storageDir, "file1.txt"))
	if string(storageBytes) != "version 1 content" {
		t.Errorf("expected storage file restored, got %s", string(storageBytes))
	}
}

func TestBackupEngine_AutoSnapshot(t *testing.T) {
	env := setupEngineTestEnv(t)

	archivePath, filename, manifest, size, err := env.engine.CreateAutoSnapshot()
	if err != nil {
		t.Fatalf("CreateAutoSnapshot failed: %v", err)
	}
	if archivePath == "" || filename == "" || size <= 0 {
		t.Fatalf("invalid auto snapshot result: path=%s, name=%s, size=%d", archivePath, filename, size)
	}
	if manifest.Note != "pre-restore auto snapshot" {
		t.Errorf("unexpected note: %s", manifest.Note)
	}
	if len(manifest.Modules) != len(model.AllBackupModules) {
		t.Errorf("expected all %d modules in auto snapshot, got %d", len(model.AllBackupModules), len(manifest.Modules))
	}
}

func TestBackupEngine_ZipSlipProtection(t *testing.T) {
	env := setupEngineTestEnv(t)

	// Create a malicious zip with ../../../evil.txt
	maliciousZip := filepath.Join(env.rootDir, "malicious.zip")
	f, err := os.Create(maliciousZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)

	// Add valid manifest
	mw, _ := zw.Create("manifest.json")
	m := model.BackupManifest{
		Version:  model.BackupManifestVersion,
		DBDriver: "sqlite",
		Modules:  []string{model.BackupModuleStorage},
	}
	mBytes, _ := json.MarshalIndent(m, "", "  ")
	_, _ = mw.Write(mBytes)

	// Add malicious path in storage/
	badEntry, _ := zw.Create("storage/../../evil.txt")
	_, _ = badEntry.Write([]byte("malicious content"))

	_ = zw.Close()
	_ = f.Close()

	// Attempt restore
	err = env.engine.Restore(maliciousZip)
	if err == nil {
		t.Fatal("expected Restore to fail due to Zip Slip vulnerability")
	}
	if !strings.Contains(err.Error(), "illegal zip path") && !strings.Contains(err.Error(), "escaping") {
		t.Errorf("expected illegal zip path error, got %v", err)
	}
}
