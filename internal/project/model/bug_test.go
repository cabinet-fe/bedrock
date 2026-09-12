package model_test

import (
	"testing"

	"bedrock/internal/project/model"
)

func TestBugModels_TableNames(t *testing.T) {
	if (model.ProjectBug{}).TableName() != "project_bugs" {
		t.Errorf("unexpected table name for ProjectBug")
	}
	if (model.ProjectBugComment{}).TableName() != "project_bug_comments" {
		t.Errorf("unexpected table name for ProjectBugComment")
	}
	if (model.ProjectBugAttachment{}).TableName() != "project_bug_attachments" {
		t.Errorf("unexpected table name for ProjectBugAttachment")
	}
	if (model.ProjectBugActivity{}).TableName() != "project_bug_activities" {
		t.Errorf("unexpected table name for ProjectBugActivity")
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
