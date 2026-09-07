package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"bedrock/internal/platform/config"
)

// Open creates a GORM connection for the configured driver and fails fast on ping.
// Changing driver does not migrate data between engines (2.0 fresh install only).
func Open(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config is nil")
	}

	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening database (%s): %w", cfg.Driver, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting underlying db: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetimeDuration())

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("database connectivity check failed (%s): %w", cfg.Driver, err)
	}

	return db, nil
}

// Close safely closes the underlying sql.DB connection pool of db.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Reset safely closes the old connection pool of target (if open) and reinitializes
// target with a new connection pool according to cfg, preserving existing pointers to target.
func Reset(target *gorm.DB, cfg *config.DatabaseConfig) error {
	if target == nil {
		return fmt.Errorf("target database is nil")
	}
	if cfg == nil {
		return fmt.Errorf("database config is nil")
	}

	// Safely close the old sql.DB if present
	if oldSQLDB, err := target.DB(); err == nil && oldSQLDB != nil {
		_ = oldSQLDB.Close()
	}

	newDB, err := Open(cfg)
	if err != nil {
		return fmt.Errorf("reopening database: %w", err)
	}

	newSQLDB, err := newDB.DB()
	if err != nil {
		return fmt.Errorf("getting new sql db: %w", err)
	}

	target.Config.ConnPool = newSQLDB
	target.Config.Dialector = newDB.Config.Dialector
	target.ConnPool = newSQLDB
	if target.Statement != nil {
		target.Statement.ConnPool = newSQLDB
	}
	target.Error = nil
	return nil
}

func dialectorFor(cfg *config.DatabaseConfig) (gorm.Dialector, error) {
	switch cfg.Driver {
	case "sqlite":
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
		dsn := cfg.Path + "?_journal_mode=WAL&_busy_timeout=5000"
		return sqlite.Open(dsn), nil
	case "postgres":
		ssl := cfg.SSLMode
		if ssl == "" {
			ssl = "disable"
		}
		port := cfg.Port
		if port == 0 {
			port = 5432
		}
		dsn := fmt.Sprintf(
			"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
			cfg.Host, cfg.User, cfg.Password, cfg.Name, port, ssl,
		)
		return postgres.Open(dsn), nil
	case "mysql":
		port := cfg.Port
		if port == 0 {
			port = 3306
		}
		dsn := fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.User, cfg.Password, cfg.Host, port, cfg.Name,
		)
		return mysql.Open(dsn), nil
	default:
		return nil, fmt.Errorf("unsupported database.driver %q (want sqlite|postgres|mysql)", cfg.Driver)
	}
}
