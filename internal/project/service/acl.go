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

// NewAccessContextWithDataScope 供 handler 注入 ResolveDataScope 结果。
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

// bypassProjectListFilter 列表查询时超管、data_scope=all 或 manage_all 可见全部项目。
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
)

// projectACL implements DESIGN §4.4。
// 读侧：data_scope=self 时仅成员或创建人可见；写操作仍需 manage_all 或项目成员角色。
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

// CanListProjects 校验列表权限；数据范围过滤在 service/repo 层。
func (a *projectACL) CanListProjects(actor AccessContext) error {
	if !actor.Has("project_projects:view") {
		return NewForbidden("缺少全局权限: project_projects:view")
	}
	return nil
}

// requireProjectReadAccess data_scope=self 时要求成员身份或创建人。
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
	case capProjectView, capMemberView, capRequirementView, capDocView, capDevDocView:
		return true
	default:
		return false
	}
}

func roleAllows(role string, capability aclCapability) bool {
	switch capability {
	case capProjectView, capMemberView, capRequirementView, capDocView, capDevDocView:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin ||
			role == model.ProjectRoleMember || role == model.ProjectRoleReadonly
	case capProjectManage, capOwnerTransfer:
		return role == model.ProjectRoleOwner
	case capMemberManage:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	case capRequirementEdit, capDocEdit, capDevDocEdit:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin || role == model.ProjectRoleMember
	case capRequirementAdmin, capDocAdmin, capDevDocAdmin:
		return role == model.ProjectRoleOwner || role == model.ProjectRoleAdmin
	default:
		return false
	}
}
