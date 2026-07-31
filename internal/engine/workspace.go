package engine

import (
	"fmt"
	"path/filepath"
)

// JobWorkspace returns the persistent build checkout directory for a job:
// {workspaceRoot}/jobs/job-{id}/
func JobWorkspace(workspaceRoot string, jobID uint) string {
	return filepath.Join(workspaceRoot, "jobs", fmt.Sprintf("job-%d", jobID))
}

// AbsoluteJobWorkspace resolves JobWorkspace to an absolute path for API display.
func AbsoluteJobWorkspace(workspaceRoot string, jobID uint) (string, error) {
	return filepath.Abs(JobWorkspace(workspaceRoot, jobID))
}

// ScriptWorkspace returns the persistent script working directory:
// {workspaceRoot}/scripts/script-{id}/
func ScriptWorkspace(workspaceRoot string, jobID uint) string {
	return filepath.Join(workspaceRoot, "scripts", fmt.Sprintf("script-%d", jobID))
}

// AbsoluteScriptWorkspace resolves ScriptWorkspace to an absolute path for API display.
func AbsoluteScriptWorkspace(workspaceRoot string, jobID uint) (string, error) {
	return filepath.Abs(ScriptWorkspace(workspaceRoot, jobID))
}
