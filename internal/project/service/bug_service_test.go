package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"

	"gorm.io/gorm"
)

func newTestEnv(t *testing.T) (*BugService, *ProjectService, *projectrepo.BugRepository, *projectrepo.ProjectRepository, *gorm.DB) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: t.TempDir() + "/bedrock-bug.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	storage, err := storageservice.NewStorageService(
		storagerepo.NewStorageRepository(gdb),
		t.TempDir(),
		storageservice.Limits{},
	)
	if err != nil {
		t.Fatal(err)
	}

	projectRepo := projectrepo.NewProjectRepository(gdb)
	bugRepo := projectrepo.NewBugRepository(gdb)
	projectSvc := NewProjectService(projectRepo, storage)
	bugSvc := NewBugService(bugRepo, projectRepo, storage)

	// Seed dummy users for creator/assignee attachment
	userRepo := authrepo.NewUserRepository(gdb)
	for i := 1; i <= 10; i++ {
		_ = userRepo.Create(&authmodel.User{
			Username:     "user" + string(rune('0'+i)),
			DisplayName:  "User " + string(rune('0'+i)),
			PasswordHash: "hashed",
			IsActive:     true,
		})
	}

	return bugSvc, projectSvc, bugRepo, projectRepo, gdb
}

func allBugPermissions() []string {
	return []string{
		"project_projects:create", "project_projects:view", "project_projects:update", "project_projects:delete",
		"project_bugs:create", "project_bugs:view", "project_bugs:update", "project_bugs:delete", "project_bugs:execute",
	}
}

func TestBugCRUD(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-crud-project")

	// 1. Validation errors on create
	if _, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: ""}); !IsBadRequest(err) {
		t.Fatalf("empty title must fail with bad request, got %v", err)
	}
	if _, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "T", Severity: "invalid"}); !IsBadRequest(err) {
		t.Fatalf("invalid severity must fail with bad request, got %v", err)
	}
	if _, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "T", Priority: "invalid"}); !IsBadRequest(err) {
		t.Fatalf("invalid priority must fail with bad request, got %v", err)
	}

	// 2. Successful creation with defaults
	assigneeID := uint(2)
	created, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{
		Title:       "Test Bug 1",
		Description: "Detailed description",
		AssigneeID:  &assigneeID,
		Branch:      "main",
	})
	if err != nil {
		t.Fatalf("create bug failed: %v", err)
	}
	if created.ID == 0 || created.Status != projectmodel.BugStatusOpen {
		t.Fatalf("unexpected bug state: %+v", created)
	}
	if created.Severity != projectmodel.BugSeverityNormal || created.Priority != projectmodel.BugPriorityNormal {
		t.Fatalf("unexpected defaults: severity=%s, priority=%s", created.Severity, created.Priority)
	}
	if created.ProjectName != "bug-crud-project" {
		t.Fatalf("expected attached project name 'bug-crud-project', got %s", created.ProjectName)
	}

	// 3. GetBug
	fetched, err := bugSvc.GetBug(owner, project.ID, created.ID)
	if err != nil {
		t.Fatalf("get bug failed: %v", err)
	}
	if fetched.Title != "Test Bug 1" || fetched.Description != "Detailed description" {
		t.Fatalf("unexpected fetched content: %+v", fetched)
	}

	// 4. UpdateBug
	newTitle := "Updated Bug 1"
	newSev := projectmodel.BugSeverityCritical
	newPri := projectmodel.BugPriorityUrgent
	clearAssignee := uint(0)
	updated, err := bugSvc.UpdateBug(owner, project.ID, created.ID, UpdateBugInput{
		Title:      &newTitle,
		Severity:   &newSev,
		Priority:   &newPri,
		AssigneeID: &clearAssignee,
	})
	if err != nil {
		t.Fatalf("update bug failed: %v", err)
	}
	if updated.Title != newTitle || updated.Severity != newSev || updated.Priority != newPri || updated.AssigneeID != nil {
		t.Fatalf("unexpected updated bug: %+v", updated)
	}

	// 5. DeleteBug
	if err := bugSvc.DeleteBug(owner, project.ID, created.ID); err != nil {
		t.Fatalf("delete bug failed: %v", err)
	}
	if _, err := bugSvc.GetBug(owner, project.ID, created.ID); !IsNotFound(err) {
		t.Fatalf("deleted bug must return not found, got %v", err)
	}
}

func TestBugStatusTransitions(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-status-project")

	bug, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{
		Title: "Status Flow Bug",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flow through legal statuses
	transitions := []string{
		projectmodel.BugStatusInProgress,
		projectmodel.BugStatusResolved,
		projectmodel.BugStatusClosed,
		projectmodel.BugStatusRejected,
		projectmodel.BugStatusOpen,
	}

	for _, target := range transitions {
		updated, err := bugSvc.TransitionBugStatus(owner, project.ID, bug.ID, TransitionBugStatusInput{
			Status:  target,
			Comment: "Moving to " + target,
		})
		if err != nil {
			t.Fatalf("transition to %s failed: %v", target, err)
		}
		if updated.Status != target {
			t.Fatalf("expected status %s, got %s", target, updated.Status)
		}
	}

	// Invalid transition status
	if _, err := bugSvc.TransitionBugStatus(owner, project.ID, bug.ID, TransitionBugStatusInput{
		Status: "unknown_status",
	}); !IsBadRequest(err) {
		t.Fatalf("unknown status must fail with bad request, got %v", err)
	}

	// Verify activity logs
	activities, err := bugSvc.ListBugActivities(owner, project.ID, bug.ID)
	if err != nil {
		t.Fatalf("list activities failed: %v", err)
	}
	// 1 create activity + 5 status change activities = 6
	if len(activities) != 6 {
		t.Fatalf("expected 6 activities, got %d", len(activities))
	}
	if activities[0].Action != projectmodel.BugActivityCreate {
		t.Fatalf("first activity should be create, got %s", activities[0].Action)
	}
	for i := 1; i <= 5; i++ {
		if activities[i].Action != projectmodel.BugActivityStatusChange {
			t.Fatalf("activity %d should be status_change, got %s", i, activities[i].Action)
		}
		if activities[i].ToStatus != transitions[i-1] {
			t.Fatalf("activity %d expected to_status %s, got %s", i, transitions[i-1], activities[i].ToStatus)
		}
	}
}

func TestBugACLAndDataScope(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	projectA := createProject(t, projectSvc, owner, "project-a")
	projectB := createProject(t, projectSvc, owner, "project-b")

	// Add member with readonly role to ProjectA
	if _, err := projectSvc.AddMember(owner, projectA.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleReadonly}); err != nil {
		t.Fatal(err)
	}
	// Add member with standard member role to ProjectA
	if _, err := projectSvc.AddMember(owner, projectA.ID, MemberInput{UserID: 3, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}

	// Create bug in ProjectA and ProjectB
	bugA, err := bugSvc.CreateBug(owner, projectA.ID, CreateBugInput{Title: "Bug in A"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bugSvc.CreateBug(owner, projectB.ID, CreateBugInput{Title: "Bug in B"})
	if err != nil {
		t.Fatal(err)
	}

	readonlyUser := actor(2, allBugPermissions()...)
	memberUser := actor(3, allBugPermissions()...)
	nonMemberUser := actor(4, allBugPermissions()...)

	// 1. Non-member cannot view ProjectA bugs
	if _, _, err := bugSvc.ListProjectBugs(nonMemberUser, projectA.ID, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10}); !IsForbidden(err) {
		t.Fatalf("non-member list must be forbidden, got %v", err)
	}
	if _, err := bugSvc.GetBug(nonMemberUser, projectA.ID, bugA.ID); !IsForbidden(err) {
		t.Fatalf("non-member get must be forbidden, got %v", err)
	}

	// 2. Readonly user can view, but cannot create / update / delete
	if _, _, err := bugSvc.ListProjectBugs(readonlyUser, projectA.ID, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("readonly user should be able to view bugs: %v", err)
	}
	if _, err := bugSvc.CreateBug(readonlyUser, projectA.ID, CreateBugInput{Title: "Fail"}); !IsForbidden(err) {
		t.Fatalf("readonly user create must be forbidden, got %v", err)
	}
	title := "Fail"
	if _, err := bugSvc.UpdateBug(readonlyUser, projectA.ID, bugA.ID, UpdateBugInput{Title: &title}); !IsForbidden(err) {
		t.Fatalf("readonly user update must be forbidden, got %v", err)
	}
	if _, err := bugSvc.TransitionBugStatus(readonlyUser, projectA.ID, bugA.ID, TransitionBugStatusInput{Status: projectmodel.BugStatusClosed}); !IsForbidden(err) {
		t.Fatalf("readonly user transition status must be forbidden, got %v", err)
	}
	if err := bugSvc.DeleteBug(readonlyUser, projectA.ID, bugA.ID); !IsForbidden(err) {
		t.Fatalf("readonly user delete must be forbidden, got %v", err)
	}

	// 3. Member user can create and update, but cannot delete (only admin/owner)
	newBug, err := bugSvc.CreateBug(memberUser, projectA.ID, CreateBugInput{Title: "Member Bug"})
	if err != nil {
		t.Fatalf("member should create bug: %v", err)
	}
	if _, err := bugSvc.UpdateBug(memberUser, projectA.ID, newBug.ID, UpdateBugInput{Title: &title}); err != nil {
		t.Fatalf("member should update bug: %v", err)
	}
	if err := bugSvc.DeleteBug(memberUser, projectA.ID, newBug.ID); !IsForbidden(err) {
		t.Fatalf("regular member delete bug must be forbidden (requires admin/owner), got %v", err)
	}

	// 4. ListAcrossProjects:
	// - User 3 (member of ProjectA only, data_scope=self) sees only Bug in A
	bugsUser3, totalUser3, err := bugSvc.ListAcrossProjects(memberUser, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list across projects for user3 failed: %v", err)
	}
	if totalUser3 != 2 || len(bugsUser3) != 2 { // bugA and newBug
		t.Fatalf("expected user3 to see 2 bugs in ProjectA, got total=%d, len=%d", totalUser3, len(bugsUser3))
	}
	for _, b := range bugsUser3 {
		if b.ProjectID != projectA.ID {
			t.Fatalf("user3 must only see ProjectA bugs, got project %d", b.ProjectID)
		}
	}

	// - SuperAdmin sees all bugs across both projects
	superAdmin := NewAccessContext(99, true, nil)
	bugsSuper, totalSuper, err := bugSvc.ListAcrossProjects(superAdmin, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list across projects for superadmin failed: %v", err)
	}
	if totalSuper != 3 || len(bugsSuper) != 3 {
		t.Fatalf("expected superadmin to see all 3 bugs, got total=%d, len=%d", totalSuper, len(bugsSuper))
	}
}

func TestBugLifecycleOnProjectDelete(t *testing.T) {
	bugSvc, projectSvc, bugRepo, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-lifecycle-project")

	bug, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Lifecycle Bug"})
	if err != nil {
		t.Fatal(err)
	}

	// Delete the project
	if err := projectSvc.DeleteProject(owner, project.ID); err != nil {
		t.Fatalf("delete project failed: %v", err)
	}

	// Bug should be deleted
	if _, err := bugRepo.FindByID(bug.ID); err == nil {
		t.Fatalf("bug should have been deleted along with the project")
	}

	// Status counts should be empty
	counts, err := bugRepo.CountByStatus(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected 0 status counts, got %v", counts)
	}
}

func TestBugCountByStatus(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-count-project")

	_, _ = bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Bug 1"})
	b2, _ := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Bug 2"})
	_, _ = bugSvc.TransitionBugStatus(owner, project.ID, b2.ID, TransitionBugStatusInput{Status: projectmodel.BugStatusResolved})

	counts, err := bugSvc.CountByStatus(owner, project.ID)
	if err != nil {
		t.Fatalf("count by status failed: %v", err)
	}
	if counts[projectmodel.BugStatusOpen] != 1 || counts[projectmodel.BugStatusResolved] != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}

func TestBugComments(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	memberUser := actor(2, allBugPermissions()...)
	outsider := actor(3, allBugPermissions()...)

	project := createProject(t, projectSvc, owner, "bug-comment-project")
	if _, err := projectSvc.AddMember(owner, project.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}

	bug, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Bug with Comments"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Validation on empty comment
	if _, err := bugSvc.CreateComment(owner, project.ID, bug.ID, "  "); !IsBadRequest(err) {
		t.Fatalf("empty comment should fail with bad request, got %v", err)
	}

	// 2. Outsider cannot comment
	if _, err := bugSvc.CreateComment(outsider, project.ID, bug.ID, "hello"); !IsForbidden(err) {
		t.Fatalf("outsider comment should be forbidden, got %v", err)
	}

	// 3. Member creates comment
	comment, err := bugSvc.CreateComment(memberUser, project.ID, bug.ID, "First comment by member")
	if err != nil {
		t.Fatalf("create comment failed: %v", err)
	}
	if comment.Content != "First comment by member" || comment.CreatedBy != 2 {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if comment.CreatorUsername != "user2" {
		t.Fatalf("expected creator username 'user2', got '%s'", comment.CreatorUsername)
	}

	// 4. List comments
	comments, err := bugSvc.ListComments(memberUser, project.ID, bug.ID)
	if err != nil {
		t.Fatalf("list comments failed: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}

	// 5. Update comment: another member (not author, not admin) cannot edit
	member2User := actor(4, allBugPermissions()...)
	if _, err := projectSvc.AddMember(owner, project.ID, MemberInput{UserID: 4, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	if _, err := bugSvc.UpdateComment(member2User, project.ID, bug.ID, comment.ID, "Hacked"); !IsForbidden(err) {
		t.Fatalf("non-author member editing comment should be forbidden, got %v", err)
	}

	// 6. Author can edit
	updated, err := bugSvc.UpdateComment(memberUser, project.ID, bug.ID, comment.ID, "Updated by author")
	if err != nil {
		t.Fatalf("author update comment failed: %v", err)
	}
	if updated.Content != "Updated by author" {
		t.Fatalf("expected updated content, got %s", updated.Content)
	}

	// 7. Delete comment: non-author member cannot delete
	if err := bugSvc.DeleteComment(member2User, project.ID, bug.ID, comment.ID); !IsForbidden(err) {
		t.Fatalf("non-author member deleting comment should be forbidden, got %v", err)
	}

	// 7.5 Cross-project isolation: cannot update or delete comment using another project ID
	project2 := createProject(t, projectSvc, owner, "bug-comment-project-2")
	if _, err := bugSvc.UpdateComment(owner, project2.ID, bug.ID, comment.ID, "Cross project hack"); !IsNotFound(err) {
		t.Fatalf("cross-project comment update should return not found, got %v", err)
	}
	if err := bugSvc.DeleteComment(owner, project2.ID, bug.ID, comment.ID); !IsNotFound(err) {
		t.Fatalf("cross-project comment delete should return not found, got %v", err)
	}

	// 8. Project owner/admin CAN delete any comment
	if err := bugSvc.DeleteComment(owner, project.ID, bug.ID, comment.ID); err != nil {
		t.Fatalf("project owner deleting comment should succeed, got %v", err)
	}

	// 9. List comments should now be empty
	comments, err = bugSvc.ListComments(owner, project.ID, bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments after delete, got %d", len(comments))
	}
}

func TestBugAttachments(t *testing.T) {
	bugSvc, projectSvc, _, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-attachment-project")

	bug, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Bug with Attachment"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Disallowed file type rejected
	disallowedFile := strings.NewReader("executable binary")
	if _, err := bugSvc.AddAttachment(owner, project.ID, bug.ID, "malware.exe", "application/x-msdownload", disallowedFile, int64(disallowedFile.Len())); !IsBadRequest(err) {
		t.Fatalf("disallowed extension .exe should fail with bad request, got %v", err)
	}

	// 2. Allowed file type: .png upload
	pngContent := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRtest")
	att, err := bugSvc.AddAttachment(owner, project.ID, bug.ID, "screenshot.png", "image/png", bytes.NewReader(pngContent), int64(len(pngContent)))
	if err != nil {
		t.Fatalf("add attachment failed: %v", err)
	}
	if att.Filename != "screenshot.png" || att.StorageObjectID == 0 {
		t.Fatalf("unexpected attachment: %+v", att)
	}

	// 3. List attachments
	atts, err := bugSvc.ListAttachments(owner, project.ID, bug.ID)
	if err != nil {
		t.Fatalf("list attachments failed: %v", err)
	}
	if len(atts) != 1 || atts[0].Filename != "screenshot.png" {
		t.Fatalf("unexpected attachments list: %+v", atts)
	}
	if atts[0].FileSize != int64(len(pngContent)) {
		t.Fatalf("expected FileSize %d, got %d", len(pngContent), atts[0].FileSize)
	}

	// 4. Download attachment
	file, downloadedAtt, ct, err := bugSvc.OpenAttachment(owner, project.ID, bug.ID, att.ID)
	if err != nil {
		t.Fatalf("open attachment failed: %v", err)
	}
	defer file.Close()
	if downloadedAtt.Filename != "screenshot.png" || ct != "image/png" {
		t.Fatalf("unexpected open attachment: att=%+v, ct=%s", downloadedAtt, ct)
	}
	readBytes, _ := io.ReadAll(file)
	if !bytes.Equal(readBytes, pngContent) {
		t.Fatalf("read content mismatch")
	}

	// 5. Delete attachment
	if err := bugSvc.DeleteAttachment(owner, project.ID, bug.ID, att.ID); err != nil {
		t.Fatalf("delete attachment failed: %v", err)
	}

	// 6. Confirm deleted from list
	atts, err = bugSvc.ListAttachments(owner, project.ID, bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("expected 0 attachments after delete, got %d", len(atts))
	}
}

type mockBugAIBridge struct {
	extractResult *BugAIExtractResult
	extractErr    error
	analyzeResult string
	analyzeErr    error
	dispatchRunID uint
	dispatchErr   error
}

func (m *mockBugAIBridge) Extract(ctx context.Context, content string) (*BugAIExtractResult, error) {
	return m.extractResult, m.extractErr
}

func (m *mockBugAIBridge) Analyze(ctx context.Context, bug *projectmodel.ProjectBug, prompt string) (string, error) {
	return m.analyzeResult, m.analyzeErr
}

func (m *mockBugAIBridge) DispatchAgent(ctx context.Context, bug *projectmodel.ProjectBug, agentID, userID uint, userPrompt string) (uint, error) {
	return m.dispatchRunID, m.dispatchErr
}

func TestBugAIAndAgent(t *testing.T) {
	bugSvc, projectSvc, bugRepo, _, _ := newTestEnv(t)
	owner := actor(1, allBugPermissions()...)
	project := createProject(t, projectSvc, owner, "bug-ai-project")

	mockBridge := &mockBugAIBridge{
		extractResult: &BugAIExtractResult{
			Title:       "NullPointerException in UserService",
			Description: "Stack trace at UserService.java:42",
			Severity:    projectmodel.BugSeverityHigh,
			Priority:    projectmodel.BugPriorityHigh,
		},
		analyzeResult: "根因推断：用户认证上下文未正确注入导致空指针。修复建议：添加判空校验与防御性编程。",
		dispatchRunID: 888,
	}
	bugSvc.SetAIBridge(mockBridge)

	// 1. Test AIExtract
	extractRes, err := bugSvc.AIExtract(owner, project.ID, "ERROR NullPointerException at UserService.java:42")
	if err != nil {
		t.Fatalf("ai-extract failed: %v", err)
	}
	if extractRes.Title != "NullPointerException in UserService" || extractRes.Severity != projectmodel.BugSeverityHigh {
		t.Fatalf("unexpected extract result: %+v", extractRes)
	}

	// 2. Create bug
	bug, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{
		Title:       extractRes.Title,
		Description: extractRes.Description,
		Severity:    extractRes.Severity,
		Priority:    extractRes.Priority,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Test AIAnalyze
	analysis, err := bugSvc.AIAnalyze(owner, project.ID, bug.ID, "请重点排查中间件拦截链路")
	if err != nil {
		t.Fatalf("ai-analyze failed: %v", err)
	}
	if analysis != mockBridge.analyzeResult {
		t.Fatalf("unexpected analysis result: %s", analysis)
	}

	// Verify persistence in DB
	refreshed, err := bugRepo.FindByID(bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AIAnalysis != mockBridge.analyzeResult {
		t.Fatalf("ai_analysis was not persisted to bug entity, got %s", refreshed.AIAnalysis)
	}

	// 4. Test DispatchAgent
	runID, err := bugSvc.DispatchAgent(owner, project.ID, bug.ID, 12, "重点分析堆栈")
	if err != nil {
		t.Fatalf("dispatch-agent failed: %v", err)
	}
	if runID != 888 {
		t.Fatalf("expected runID 888, got %d", runID)
	}

	// Verify last_agent_run_id updated on bug
	refreshed, err = bugRepo.FindByID(bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.LastAgentRunID == nil || *refreshed.LastAgentRunID != 888 {
		t.Fatalf("last_agent_run_id not updated, got %v", refreshed.LastAgentRunID)
	}

	// Verify activity recorded
	activities, err := bugSvc.ListBugActivities(owner, project.ID, bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	var foundDispatch bool
	for _, a := range activities {
		if a.Action == projectmodel.BugActivityAgentDispatch {
			foundDispatch = true
			if a.Comment != "重点分析堆栈" {
				t.Fatalf("unexpected dispatch activity comment: %s", a.Comment)
			}
		}
	}
	if !foundDispatch {
		t.Fatalf("agent_dispatch activity was not recorded")
	}
}
