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
