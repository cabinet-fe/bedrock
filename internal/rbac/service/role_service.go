package service

import (
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	"bedrock/internal/rbac/repository"
)

// RoleService manages roles and their feature-permission bindings. Builtin
// roles ship with the platform (developer/tester/... plus super_admin): their
// name/code stay fixed, but description, data_scope and permission bindings
// are editable. super_admin is the only fully locked role.
type RoleService struct {
	roles     *repository.RoleRepository
	resources *repository.ResourceRepository
}

func NewRoleService(roles *repository.RoleRepository, resources *repository.ResourceRepository) *RoleService {
	return &RoleService{roles: roles, resources: resources}
}

func (s *RoleService) Create(name, code, description, dataScope string, permissions []string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	code = strings.TrimSpace(code)
	if name == "" || code == "" {
		return nil, errors.New("名称与编码不能为空")
	}
	if code == model.RoleCodeSuperAdmin {
		return nil, errors.New("不能创建与内置超级管理员同编码的角色")
	}
	scope, err := normalizeDataScope(dataScope, model.DataScopeSelf)
	if err != nil {
		return nil, err
	}
	if err := s.validateBindablePermissions(permissions); err != nil {
		return nil, err
	}
	role := &model.Role{
		Name: name, Code: code, Description: description,
		Type: model.RoleTypeCustom, DataScope: scope,
	}
	if err := s.roles.Create(role); err != nil {
		return nil, fmt.Errorf("创建角色失败: %w", err)
	}
	if err := s.roles.ReplacePermissions(role.ID, permissions); err != nil {
		return nil, err
	}
	return s.roles.FindByID(role.ID)
}

func (s *RoleService) Get(id uint) (*model.Role, error) {
	return s.roles.FindByID(id)
}

func (s *RoleService) List(q pkg.ListQuery) ([]model.Role, int64, error) {
	return s.roles.List(q)
}

// Update edits identity fields. Custom roles may rename; builtin roles keep
// name/code and only take description / data_scope changes.
func (s *RoleService) Update(id uint, name, description, dataScope string) (*model.Role, error) {
	role, err := s.roles.FindByID(id)
	if err != nil {
		return nil, err
	}
	if role.IsSuperAdmin() {
		return nil, errors.New("不能修改内置超级管理员")
	}
	if !role.IsBuiltin() {
		if name = strings.TrimSpace(name); name != "" {
			role.Name = name
		}
	}
	role.Description = description
	if strings.TrimSpace(dataScope) != "" {
		scope, err := normalizeDataScope(dataScope, role.DataScope)
		if err != nil {
			return nil, err
		}
		role.DataScope = scope
	}
	if err := s.roles.Update(role); err != nil {
		return nil, err
	}
	return s.roles.FindByID(id)
}

func (s *RoleService) Delete(id uint) error {
	role, err := s.roles.FindByID(id)
	if err != nil {
		return err
	}
	if role.IsBuiltin() {
		return errors.New("不能删除内置角色")
	}
	return s.roles.Delete(id)
}

// SetPermissions replaces the role's feature permission bindings. Every role
// except super_admin is editable; super_admin is driven by is_super_admin.
func (s *RoleService) SetPermissions(id uint, permissions []string) (*model.Role, error) {
	role, err := s.roles.FindByID(id)
	if err != nil {
		return nil, err
	}
	if role.IsSuperAdmin() {
		return nil, errors.New("内置超级管理员拥有全部权限，不可修改绑定")
	}
	if err := s.validateBindablePermissions(permissions); err != nil {
		return nil, err
	}
	if err := s.roles.ReplacePermissions(id, permissions); err != nil {
		return nil, err
	}
	return s.roles.FindByID(id)
}

func (s *RoleService) SetUserRoles(userID uint, roleIDs []uint) error {
	filtered, err := s.filterAssignableRoleIDs(roleIDs)
	if err != nil {
		return err
	}
	return s.roles.ReplaceUserRoles(userID, filtered)
}

// ListRoleIDs returns role IDs assigned to a user.
func (s *RoleService) ListRoleIDs(userID uint) ([]uint, error) {
	return s.roles.ListRoleIDsByUserID(userID)
}

// EnsureSuperAdminRoleBound binds the builtin super_admin role to userID.
func (s *RoleService) EnsureSuperAdminRoleBound(userID uint) error {
	role, err := s.roles.FindByCode(model.RoleCodeSuperAdmin)
	if err != nil {
		return fmt.Errorf("内置超级管理员角色不存在: %w", err)
	}
	return s.roles.EnsureUserHasRole(userID, role.ID)
}

// EnsureBuiltinRoleBound binds a builtin role (by code) to userID
// (self-registration path; SetUserRoles rejects only super_admin).
func (s *RoleService) EnsureBuiltinRoleBound(userID uint, code string) error {
	role, err := s.roles.FindByCode(code)
	if err != nil {
		return fmt.Errorf("内置角色不存在: %s", code)
	}
	if !role.IsBuiltin() || role.IsSuperAdmin() {
		return fmt.Errorf("非内置可选角色: %s", code)
	}
	return s.roles.EnsureUserHasRole(userID, role.ID)
}

func (s *RoleService) filterAssignableRoleIDs(roleIDs []uint) ([]uint, error) {
	out := make([]uint, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := s.roles.FindByID(id)
		if err != nil {
			return nil, fmt.Errorf("角色不存在: %d", id)
		}
		if role.IsSuperAdmin() {
			return nil, errors.New("不能通过用户角色绑定分配内置超级管理员角色")
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *RoleService) validateBindablePermissions(permissions []string) error {
	for _, p := range permissions {
		if p == "" {
			continue
		}
		if _, _, ok := rbac.SplitPermission(p); !ok {
			return fmt.Errorf("无效权限码: %s", p)
		}
		res, err := s.resources.FindByFullCode(p)
		if err != nil {
			return fmt.Errorf("权限资源不存在: %s", p)
		}
		if !res.IsFeature() {
			return fmt.Errorf("只能绑定功能权限: %s", p)
		}
		if res.SuperAdminOnly {
			return fmt.Errorf("不能绑定仅超级管理员功能: %s", p)
		}
		only, err := s.resources.IsSuperAdminOnly(p)
		if err != nil {
			return err
		}
		if only {
			return fmt.Errorf("不能绑定仅超级管理员功能: %s", p)
		}
	}
	return nil
}

func normalizeDataScope(raw, fallback string) (string, error) {
	scope := strings.TrimSpace(raw)
	if scope == "" {
		if fallback == "" {
			return model.DataScopeSelf, nil
		}
		return fallback, nil
	}
	switch scope {
	case model.DataScopeSelf, model.DataScopeAll:
		return scope, nil
	default:
		return "", errors.New("data_scope 必须为 self 或 all")
	}
}
