package service

import (
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	"bedrock/internal/rbac/repository"
)

type RoleService struct {
	roles     *repository.RoleRepository
	resources *repository.ResourceRepository
}

func NewRoleService(roles *repository.RoleRepository, resources *repository.ResourceRepository) *RoleService {
	return &RoleService{roles: roles, resources: resources}
}

func (s *RoleService) Create(name, code, description string, permissions []string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	code = strings.TrimSpace(code)
	if name == "" || code == "" {
		return nil, errors.New("名称与编码不能为空")
	}
	if code == model.RoleCodeSuperAdmin {
		return nil, errors.New("不能创建与内置超级管理员同编码的角色")
	}
	if err := s.validateBindablePermissions(permissions); err != nil {
		return nil, err
	}
	role := &model.Role{Name: name, Code: code, Description: description, Type: model.RoleTypeCustom}
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

func (s *RoleService) List(page, pageSize int) ([]model.Role, int64, error) {
	return s.roles.List(page, pageSize)
}

func (s *RoleService) Update(id uint, name, description string) (*model.Role, error) {
	role, err := s.roles.FindByID(id)
	if err != nil {
		return nil, err
	}
	if role.IsBuiltin() {
		return nil, errors.New("不能修改内置角色")
	}
	if name = strings.TrimSpace(name); name != "" {
		role.Name = name
	}
	role.Description = description
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

func (s *RoleService) SetPermissions(id uint, permissions []string) (*model.Role, error) {
	role, err := s.roles.FindByID(id)
	if err != nil {
		return nil, err
	}
	if role.IsBuiltin() {
		return nil, errors.New("不能修改内置角色权限")
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

func (s *RoleService) filterAssignableRoleIDs(roleIDs []uint) ([]uint, error) {
	out := make([]uint, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := s.roles.FindByID(id)
		if err != nil {
			return nil, fmt.Errorf("角色不存在: %d", id)
		}
		if role.Code == model.RoleCodeSuperAdmin || role.IsBuiltin() {
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
