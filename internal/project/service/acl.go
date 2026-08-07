package service

import (
	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"
)

// AccessContext is resolved from the authenticated user once per request.
type AccessContext struct {
	UserID      uint
	SuperAdmin  bool
	Permissions map[string]struct{}
}

func NewAccessContext(userID uint, superAdmin bool, permissions []string) AccessContext {
	set := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		set[permission] = struct{}{}
	}
	return AccessContext{UserID: userID, SuperAdmin: superAdmin, Permissions: set}
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

// projectACL implements DESIGN §4.4（读侧已按 D2 放开：全局权限通过后即可读）。
// 写操作仍需 manage_all 或项目成员角色。
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
	// D2：全局权限通过后，读能力对任意认证用户放行
	if isReadCapability(capability) {
		return nil, nil
	}
	if actor.Has("project_projects:manage_all") {
		return nil, nil
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

// CanListProjects 校验列表权限。D2：持有 project_projects:view 即可列出全部项目。
func (a *projectACL) CanListProjects(actor AccessContext) error {
	if !actor.Has("project_projects:view") {
		return NewForbidden("缺少全局权限: project_projects:view")
	}
	return nil
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
