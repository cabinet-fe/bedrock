package migration

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

// Driver is the canonical DB driver name (sqlite|postgres|mysql).
type Driver string

// Migration is a versioned schema change.
type Migration struct {
	Version string
	Up      func(ctx context.Context, db *gorm.DB, driver Driver) error
}

var registry []Migration

// Register adds a migration. Versions must be unique; applied in lexicographic order.
func Register(version string, up func(ctx context.Context, db *gorm.DB, driver Driver) error) {
	if version == "" {
		panic("migration version must not be empty")
	}
	if up == nil {
		panic("migration up must not be nil")
	}
	for _, m := range registry {
		if m.Version == version {
			panic(fmt.Sprintf("duplicate migration version %q", version))
		}
	}
	registry = append(registry, Migration{Version: version, Up: up})
}

// Registered returns a copy of registered migrations sorted by version.
func Registered() []Migration {
	out := make([]Migration, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TIMESTAMP NOT NULL
)
`

type schemaMigrationRow struct {
	Version   string    `gorm:"column:version;primaryKey"`
	AppliedAt time.Time `gorm:"column:applied_at"`
}

func (schemaMigrationRow) TableName() string { return "schema_migrations" }

// EnsureSchemaMigrationsTable creates the tracking table if missing.
func EnsureSchemaMigrationsTable(db *gorm.DB) error {
	return db.Exec(schemaMigrationsDDL).Error
}

// AppliedVersions returns versions already recorded.
func AppliedVersions(db *gorm.DB) (map[string]struct{}, error) {
	var rows []schemaMigrationRow
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[r.Version] = struct{}{}
	}
	return out, nil
}

// Up runs all pending migrations in version order. Idempotent when re-run.
func Up(ctx context.Context, db *gorm.DB, driver Driver) error {
	if err := EnsureSchemaMigrationsTable(db); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	applied, err := AppliedVersions(db)
	if err != nil {
		return fmt.Errorf("list applied migrations: %w", err)
	}

	for _, m := range Registered() {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := m.Up(ctx, tx, driver); err != nil {
				return err
			}
			row := schemaMigrationRow{Version: m.Version, AppliedAt: time.Now().UTC()}
			return tx.Create(&row).Error
		})
		if err != nil {
			return fmt.Errorf("migration %s failed: %w", m.Version, err)
		}
	}
	return nil
}
