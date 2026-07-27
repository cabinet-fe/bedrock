package service

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	"bedrock/internal/rbac/repository"
)

// PermissionService computes permission unions and trimmed menu groups.
type PermissionService struct {
	roles     *repository.RoleRepository
	resources *repository.ResourceRepository
	groups    *repository.MenuGroupRepository
}

func NewPermissionService(
	roles *repository.RoleRepository,
	resources *repository.ResourceRepository,
	groups *repository.MenuGroupRepository,
) *PermissionService {
	return &PermissionService{roles: roles, resources: resources, groups: groups}
}

// ResolvePermissions returns the effective permission code set for a user.
// Super-admin receives every feature full_code. Non-super gets role union with
// super_admin_only features stripped.
func (s *PermissionService) ResolvePermissions(userID uint, isSuperAdmin bool) ([]string, error) {
	if isSuperAdmin {
		return s.allFeaturePermissions()
	}
	codes, err := s.roles.ListPermissionsByUserID(userID)
	if err != nil {
		return nil, err
	}
	return s.filterSuperAdminOnly(uniqSorted(codes))
}

// ResolveDataScope returns the widest data_scope among the user's roles.
// Super-admin is always all; otherwise any role with all wins, else self.
func (s *PermissionService) ResolveDataScope(userID uint, isSuperAdmin bool) (string, error) {
	if isSuperAdmin {
		return model.DataScopeAll, nil
	}
	scopes, err := s.roles.ListDataScopesByUserID(userID)
	if err != nil {
		return "", err
	}
	for _, scope := range scopes {
		if scope == model.DataScopeAll {
			return model.DataScopeAll, nil
		}
	}
	return model.DataScopeSelf, nil
}

// CheckAccess returns nil if the user may perform required permission.
// Resources marked super_admin_only always require is_super_admin.
func (s *PermissionService) CheckAccess(userID uint, isSuperAdmin bool, required string) error {
	if required == "" {
		return errForbidden("missing permission")
	}
	if isSuperAdmin {
		return nil
	}
	only, err := s.resources.IsSuperAdminOnly(required)
	if err != nil {
		return err
	}
	if only {
		return errForbidden("仅超级管理员可访问该功能")
	}
	codes, err := s.ResolvePermissions(userID, false)
	if err != nil {
		return err
	}
	if !rbac.HasPermission(rbac.ToSet(codes), required) {
		return errForbidden("没有权限: " + required)
	}
	return nil
}

// TrimMenus builds two-level menu groups for /auth/me (GroupNav).
// Rules: menu must be enabled, not hidden; non-super drops super_admin_only;
// user needs {menuCode}:view; empty groups omitted.
func (s *PermissionService) TrimMenus(userID uint, isSuperAdmin bool) ([]model.MenuGroupNode, error) {
	groups, err := s.groups.List()
	if err != nil {
		return nil, err
	}
	menus, err := s.resources.ListMenus()
	if err != nil {
		return nil, err
	}

	var viewSet map[string]struct{}
	if isSuperAdmin {
		viewSet = map[string]struct{}{}
		for _, m := range menus {
			viewSet[m.Code] = struct{}{}
		}
	} else {
		perms, err := s.ResolvePermissions(userID, false)
		if err != nil {
			return nil, err
		}
		viewSet = map[string]struct{}{}
		for _, p := range perms {
			menuCode, action, ok := rbac.SplitPermission(p)
			if ok && action == "view" {
				viewSet[menuCode] = struct{}{}
			}
		}
	}

	menusByGroup := map[uint][]model.RbacResource{}
	for _, m := range menus {
		if !m.Enabled || m.Hidden {
			continue
		}
		if m.SuperAdminOnly && !isSuperAdmin {
			continue
		}
		if _, ok := viewSet[m.Code]; !ok {
			continue
		}
		if m.GroupID == nil {
			continue
		}
		menusByGroup[*m.GroupID] = append(menusByGroup[*m.GroupID], m)
	}

	out := make([]model.MenuGroupNode, 0, len(groups))
	for _, g := range groups {
		if !g.Enabled {
			continue
		}
		items := menusByGroup[g.ID]
		if len(items) == 0 {
			continue
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].SortKey != items[j].SortKey {
				return items[i].SortKey < items[j].SortKey
			}
			return items[i].ID < items[j].ID
		})
		children := make([]model.MenuItemNode, 0, len(items))
		for _, m := range items {
			children = append(children, model.MenuItemNode{
				Title: menuTitle(m),
				Path:  m.Route,
				Icon:  menuIcon(m),
			})
		}
		out = append(out, model.MenuGroupNode{
			Title:    g.Name,
			Children: children,
		})
	}
	return out, nil
}

// PermissionCatalog returns group → menu → feature for role editors.
func (s *PermissionService) PermissionCatalog() ([]model.PermissionCatalogGroup, error) {
	groups, err := s.groups.List()
	if err != nil {
		return nil, err
	}
	menus, err := s.resources.ListMenus()
	if err != nil {
		return nil, err
	}
	features, err := s.resources.ListFeatures()
	if err != nil {
		return nil, err
	}

	featuresByParent := map[uint][]model.RbacResource{}
	for _, f := range features {
		if f.ParentID == nil {
			continue
		}
		featuresByParent[*f.ParentID] = append(featuresByParent[*f.ParentID], f)
	}
	menusByGroup := map[uint][]model.RbacResource{}
	for _, m := range menus {
		if m.GroupID == nil {
			continue
		}
		menusByGroup[*m.GroupID] = append(menusByGroup[*m.GroupID], m)
	}

	out := make([]model.PermissionCatalogGroup, 0, len(groups))
	for _, g := range groups {
		ms := menusByGroup[g.ID]
		sort.Slice(ms, func(i, j int) bool {
			if ms[i].SortKey != ms[j].SortKey {
				return ms[i].SortKey < ms[j].SortKey
			}
			return ms[i].ID < ms[j].ID
		})
		catMenus := make([]model.PermissionCatalogMenu, 0, len(ms))
		for _, m := range ms {
			fs := featuresByParent[m.ID]
			sort.Slice(fs, func(i, j int) bool {
				if fs[i].SortKey != fs[j].SortKey {
					return fs[i].SortKey < fs[j].SortKey
				}
				return fs[i].ID < fs[j].ID
			})
			catFeats := make([]model.PermissionCatalogFeature, 0, len(fs))
			for _, f := range fs {
				catFeats = append(catFeats, model.PermissionCatalogFeature{
					ID: f.ID, Code: f.Code, FullCode: f.FullCode, Type: f.Type,
					Title: f.Title, SuperAdminOnly: f.SuperAdminOnly, Enabled: f.Enabled,
				})
			}
			catMenus = append(catMenus, model.PermissionCatalogMenu{
				ID: m.ID, Code: m.Code, FullCode: m.FullCode, Title: menuTitle(m),
				SuperAdminOnly: m.SuperAdminOnly, Hidden: m.Hidden, Enabled: m.Enabled,
				Features: catFeats,
			})
		}
		out = append(out, model.PermissionCatalogGroup{
			ID: g.ID, Name: g.Name, Code: g.Code, Menus: catMenus,
		})
	}
	return out, nil
}

func (s *PermissionService) allFeaturePermissions() ([]string, error) {
	features, err := s.resources.ListFeatures()
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, f := range features {
		if f.FullCode != "" {
			set[f.FullCode] = struct{}{}
		}
	}
	stored, err := s.roles.ListDistinctPermissions()
	if err != nil {
		return nil, err
	}
	for _, p := range stored {
		set[p] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (s *PermissionService) filterSuperAdminOnly(codes []string) ([]string, error) {
	resources, err := s.resources.ListByFullCodes(codes)
	if err != nil {
		return nil, err
	}

	exactGates := make(map[string]bool, len(resources))
	for _, resource := range resources {
		exactGates[resource.FullCode] = resource.SuperAdminOnly
	}

	menuCodes := make(map[string]struct{})
	missingMenus := make(map[string]string)
	for _, code := range codes {
		if _, found := exactGates[code]; found {
			continue
		}
		menuCode, _, ok := rbac.SplitPermission(code)
		if !ok {
			continue
		}
		missingMenus[code] = menuCode
		menuCodes[menuCode] = struct{}{}
	}
	menus, err := s.resources.ListByFullCodes(slices.Collect(maps.Keys(menuCodes)))
	if err != nil {
		return nil, err
	}
	menuGates := make(map[string]bool, len(menus))
	for _, menu := range menus {
		if menu.Type == model.ResourceTypeMenu {
			menuGates[menu.FullCode] = menu.SuperAdminOnly
		}
	}

	out := make([]string, 0, len(codes))
	for _, code := range codes {
		if gated, found := exactGates[code]; found {
			if !gated {
				out = append(out, code)
			}
			continue
		}
		if menuGates[missingMenus[code]] {
			continue
		}
		out = append(out, code)
	}
	return out, nil
}

type forbiddenError struct{ msg string }

func (e *forbiddenError) Error() string { return e.msg }

func errForbidden(msg string) error { return &forbiddenError{msg: msg} }

// IsForbidden reports whether err is a permission denial.
func IsForbidden(err error) bool {
	_, ok := err.(*forbiddenError)
	return ok
}

func uniqSorted(codes []string) []string {
	set := rbac.ToSet(codes)
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func menuTitle(m model.RbacResource) string {
	if strings.TrimSpace(m.Title) != "" {
		return m.Title
	}
	return m.Code
}

func menuIcon(m model.RbacResource) string {
	if m.IconBase64 == "" {
		return ""
	}
	if m.IconMime != "" && !strings.HasPrefix(m.IconBase64, "data:") {
		return "data:" + m.IconMime + ";base64," + m.IconBase64
	}
	return m.IconBase64
}
