package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bedrock/internal/pkg"
	projectmodel "bedrock/internal/project/model"
	"bedrock/internal/project/repository"
	storagemodel "bedrock/internal/storage/model"
	storageservice "bedrock/internal/storage/service"

	"gorm.io/gorm"
)

// DocsAIBridge creates AgentRuns for documentation generation (P4).
type DocsAIBridge interface {
	StartDocsGenerate(userID, projectID, nodeID, agentID uint) (runID uint, err error)
}

type ProjectService struct {
	repo    *repository.ProjectRepository
	storage *storageservice.StorageService
	acl     *projectACL
	docsAI  DocsAIBridge
}

func NewProjectService(repo *repository.ProjectRepository, storage *storageservice.StorageService) *ProjectService {
	return &ProjectService{repo: repo, storage: storage, acl: newProjectACL(repo)}
}

func (s *ProjectService) SetDocsAIBridge(bridge DocsAIBridge) {
	s.docsAI = bridge
}

type CreateProjectInput struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Description  string `json:"description"`
	RepositoryID *uint  `json:"repository_id"`
	Tags         string `json:"tags"`
	IsPublic     *bool  `json:"is_public"`
}

type UpdateProjectInput struct {
	Name            *string `json:"name"`
	Slug            *string `json:"slug"`
	Description     *string `json:"description"`
	Status          *string `json:"status"`
	RepositoryID    *uint   `json:"repository_id"`
	ClearRepository bool    `json:"clear_repository"`
	Tags            *string `json:"tags"`
	IsPublic        *bool   `json:"is_public"`
}

type ProjectListFilter struct {
	pkg.ListQuery
	Keyword string `form:"keyword"`
	Status  string `form:"status"`
}

// ProjectCapabilities reports the actions that the authenticated actor may
// perform on one project. It incorporates both global RBAC and project ACL.
type ProjectCapabilities struct {
	Update        bool `json:"update"`
	Archive       bool `json:"archive"`
	Delete        bool `json:"delete"`
	ManageMembers bool `json:"manage_members"`
	TransferOwner bool `json:"transfer_owner"`
}

// ProjectView augments a project with the caller's project-local role and
// effective capabilities. It is used only for ACL-aware read responses.
type ProjectView struct {
	projectmodel.ProductProject
	MyRole      string              `json:"my_role,omitempty"`
	Permissions ProjectCapabilities `json:"permissions"`
}

func (s *ProjectService) CreateProject(actor AccessContext, input CreateProjectInput) (*projectmodel.ProductProject, error) {
	if !actor.Has("project_projects:create") {
		return nil, NewForbidden("缺少全局权限: project_projects:create")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("项目名称不能为空")
	}
	if strings.TrimSpace(input.Slug) == "" {
		return nil, errors.New("项目标识不能为空")
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		return nil, errors.New("项目标识无效，仅支持字母、数字与连字符")
	}
	project := &projectmodel.ProductProject{
		Name: name, Slug: slug, Description: strings.TrimSpace(input.Description),
		Status: projectmodel.ProjectStatusActive, OwnerID: actor.UserID, CreatedBy: actor.UserID,
		RepositoryID: input.RepositoryID, Tags: strings.TrimSpace(input.Tags),
		IsPublic: input.IsPublic != nil && *input.IsPublic,
	}
	if err := s.repo.CreateProjectWithOwner(project); err != nil {
		if isUniqueError(err) {
			return nil, NewConflict("项目标识已存在")
		}
		return nil, err
	}
	return project, nil
}

func (s *ProjectService) ListProjects(actor AccessContext, filter ProjectListFilter) ([]ProjectView, int64, error) {
	if err := s.acl.CanListProjects(actor); err != nil {
		return nil, 0, err
	}
	projects, total, err := s.repo.ListProjects(filter.ListQuery, filter.Keyword, filter.Status)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.projectViews(actor, projects)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

func (s *ProjectService) GetProject(actor AccessContext, id uint) (*ProjectView, error) {
	if _, err := s.acl.Require(id, actor, "project_projects:view", capProjectView); err != nil {
		return nil, err
	}
	project, err := s.repo.FindProject(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("项目不存在")
	}
	if err != nil {
		return nil, err
	}
	views, err := s.projectViews(actor, []projectmodel.ProductProject{*project})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// ResolveProjectRef 解析路径参数：正整数按 ID；否则 normalizeSlug 后按 slug 查找。
func (s *ProjectService) ResolveProjectRef(ref string) (uint, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, NewNotFound("项目不存在")
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil && id > 0 {
		if _, err := s.repo.FindProject(uint(id)); err != nil {
			return 0, NewNotFound("项目不存在")
		}
		return uint(id), nil
	}
	slug := normalizeSlug(ref)
	if slug == "" {
		return 0, NewNotFound("项目不存在")
	}
	project, err := s.repo.FindProjectBySlug(slug)
	if err != nil {
		return 0, NewNotFound("项目不存在")
	}
	return project.ID, nil
}

func (s *ProjectService) projectViews(actor AccessContext, projects []projectmodel.ProductProject) ([]ProjectView, error) {
	ids := make([]uint, len(projects))
	for index, project := range projects {
		ids[index] = project.ID
	}
	roles, err := s.repo.ListMemberRoles(ids, actor.UserID)
	if err != nil {
		return nil, err
	}
	views := make([]ProjectView, len(projects))
	for index, project := range projects {
		role := roles[project.ID]
		views[index] = ProjectView{
			ProductProject: project,
			MyRole:         role,
			Permissions:    projectCapabilities(actor, role),
		}
	}
	return views, nil
}

func projectCapabilities(actor AccessContext, role string) ProjectCapabilities {
	canManageProject := actor.SuperAdmin || actor.Has("project_projects:manage_all") ||
		roleAllows(role, capProjectManage)
	canManageMembers := actor.SuperAdmin || actor.Has("project_projects:manage_all") ||
		roleAllows(role, capMemberManage)
	canTransferOwner := actor.SuperAdmin || actor.Has("project_projects:manage_all") ||
		roleAllows(role, capOwnerTransfer)

	return ProjectCapabilities{
		Update:        actor.Has("project_projects:update") && canManageProject,
		Archive:       actor.Has("project_projects:update") && canManageProject,
		Delete:        actor.Has("project_projects:delete") && canManageProject,
		ManageMembers: actor.Has("project_projects:update") && canManageMembers,
		TransferOwner: actor.Has("project_projects:update") && canTransferOwner,
	}
}

// ListRequirementStatuses returns only enabled status options. Requirement
// readers may retrieve this business metadata without dictionary-admin access
// （D2：持有 project_requirements:view 即可，无需项目成员身份）。
func (s *ProjectService) ListRequirementStatuses(actor AccessContext) ([]projectmodel.RequirementStatusOption, error) {
	if !actor.Has("project_requirements:view") {
		return nil, NewForbidden("缺少全局权限: project_requirements:view")
	}
	return s.repo.ListRequirementStatuses()
}

func (s *ProjectService) UpdateProject(actor AccessContext, id uint, input UpdateProjectInput) (*projectmodel.ProductProject, error) {
	if _, err := s.acl.Require(id, actor, "project_projects:update", capProjectManage); err != nil {
		return nil, err
	}
	project, err := s.repo.FindProject(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("项目不存在")
	}
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		if name := strings.TrimSpace(*input.Name); name == "" {
			return nil, errors.New("项目名称不能为空")
		} else {
			project.Name = name
		}
	}
	if input.Slug != nil {
		if strings.TrimSpace(*input.Slug) == "" {
			return nil, errors.New("项目标识不能为空")
		}
		slug := normalizeSlug(*input.Slug)
		if slug == "" {
			return nil, errors.New("项目标识无效，仅支持字母、数字与连字符")
		}
		project.Slug = slug
	}
	if input.Description != nil {
		project.Description = strings.TrimSpace(*input.Description)
	}
	if input.Tags != nil {
		project.Tags = strings.TrimSpace(*input.Tags)
	}
	if input.IsPublic != nil {
		project.IsPublic = *input.IsPublic
	}
	if input.ClearRepository {
		project.RepositoryID = nil
	} else if input.RepositoryID != nil {
		project.RepositoryID = input.RepositoryID
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != projectmodel.ProjectStatusActive && status != projectmodel.ProjectStatusArchived {
			return nil, errors.New("项目状态必须为 active 或 archived")
		}
		project.Status = status
	}
	if err := s.repo.UpdateProject(project); err != nil {
		if isUniqueError(err) {
			return nil, NewConflict("项目标识已存在")
		}
		return nil, err
	}
	return project, nil
}

func (s *ProjectService) ArchiveProject(actor AccessContext, id uint) (*projectmodel.ProductProject, error) {
	return s.UpdateProject(actor, id, UpdateProjectInput{Status: new(projectmodel.ProjectStatusArchived)})
}

func (s *ProjectService) DeleteProject(actor AccessContext, id uint) error {
	if _, err := s.acl.Require(id, actor, "project_projects:delete", capProjectManage); err != nil {
		return err
	}
	if _, err := s.repo.FindProject(id); errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("项目不存在")
	} else if err != nil {
		return err
	}
	attachments, err := s.repo.ListAttachmentsByProject(id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteProject(id); err != nil {
		return err
	}
	for _, attachment := range attachments {
		if err := s.storage.Delete(attachment.StorageObjectID); err != nil {
			return err
		}
	}
	return nil
}

type MemberInput struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
}

func (s *ProjectService) ListMembers(actor AccessContext, projectID uint) ([]projectmodel.ProjectMember, error) {
	if _, err := s.acl.Require(projectID, actor, "project_projects:view", capMemberView); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(projectID)
}

// ListUserOptions returns active users for member pickers. Callers need
// project update permission; membership alone is not enough (same gate as
// AddMember's global RBAC requirement).
func (s *ProjectService) ListUserOptions(actor AccessContext, keyword string) ([]projectmodel.UserOption, error) {
	if !actor.Has("project_projects:update") {
		return nil, NewForbidden("缺少全局权限: project_projects:update")
	}
	return s.repo.ListUserOptions(keyword, 200)
}

func (s *ProjectService) AddMember(actor AccessContext, projectID uint, input MemberInput) (*projectmodel.ProjectMember, error) {
	if _, err := s.acl.Require(projectID, actor, "project_projects:update", capMemberManage); err != nil {
		return nil, err
	}
	if input.UserID == 0 {
		return nil, errors.New("用户不能为空")
	}
	role := normalizeProjectRole(input.Role)
	if role == "" || role == projectmodel.ProjectRoleOwner {
		return nil, errors.New("成员角色必须为 admin、member 或 readonly")
	}
	if _, err := s.repo.FindProject(projectID); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("项目不存在")
	} else if err != nil {
		return nil, err
	}
	member := &projectmodel.ProjectMember{ProjectID: projectID, UserID: input.UserID, Role: role}
	if err := s.repo.CreateMember(member); err != nil {
		if isUniqueError(err) {
			return nil, NewConflict("该用户已是项目成员")
		}
		return nil, err
	}
	_ = s.repo.AttachMemberUser(member)
	return member, nil
}

func (s *ProjectService) UpdateMember(actor AccessContext, projectID, userID uint, role string) (*projectmodel.ProjectMember, error) {
	operator, err := s.acl.Require(projectID, actor, "project_projects:update", capMemberManage)
	if err != nil {
		return nil, err
	}
	member, err := s.repo.FindMember(projectID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("项目成员不存在")
	}
	if err != nil {
		return nil, err
	}
	newRole := normalizeProjectRole(role)
	if newRole == "" || newRole == projectmodel.ProjectRoleOwner {
		return nil, errors.New("仅可通过转让负责人操作变更 Owner")
	}
	if member.Role == projectmodel.ProjectRoleOwner {
		return nil, NewForbidden("不可直接修改 Owner")
	}
	if operator != nil && operator.Role == projectmodel.ProjectRoleAdmin && member.UserID == actor.UserID && newRole == projectmodel.ProjectRoleOwner {
		return nil, NewForbidden("项目管理员不能提升为 Owner")
	}
	member.Role = newRole
	if err := s.repo.UpdateMember(member); err != nil {
		return nil, err
	}
	_ = s.repo.AttachMemberUser(member)
	return member, nil
}

func (s *ProjectService) RemoveMember(actor AccessContext, projectID, userID uint) error {
	if _, err := s.acl.Require(projectID, actor, "project_projects:update", capMemberManage); err != nil {
		return err
	}
	member, err := s.repo.FindMember(projectID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("项目成员不存在")
	}
	if err != nil {
		return err
	}
	if member.Role == projectmodel.ProjectRoleOwner {
		return NewConflict("请先转让 Owner 后再移除")
	}
	return s.repo.DeleteMember(projectID, userID)
}

func (s *ProjectService) TransferOwner(actor AccessContext, projectID, userID uint) (*projectmodel.ProductProject, error) {
	if _, err := s.acl.Require(projectID, actor, "project_projects:update", capOwnerTransfer); err != nil {
		return nil, err
	}
	project, err := s.repo.FindProject(projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("项目不存在")
	}
	if err != nil {
		return nil, err
	}
	if project.OwnerID == userID {
		return project, nil
	}
	nextOwner, err := s.repo.FindMember(projectID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("新 Owner 必须为项目成员")
	}
	if err != nil {
		return nil, err
	}
	if nextOwner.Role == projectmodel.ProjectRoleOwner {
		return nil, NewConflict("项目 Owner 状态异常")
	}
	if err := s.repo.TransferOwner(projectID, project.OwnerID, userID); err != nil {
		return nil, err
	}
	project.OwnerID = userID
	return project, nil
}

type RequirementInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	AssigneeID   *uint  `json:"assignee_id"`
	RepositoryID *uint  `json:"repository_id"`
	Tags         string `json:"tags"`
}

type RequirementFilter struct {
	pkg.ListQuery
	Keyword  string `form:"keyword"`
	Status   string `form:"status"`
	Priority string `form:"priority"`
	Assignee string `form:"assignee_id"`
}

func (s *ProjectService) ListRequirements(actor AccessContext, projectID uint, filter RequirementFilter) ([]projectmodel.Requirement, int64, error) {
	if _, err := s.acl.Require(projectID, actor, "project_requirements:view", capRequirementView); err != nil {
		return nil, 0, err
	}
	return s.repo.ListRequirements(
		projectID, filter.ListQuery,
		filter.Keyword, filter.Status, filter.Priority, filter.Assignee,
	)
}

func (s *ProjectService) GetRequirement(actor AccessContext, id uint) (*projectmodel.Requirement, error) {
	requirement, err := s.repo.FindRequirement(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("需求不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:view", capRequirementView); err != nil {
		return nil, err
	}
	return requirement, nil
}

// CheckRequirementProject verifies that a nested route's requirement belongs to
// its project without accidentally requiring the separate :view permission for
// an update/create/delete endpoint.
func (s *ProjectService) CheckRequirementProject(actor AccessContext, projectID, requirementID uint, globalPermission string, write bool) error {
	requirement, err := s.repo.FindRequirement(requirementID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("需求不存在")
	}
	if err != nil {
		return err
	}
	if requirement.ProjectID != projectID {
		return NewNotFound("需求不存在")
	}
	capability := capRequirementView
	if write {
		capability = capRequirementEdit
	}
	_, err = s.acl.Require(projectID, actor, globalPermission, capability)
	return err
}

func (s *ProjectService) CheckCommentRequirementProject(actor AccessContext, projectID, requirementID, commentID uint, globalPermission string, write bool) error {
	comment, err := s.repo.FindComment(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("评论不存在")
	}
	if err != nil {
		return err
	}
	if comment.RequirementID != requirementID {
		return NewNotFound("评论不存在")
	}
	return s.CheckRequirementProject(actor, projectID, requirementID, globalPermission, write)
}

func (s *ProjectService) CheckAttachmentRequirementProject(actor AccessContext, projectID, requirementID, attachmentID uint, globalPermission string, write bool) error {
	attachment, err := s.repo.FindAttachment(attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("附件不存在")
	}
	if err != nil {
		return err
	}
	if attachment.RequirementID != requirementID {
		return NewNotFound("附件不存在")
	}
	return s.CheckRequirementProject(actor, projectID, requirementID, globalPermission, write)
}

func (s *ProjectService) CreateRequirement(actor AccessContext, projectID uint, input RequirementInput) (*projectmodel.Requirement, error) {
	if _, err := s.acl.Require(projectID, actor, "project_requirements:create", capRequirementEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(input.Title); title == "" {
		return nil, errors.New("需求标题不能为空")
	}
	status, err := s.requirementStatus(input.Status)
	if err != nil {
		return nil, err
	}
	requirement := &projectmodel.Requirement{
		ProjectID: projectID, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description),
		Status: status, Priority: normalizePriority(input.Priority), AssigneeID: input.AssigneeID,
		RepositoryID: input.RepositoryID, Tags: strings.TrimSpace(input.Tags), CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
	}
	if err := s.repo.CreateRequirement(requirement); err != nil {
		return nil, err
	}
	return requirement, nil
}

func (s *ProjectService) UpdateRequirement(actor AccessContext, id uint, input RequirementInput) (*projectmodel.Requirement, error) {
	requirement, err := s.repo.FindRequirement(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("需求不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:update", capRequirementEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(requirement.ProjectID); err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(input.Title); title == "" {
		return nil, errors.New("需求标题不能为空")
	} else {
		requirement.Title = title
	}
	status, err := s.requirementStatus(input.Status)
	if err != nil {
		return nil, err
	}
	requirement.Description = strings.TrimSpace(input.Description)
	requirement.Status = status
	requirement.Priority = normalizePriority(input.Priority)
	requirement.AssigneeID = input.AssigneeID
	requirement.RepositoryID = input.RepositoryID
	requirement.Tags = strings.TrimSpace(input.Tags)
	requirement.UpdatedBy = actor.UserID
	if err := s.repo.UpdateRequirement(requirement); err != nil {
		return nil, err
	}
	return requirement, nil
}

func (s *ProjectService) DeleteRequirement(actor AccessContext, id uint) error {
	requirement, err := s.repo.FindRequirement(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("需求不存在")
	}
	if err != nil {
		return err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:delete", capRequirementAdmin); err != nil {
		return err
	}
	attachments, err := s.repo.ListAttachments(id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteRequirement(id); err != nil {
		return err
	}
	for _, attachment := range attachments {
		if err := s.storage.Delete(attachment.StorageObjectID); err != nil {
			return err
		}
	}
	return nil
}

type CommentInput struct {
	Content string `json:"content"`
}

func (s *ProjectService) ListComments(actor AccessContext, requirementID uint) ([]projectmodel.RequirementComment, error) {
	requirement, err := s.GetRequirement(actor, requirementID)
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:view", capRequirementView); err != nil {
		return nil, err
	}
	return s.repo.ListComments(requirementID)
}

func (s *ProjectService) CreateComment(actor AccessContext, requirementID uint, input CommentInput) (*projectmodel.RequirementComment, error) {
	requirement, err := s.repo.FindRequirement(requirementID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("需求不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:create", capRequirementEdit); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, errors.New("评论不能为空")
	}
	comment := &projectmodel.RequirementComment{RequirementID: requirementID, Content: content, CreatedBy: actor.UserID}
	if err := s.repo.CreateComment(comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func (s *ProjectService) UpdateComment(actor AccessContext, id uint, input CommentInput) (*projectmodel.RequirementComment, error) {
	comment, err := s.repo.FindComment(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("评论不存在")
	}
	if err != nil {
		return nil, err
	}
	requirement, err := s.repo.FindRequirement(comment.RequirementID)
	if err != nil {
		return nil, NewNotFound("需求不存在")
	}
	member, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:update", capRequirementEdit)
	if err != nil {
		return nil, err
	}
	if !actor.SuperAdmin && !actor.Has("project_projects:manage_all") && comment.CreatedBy != actor.UserID &&
		(member == nil || (member.Role != projectmodel.ProjectRoleOwner && member.Role != projectmodel.ProjectRoleAdmin)) {
		return nil, NewForbidden("只能编辑自己的评论")
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, errors.New("评论不能为空")
	}
	comment.Content = content
	if err := s.repo.UpdateComment(comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func (s *ProjectService) DeleteComment(actor AccessContext, id uint) error {
	comment, err := s.repo.FindComment(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("评论不存在")
	}
	if err != nil {
		return err
	}
	requirement, err := s.repo.FindRequirement(comment.RequirementID)
	if err != nil {
		return NewNotFound("需求不存在")
	}
	member, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:delete", capRequirementEdit)
	if err != nil {
		return err
	}
	if !actor.SuperAdmin && !actor.Has("project_projects:manage_all") && comment.CreatedBy != actor.UserID &&
		(member == nil || (member.Role != projectmodel.ProjectRoleOwner && member.Role != projectmodel.ProjectRoleAdmin)) {
		return NewForbidden("只能删除自己的评论")
	}
	return s.repo.DeleteComment(id)
}

func (s *ProjectService) ListAttachments(actor AccessContext, requirementID uint) ([]projectmodel.RequirementAttachment, error) {
	if _, err := s.GetRequirement(actor, requirementID); err != nil {
		return nil, err
	}
	return s.repo.ListAttachments(requirementID)
}

func (s *ProjectService) AddAttachment(actor AccessContext, requirementID uint, filename, contentType string, source io.Reader, size int64) (*projectmodel.RequirementAttachment, error) {
	requirement, err := s.repo.FindRequirement(requirementID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("需求不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:update", capRequirementEdit); err != nil {
		return nil, err
	}
	filename = safeFilename(filename)
	if filename == "" {
		return nil, errors.New("附件文件名不能为空")
	}
	object, err := s.storage.Put(storagemodel.KindAttachment, contentType, source, size, actor.UserID)
	if err != nil {
		return nil, err
	}
	attachment := &projectmodel.RequirementAttachment{
		RequirementID: requirementID, StorageObjectID: object.ID, Filename: filename, CreatedBy: actor.UserID,
	}
	if err := s.repo.CreateAttachment(attachment); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	return attachment, nil
}

func (s *ProjectService) OpenAttachment(actor AccessContext, id uint) (*os.File, *projectmodel.RequirementAttachment, string, error) {
	attachment, err := s.repo.FindAttachment(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, "", NewNotFound("附件不存在")
	}
	if err != nil {
		return nil, nil, "", err
	}
	requirement, err := s.repo.FindRequirement(attachment.RequirementID)
	if err != nil {
		return nil, nil, "", NewNotFound("需求不存在")
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:view", capRequirementView); err != nil {
		return nil, nil, "", err
	}
	file, object, err := s.storage.Open(attachment.StorageObjectID)
	if err != nil {
		return nil, nil, "", err
	}
	return file, attachment, object.ContentType, nil
}

func (s *ProjectService) DeleteAttachment(actor AccessContext, id uint) error {
	attachment, err := s.repo.FindAttachment(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("附件不存在")
	}
	if err != nil {
		return err
	}
	requirement, err := s.repo.FindRequirement(attachment.RequirementID)
	if err != nil {
		return NewNotFound("需求不存在")
	}
	if _, err := s.acl.Require(requirement.ProjectID, actor, "project_requirements:update", capRequirementEdit); err != nil {
		return err
	}
	if err := s.repo.DeleteAttachment(id); err != nil {
		return err
	}
	return s.storage.Delete(attachment.StorageObjectID)
}

func (s *ProjectService) requireActiveProject(projectID uint) error {
	project, err := s.repo.FindProject(projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("项目不存在")
	}
	if err != nil {
		return err
	}
	if project.Status == projectmodel.ProjectStatusArchived {
		return NewConflict("归档项目不可编辑内容")
	}
	return nil
}

func (s *ProjectService) requirementStatus(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "backlog"
	}
	exists, err := s.repo.RequirementStatusExists(value)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New("无效需求状态")
	}
	return value, nil
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "normal", "high", "urgent":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "normal"
	}
}

func normalizeProjectRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case projectmodel.ProjectRoleOwner, projectmodel.ProjectRoleAdmin, projectmodel.ProjectRoleMember, projectmodel.ProjectRoleReadonly:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
		} else if r == '-' && !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func safeFilename(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func isUniqueError(err error) bool {
	message := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
