package model_test

import (
	"testing"

	"bedrock/internal/project/model"
)

func TestBugModels_TableNames(t *testing.T) {
	// Bug persistence is unified into project_issues (DESIGN D37); the legacy
	// ProjectBug* structs are wire DTOs only.
	if (model.ProjectIssue{}).TableName() != "project_issues" {
		t.Errorf("unexpected table name for ProjectIssue")
	}
	if (model.ProjectIssueComment{}).TableName() != "project_issue_comments" {
		t.Errorf("unexpected table name for ProjectIssueComment")
	}
	if (model.ProjectIssueAttachment{}).TableName() != "project_issue_attachments" {
		t.Errorf("unexpected table name for ProjectIssueAttachment")
	}
	if (model.ProjectIssueActivity{}).TableName() != "project_issue_activities" {
		t.Errorf("unexpected table name for ProjectIssueActivity")
	}
	if (model.ProjectIssueWatcher{}).TableName() != "project_issue_watchers" {
		t.Errorf("unexpected table name for ProjectIssueWatcher")
	}
	if (model.ProjectIteration{}).TableName() != "project_iterations" {
		t.Errorf("unexpected table name for ProjectIteration")
	}
}

func TestIssueHelpers(t *testing.T) {
	if !model.IsValidIssueType(model.IssueTypeBug) || !model.IsValidIssueType(model.IssueTypeRequirement) || !model.IsValidIssueType(model.IssueTypeTask) {
		t.Errorf("expected all issue types valid")
	}
	if model.IsValidIssueType("epic") {
		t.Errorf("expected unknown issue type rejected")
	}
	for _, s := range []string{"closed", "rejected", "done", "cancelled"} {
		if !model.IsTerminalStatus(s) {
			t.Errorf("expected %s terminal", s)
		}
	}
	for _, s := range []string{"open", "in_progress", "resolved", "todo", "doing"} {
		if model.IsTerminalStatus(s) {
			t.Errorf("expected %s non-terminal", s)
		}
	}
	if model.StatusDictCode(model.IssueTypeBug) != "bug_status" {
		t.Errorf("bug status dict code mismatch")
	}
	if model.StatusDictCode(model.IssueTypeRequirement) != "requirement_status" || model.StatusDictCode(model.IssueTypeTask) != "requirement_status" {
		t.Errorf("requirement/task status dict code mismatch")
	}
	if model.DefaultIssueStatus(model.IssueTypeBug) != model.BugStatusOpen {
		t.Errorf("bug default status mismatch")
	}
	if model.DefaultIssueStatus(model.IssueTypeTask) != model.RequirementStatusBacklog {
		t.Errorf("task default status mismatch")
	}
}

func TestBugValidationHelpers(t *testing.T) {
	validStatuses := []string{
		model.BugStatusOpen,
		model.BugStatusInProgress,
		model.BugStatusResolved,
		model.BugStatusClosed,
		model.BugStatusRejected,
	}
	for _, s := range validStatuses {
		if !model.IsValidBugStatus(s) {
			t.Errorf("expected status %s to be valid", s)
		}
	}
	if model.IsValidBugStatus("invalid") {
		t.Errorf("expected invalid status to be rejected")
	}

	validSeverities := []string{
		model.BugSeverityLow,
		model.BugSeverityNormal,
		model.BugSeverityHigh,
		model.BugSeverityCritical,
	}
	for _, s := range validSeverities {
		if !model.IsValidBugSeverity(s) {
			t.Errorf("expected severity %s to be valid", s)
		}
	}
	if model.IsValidBugSeverity("unknown") {
		t.Errorf("expected unknown severity to be rejected")
	}

	validPriorities := []string{
		model.BugPriorityLow,
		model.BugPriorityNormal,
		model.BugPriorityHigh,
		model.BugPriorityUrgent,
	}
	for _, p := range validPriorities {
		if !model.IsValidBugPriority(p) {
			t.Errorf("expected priority %s to be valid", p)
		}
	}
	if model.IsValidBugPriority("unknown") {
		t.Errorf("expected unknown priority to be rejected")
	}
}

func TestAllProjectModels(t *testing.T) {
	models := model.AllProjectModels()
	if len(models) == 0 {
		t.Fatalf("expected non-empty AllProjectModels")
	}
}
