package service

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/system/model"

	"gorm.io/gorm"
)

// BackupEngine handles low-level archive packing, manifest generation,
// archive validation, and full restoration across database and file modules.
type BackupEngine struct {
	db         *gorm.DB
	cfg        *config.Config
	appVersion string
	configPath string
	backupDir  string
}

// BackupEngineOptions provides configuration options for BackupEngine.
type BackupEngineOptions struct {
	DB         *gorm.DB
	Config     *config.Config
	AppVersion string
	ConfigPath string
	BackupDir  string
}

// NewBackupEngine constructs a new BackupEngine.
func NewBackupEngine(opts BackupEngineOptions) *BackupEngine {
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = "config.yaml"
	}
	backupDir := opts.BackupDir
	if backupDir == "" && opts.Config != nil {
		backupDir = filepath.Join(opts.Config.Storage.Root, "backups")
	}
	appVersion := opts.AppVersion
	if appVersion == "" {
		appVersion = "dev"
	}
	return &BackupEngine{
		db:         opts.DB,
		cfg:        opts.Config,
		appVersion: appVersion,
		configPath: configPath,
		backupDir:  backupDir,
	}
}

// SetDB allows updating the DB instance if needed.
func (e *BackupEngine) SetDB(db *gorm.DB) {
	e.db = db
}

// BackupDir returns the directory where backups are stored.
func (e *BackupEngine) BackupDir() string {
	return e.backupDir
}

// CreateBackupArchive packs selected modules into a new zip archive in backupDir.
func (e *BackupEngine) CreateBackupArchive(modules []string, note string) (string, string, *model.BackupManifest, int64, error) {
	if len(modules) == 0 {
		return "", "", nil, 0, fmt.Errorf("no backup modules specified")
	}
	for _, m := range modules {
		if !model.IsValidBackupModule(m) {
			return "", "", nil, 0, fmt.Errorf("invalid backup module: %s", m)
		}
	}

	if err := os.MkdirAll(e.backupDir, 0o755); err != nil {
		return "", "", nil, 0, fmt.Errorf("creating backup directory: %w", err)
	}

	filename := fmt.Sprintf("backup_%s_%04d.zip", time.Now().UTC().Format("20060102_150405"), rand.Intn(10000))
	targetPath := filepath.Join(e.backupDir, filename)

	manifest, fileSize, err := e.Pack(targetPath, modules, note)
	if err != nil {
		_ = os.Remove(targetPath)
		return "", "", nil, 0, err
	}

	return targetPath, filename, manifest, fileSize, nil
}

// CreateAutoSnapshot generates a pre-restore emergency snapshot of all available modules.
func (e *BackupEngine) CreateAutoSnapshot() (string, string, *model.BackupManifest, int64, error) {
	return e.CreateBackupArchive(model.AllBackupModules, "pre-restore auto snapshot")
}

// Pack creates a zip archive at destZipPath containing the requested modules and manifest.json.
func (e *BackupEngine) Pack(destZipPath string, modules []string, note string) (*model.BackupManifest, int64, error) {
	if err := os.MkdirAll(filepath.Dir(destZipPath), 0o755); err != nil {
		return nil, 0, fmt.Errorf("creating destination directory: %w", err)
	}

	tmpZipPath := destZipPath + ".tmp"
	zipFile, err := os.Create(tmpZipPath)
	if err != nil {
		return nil, 0, fmt.Errorf("creating zip file: %w", err)
	}

	zw := zip.NewWriter(zipFile)

	manifest := &model.BackupManifest{
		Version:    model.BackupManifestVersion,
		AppVersion: e.appVersion,
		DBDriver:   e.cfg.Database.Driver,
		Modules:    modules,
		CreatedAt:  time.Now().UTC(),
		Note:       note,
	}

	// 1. Write manifest.json to zip root
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("marshaling manifest: %w", err)
	}
	manifestWriter, err := zw.Create(model.BackupManifestFilename)
	if err != nil {
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("writing manifest to zip: %w", err)
	}
	if _, err := manifestWriter.Write(manifestBytes); err != nil {
		_ = zw.Close()
		_ = zipFile.Close()
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("writing manifest content: %w", err)
	}

	// 2. Pack each requested module
	hasModule := func(name string) bool {
		for _, m := range modules {
			if m == name {
				return true
			}
		}
		return false
	}

	if hasModule(model.BackupModuleDatabase) {
		if err := e.packDatabase(zw); err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			_ = os.Remove(tmpZipPath)
			return nil, 0, fmt.Errorf("packing database: %w", err)
		}
	}

	if hasModule(model.BackupModuleConfig) {
		if err := e.packConfig(zw); err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			_ = os.Remove(tmpZipPath)
			return nil, 0, fmt.Errorf("packing config: %w", err)
		}
	}

	if hasModule(model.BackupModuleStorage) {
		if err := e.packStorage(zw); err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			_ = os.Remove(tmpZipPath)
			return nil, 0, fmt.Errorf("packing storage: %w", err)
		}
	}

	if hasModule(model.BackupModuleArtifacts) {
		if err := e.packArtifacts(zw); err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			_ = os.Remove(tmpZipPath)
			return nil, 0, fmt.Errorf("packing artifacts: %w", err)
		}
	}

	if hasModule(model.BackupModuleLogs) {
		if err := e.packLogs(zw); err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			_ = os.Remove(tmpZipPath)
			return nil, 0, fmt.Errorf("packing logs: %w", err)
		}
	}

	if err := zw.Close(); err != nil {
		_ = zipFile.Close()
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("closing zip writer: %w", err)
	}

	stat, err := zipFile.Stat()
	_ = zipFile.Close()
	if err != nil {
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("getting file stat: %w", err)
	}

	if err := os.Rename(tmpZipPath, destZipPath); err != nil {
		_ = os.Remove(tmpZipPath)
		return nil, 0, fmt.Errorf("finalizing backup archive: %w", err)
	}

	return manifest, stat.Size(), nil
}

// packDatabase archives database snapshot (SQLite physical file or MySQL/Postgres SQL dump).
func (e *BackupEngine) packDatabase(zw *zip.Writer) error {
	switch e.cfg.Database.Driver {
	case "sqlite":
		tmpSnapshot := filepath.Join(os.TempDir(), fmt.Sprintf("sqlite_snap_%d.db", time.Now().UnixNano()))
		defer os.Remove(tmpSnapshot)

		if err := e.snapshotSQLite(tmpSnapshot); err != nil {
			return fmt.Errorf("sqlite snapshot: %w", err)
		}
		return addFileToZip(zw, tmpSnapshot, "database/db.sqlite")

	case "mysql", "postgres":
		entry, err := zw.Create("database/dump.sql")
		if err != nil {
			return err
		}
		return e.dumpDatabaseSQL(entry)

	default:
		return fmt.Errorf("unsupported database driver %q", e.cfg.Database.Driver)
	}
}

// snapshotSQLite creates a consistent physical snapshot of the active SQLite database.
func (e *BackupEngine) snapshotSQLite(destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	// Try VACUUM INTO for modern SQLite
	err := e.db.Exec("VACUUM INTO ?", destPath).Error
	if err == nil {
		return nil
	}

	// Fallback: WAL checkpoint truncate and direct copy
	_ = e.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
	return copyFile(e.cfg.Database.Path, destPath)
}

// packConfig packages the config.yaml file if it exists.
func (e *BackupEngine) packConfig(zw *zip.Writer) error {
	if e.configPath == "" {
		return nil
	}
	if _, err := os.Stat(e.configPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return addFileToZip(zw, e.configPath, "config/config.yaml")
}

// packStorage packages the storage root directory, explicitly skipping backups subdirectory.
func (e *BackupEngine) packStorage(zw *zip.Writer) error {
	if e.cfg == nil || e.cfg.Storage.Root == "" {
		return nil
	}
	if _, err := os.Stat(e.cfg.Storage.Root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return addDirToZip(zw, e.cfg.Storage.Root, "storage", "backups")
}

// packArtifacts packages the artifacts directory.
func (e *BackupEngine) packArtifacts(zw *zip.Writer) error {
	if e.cfg == nil || e.cfg.Build.ArtifactDir == "" {
		return nil
	}
	if _, err := os.Stat(e.cfg.Build.ArtifactDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return addDirToZip(zw, e.cfg.Build.ArtifactDir, "artifacts", "")
}

// packLogs packages the build log directory.
func (e *BackupEngine) packLogs(zw *zip.Writer) error {
	if e.cfg == nil || e.cfg.Build.LogDir == "" {
		return nil
	}
	if _, err := os.Stat(e.cfg.Build.LogDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return addDirToZip(zw, e.cfg.Build.LogDir, "logs", "")
}

// Inspect opens a backup archive and parses manifest.json.
func (e *BackupEngine) Inspect(zipPath string) (*model.BackupManifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("opening zip file: %w", err)
	}
	defer zr.Close()

	var manifestFile *zip.File
	for _, f := range zr.File {
		if filepath.Clean(f.Name) == model.BackupManifestFilename {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		return nil, fmt.Errorf("invalid backup archive: missing manifest.json")
	}

	rc, err := manifestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("reading manifest.json: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading manifest data: %w", err)
	}

	var manifest model.BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest.json: %w", err)
	}

	if manifest.Version == "" {
		return nil, fmt.Errorf("invalid manifest: missing version")
	}
	if len(manifest.Modules) == 0 {
		return nil, fmt.Errorf("invalid manifest: modules list is empty")
	}
	for _, m := range manifest.Modules {
		if !model.IsValidBackupModule(m) {
			return nil, fmt.Errorf("invalid module %q in manifest", m)
		}
	}

	return &manifest, nil
}

// CheckCompatibility verifies whether the backup manifest is compatible with current environment.
func (e *BackupEngine) CheckCompatibility(manifest *model.BackupManifest) (bool, string) {
	if manifest == nil {
		return false, "manifest is nil"
	}

	hasDB := false
	for _, m := range manifest.Modules {
		if m == model.BackupModuleDatabase {
			hasDB = true
			break
		}
	}

	if hasDB && e.cfg != nil && e.cfg.Database.Driver != "" {
		if manifest.DBDriver != e.cfg.Database.Driver {
			return false, fmt.Sprintf("database driver mismatch: backup is %q, current system is %q",
				manifest.DBDriver, e.cfg.Database.Driver)
		}
	}

	return true, ""
}

// InspectPackage wraps Inspect and CheckCompatibility for upload pre-inspection results.
func (e *BackupEngine) InspectPackage(zipPath, uploadToken string) (*model.BackupInspectResult, error) {
	manifest, err := e.Inspect(zipPath)
	if err != nil {
		return &model.BackupInspectResult{
			UploadToken: uploadToken,
			Compatible:  false,
			Message:     err.Error(),
		}, nil
	}

	compatible, reason := e.CheckCompatibility(manifest)
	return &model.BackupInspectResult{
		UploadToken: uploadToken,
		Manifest:    manifest,
		Compatible:  compatible,
		Message:     reason,
	}, nil
}

// Restore restores all modules present in the backup archive.
func (e *BackupEngine) Restore(zipPath string) error {
	manifest, err := e.Inspect(zipPath)
	if err != nil {
		return fmt.Errorf("inspecting backup package: %w", err)
	}

	compatible, reason := e.CheckCompatibility(manifest)
	if !compatible {
		return fmt.Errorf("incompatible backup package: %s", reason)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening backup zip: %w", err)
	}
	defer zr.Close()

	hasModule := func(name string) bool {
		for _, m := range manifest.Modules {
			if m == name {
				return true
			}
		}
		return false
	}

	// 1. Restore database if present
	if hasModule(model.BackupModuleDatabase) {
		if err := e.restoreDatabase(&zr.Reader, manifest); err != nil {
			return fmt.Errorf("restoring database: %w", err)
		}
	}

	// 2. Restore config if present
	if hasModule(model.BackupModuleConfig) {
		if err := e.restoreConfig(&zr.Reader); err != nil {
			return fmt.Errorf("restoring config: %w", err)
		}
	}

	// 3. Restore storage files if present
	if hasModule(model.BackupModuleStorage) {
		if err := e.restoreStorage(&zr.Reader); err != nil {
			return fmt.Errorf("restoring storage: %w", err)
		}
	}

	// 4. Restore build artifacts if present
	if hasModule(model.BackupModuleArtifacts) {
		if err := e.restoreArtifacts(&zr.Reader); err != nil {
			return fmt.Errorf("restoring artifacts: %w", err)
		}
	}

	// 5. Restore build logs if present
	if hasModule(model.BackupModuleLogs) {
		if err := e.restoreLogs(&zr.Reader); err != nil {
			return fmt.Errorf("restoring logs: %w", err)
		}
	}

	// 6. Ensure connection pool is reset and healthy
	if err := db.Reset(e.db, &e.cfg.Database); err != nil {
		return fmt.Errorf("reloading database connection pool: %w", err)
	}

	return nil
}

// restoreDatabase restores the database module from zip.
func (e *BackupEngine) restoreDatabase(zr *zip.Reader, manifest *model.BackupManifest) error {
	switch e.cfg.Database.Driver {
	case "sqlite":
		var dbFile *zip.File
		for _, f := range zr.File {
			if f.Name == "database/db.sqlite" {
				dbFile = f
				break
			}
		}
		if dbFile == nil {
			return fmt.Errorf("database/db.sqlite not found in backup archive")
		}

		// Close existing connections before file overwrite
		if err := db.Close(e.db); err != nil {
			return fmt.Errorf("closing old database connection: %w", err)
		}

		targetDBPath := e.cfg.Database.Path
		if err := os.MkdirAll(filepath.Dir(targetDBPath), 0o755); err != nil {
			return fmt.Errorf("creating database dir: %w", err)
		}

		tmpExtracted := targetDBPath + ".restore.tmp"
		if err := extractZipFile(dbFile, tmpExtracted); err != nil {
			_ = os.Remove(tmpExtracted)
			return fmt.Errorf("extracting database file: %w", err)
		}

		// Remove existing WAL and SHM journal files
		_ = os.Remove(targetDBPath + "-wal")
		_ = os.Remove(targetDBPath + "-shm")

		if err := os.Rename(tmpExtracted, targetDBPath); err != nil {
			// Fallback copy if rename fails
			if copyErr := copyFile(tmpExtracted, targetDBPath); copyErr != nil {
				_ = os.Remove(tmpExtracted)
				return fmt.Errorf("overwriting sqlite database: %w", copyErr)
			}
			_ = os.Remove(tmpExtracted)
		}

		// Smooth reload connection pool
		return db.Reset(e.db, &e.cfg.Database)

	case "mysql", "postgres":
		var dumpFile *zip.File
		for _, f := range zr.File {
			if f.Name == "database/dump.sql" {
				dumpFile = f
				break
			}
		}
		if dumpFile == nil {
			return fmt.Errorf("database/dump.sql not found in backup archive")
		}

		rc, err := dumpFile.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		sqlBytes, err := io.ReadAll(rc)
		if err != nil {
			return err
		}

		return e.restoreDatabaseSQL(string(sqlBytes))

	default:
		return fmt.Errorf("unsupported database driver: %s", e.cfg.Database.Driver)
	}
}

// restoreDatabaseSQL executes SQL dump statements inside a transaction.
func (e *BackupEngine) restoreDatabaseSQL(sqlText string) error {
	statements := splitSQLStatements(sqlText)
	return e.db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("executing restore sql (%s): %w", truncate(stmt, 80), err)
			}
		}
		return nil
	})
}

// restoreConfig extracts config/config.yaml to configPath.
func (e *BackupEngine) restoreConfig(zr *zip.Reader) error {
	if e.configPath == "" {
		return nil
	}
	var configFile *zip.File
	for _, f := range zr.File {
		if f.Name == "config/config.yaml" {
			configFile = f
			break
		}
	}
	if configFile == nil {
		return nil
	}
	return extractZipFile(configFile, e.configPath)
}

// restoreStorage extracts storage files into storage.root.
func (e *BackupEngine) restoreStorage(zr *zip.Reader) error {
	if e.cfg == nil || e.cfg.Storage.Root == "" {
		return nil
	}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "storage/") && !f.FileInfo().IsDir() {
			rel := strings.TrimPrefix(f.Name, "storage/")
			targetPath, err := sanitizeExtractPath(e.cfg.Storage.Root, rel)
			if err != nil {
				return err
			}
			if err := extractZipFile(f, targetPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// restoreArtifacts extracts artifact files into build.artifact_dir.
func (e *BackupEngine) restoreArtifacts(zr *zip.Reader) error {
	if e.cfg == nil || e.cfg.Build.ArtifactDir == "" {
		return nil
	}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "artifacts/") && !f.FileInfo().IsDir() {
			rel := strings.TrimPrefix(f.Name, "artifacts/")
			targetPath, err := sanitizeExtractPath(e.cfg.Build.ArtifactDir, rel)
			if err != nil {
				return err
			}
			if err := extractZipFile(f, targetPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// restoreLogs extracts log files into build.log_dir.
func (e *BackupEngine) restoreLogs(zr *zip.Reader) error {
	if e.cfg == nil || e.cfg.Build.LogDir == "" {
		return nil
	}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "logs/") && !f.FileInfo().IsDir() {
			rel := strings.TrimPrefix(f.Name, "logs/")
			targetPath, err := sanitizeExtractPath(e.cfg.Build.LogDir, rel)
			if err != nil {
				return err
			}
			if err := extractZipFile(f, targetPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// dumpDatabaseSQL exports database tables and data as SQL statements.
func (e *BackupEngine) dumpDatabaseSQL(w io.Writer) error {
	switch e.cfg.Database.Driver {
	case "mysql":
		return e.dumpMySQL(w)
	case "postgres":
		return e.dumpPostgres(w)
	default:
		return fmt.Errorf("unsupported database driver for sql dump: %s", e.cfg.Database.Driver)
	}
}

// dumpMySQL generates MySQL schema DDL and INSERT statements.
func (e *BackupEngine) dumpMySQL(w io.Writer) error {
	sqlDB, err := e.db.DB()
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS=0;")
	defer fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS=1;")

	rows, err := sqlDB.Query("SHOW TABLES")
	if err != nil {
		return fmt.Errorf("listing mysql tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return err
		}
		tables = append(tables, table)
	}

	for _, table := range tables {
		var tblName, createStmt string
		row := sqlDB.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`", table))
		if err := row.Scan(&tblName, &createStmt); err != nil {
			return fmt.Errorf("fetching create table for %s: %w", table, err)
		}
		fmt.Fprintf(w, "\nDROP TABLE IF EXISTS `%s`;\n", table)
		fmt.Fprintf(w, "%s;\n", createStmt)

		if err := dumpTableRows(sqlDB, table, "`", w); err != nil {
			return err
		}
	}
	return nil
}

// dumpPostgres generates PostgreSQL INSERT statements for public tables.
func (e *BackupEngine) dumpPostgres(w io.Writer) error {
	sqlDB, err := e.db.DB()
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "SET session_replication_role = 'replica';")
	defer fmt.Fprintln(w, "SET session_replication_role = 'origin';")

	rows, err := sqlDB.Query(`
		SELECT tablename 
		FROM pg_tables 
		WHERE schemaname = 'public' 
		ORDER BY tablename
	`)
	if err != nil {
		return fmt.Errorf("listing postgres tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return err
		}
		tables = append(tables, table)
	}

	for _, table := range tables {
		if err := dumpTableRows(sqlDB, table, `"`, w); err != nil {
			return err
		}
	}
	return nil
}

// dumpTableRows writes INSERT INTO statements for all rows in the given table.
func dumpTableRows(sqlDB *sql.DB, table, quoteChar string, w io.Writer) error {
	rows, err := sqlDB.Query(fmt.Sprintf("SELECT * FROM %s%s%s", quoteChar, table, quoteChar))
	if err != nil {
		return fmt.Errorf("selecting rows from %s: %w", table, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil
	}

	var quotedCols []string
	for _, col := range cols {
		quotedCols = append(quotedCols, fmt.Sprintf("%s%s%s", quoteChar, col, quoteChar))
	}
	colList := strings.Join(quotedCols, ", ")

	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		var formattedVals []string
		for _, val := range values {
			formattedVals = append(formattedVals, formatSQLValue(val))
		}

		fmt.Fprintf(w, "INSERT INTO %s%s%s (%s) VALUES (%s);\n",
			quoteChar, table, quoteChar, colList, strings.Join(formattedVals, ", "))
	}
	return rows.Err()
}

func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		return "'" + strings.ReplaceAll(string(val), "'", "''") + "'"
	case string:
		return "'" + strings.ReplaceAll(val, "'", "''") + "'"
	case time.Time:
		return "'" + val.Format("2006-01-02 15:04:05") + "'"
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// splitSQLStatements splits a multi-statement SQL script by semicolon while respecting string literals.
func splitSQLStatements(sqlText string) []string {
	var statements []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune
	escaped := false

	for _, r := range sqlText {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			current.WriteRune(r)
			escaped = true
			continue
		}

		if inQuote {
			current.WriteRune(r)
			if r == quoteChar {
				inQuote = false
			}
			continue
		}

		if r == '\'' || r == '"' || r == '`' {
			inQuote = true
			quoteChar = r
			current.WriteRune(r)
			continue
		}

		if r == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	last := strings.TrimSpace(current.String())
	if last != "" {
		statements = append(statements, last)
	}
	return statements
}

func addFileToZip(w *zip.Writer, srcPath, zipEntryName string) error {
	file, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(zipEntryName)
	header.Method = zip.Deflate

	writer, err := w.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}

func addDirToZip(w *zip.Writer, srcDir, zipPrefix string, excludeDir string) error {
	cleanSrc := filepath.Clean(srcDir)
	return filepath.Walk(cleanSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if excludeDir != "" && filepath.Base(path) == excludeDir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(cleanSrc, path)
		if err != nil {
			return err
		}
		if excludeDir != "" {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 && parts[0] == excludeDir {
				return nil
			}
		}
		entryName := filepath.ToSlash(filepath.Join(zipPrefix, rel))
		return addFileToZip(w, path, entryName)
	})
}

func extractZipFile(zf *zip.File, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, zf.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func sanitizeExtractPath(destDir, filePath string) (string, error) {
	cleanDest := filepath.Clean(destDir)
	target := filepath.Clean(filepath.Join(cleanDest, filePath))
	if !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) && target != cleanDest {
		return "", fmt.Errorf("illegal zip path escaping destination: %s", filePath)
	}
	return target, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
