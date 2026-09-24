package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	"bedrock/internal/rbac/repository"
)

type ResourceService struct {
	resources *repository.ResourceRepository
	groups    *repository.MenuGroupRepository
}

func NewResourceService(resources *repository.ResourceRepository, groups *repository.MenuGroupRepository) *ResourceService {
	return &ResourceService{resources: resources, groups: groups}
}

type CreateResourceInput struct {
	Code           string `json:"code"`
	Type           string `json:"type"`
	GroupID        *uint  `json:"group_id"`
	ParentID       *uint  `json:"parent_id"`
	Enabled        *bool  `json:"enabled"`
	SortKey        int    `json:"sort_key"`
	Title          string `json:"title"`
	Route          string `json:"route"`
	Hidden         *bool  `json:"hidden"`
	SuperAdminOnly *bool  `json:"super_admin_only"`
}

func (s *ResourceService) Create(in CreateResourceInput, actorIsSuperAdmin bool) (*model.RbacResource, error) {
	code := strings.TrimSpace(in.Code)
	typ := strings.TrimSpace(in.Type)
	if code == "" || typ == "" {
		return nil, errors.New("code 与 type 不能为空")
	}
	if !rbac.ValidCode(code) {
		return nil, errors.New("code 不能包含 '.'")
	}
	if !validResourceType(typ) {
		return nil, errors.New("type 必须为 menu|action|card")
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	hidden := false
	if in.Hidden != nil {
		hidden = *in.Hidden
	}
	superOnly := false
	if in.SuperAdminOnly != nil {
		if *in.SuperAdminOnly && !actorIsSuperAdmin {
			return nil, errors.New("仅超级管理员可设置 super_admin_only")
		}
		superOnly = *in.SuperAdminOnly
	}

	res := &model.RbacResource{
		Code: code, Type: typ, Enabled: enabled, SortKey: in.SortKey,
		Title: strings.TrimSpace(in.Title), Route: strings.TrimSpace(in.Route),
		Hidden: hidden, SuperAdminOnly: superOnly,
	}

	switch typ {
	case model.ResourceTypeMenu:
		if in.GroupID == nil || *in.GroupID == 0 {
			return nil, errors.New("创建菜单必须指定 group_id")
		}
		if _, err := s.groups.FindByID(*in.GroupID); err != nil {
			return nil, errors.New("菜单分组不存在")
		}
		if in.ParentID != nil {
			return nil, errors.New("菜单不能设置 parent_id")
		}
		res.GroupID = in.GroupID
		res.FullCode = code
		if res.Title == "" {
			res.Title = code
		}
	default:
		if in.ParentID == nil || *in.ParentID == 0 {
			return nil, errors.New("创建功能必须挂到菜单 parent_id")
		}
		parent, err := s.resources.FindByID(*in.ParentID)
		if err != nil || !parent.IsMenu() {
			return nil, errors.New("父资源必须是菜单")
		}
		if in.GroupID != nil {
			return nil, errors.New("功能不能设置 group_id")
		}
		res.ParentID = in.ParentID
		res.FullCode = rbac.FeatureFullCode(parent.Code, code)
		if parent.SuperAdminOnly {
			res.SuperAdminOnly = true
		}
		if res.Title == "" {
			res.Title = code
		}
	}

	if err := s.resources.Create(res); err != nil {
		return nil, fmt.Errorf("创建资源失败: %w", err)
	}
	return s.resources.FindByID(res.ID)
}

func (s *ResourceService) Get(id uint) (*model.RbacResource, error) {
	return s.resources.FindByID(id)
}

type ListResourcesFilter struct {
	Keyword string
	Type    string
	Enabled *bool
	GroupID *uint
}

func (f ListResourcesFilter) active() bool {
	return strings.TrimSpace(f.Keyword) != "" || strings.TrimSpace(f.Type) != "" || f.Enabled != nil || f.GroupID != nil
}

func (s *ResourceService) ListTree(filter ListResourcesFilter) ([]model.RbacResource, error) {
	items, err := s.resources.ListAll()
	if err != nil {
		return nil, err
	}
	if filter.active() {
		if typ := strings.TrimSpace(filter.Type); typ != "" && !validResourceType(typ) {
			return nil, errors.New("type 必须为 menu|action|card")
		}
		items = filterResourcesWithAncestors(items, filter)
	}
	return buildResourceTree(items), nil
}

type UpdateResourceInput struct {
	Enabled        *bool  `json:"enabled"`
	SortKey        *int   `json:"sort_key"`
	Title          string `json:"title"`
	Route          *string `json:"route"`
	Code           *string `json:"code"`
	GroupID        *uint  `json:"group_id"`
	Hidden         *bool  `json:"hidden"`
	SuperAdminOnly *bool  `json:"super_admin_only"`
}

func (s *ResourceService) Update(id uint, in UpdateResourceInput, actorIsSuperAdmin bool) (*model.RbacResource, error) {
	res, err := s.resources.FindByID(id)
	if err != nil {
		return nil, err
	}
	oldFullCode := res.FullCode
	codeChanged := false

	if in.SuperAdminOnly != nil {
		if !actorIsSuperAdmin {
			return nil, errors.New("仅超级管理员可修改 super_admin_only")
		}
		res.SuperAdminOnly = *in.SuperAdminOnly
	}
	if in.Enabled != nil {
		res.Enabled = *in.Enabled
	}
	if in.SortKey != nil {
		res.SortKey = *in.SortKey
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		res.Title = t
	}
	if in.Route != nil {
		res.Route = strings.TrimSpace(*in.Route)
	}
	if in.Hidden != nil {
		if !res.IsMenu() {
			return nil, errors.New("仅菜单可设置 hidden")
		}
		res.Hidden = *in.Hidden
	}
	if in.GroupID != nil {
		if !res.IsMenu() {
			return nil, errors.New("仅菜单可设置 group_id")
		}
		if _, err := s.groups.FindByID(*in.GroupID); err != nil {
			return nil, errors.New("菜单分组不存在")
		}
		res.GroupID = in.GroupID
	}
	if in.Code != nil {
		newCode := strings.TrimSpace(*in.Code)
		if newCode == "" {
			return nil, errors.New("code 不能为空")
		}
		if !rbac.ValidCode(newCode) {
			return nil, errors.New("code 不能包含 '.'")
		}
		if newCode != res.Code {
			codeChanged = true
			res.Code = newCode
			if res.IsMenu() {
				res.FullCode = newCode
			} else if res.ParentID != nil {
				parent, err := s.resources.FindByID(*res.ParentID)
				if err != nil {
					return nil, err
				}
				res.FullCode = rbac.FeatureFullCode(parent.Code, newCode)
			}
		}
	}

	if err := s.resources.Update(res); err != nil {
		return nil, err
	}

	if codeChanged && res.IsMenu() {
		if err := s.cascadeMenuCodeChange(res, oldFullCode); err != nil {
			return nil, err
		}
	} else if codeChanged && res.IsFeature() && oldFullCode != res.FullCode {
		_ = s.resources.DeleteRolePermissionsByFullCodes([]string{oldFullCode})
	}

	return s.resources.FindByID(id)
}

func (s *ResourceService) cascadeMenuCodeChange(menu *model.RbacResource, oldMenuFullCode string) error {
	children, err := s.resources.ListByParentID(menu.ID)
	if err != nil {
		return err
	}
	stale := []string{oldMenuFullCode}
	for _, child := range children {
		stale = append(stale, child.FullCode)
		child.FullCode = rbac.FeatureFullCode(menu.Code, child.Code)
		if menu.SuperAdminOnly {
			child.SuperAdminOnly = true
		}
		if err := s.resources.Update(&child); err != nil {
			return err
		}
	}
	return s.resources.DeleteRolePermissionsByFullCodes(stale)
}

func (s *ResourceService) Delete(id uint) error {
	n, err := s.resources.CountChildren(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("请先删除子资源")
	}
	res, err := s.resources.FindByID(id)
	if err != nil {
		return err
	}
	if err := s.resources.DeleteRolePermissionsByFullCodes([]string{res.FullCode}); err != nil {
		return err
	}
	return s.resources.Delete(id)
}

// UpdateMenuIcon stores Base64 icon; rejects when raw decoded size > 32KB.
func (s *ResourceService) UpdateMenuIcon(resourceID uint, iconBase64, iconMime string) (*model.RbacResource, error) {
	res, err := s.resources.FindByID(resourceID)
	if err != nil {
		return nil, err
	}
	if res.Type != model.ResourceTypeMenu {
		return nil, errors.New("仅菜单资源可设置图标")
	}
	raw, mime, err := decodeIconPayload(iconBase64, iconMime)
	if err != nil {
		return nil, err
	}
	if len(raw) > rbac.MaxMenuIconBytes {
		return nil, fmt.Errorf("图标原始体积不得超过 32KB（当前 %d 字节）", len(raw))
	}
	res.IconBase64 = base64.StdEncoding.EncodeToString(raw)
	res.IconMime = mime
	if err := s.resources.Update(res); err != nil {
		return nil, err
	}
	return s.resources.FindByID(resourceID)
}

func decodeIconPayload(iconBase64, iconMime string) ([]byte, string, error) {
	payload := strings.TrimSpace(iconBase64)
	mime := strings.TrimSpace(iconMime)
	if payload == "" {
		return nil, "", errors.New("图标不能为空")
	}
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 {
			return nil, "", errors.New("无效的 data URL")
		}
		header := parts[0]
		payload = parts[1]
		if mime == "" {
			header = strings.TrimPrefix(header, "data:")
			header = strings.TrimSuffix(header, ";base64")
			mime = header
		}
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", errors.New("图标 Base64 解码失败")
		}
	}
	if mime == "" {
		mime = "image/png"
	}
	return raw, mime, nil
}

func validResourceType(t string) bool {
	switch t {
	case model.ResourceTypeMenu, model.ResourceTypeAction, model.ResourceTypeCard:
		return true
	default:
		return false
	}
}

func resourceMatchesFilter(item model.RbacResource, filter ListResourcesFilter) bool {
	if typ := strings.TrimSpace(filter.Type); typ != "" && item.Type != typ {
		return false
	}
	if filter.Enabled != nil && item.Enabled != *filter.Enabled {
		return false
	}
	if filter.GroupID != nil {
		if item.IsMenu() {
			if item.GroupID == nil || *item.GroupID != *filter.GroupID {
				return false
			}
		} else if item.ParentID == nil {
			return false
			// features matched via ancestor menu below
		}
	}
	keyword := strings.TrimSpace(filter.Keyword)
	if keyword == "" {
		if filter.GroupID != nil && item.IsFeature() {
			return false // features kept only via ancestor when keyword empty — handled in filterResourcesWithAncestors
		}
		return true
	}
	kw := strings.ToLower(keyword)
	if strings.Contains(strings.ToLower(item.Code), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(item.FullCode), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Title), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Route), kw) {
		return true
	}
	return false
}

func filterResourcesWithAncestors(items []model.RbacResource, filter ListResourcesFilter) []model.RbacResource {
	byID := make(map[uint]model.RbacResource, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	// When filtering by group, first collect menu IDs in that group.
	groupMenuIDs := map[uint]struct{}{}
	if filter.GroupID != nil {
		for _, item := range items {
			if item.IsMenu() && item.GroupID != nil && *item.GroupID == *filter.GroupID {
				groupMenuIDs[item.ID] = struct{}{}
			}
		}
	}

	keep := make(map[uint]struct{})
	for _, item := range items {
		match := resourceMatchesFilter(item, filter)
		if filter.GroupID != nil {
			if item.IsMenu() {
				match = match && item.GroupID != nil && *item.GroupID == *filter.GroupID
			} else if item.ParentID != nil {
				_, inGroup := groupMenuIDs[*item.ParentID]
				if !inGroup {
					match = false
				} else if strings.TrimSpace(filter.Keyword) == "" && strings.TrimSpace(filter.Type) == "" && filter.Enabled == nil {
					match = true
				} else {
					match = resourceMatchesFilter(item, ListResourcesFilter{
						Keyword: filter.Keyword, Type: filter.Type, Enabled: filter.Enabled,
					})
				}
			} else {
				match = false
			}
		}
		if !match {
			continue
		}
		cur := item.ID
		for {
			if _, ok := keep[cur]; ok {
				break
			}
			keep[cur] = struct{}{}
			node, ok := byID[cur]
			if !ok || node.ParentID == nil {
				break
			}
			cur = *node.ParentID
		}
	}
	out := make([]model.RbacResource, 0, len(keep))
	for _, item := range items {
		if _, ok := keep[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func buildResourceTree(items []model.RbacResource) []model.RbacResource {
	byID := make(map[uint]*model.RbacResource, len(items))
	for i := range items {
		cp := items[i]
		cp.Children = nil
		byID[cp.ID] = &cp
	}
	childrenOf := map[uint][]uint{}
	var rootIDs []uint
	for _, item := range items {
		if item.ParentID == nil {
			rootIDs = append(rootIDs, item.ID)
			continue
		}
		if _, ok := byID[*item.ParentID]; !ok {
			rootIDs = append(rootIDs, item.ID)
			continue
		}
		childrenOf[*item.ParentID] = append(childrenOf[*item.ParentID], item.ID)
	}
	var attach func(id uint) model.RbacResource
	attach = func(id uint) model.RbacResource {
		n := *byID[id]
		for _, cid := range childrenOf[id] {
			n.Children = append(n.Children, attach(cid))
		}
		return n
	}
	out := make([]model.RbacResource, 0, len(rootIDs))
	for _, id := range rootIDs {
		out = append(out, attach(id))
	}
	return out
}
