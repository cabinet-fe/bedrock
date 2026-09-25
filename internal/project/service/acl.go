package service

import (
	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"
	rbacmodel "bedrock/internal/rbac/model"
)

// AccessContext is resolved from the authenticated user once per request.
type AccessContext struct {
	UserID      uint
	SuperAdmin  bool
	DataScope   string
	Permissions map[string]struct{}
}

func NewAccessContext(userID uint, superAdmin bool, permissions []string) AccessContext {
	scope := rbacmodel.DataScopeSelf
	if superAdmin {
		scope = rbacmodel.DataScopeAll
	}
	return newAccessContext(userID, superAdmin, scope, permissions)
}

// NewAccessContextWithDataScope lets handlers inject ResolveDataScope results.
func NewAccessContextWithDataScope(userID uint, superAdmin bool, permissions []string, dataScope string) AccessContext {
	return newAccessContext(userID, superAdmin, dataScope, permissions)
}

func newAccessContext(userID uint, superAdmin bool, dataScope string, permissions []string) AccessContext {
	set := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		set[permission] = struct{}{}
	}
	return AccessContext{UserID: userID, SuperAdmin: superAdmin, DataScope: dataScope, Permissions: set}
}

// bypassProjectListFilter lets super admins, data_scope=all, or manage_all see all projects in list queries.
func (a AccessContext) bypassProjectListFilter() bool {
	return a.SuperAdmin || a.DataScope == rbacmodel.DataScopeAll || a.Has("project_projects:manage_all")
}

func (a AccessContext) Has(permission string) bool {
	if a.SuperAdmin {
		return true
	}
	_, ok := a.Permissions[permission]
	return ok
}

type aclCapability string

const (
	capProjectView      aclCapability = "project_view"
	capProjectManage    aclCapability = "project_manage"
	capMemberView       aclCapability = "member_view"
	capMemberManage     aclCapability = "member_manage"
	capOwnerTransfer    aclCapability = "owner_transfer"
	capRequirementView  aclCapability = "requirement_view"
	capRequirementEdit  aclCapability = "requirement_edit"
	capRequirementAdmin aclCapability = "requirement_admin"
	capDocView          aclCapability = "doc_view"
	capDocEdit          aclCapability = "doc_edit"
	capDocAdmin         aclCapability = "doc_admin"
	capDevDocView       aclCapability = "dev_doc_view"
	capDevDocEdit       aclCapability = "dev_doc_edit"
	capDevDocAdmin      aclCapability = "dev_doc_admin"
	capBugView          aclCapability = "bug_view"
	capBugEdit          aclCapability = "bug_edit"
	capBugAdmin         aclCapability = "bug_admin"
)

// projectACL implements DESIGN §4.4。
// Read side: with data_scope=self only members or creators are visible; writes still require manage_all or a project member role.
type projectACL struct {
	repo *repository.ProjectRepository
}

func newProjectACL(repo *repository.ProjectRepository) *projectACL {
	return &projectACL{repo: repo}
}

func (a *projectACL) Require(projectID uint, actor AccessContext, globalPermission string, capability aclCapability) (*model.ProjectMember, error) {
	if !actor.Has(globalPermission) {
		return nil, NewForbidden("缺少全局权限: " + globalPermission)
	}
	if actor.SuperAdmin || actor.Has("project_projects:manage_all") {
		return nil, nil
	}
	if isReadCapability(capability) {
		if actor.DataScope == rbacmodel.DataScopeAll {
			return nil, nil
		}
		return a.requireProjectReadAccess(projectID, actor, capability)
	}

	member, err := a.repo.FindMember(projectID, actor.UserID)
	if err != nil {
		if _, projErr := a.repo.FindProject(projectID); projErr != nil {
			return nil, NewNotFound("项目不存在")
		}
		return nil, NewForbidden("非项目成员无权操作")
	}
	if roleAllows(member.Role, capability) {
		return member, nil
	}
	return nil, NewForbidden("项目角色无此操作权限")
}

// CanListProjects checks list permission; data scope filtering happens in service/repo.
func (a *projectACL) CanListProjects(actor AccessContext) error {
	if !actor.Has("project_projects:view") {
		return NewForbidden("缺少全局权限: project_projects:view")
	}
	return nil
}

// requireProjectReadAccess requires membership or creator when data_scope=self.
func (a *projectACL) requireProjectReadAccess(projectID uint, actor AccessContext, capability aclCapability) (*model.ProjectMember, error) {
	member, err := a.repo.FindMember(projectID, actor.UserID)
	if err == nil {
		if roleAllows(member.Role, capability) {
			return member, nil
		}
		return nil, NewForbidden("项目角色无此操作权限")
	}
	project, err := a.repo.FindProject(projectID)
	if err != nil {
		return nil, NewNotFound("项目不存在")
	}
	if project.CreatedBy == actor.UserID {
		return nil, nil
	}
	return nil, NewForbidden("非项目成员无权查看")
}

func isReadCapability(capability aclCapability) bool {
	switch capability {
	case capProjectView, capMemberView, capRequirementView, capDocView, capDevDocView, capBugView:
		return true
	default:
		return false
	}
}

// issueReadScope returns the user ID that an actor's bug/requirement reads are
// limited to: collaborators (project member/readonly roles, or data-scope-self
// non-members) only see items they created or are assigned to. Project
// owner/admin and actors with full data scope (super admin, data_scope=all,
// manage_all) see everything, so nil is returned unrestricted.
func issueReadScope(actor AccessContext, member *model.ProjectMember) *uint {
	if actor.bypassProjectListFilter() {
		return nil
	}
	if member != nil && (member.Role == model.ProjectRoleOwner || member.Role == model.ProjectRoleAdmin) {
		return nil
	}
	userID := actor.UserID
	return &userID
}

// issueInvolvesUser reports whether a bug/requirement was created by or
// assigned to the user (the collaborator read scope).
func issueInvolvesUser(createdBy uint, assigneeID *uint, userID uint) bool {
	if createdBy == userID {
		return true
	}
	return assigneeID != nil && *assigneeID == userID
}

func roleAllows(role string, capability aclCapability) bool {
	switch capability {
	case capProjectView, capMemberView, capRequirementView, capDocView, capDevDocView, capBugView:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin ||
			role == model.ProjectRoleMember || role == model.ProjectRoleReadonly
	case capProjectManage, capOwnerTransfer:
		return role == model.ProjectRoleOwner
	case capMemberManage:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	case capRequirementEdit, capDocEdit, capDevDocEdit, capBugEdit:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin || role == model.ProjectRoleMember
	case capRequirementAdmin, capDocAdmin, capDevDocAdmin, capBugAdmin:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	default:
		return false
	}
}
