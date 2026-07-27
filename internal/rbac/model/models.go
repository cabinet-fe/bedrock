package model

import "time"

const (
	ResourceTypeMenu   = "menu"
	ResourceTypeAction = "action"
	ResourceTypeCard   = "card"

	RoleTypeBuiltin = "builtin"
	RoleTypeCustom  = "custom"

	RoleCodeSuperAdmin = "super_admin"

	// DataScopeSelf：仅自己创建的数据（项目另含成员例外）
	DataScopeSelf = "self"
	// DataScopeAll：可读全部（项目写权限仍靠成员 / manage_all）
	DataScopeAll = "all"
)

// MenuGroup organizes menus for navigation and resource admin (not permission-checked).
type MenuGroup struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Code        string    `json:"code" gorm:"size:100;uniqueIndex;not null"`
	RoutePrefix string    `json:"route_prefix" gorm:"size:200"`
	SortKey     int       `json:"sort_key" gorm:"not null;default:0"`
	Enabled     bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (MenuGroup) TableName() string { return "menu_groups" }

// Role is a permission bundle. Builtin super_admin is synced 1:1 with is_super_admin.
type Role struct {
	ID          uint             `json:"id" gorm:"primaryKey"`
	Name        string           `json:"name" gorm:"size:100;uniqueIndex;not null"`
	Code        string           `json:"code" gorm:"size:100;uniqueIndex;not null"`
	Description string           `json:"description" gorm:"size:500"`
	Type        string           `json:"type" gorm:"size:20;not null;default:custom"`
	DataScope   string           `json:"data_scope" gorm:"size:20;not null;default:self"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Permissions []RolePermission `json:"permissions,omitempty" gorm:"foreignKey:RoleID"`
}

func (Role) TableName() string { return "roles" }

func (r Role) IsBuiltin() bool { return r.Type == RoleTypeBuiltin }

// RolePermission binds a feature full_code to a role.
type RolePermission struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	RoleID     uint   `json:"role_id" gorm:"index;not null"`
	Permission string `json:"permission" gorm:"size:200;not null;index"`
}

func (RolePermission) TableName() string { return "role_permissions" }

// UserRole is the user↔role M2M join row.
type UserRole struct {
	UserID uint `json:"user_id" gorm:"primaryKey"`
	RoleID uint `json:"role_id" gorm:"primaryKey"`
}

func (UserRole) TableName() string { return "user_roles" }

// RbacResource is a menu or feature (action/card). Menu display fields live on the row.
type RbacResource struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	Code           string         `json:"code" gorm:"size:100;not null;index"`
	FullCode       string         `json:"full_code" gorm:"size:200;uniqueIndex;not null"`
	Type           string         `json:"type" gorm:"size:20;not null;index"`
	GroupID        *uint          `json:"group_id" gorm:"index"`
	ParentID       *uint          `json:"parent_id" gorm:"index"`
	SuperAdminOnly bool           `json:"super_admin_only" gorm:"not null;default:false"`
	Hidden         bool           `json:"hidden" gorm:"not null;default:false"`
	Enabled        bool           `json:"enabled" gorm:"not null;default:true"`
	SortKey        int            `json:"sort_key" gorm:"not null;default:0"`
	Title          string         `json:"title" gorm:"size:100"`
	Route          string         `json:"route" gorm:"size:200"`
	IconBase64     string         `json:"icon_base64,omitempty" gorm:"type:text"`
	IconMime       string         `json:"icon_mime,omitempty" gorm:"size:64"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Children       []RbacResource `json:"children,omitempty" gorm:"-"`
}

func (RbacResource) TableName() string { return "rbac_resources" }

func (r RbacResource) IsMenu() bool {
	return r.Type == ResourceTypeMenu
}

func (r RbacResource) IsFeature() bool {
	return r.Type == ResourceTypeAction || r.Type == ResourceTypeCard
}

// MenuGroupNode is the two-level nav shape for login /auth/me (GroupNavGroup).
type MenuGroupNode struct {
	Title    string         `json:"title"`
	Children []MenuItemNode `json:"children"`
}

// MenuItemNode is a leaf nav item under a group.
type MenuItemNode struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	Icon  string `json:"icon,omitempty"`
}

// PermissionCatalogGroup is the three-level catalog for role permission editors.
type PermissionCatalogGroup struct {
	ID    uint                     `json:"id"`
	Name  string                   `json:"name"`
	Code  string                   `json:"code"`
	Menus []PermissionCatalogMenu  `json:"menus"`
}

type PermissionCatalogMenu struct {
	ID             uint                        `json:"id"`
	Code           string                      `json:"code"`
	FullCode       string                      `json:"full_code"`
	Title          string                      `json:"title"`
	SuperAdminOnly bool                        `json:"super_admin_only"`
	Hidden         bool                        `json:"hidden"`
	Enabled        bool                        `json:"enabled"`
	Features       []PermissionCatalogFeature  `json:"features"`
}

type PermissionCatalogFeature struct {
	ID             uint   `json:"id"`
	Code           string `json:"code"`
	FullCode       string `json:"full_code"`
	Type           string `json:"type"`
	Title          string `json:"title,omitempty"`
	SuperAdminOnly bool   `json:"super_admin_only"`
	Enabled        bool   `json:"enabled"`
}
