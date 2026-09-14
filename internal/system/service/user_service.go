package service

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	rbacservice "bedrock/internal/rbac/service"
)

type UserService struct {
	users *authrepo.UserRepository
	roles *rbacservice.RoleService
}

func NewUserService(users *authrepo.UserRepository, roles *rbacservice.RoleService) *UserService {
	return &UserService{users: users, roles: roles}
}

// UserDTO includes assigned role IDs for admin UIs.
type UserDTO struct {
	authmodel.User
	RoleIDs []uint `json:"role_ids"`
}

func (s *UserService) List(q pkg.ListQuery) ([]UserDTO, int64, error) {
	users, total, err := s.users.List(q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]UserDTO, 0, len(users))
	for i := range users {
		ids, _ := s.roles.ListRoleIDs(users[i].ID)
		out = append(out, UserDTO{User: users[i], RoleIDs: ids})
	}
	return out, total, nil
}

func (s *UserService) Get(id uint) (*UserDTO, error) {
	u, err := s.users.FindByID(id)
	if err != nil {
		return nil, err
	}
	ids, err := s.roles.ListRoleIDs(id)
	if err != nil {
		return nil, err
	}
	return &UserDTO{User: *u, RoleIDs: ids}, nil
}

type CreateUserInput struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    *bool  `json:"is_active"`
	RoleIDs     []uint `json:"role_ids"`
}

func (s *UserService) Create(in CreateUserInput) (*UserDTO, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" || in.Password == "" {
		return nil, errors.New("用户名与密码不能为空")
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	hash, err := pkg.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	u := &authmodel.User{
		Username:     username,
		PasswordHash: hash,
		DisplayName:  strings.TrimSpace(in.DisplayName),
		Email:        strings.TrimSpace(in.Email),
		IsActive:     active,
		IsSuperAdmin: false,
	}
	if err := s.users.Create(u); err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	if err := s.roles.SetUserRoles(u.ID, in.RoleIDs); err != nil {
		return nil, err
	}
	return s.Get(u.ID)
}

type UpdateUserInput struct {
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Password    *string `json:"password"`
	IsActive    *bool   `json:"is_active"`
	RoleIDs     *[]uint `json:"role_ids"`
}

func (s *UserService) Update(id uint, in UpdateUserInput) (*UserDTO, error) {
	u, err := s.users.FindByID(id)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Email != nil {
		u.Email = strings.TrimSpace(*in.Email)
	}
	if in.IsActive != nil {
		if u.IsSuperAdmin && !*in.IsActive {
			return nil, errors.New("不能禁用内置超级管理员")
		}
		u.IsActive = *in.IsActive
	}
	if in.Password != nil && *in.Password != "" {
		hash, err := pkg.HashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = hash
	}
	if err := s.users.Update(u); err != nil {
		return nil, err
	}
	if in.RoleIDs != nil {
		if u.IsSuperAdmin {
			// Super-admin keeps builtin role via seed sync; API may still set custom roles.
			// Reject if payload tries to include/exclude in a way that binds super_admin role.
		}
		if err := s.roles.SetUserRoles(id, *in.RoleIDs); err != nil {
			return nil, err
		}
		// Re-bind builtin role for the sole super-admin after ReplaceUserRoles wiped joins.
		if u.IsSuperAdmin {
			if err := s.roles.EnsureSuperAdminRoleBound(id); err != nil {
				return nil, err
			}
		}
	}
	return s.Get(id)
}

func (s *UserService) Delete(id uint) error {
	u, err := s.users.FindByID(id)
	if err != nil {
		return err
	}
	if u.IsSuperAdmin {
		return errors.New("不能删除内置超级管理员")
	}
	if err := s.roles.SetUserRoles(id, nil); err != nil {
		return err
	}
	return s.users.Delete(id)
}

// UpdateMyEmailInput carries the new email for the authenticated user.
type UpdateMyEmailInput struct {
	Email string `json:"email"`
}

// UpdateMyEmail lets the authenticated user change only their own email.
func (s *UserService) UpdateMyEmail(userID uint, in UpdateMyEmailInput) (*UserDTO, error) {
	email := strings.TrimSpace(in.Email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	taken, err := s.users.CountByEmail(email, userID)
	if err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, errors.New("邮箱已被使用")
	}
	u, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	u.Email = email
	if err := s.users.Update(u); err != nil {
		return nil, fmt.Errorf("更新邮箱失败: %w", err)
	}
	return s.Get(userID)
}

// ChangeMyPasswordInput carries old/new passwords for self-service change.
type ChangeMyPasswordInput struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangeMyPassword verifies the old password, then rotates the hash for the authenticated user.
func (s *UserService) ChangeMyPassword(userID uint, in ChangeMyPasswordInput) error {
	if in.OldPassword == "" || in.NewPassword == "" {
		return errors.New("原密码与新密码不能为空")
	}
	u, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if !pkg.CheckPassword(in.OldPassword, u.PasswordHash) {
		return errors.New("原密码错误")
	}
	hash, err := pkg.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	return s.users.Update(u)
}

func validateEmail(email string) error {
	if email == "" {
		return errors.New("邮箱不能为空")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("邮箱格式不正确")
	}
	return nil
}
