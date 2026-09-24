package service

import (
	"testing"

	"bedrock/internal/pkg"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	rbacmodel "bedrock/internal/rbac/model"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
)

// TestIssueCollaboratorReadScope verifies the collaborator data scope:
// member/readonly project roles only see bugs and requirements they created
// or are assigned to; owner/admin and full-data-scope actors see everything.
func TestIssueCollaboratorReadScope(t *testing.T) {
	bugSvc, projectSvc, _, _, gdb := newTestEnv(t)
	storage, err := storageservice.NewStorageService(
		storagerepo.NewStorageRepository(gdb), t.TempDir(), storageservice.Limits{},
	)
	if err != nil {
		t.Fatal(err)
	}
	issueSvc := NewIssueService(projectrepo.NewIssueRepository(gdb), projectrepo.NewProjectRepository(gdb), storage)

	requirementPerms := []string{
		"project_requirements:create", "project_requirements:view",
		"project_requirements:update", "project_requirements:delete",
	}
	owner := actor(1, append(allBugPermissions(), requirementPerms...)...)
	project := createProject(t, projectSvc, owner, "scope-project")
	for _, m := range []struct {
		id   uint
		role string
	}{
		{2, projectmodel.ProjectRoleMember},
		{3, projectmodel.ProjectRoleReadonly},
		{5, projectmodel.ProjectRoleAdmin},
	} {
		if _, err := projectSvc.AddMember(owner, project.ID, MemberInput{UserID: m.id, Role: m.role}); err != nil {
			t.Fatal(err)
		}
	}
	memberUser := actor(2, append(allBugPermissions(), requirementPerms...)...)
	readonlyUser := actor(3, allBugPermissions()...)
	adminUser := actor(5, allBugPermissions()...)

	// bugAssigned is assigned to member 2; bugOther belongs to nobody but owner.
	assignee := uint(2)
	bugAssigned, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Assigned to member", AssigneeID: &assignee})
	if err != nil {
		t.Fatal(err)
	}
	bugOther, err := bugSvc.CreateBug(owner, project.ID, CreateBugInput{Title: "Other bug"})
	if err != nil {
		t.Fatal(err)
	}
	bugMine, err := bugSvc.CreateBug(memberUser, project.ID, CreateBugInput{Title: "Created by member"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Member list: only the assigned + self-created bugs.
	items, total, err := bugSvc.ListProjectBugs(memberUser, project.ID, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("member should see 2 bugs, got total=%d items=%d", total, len(items))
	}
	seen := map[uint]bool{}
	for _, b := range items {
		seen[b.ID] = true
	}
	if !seen[bugAssigned.ID] || !seen[bugMine.ID] || seen[bugOther.ID] {
		t.Fatalf("member visibility mismatch: %+v", seen)
	}

	// 2. Member detail: foreign bug is not found, own-visible bug is readable.
	if _, err := bugSvc.GetBug(memberUser, project.ID, bugOther.ID); !IsNotFound(err) {
		t.Fatalf("member reading a foreign bug must be 404, got %v", err)
	}
	if _, err := bugSvc.GetBug(memberUser, project.ID, bugAssigned.ID); err != nil {
		t.Fatalf("member reading the assigned bug failed: %v", err)
	}

	// 3. Kanban cards follow the same scope.
	board, err := issueSvc.Kanban(memberUser, &project.ID, projectmodel.IssueTypeBug, true, projectrepo.IssueFilter{})
	if err != nil {
		t.Fatal(err)
	}
	cardCount := 0
	for _, column := range board.Columns {
		cardCount += len(column.Cards)
	}
	if cardCount != 2 {
		t.Fatalf("member kanban should render 2 cards, got %d", cardCount)
	}

	// 4. Status counts follow the same scope.
	counts, err := bugSvc.CountByStatus(memberUser, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var memberTotal int64
	for _, c := range counts {
		memberTotal += c
	}
	if memberTotal != 2 {
		t.Fatalf("member status counts should sum to 2, got %d", memberTotal)
	}

	// 5. Readonly member with no participation sees nothing.
	_, totalReadonly, err := bugSvc.ListProjectBugs(readonlyUser, project.ID, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if totalReadonly != 0 {
		t.Fatalf("readonly member without participation should see 0 bugs, got %d", totalReadonly)
	}

	// 6. Admin and full-data-scope actors see everything.
	for name, user := range map[string]AccessContext{
		"admin":          adminUser,
		"data-scope-all": actorWithScope(6, rbacmodel.DataScopeAll, allBugPermissions()...),
	} {
		_, totalAll, err := bugSvc.ListProjectBugs(user, project.ID, projectrepo.BugFilter{}, pkg.ListQuery{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("%s list failed: %v", name, err)
		}
		if totalAll != 3 {
			t.Fatalf("%s should see all 3 bugs, got %d", name, totalAll)
		}
	}

	// 7. Requirements follow the same collaborator scope.
	reqAssigned, err := projectSvc.CreateRequirement(owner, project.ID, RequirementInput{Title: "Assigned requirement", AssigneeID: &assignee})
	if err != nil {
		t.Fatal(err)
	}
	reqOther, err := projectSvc.CreateRequirement(owner, project.ID, RequirementInput{Title: "Other requirement"})
	if err != nil {
		t.Fatal(err)
	}
	reqs, totalReqs, err := projectSvc.ListRequirements(memberUser, project.ID, RequirementFilter{
		ListQuery: pkg.ListQuery{Page: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if totalReqs != 1 || len(reqs) != 1 || reqs[0].ID != reqAssigned.ID {
		t.Fatalf("member should only see the assigned requirement, got total=%d items=%+v", totalReqs, reqs)
	}
	if _, err := projectSvc.GetRequirement(memberUser, reqOther.ID); !IsNotFound(err) {
		t.Fatalf("member reading a foreign requirement must be 404, got %v", err)
	}
}
