package db

import (
	"os"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
)

func TestOpen_sqlite(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "test.sqlite"),
	}
	gdb, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_rejectsUnsupportedDriver(t *testing.T) {
	_, err := Open(&config.DatabaseConfig{Driver: "oracle"})
	if err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestOpen_rejectsEmptySQLitePath(t *testing.T) {
	// Parent path is a file → MkdirAll fails
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(blocker, "db.sqlite"),
	})
	if err == nil {
		t.Fatal("expected error for invalid sqlite path parent")
	}
}

func TestOpen_postgresBadHostFailsFast(t *testing.T) {
	_, err := Open(&config.DatabaseConfig{
		Driver:   "postgres",
		Host:     "127.0.0.1",
		Port:     1, // closed port
		Name:     "bedrock",
		User:     "bedrock",
		Password: "x",
		SSLMode:  "disable",
	})
	if err == nil {
		t.Fatal("expected connectivity failure")
	}
}

func TestClose_nil(t *testing.T) {
	if err := Close(nil); err != nil {
		t.Fatalf("expected nil error on nil db, got %v", err)
	}
}

func TestReset_sqlite(t *testing.T) {
	dir := t.TempDir()
	dbPath1 := filepath.Join(dir, "app1.sqlite")
	cfg1 := &config.DatabaseConfig{
		Driver: "sqlite",
		Path:   dbPath1,
	}

	gdb, err := Open(cfg1)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Create table and insert in app1
	if err := gdb.Exec("CREATE TABLE test_items (id INT, name TEXT)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := gdb.Exec("INSERT INTO test_items VALUES (1, 'first')").Error; err != nil {
		t.Fatalf("insert item: %v", err)
	}

	// Reset with same config
	if err := Reset(gdb, cfg1); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify query still works on gdb pointer
	var count int64
	if err := gdb.Table("test_items").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected 1 item after reset, got count=%d, err=%v", count, err)
	}

	// Create another database app2
	dbPath2 := filepath.Join(dir, "app2.sqlite")
	cfg2 := &config.DatabaseConfig{
		Driver: "sqlite",
		Path:   dbPath2,
	}
	gdb2, err := Open(cfg2)
	if err != nil {
		t.Fatalf("Open app2: %v", err)
	}
	if err := gdb2.Exec("CREATE TABLE test_items (id INT, name TEXT)").Error; err != nil {
		t.Fatalf("create table in app2: %v", err)
	}
	if err := gdb2.Exec("INSERT INTO test_items VALUES (2, 'second'), (3, 'third')").Error; err != nil {
		t.Fatalf("insert items in app2: %v", err)
	}
	_ = Close(gdb2)

	// Now reset original gdb to cfg2
	if err := Reset(gdb, cfg2); err != nil {
		t.Fatalf("Reset to app2 failed: %v", err)
	}

	if err := gdb.Table("test_items").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("expected 2 items after reset to app2, got count=%d, err=%v", count, err)
	}

	// Clean up
	_ = Close(gdb)
}
