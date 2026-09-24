package repository

import (
	"errors"
	"strconv"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"

	"gorm.io/gorm"
)

type ProjectRepository struct {
	db     *gorm.DB
	issues *IssueRepository
}

func NewProjectRepository(db *gorm.DB) *ProjectRepository {
	return &ProjectRepository{db: db, issues: NewIssueRepository(db)}
}

// IssueRepo exposes the unified issue repository for shared services.
func (r *ProjectRepository) IssueRepo() *IssueRepository {
	return r.issues
}

func (r *ProjectRepository) CreateProject(project *model.ProductProject) error {
	return r.db.Create(project).Error
}

func (r *ProjectRepository) CreateProjectWithOwner(project *model.ProductProject) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return err
		}
		return tx.Create(&model.ProjectMember{
			ProjectID: project.ID,
			UserID:    project.OwnerID,
			Role:      model.ProjectRoleOwner,
		}).Error
	})
}

func (r *ProjectRepository) FindProject(id uint) (*model.ProductProject, error) {
	var project model.ProductProject
	if err := r.db.First(&project, id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *ProjectRepository) FindProjectBySlug(slug string) (*model.ProductProject, error) {
	var project model.ProductProject
	if err := r.db.Where("slug = ?", slug).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// ListProjects 列出项目；scopeUserID 非空时仅返回该用户为成员或创建人的项目。
func (r *ProjectRepository) ListProjects(q pkg.ListQuery, keyword, status string, scopeUserID *uint) ([]model.ProductProject, int64, error) {
	db := r.db.Model(&model.ProductProject{})
	if scopeUserID != nil {
		db = db.Where(
			"product_projects.id IN (SELECT project_id FROM project_members WHERE user_id = ?) OR product_projects.created_by = ?",
			*scopeUserID, *scopeUserID,
		)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("product_projects.name LIKE ? OR product_projects.slug LIKE ? OR product_projects.tags LIKE ?", like, like, like)
	}
	if status = strings.TrimSpace(status); status != "" {
		db = db.Where("product_projects.status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := pkg.OrderBy(q.Sort, map[string]string{
		"name":       "product_projects.name",
		"updated_at": "product_projects.updated_at",
	}, "product_projects.id", "product_projects.updated_at DESC, product_projects.id DESC")
	var projects []model.ProductProject
	err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&projects).Error
	return projects, total, err
}

func (r *ProjectRepository) UpdateProject(project *model.ProductProject) error {
	return r.db.Save(project).Error
}

func (r *ProjectRepository) DeleteProject(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&model.ProjectMember{}).Error; err != nil {
			return err
		}
		issues := tx.Model(&model.ProjectIssue{}).Select("id").Where("project_id = ?", id)
		if err := tx.Where("issue_id IN (?)", issues).Delete(&model.ProjectIssueComment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id IN (?)", issues).Delete(&model.ProjectIssueAttachment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id IN (?)", issues).Delete(&model.ProjectIssueActivity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id IN (?)", issues).Delete(&model.ProjectIssueWatcher{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&model.ProjectIssue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&model.ProjectIteration{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&model.ApiDocNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&model.DevDocNode{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ProductProject{}, id).Error
	})
}

func (r *ProjectRepository) CreateMember(member *model.ProjectMember) error {
	return r.db.Create(member).Error
}

func (r *ProjectRepository) FindMember(projectID, userID uint) (*model.ProjectMember, error) {
	var member model.ProjectMember
	if err := r.db.Where("project_id = ? AND user_id = ?", projectID, userID).First(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *ProjectRepository) ListMemberRoles(projectIDs []uint, userID uint) (map[uint]string, error) {
	if len(projectIDs) == 0 {
		return map[uint]string{}, nil
	}
	var members []model.ProjectMember
	if err := r.db.Select("project_id", "role").
		Where("user_id = ? AND project_id IN ?", userID, projectIDs).
		Find(&members).Error; err != nil {
		return nil, err
	}
	roles := make(map[uint]string, len(members))
	for _, member := range members {
		roles[member.ProjectID] = member.Role
	}
	return roles, nil
}

// ListUserProjectIDs returns all project IDs where the user is a member or creator.
func (r *ProjectRepository) ListUserProjectIDs(userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.Model(&model.ProductProject{}).
		Where("product_projects.id IN (SELECT project_id FROM project_members WHERE user_id = ?) OR product_projects.created_by = ?", userID, userID).
		Pluck("id", &ids).Error
	return ids, err
}

func (r *ProjectRepository) ListMembers(projectID uint) ([]model.ProjectMember, error) {
	var members []model.ProjectMember
	err := r.db.Where("project_id = ?", projectID).
		Order("CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END, id ASC").
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	if err := r.attachMemberUsers(members); err != nil {
		return nil, err
	}
	return members, nil
}

func (r *ProjectRepository) AttachMemberUser(member *model.ProjectMember) error {
	if member == nil {
		return nil
	}
	var user model.UserOption
	err := r.db.Table("users").
		Select("id, username, display_name").
		Where("id = ?", member.UserID).
		Take(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	member.Username = user.Username
	member.DisplayName = user.DisplayName
	return nil
}

func (r *ProjectRepository) attachMemberUsers(members []model.ProjectMember) error {
	if len(members) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(members))
	seen := make(map[uint]struct{}, len(members))
	for _, m := range members {
		if _, ok := seen[m.UserID]; ok {
			continue
		}
		seen[m.UserID] = struct{}{}
		ids = append(ids, m.UserID)
	}
	var users []model.UserOption
	if err := r.db.Table("users").
		Select("id, username, display_name").
		Where("id IN ?", ids).
		Find(&users).Error; err != nil {
		return err
	}
	byID := make(map[uint]model.UserOption, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	for i := range members {
		if u, ok := byID[members[i].UserID]; ok {
			members[i].Username = u.Username
			members[i].DisplayName = u.DisplayName
		}
	}
	return nil
}

// ListUserOptions returns active users for member/assignee pickers.
func (r *ProjectRepository) ListUserOptions(keyword string, limit int) ([]model.UserOption, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q := r.db.Table("users").
		Select("id, username, display_name").
		Where("is_active = ?", true)
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	var items []model.UserOption
	err := q.Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

// FindUserIDByUsername resolves an exact username to the user ID (any active state).
func (r *ProjectRepository) FindUserIDByUsername(username string) (uint, error) {
	var row struct {
		ID uint
	}
	err := r.db.Table("users").Select("id").Where("username = ?", username).Take(&row).Error
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}

func (r *ProjectRepository) UpdateMember(member *model.ProjectMember) error {
	return r.db.Save(member).Error
}

func (r *ProjectRepository) DeleteMember(projectID, userID uint) error {
	return r.db.Where("project_id = ? AND user_id = ?", projectID, userID).Delete(&model.ProjectMember{}).Error
}

func (r *ProjectRepository) TransferOwner(projectID, previousOwnerID, nextOwnerID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ProjectMember{}).
			Where("project_id = ? AND user_id = ?", projectID, previousOwnerID).
			Update("role", model.ProjectRoleAdmin).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProjectMember{}).
			Where("project_id = ? AND user_id = ?", projectID, nextOwnerID).
			Update("role", model.ProjectRoleOwner).Error; err != nil {
			return err
		}
		return tx.Model(&model.ProductProject{}).Where("id = ?", projectID).Update("owner_id", nextOwnerID).Error
	})
}

// Requirement persistence is a type=requirement facade over the unified
// IssueRepository (DESIGN D37), preserving the legacy surface for the
// /requirements compatibility aliases.

func requirementToIssue(requirement model.Requirement) model.ProjectIssue {
	return model.ProjectIssue{
		ID: requirement.ID, ProjectID: requirement.ProjectID, Type: model.IssueTypeRequirement,
		Title: requirement.Title, Description: requirement.Description,
		Status: requirement.Status, Priority: requirement.Priority,
		AssigneeID: requirement.AssigneeID, RepositoryID: requirement.RepositoryID,
		Tags: requirement.Tags, CreatedBy: requirement.CreatedBy, UpdatedBy: requirement.UpdatedBy,
		CreatedAt: requirement.CreatedAt, UpdatedAt: requirement.UpdatedAt,
	}
}

func issueToRequirement(issue model.ProjectIssue) model.Requirement {
	return model.Requirement{
		ID: issue.ID, ProjectID: issue.ProjectID,
		Title: issue.Title, Description: issue.Description,
		Status: issue.Status, Priority: issue.Priority,
		AssigneeID: issue.AssigneeID, RepositoryID: issue.RepositoryID,
		Tags: issue.Tags, CreatedBy: issue.CreatedBy, UpdatedBy: issue.UpdatedBy,
		CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt,
	}
}

func (r *ProjectRepository) CreateRequirement(requirement *model.Requirement) error {
	issue := requirementToIssue(*requirement)
	if err := r.issues.Create(&issue); err != nil {
		return err
	}
	*requirement = issueToRequirement(issue)
	return nil
}

func (r *ProjectRepository) FindRequirement(id uint) (*model.Requirement, error) {
	issue, err := r.issues.FindByID(id)
	if err != nil {
		return nil, err
	}
	if issue.Type != model.IssueTypeRequirement {
		return nil, gorm.ErrRecordNotFound
	}
	requirement := issueToRequirement(*issue)
	return &requirement, nil
}

func (r *ProjectRepository) ListRequirements(projectID uint, q pkg.ListQuery, keyword, status, priority, assignee string, participantID *uint) ([]model.Requirement, int64, error) {
	filter := IssueFilter{Type: model.IssueTypeRequirement, Keyword: keyword, Status: status, Priority: priority, ParticipantID: participantID}
	if assignee = strings.TrimSpace(assignee); assignee != "" {
		if id, err := strconv.ParseUint(assignee, 10, 64); err == nil {
			uid := uint(id)
			filter.AssigneeID = &uid
		}
	}
	issues, total, err := r.issues.ListByProject(projectID, filter, q)
	if err != nil {
		return nil, 0, err
	}
	requirements := make([]model.Requirement, 0, len(issues))
	for _, issue := range issues {
		requirements = append(requirements, issueToRequirement(issue))
	}
	return requirements, total, nil
}

func (r *ProjectRepository) UpdateRequirement(requirement *model.Requirement) error {
	issue := requirementToIssue(*requirement)
	if err := r.issues.Update(&issue); err != nil {
		return err
	}
	*requirement = issueToRequirement(issue)
	return nil
}

func (r *ProjectRepository) DeleteRequirement(id uint) error {
	return r.issues.Delete(id)
}

func (r *ProjectRepository) CreateComment(comment *model.RequirementComment) error {
	issueComment := &model.ProjectIssueComment{
		IssueID: comment.RequirementID, Content: comment.Content, CreatedBy: comment.CreatedBy,
		CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
	}
	if err := r.issues.CreateComment(issueComment); err != nil {
		return err
	}
	comment.ID = issueComment.ID
	return nil
}

func (r *ProjectRepository) FindComment(id uint) (*model.RequirementComment, error) {
	issueComment, err := r.issues.FindCommentByID(id)
	if err != nil {
		return nil, err
	}
	return &model.RequirementComment{
		ID:            issueComment.ID,
		RequirementID: issueComment.IssueID,
		Content:       issueComment.Content,
		CreatedBy:     issueComment.CreatedBy,
		CreatedAt:     issueComment.CreatedAt,
		UpdatedAt:     issueComment.UpdatedAt,
	}, nil
}

func (r *ProjectRepository) ListComments(requirementID uint) ([]model.RequirementComment, error) {
	comments, err := r.issues.ListComments(requirementID)
	if err != nil {
		return nil, err
	}
	result := make([]model.RequirementComment, 0, len(comments))
	for _, c := range comments {
		result = append(result, model.RequirementComment{
			ID: c.ID, RequirementID: c.IssueID, Content: c.Content,
			CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	return result, nil
}

func (r *ProjectRepository) UpdateComment(comment *model.RequirementComment) error {
	issueComment := &model.ProjectIssueComment{
		ID: comment.ID, IssueID: comment.RequirementID, Content: comment.Content,
		CreatedBy: comment.CreatedBy, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
	}
	return r.issues.UpdateComment(issueComment)
}

func (r *ProjectRepository) DeleteComment(id uint) error {
	return r.issues.DeleteComment(id)
}

func (r *ProjectRepository) CreateAttachment(attachment *model.RequirementAttachment) error {
	issueAtt := &model.ProjectIssueAttachment{
		ID: attachment.ID, IssueID: attachment.RequirementID,
		StorageObjectID: attachment.StorageObjectID, Filename: attachment.Filename,
		CreatedBy: attachment.CreatedBy, CreatedAt: attachment.CreatedAt,
	}
	if err := r.issues.CreateAttachment(issueAtt); err != nil {
		return err
	}
	attachment.ID = issueAtt.ID
	return nil
}

func (r *ProjectRepository) FindAttachment(id uint) (*model.RequirementAttachment, error) {
	att, err := r.issues.FindAttachmentByID(id)
	if err != nil {
		return nil, err
	}
	return &model.RequirementAttachment{
		ID: att.ID, RequirementID: att.IssueID, StorageObjectID: att.StorageObjectID,
		Filename: att.Filename, CreatedBy: att.CreatedBy, CreatedAt: att.CreatedAt,
	}, nil
}

func (r *ProjectRepository) ListAttachments(requirementID uint) ([]model.RequirementAttachment, error) {
	atts, err := r.issues.ListAttachments(requirementID)
	if err != nil {
		return nil, err
	}
	result := make([]model.RequirementAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, model.RequirementAttachment{
			ID: a.ID, RequirementID: a.IssueID, StorageObjectID: a.StorageObjectID,
			Filename: a.Filename, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
		})
	}
	return result, nil
}

func (r *ProjectRepository) ListAttachmentsByProject(projectID uint) ([]model.RequirementAttachment, error) {
	atts, err := r.issues.ListAttachmentsByProject(projectID)
	if err != nil {
		return nil, err
	}
	result := make([]model.RequirementAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, model.RequirementAttachment{
			ID: a.ID, RequirementID: a.IssueID, StorageObjectID: a.StorageObjectID,
			Filename: a.Filename, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
		})
	}
	return result, nil
}

func (r *ProjectRepository) ListBugAttachmentsByProject(projectID uint) ([]model.ProjectBugAttachment, error) {
	atts, err := r.issues.ListAttachmentsByProject(projectID)
	if err != nil {
		return nil, err
	}
	result := make([]model.ProjectBugAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, model.ProjectBugAttachment{
			ID: a.ID, BugID: a.IssueID, StorageObjectID: a.StorageObjectID,
			Filename: a.Filename, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
		})
	}
	return result, nil
}

func (r *ProjectRepository) DeleteAttachment(id uint) error {
	return r.issues.DeleteAttachment(id)
}

// Iterations -----------------------------------------------------------------

func (r *ProjectRepository) CreateIteration(iteration *model.ProjectIteration) error {
	return r.db.Create(iteration).Error
}

func (r *ProjectRepository) FindIteration(id uint) (*model.ProjectIteration, error) {
	var iteration model.ProjectIteration
	if err := r.db.First(&iteration, id).Error; err != nil {
		return nil, err
	}
	return &iteration, nil
}

func (r *ProjectRepository) ListIterations(projectID uint) ([]model.ProjectIteration, error) {
	var iterations []model.ProjectIteration
	err := r.db.Where("project_id = ?", projectID).Order("created_at DESC, id DESC").Find(&iterations).Error
	return iterations, err
}

func (r *ProjectRepository) UpdateIteration(iteration *model.ProjectIteration) error {
	return r.db.Save(iteration).Error
}

func (r *ProjectRepository) DeleteIteration(id uint) error {
	return r.db.Delete(&model.ProjectIteration{}, id).Error
}

func (r *ProjectRepository) CreateDocNode(node *model.ApiDocNode) error {
	return r.db.Create(node).Error
}

func (r *ProjectRepository) FindDocNode(id uint) (*model.ApiDocNode, error) {
	var node model.ApiDocNode
	if err := r.db.First(&node, id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *ProjectRepository) ListDocNodes(projectID uint) ([]model.ApiDocNode, error) {
	var nodes []model.ApiDocNode
	err := r.db.Where("project_id = ?", projectID).Order("sort_order ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

// ListDocTreeNodes 仅供文档树：不加载 content，避免首屏拉全文。
func (r *ProjectRepository) ListDocTreeNodes(projectID uint) ([]model.ApiDocNode, error) {
	var nodes []model.ApiDocNode
	err := r.db.Omit("Content").Where("project_id = ?", projectID).Order("sort_order ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *ProjectRepository) UpdateDocNode(node *model.ApiDocNode) error {
	return r.db.Save(node).Error
}

func (r *ProjectRepository) DeleteDocNodes(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", ids).Delete(&model.ApiDocNode{}).Error
}

func (r *ProjectRepository) CreateDevDocNode(node *model.DevDocNode) error {
	return r.db.Create(node).Error
}

func (r *ProjectRepository) FindDevDocNode(id uint) (*model.DevDocNode, error) {
	var node model.DevDocNode
	if err := r.db.First(&node, id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *ProjectRepository) ListDevDocNodes(projectID uint) ([]model.DevDocNode, error) {
	var nodes []model.DevDocNode
	err := r.db.Where("project_id = ?", projectID).Order("sort_order ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

// ListDevDocTreeNodes 仅供开发文档树：不加载 content，避免首屏拉全文。
func (r *ProjectRepository) ListDevDocTreeNodes(projectID uint) ([]model.DevDocNode, error) {
	var nodes []model.DevDocNode
	err := r.db.Omit("Content").Where("project_id = ?", projectID).Order("sort_order ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *ProjectRepository) UpdateDevDocNode(node *model.DevDocNode) error {
	return r.db.Save(node).Error
}

func (r *ProjectRepository) DeleteDevDocNodes(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", ids).Delete(&model.DevDocNode{}).Error
}

func (r *ProjectRepository) RequirementStatusExists(value string) (bool, error) {
	return r.IssueStatusExists(model.IssueTypeRequirement, value)
}

func (r *ProjectRepository) ListRequirementStatuses() ([]model.RequirementStatusOption, error) {
	return r.ListIssueStatuses(model.IssueTypeRequirement)
}

// IssueStatusExists checks whether value is an enabled status in the type's
// status dictionary (requirement_status / bug_status; task reuses requirement).
func (r *ProjectRepository) IssueStatusExists(issueType, value string) (bool, error) {
	var count int64
	err := r.db.Table("dict_items").
		Joins("JOIN dictionaries ON dictionaries.id = dict_items.dictionary_id").
		Where("dictionaries.code = ? AND dict_items.value = ? AND dict_items.enabled = ?", model.StatusDictCode(issueType), value, true).
		Count(&count).Error
	return count > 0, err
}

func (r *ProjectRepository) ListIssueStatuses(issueType string) ([]model.RequirementStatusOption, error) {
	var statuses []model.RequirementStatusOption
	err := r.db.Table("dict_items").
		Select("dict_items.label, dict_items.value, dict_items.sort_order, dict_items.enabled").
		Joins("JOIN dictionaries ON dictionaries.id = dict_items.dictionary_id").
		Where("dictionaries.code = ? AND dict_items.enabled = ?", model.StatusDictCode(issueType), true).
		Order("dict_items.sort_order ASC, dict_items.id ASC").
		Scan(&statuses).Error
	return statuses, err
}
