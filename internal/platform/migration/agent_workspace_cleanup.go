package migration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gorm.io/gorm"
)

const AgentPersistentWorkspaceMigrationVersion = "000018_agent_persistent_workspace"

const (
	workspaceQuarantineName = ".bedrock-000018-agent-workspace-quarantine"
	artifactQuarantineName  = ".bedrock-000018-agent-artifact-quarantine"
	cleanupMarkerName       = ".bedrock-cleanup-marker"
	cleanupMarkerContent    = AgentPersistentWorkspaceMigrationVersion + "\n"
)

type cleanupRoot struct {
	root           string
	quarantineName string
}

// AgentPersistentWorkspaceCleanup finalizes filesystem isolation after the
// matching schema migration commits.
type AgentPersistentWorkspaceCleanup struct {
	roots []cleanupRoot
}

type agentRunCleanupRow struct {
	ID           uint
	AgentID      uint
	ArtifactPath string
}

// PrepareAgentPersistentWorkspaceCleanup isolates only legacy Agent run
// directories and archives named by database IDs. Renames stay within each
// storage root, so they are atomic and resumable across process crashes.
func PrepareAgentPersistentWorkspaceCleanup(db *gorm.DB, workspaceRoot, artifactRoot string) (*AgentPersistentWorkspaceCleanup, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	workspace, err := normalizeCleanupRoot(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	artifacts, err := normalizeCleanupRoot(artifactRoot)
	if err != nil {
		return nil, fmt.Errorf("artifact root: %w", err)
	}
	cleanup := &AgentPersistentWorkspaceCleanup{roots: []cleanupRoot{
		{root: workspace, quarantineName: workspaceQuarantineName},
		{root: artifacts, quarantineName: artifactQuarantineName},
	}}
	for _, root := range cleanup.roots {
		if err := validateExistingQuarantine(root); err != nil {
			return nil, err
		}
	}

	if err := EnsureSchemaMigrationsTable(db); err != nil {
		return nil, fmt.Errorf("ensure schema migrations before agent cleanup: %w", err)
	}
	applied, err := AppliedVersions(db)
	if err != nil {
		return nil, fmt.Errorf("read migrations before agent cleanup: %w", err)
	}
	if _, ok := applied[AgentPersistentWorkspaceMigrationVersion]; ok {
		return cleanup, nil
	}

	if !hasLegacyAgentArtifactSchema(db) {
		return cleanup, nil
	}
	agentIDs, runs, err := loadLegacyAgentCleanupRows(db)
	if err != nil {
		return nil, err
	}
	if err := validateRecordedAgentArtifacts(artifacts, runs); err != nil {
		return nil, err
	}

	for _, agentID := range agentIDs {
		source := filepath.Join(workspace, "agents", fmt.Sprintf("agent-%d", agentID), "runs")
		relative := filepath.Join(fmt.Sprintf("agent-%d", agentID), "runs")
		if err := moveCleanupCandidate(workspace, workspaceQuarantineName, source, relative, true); err != nil {
			return nil, fmt.Errorf("isolate agent %d runs: %w", agentID, err)
		}
	}
	for _, run := range runs {
		for _, extension := range []string{".zip", ".tar.gz"} {
			name := fmt.Sprintf("run-%d%s", run.ID, extension)
			source := filepath.Join(artifacts, fmt.Sprintf("agent-%d", run.AgentID), name)
			relative := filepath.Join(fmt.Sprintf("agent-%d", run.AgentID), name)
			if err := moveCleanupCandidate(artifacts, artifactQuarantineName, source, relative, false); err != nil {
				return nil, fmt.Errorf("isolate agent %d run %d archive: %w", run.AgentID, run.ID, err)
			}
		}
	}
	return cleanup, nil
}

// Finalize removes validated quarantine directories. Calling it repeatedly is
// safe, including after a crash partway through removal.
func (c *AgentPersistentWorkspaceCleanup) Finalize() error {
	if c == nil {
		return nil
	}
	for _, root := range c.roots {
		if err := removeValidatedQuarantine(root); err != nil {
			return err
		}
	}
	return nil
}

func hasLegacyAgentArtifactSchema(db *gorm.DB) bool {
	if !db.Migrator().HasTable("ai_agents") || !db.Migrator().HasTable("agent_runs") {
		return false
	}
	for _, column := range []string{"artifact_format", "max_artifacts"} {
		if db.Migrator().HasColumn("ai_agents", column) {
			return true
		}
	}
	return db.Migrator().HasColumn("agent_runs", "artifact_path")
}

func loadLegacyAgentCleanupRows(db *gorm.DB) ([]uint, []agentRunCleanupRow, error) {
	var agentIDs []uint
	if err := db.Table("ai_agents").Order("id ASC").Pluck("id", &agentIDs).Error; err != nil {
		return nil, nil, fmt.Errorf("list agents for cleanup: %w", err)
	}

	selectColumns := "id, agent_id"
	if db.Migrator().HasColumn("agent_runs", "artifact_path") {
		selectColumns += ", artifact_path"
	}
	var runs []agentRunCleanupRow
	if err := db.Table("agent_runs").Select(selectColumns).Order("id ASC").Scan(&runs).Error; err != nil {
		return nil, nil, fmt.Errorf("list agent runs for cleanup: %w", err)
	}

	known := make(map[uint]struct{}, len(agentIDs)+len(runs))
	for _, id := range agentIDs {
		if id == 0 {
			return nil, nil, fmt.Errorf("invalid zero agent ID")
		}
		known[id] = struct{}{}
	}
	for _, run := range runs {
		if run.ID == 0 || run.AgentID == 0 {
			return nil, nil, fmt.Errorf("invalid agent run identity id=%d agent_id=%d", run.ID, run.AgentID)
		}
		known[run.AgentID] = struct{}{}
	}
	agentIDs = agentIDs[:0]
	for id := range known {
		agentIDs = append(agentIDs, id)
	}
	slices.Sort(agentIDs)
	return agentIDs, runs, nil
}

func validateRecordedAgentArtifacts(artifactRoot string, runs []agentRunCleanupRow) error {
	for _, run := range runs {
		recorded := strings.TrimSpace(run.ArtifactPath)
		if recorded == "" {
			continue
		}
		recordedAbs, err := filepath.Abs(recorded)
		if err != nil {
			return fmt.Errorf("resolve agent run %d artifact_path: %w", run.ID, err)
		}
		base := filepath.Join(artifactRoot, fmt.Sprintf("agent-%d", run.AgentID), fmt.Sprintf("run-%d", run.ID))
		allowed := map[string]struct{}{
			filepath.Clean(base + ".zip"):    {},
			filepath.Clean(base + ".tar.gz"): {},
		}
		if _, ok := allowed[filepath.Clean(recordedAbs)]; !ok {
			return fmt.Errorf("agent run %d artifact_path is outside its strictly bounded archive path: %q", run.ID, recorded)
		}
	}
	return nil
}

func normalizeCleanupRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	if absolute == volume+string(os.PathSeparator) {
		return "", fmt.Errorf("filesystem root is not allowed")
	}
	if err := rejectSymlinkPath(absolute); err != nil {
		return "", err
	}
	return absolute, nil
}

func moveCleanupCandidate(root, quarantineName, source, relative string, wantDir bool) error {
	if err := ensurePathWithinRoot(root, source); err != nil {
		return err
	}
	quarantine := filepath.Join(root, quarantineName)
	destination := filepath.Join(quarantine, relative)
	if err := ensurePathWithinRoot(quarantine, destination); err != nil {
		return err
	}

	sourceInfo, sourceErr := os.Lstat(source)
	if sourceErr != nil && !errors.Is(sourceErr, os.ErrNotExist) {
		return sourceErr
	}
	destinationInfo, destinationErr := os.Lstat(destination)
	if destinationErr != nil && !errors.Is(destinationErr, os.ErrNotExist) {
		return destinationErr
	}
	if destinationErr == nil {
		if err := validateQuarantine(quarantine); err != nil {
			return err
		}
		if sourceErr == nil {
			return fmt.Errorf("both source and quarantine destination exist")
		}
		if destinationInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("quarantine destination is a symlink: %s", destination)
		}
		return nil
	}
	if errors.Is(sourceErr, os.ErrNotExist) {
		return nil
	}
	if err := rejectSymlinkPath(source); err != nil {
		return err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cleanup source is a symlink: %s", source)
	}
	if wantDir != sourceInfo.IsDir() {
		return fmt.Errorf("cleanup source has unexpected type: %s", source)
	}

	if err := ensureQuarantine(quarantine); err != nil {
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkPath(parent); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(source)); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func ensureQuarantine(quarantine string) error {
	info, err := os.Lstat(quarantine)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid cleanup quarantine: %s", quarantine)
		}
		return validateQuarantine(quarantine)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := rejectSymlinkPath(filepath.Dir(quarantine)); err != nil {
		return err
	}
	if err := os.Mkdir(quarantine, 0o700); err != nil {
		return err
	}
	marker := filepath.Join(quarantine, cleanupMarkerName)
	if err := os.WriteFile(marker, []byte(cleanupMarkerContent), 0o600); err != nil {
		return err
	}
	if err := syncDirectory(quarantine); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(quarantine))
}

func validateExistingQuarantine(root cleanupRoot) error {
	quarantine := filepath.Join(root.root, root.quarantineName)
	_, err := os.Lstat(quarantine)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return validateQuarantine(quarantine)
}

func validateQuarantine(quarantine string) error {
	if err := rejectSymlinkPath(quarantine); err != nil {
		return err
	}
	info, err := os.Lstat(quarantine)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid cleanup quarantine: %s", quarantine)
	}
	content, err := os.ReadFile(filepath.Join(quarantine, cleanupMarkerName))
	if err != nil {
		return fmt.Errorf("read cleanup quarantine marker: %w", err)
	}
	if string(content) != cleanupMarkerContent {
		return fmt.Errorf("invalid cleanup quarantine marker: %s", quarantine)
	}
	return nil
}

func removeValidatedQuarantine(root cleanupRoot) error {
	quarantine := filepath.Join(root.root, root.quarantineName)
	_, err := os.Lstat(quarantine)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateQuarantine(quarantine); err != nil {
		return err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return fmt.Errorf("remove cleanup quarantine %s: %w", quarantine, err)
	}
	return syncDirectory(root.root)
}

func ensurePathWithinRoot(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("path escapes cleanup root: %s", path)
	}
	return nil
}

func rejectSymlinkPath(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component is not allowed: %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
