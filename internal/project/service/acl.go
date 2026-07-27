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
	Permissions map[string]struct{}
	// DataScope is the widest role data_scope (self|all). Super-admin is always all.
	DataScope string
}

func NewAccessContext(userID uint, superAdmin bool, permissions []string) AccessContext {
	set := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		set[permission] = struct{}{}
	}
	scope := rbacmodel.DataScopeSelf
	if superAdmin {
		scope = rbacmodel.DataScopeAll
	}
	return AccessContext{UserID: userID, SuperAdmin: superAdmin, Permissions: set, DataScope: scope}
}

func (a AccessContext) Has(permission string) bool {
	if a.SuperAdmin {
		return true
	}
	_, ok := a.Permissions[permission]
	return ok
}

// HasDataScopeAll reports whether the actor may read all projects (role data_scope).
func (a AccessContext) HasDataScopeAll() bool {
	return a.SuperAdmin || a.DataScope == rbacmodel.DataScopeAll
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
)

// projectACL implements DESIGN §4.4. Every object operation requires the
// endpoint's global permission first, then manage_all/view_all/data_scope or membership.
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
	if actor.SuperAdmin {
		return nil, nil
	}
	manageAll := actor.Has("project_projects:manage_all")
	viewAll := actor.Has("project_projects:view_all") || manageAll || actor.HasDataScopeAll()
	if isReadCapability(capability) && viewAll {
		return nil, nil
	}
	if isWriteCapability(capability) && manageAll {
		return nil, nil
	}

	member, err := a.repo.FindMember(projectID, actor.UserID)
	if err != nil {
		// 非成员：公开项目仅放宽读
		if isReadCapability(capability) {
			project, perr := a.repo.FindProject(projectID)
			if perr == nil && project.IsPublic {
				return nil, nil
			}
		}
		return nil, NewNotFound("项目不存在或无权访问")
	}
	if roleAllows(member.Role, capability) {
		return member, nil
	}
	return nil, NewForbidden("项目角色无此操作权限")
}

func (a *projectACL) CanListProjects(actor AccessContext) (bool, error) {
	if !actor.Has("project_projects:view") {
		return false, NewForbidden("缺少全局权限: project_projects:view")
	}
	return actor.SuperAdmin || actor.Has("project_projects:view_all") || actor.Has("project_projects:manage_all") || actor.HasDataScopeAll(), nil
}

func isReadCapability(capability aclCapability) bool {
	switch capability {
	case capProjectView, capMemberView, capRequirementView, capDocView:
		return true
	default:
		return false
	}
}

func isWriteCapability(capability aclCapability) bool {
	return !isReadCapability(capability)
}

func roleAllows(role string, capability aclCapability) bool {
	switch capability {
	case capProjectView, capMemberView, capRequirementView, capDocView:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin ||
			role == model.ProjectRoleMember || role == model.ProjectRoleReadonly
	case capProjectManage, capOwnerTransfer:
		return role == model.ProjectRoleOwner
	case capMemberManage:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	case capRequirementEdit, capDocEdit:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin || role == model.ProjectRoleMember
	case capRequirementAdmin, capDocAdmin:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	default:
		return false
	}
}
