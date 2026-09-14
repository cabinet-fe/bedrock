package repository

import (
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"

	"gorm.io/gorm"
)

type BugRepository struct {
	db *gorm.DB
}

func NewBugRepository(db *gorm.DB) *BugRepository {
	return &BugRepository{db: db}
}

// BugFilter encapsulates query parameters for bug searches.
type BugFilter struct {
	Keyword       string
	ProjectID     *uint
	Status        string
	Severity      string
	Priority      string
	AssigneeID    *uint
	ExcludeClosed bool
}

func (r *BugRepository) Create(bug *model.ProjectBug) error {
	return r.db.Create(bug).Error
}

func (r *BugRepository) FindByID(id uint) (*model.ProjectBug, error) {
	var bug model.ProjectBug
	if err := r.db.First(&bug, id).Error; err != nil {
		return nil, err
	}
	if err := r.AttachBugDetails(&bug); err != nil {
		return nil, err
	}
	return &bug, nil
}

func (r *BugRepository) Update(bug *model.ProjectBug) error {
	return r.db.Save(bug).Error
}

func (r *BugRepository) Delete(id uint) error {
	return r.db.Delete(&model.ProjectBug{}, id).Error
}

// ListByProject queries bugs within a specific project.
func (r *BugRepository) ListByProject(projectID uint, filter BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	db := r.db.Model(&model.ProjectBug{}).Where("project_bugs.project_id = ?", projectID)
	db = applyBugFilter(db, filter)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := pkg.OrderBy(q.Sort, map[string]string{
		"title":      "project_bugs.title",
		"status":     "project_bugs.status",
		"severity":   "project_bugs.severity",
		"priority":   "project_bugs.priority",
		"created_at": "project_bugs.created_at",
		"updated_at": "project_bugs.updated_at",
	}, "project_bugs.id", "project_bugs.updated_at DESC, project_bugs.id DESC")

	var bugs []model.ProjectBug
	if err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&bugs).Error; err != nil {
		return nil, 0, err
	}
	if err := r.AttachBugListDetails(bugs); err != nil {
		return nil, 0, err
	}
	return bugs, total, nil
}

// ListAcrossProjects queries bugs across projects, optionally scoped to accessible projectIDs.
func (r *BugRepository) ListAcrossProjects(projectIDs []uint, filter BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	db := r.db.Model(&model.ProjectBug{})
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return []model.ProjectBug{}, 0, nil
		}
		db = db.Where("project_bugs.project_id IN ?", projectIDs)
	}
	if filter.ProjectID != nil {
		db = db.Where("project_bugs.project_id = ?", *filter.ProjectID)
	}
	db = applyBugFilter(db, filter)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := pkg.OrderBy(q.Sort, map[string]string{
		"title":      "project_bugs.title",
		"status":     "project_bugs.status",
		"severity":   "project_bugs.severity",
		"priority":   "project_bugs.priority",
		"created_at": "project_bugs.created_at",
		"updated_at": "project_bugs.updated_at",
	}, "project_bugs.id", "project_bugs.updated_at DESC, project_bugs.id DESC")

	var bugs []model.ProjectBug
	if err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&bugs).Error; err != nil {
		return nil, 0, err
	}
	if err := r.AttachBugListDetails(bugs); err != nil {
		return nil, 0, err
	}
	return bugs, total, nil
}

func applyBugFilter(db *gorm.DB, filter BugFilter) *gorm.DB {
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("project_bugs.title LIKE ? OR project_bugs.description LIKE ?", like, like)
	}
	if st := strings.TrimSpace(filter.Status); st != "" {
		db = db.Where("project_bugs.status = ?", st)
	}
	if sev := strings.TrimSpace(filter.Severity); sev != "" {
		db = db.Where("project_bugs.severity = ?", sev)
	}
	if pri := strings.TrimSpace(filter.Priority); pri != "" {
		db = db.Where("project_bugs.priority = ?", pri)
	}
	if filter.AssigneeID != nil {
		db = db.Where("project_bugs.assignee_id = ?", *filter.AssigneeID)
	}
	if filter.ExcludeClosed {
		db = db.Where("project_bugs.status <> ?", model.BugStatusClosed)
	}
	return db
}

func (r *BugRepository) RecordActivity(activity *model.ProjectBugActivity) error {
	return r.db.Create(activity).Error
}

func (r *BugRepository) ListActivities(bugID uint) ([]model.ProjectBugActivity, error) {
	var activities []model.ProjectBugActivity
	err := r.db.Where("bug_id = ?", bugID).Order("created_at ASC, id ASC").Find(&activities).Error
	if err != nil {
		return nil, err
	}
	if err := r.attachActivityUsers(activities); err != nil {
		return nil, err
	}
	return activities, nil
}

func (r *BugRepository) CountByStatus(projectID uint) (map[string]int64, error) {
	type statusCount struct {
		Status string
		Count  int64
	}
	var results []statusCount
	err := r.db.Model(&model.ProjectBug{}).
		Select("status, count(*) as count").
		Where("project_id = ?", projectID).
		Group("status").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, sc := range results {
		counts[sc.Status] = sc.Count
	}
	return counts, nil
}

func (r *BugRepository) AttachBugDetails(bug *model.ProjectBug) error {
	if bug == nil {
		return nil
	}
	return r.attachBugPointers([]*model.ProjectBug{bug})
}

func (r *BugRepository) AttachBugListDetails(bugs []model.ProjectBug) error {
	if len(bugs) == 0 {
		return nil
	}
	ptrs := make([]*model.ProjectBug, len(bugs))
	for i := range bugs {
		ptrs[i] = &bugs[i]
	}
	return r.attachBugPointers(ptrs)
}

func (r *BugRepository) attachBugPointers(bugs []*model.ProjectBug) error {
	if len(bugs) == 0 {
		return nil
	}

	userIDs := make([]uint, 0, len(bugs)*2)
	projectIDs := make([]uint, 0, len(bugs))
	repoIDs := make([]uint, 0, len(bugs))
	seenUsers := make(map[uint]struct{})
	seenProjects := make(map[uint]struct{})
	seenRepos := make(map[uint]struct{})

	for _, b := range bugs {
		if b.CreatedBy > 0 {
			if _, ok := seenUsers[b.CreatedBy]; !ok {
				seenUsers[b.CreatedBy] = struct{}{}
				userIDs = append(userIDs, b.CreatedBy)
			}
		}
		if b.AssigneeID != nil && *b.AssigneeID > 0 {
			if _, ok := seenUsers[*b.AssigneeID]; !ok {
				seenUsers[*b.AssigneeID] = struct{}{}
				userIDs = append(userIDs, *b.AssigneeID)
			}
		}
		if b.ProjectID > 0 {
			if _, ok := seenProjects[b.ProjectID]; !ok {
				seenProjects[b.ProjectID] = struct{}{}
				projectIDs = append(projectIDs, b.ProjectID)
			}
		}
		if b.RepositoryID != nil && *b.RepositoryID > 0 {
			if _, ok := seenRepos[*b.RepositoryID]; !ok {
				seenRepos[*b.RepositoryID] = struct{}{}
				repoIDs = append(repoIDs, *b.RepositoryID)
			}
		}
	}

	// Attach users
	if len(userIDs) > 0 {
		var users []model.UserOption
		if err := r.db.Table("users").
			Select("id, username, display_name").
			Where("id IN ?", userIDs).
			Find(&users).Error; err != nil {
			return err
		}
		userMap := make(map[uint]model.UserOption, len(users))
		for _, u := range users {
			userMap[u.ID] = u
		}
		for _, b := range bugs {
			if u, ok := userMap[b.CreatedBy]; ok {
				b.CreatorUsername = u.Username
				if u.DisplayName != "" {
					b.CreatorName = u.DisplayName
				} else {
					b.CreatorName = u.Username
				}
			}
			if b.AssigneeID != nil {
				if u, ok := userMap[*b.AssigneeID]; ok {
					b.AssigneeUsername = u.Username
					if u.DisplayName != "" {
						b.AssigneeName = u.DisplayName
					} else {
						b.AssigneeName = u.Username
					}
				}
			}
		}
	}

	// Attach projects
	if len(projectIDs) > 0 {
		type projName struct {
			ID   uint
			Name string
		}
		var projs []projName
		if err := r.db.Table("product_projects").
			Select("id, name").
			Where("id IN ?", projectIDs).
			Find(&projs).Error; err != nil {
			return err
		}
		projMap := make(map[uint]string, len(projs))
		for _, p := range projs {
			projMap[p.ID] = p.Name
		}
		for _, b := range bugs {
			if name, ok := projMap[b.ProjectID]; ok {
				b.ProjectName = name
			}
		}
	}

	// Attach repositories
	if len(repoIDs) > 0 {
		type repoName struct {
			ID   uint
			Name string
		}
		var repos []repoName
		if err := r.db.Table("repositories").
			Select("id, name").
			Where("id IN ?", repoIDs).
			Find(&repos).Error; err != nil {
			return err
		}
		repoMap := make(map[uint]string, len(repos))
		for _, rp := range repos {
			repoMap[rp.ID] = rp.Name
		}
		for _, b := range bugs {
			if b.RepositoryID != nil {
				if name, ok := repoMap[*b.RepositoryID]; ok {
					b.RepositoryName = name
				}
			}
		}
	}

	return nil
}

func (r *BugRepository) attachActivityUsers(activities []model.ProjectBugActivity) error {
	if len(activities) == 0 {
		return nil
	}
	userIDs := make([]uint, 0, len(activities))
	seen := make(map[uint]struct{}, len(activities))
	for _, a := range activities {
		if a.CreatedBy > 0 {
			if _, ok := seen[a.CreatedBy]; !ok {
				seen[a.CreatedBy] = struct{}{}
				userIDs = append(userIDs, a.CreatedBy)
			}
		}
	}
	if len(userIDs) == 0 {
		return nil
	}
	var users []model.UserOption
	if err := r.db.Table("users").
		Select("id, username, display_name").
		Where("id IN ?", userIDs).
		Find(&users).Error; err != nil {
		return err
	}
	userMap := make(map[uint]model.UserOption, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}
	for i := range activities {
		if u, ok := userMap[activities[i].CreatedBy]; ok {
			activities[i].CreatorUsername = u.Username
			if u.DisplayName != "" {
				activities[i].CreatorName = u.DisplayName
			} else {
				activities[i].CreatorName = u.Username
			}
		}
	}
	return nil
}

// CreateComment persists a new bug comment.
func (r *BugRepository) CreateComment(comment *model.ProjectBugComment) error {
	return r.db.Create(comment).Error
}

// FindCommentByID retrieves a bug comment by ID with creator info attached.
func (r *BugRepository) FindCommentByID(id uint) (*model.ProjectBugComment, error) {
	var comment model.ProjectBugComment
	if err := r.db.First(&comment, id).Error; err != nil {
		return nil, err
	}
	comments := []model.ProjectBugComment{comment}
	if err := r.attachCommentUsers(comments); err != nil {
		return nil, err
	}
	return &comments[0], nil
}

// ListComments retrieves all comments for a bug in ascending chronological order.
func (r *BugRepository) ListComments(bugID uint) ([]model.ProjectBugComment, error) {
	var comments []model.ProjectBugComment
	if err := r.db.Where("bug_id = ?", bugID).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	if err := r.attachCommentUsers(comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// UpdateComment updates an existing bug comment.
func (r *BugRepository) UpdateComment(comment *model.ProjectBugComment) error {
	return r.db.Save(comment).Error
}

// DeleteComment removes a bug comment.
func (r *BugRepository) DeleteComment(id uint) error {
	return r.db.Delete(&model.ProjectBugComment{}, id).Error
}

func (r *BugRepository) attachCommentUsers(comments []model.ProjectBugComment) error {
	if len(comments) == 0 {
		return nil
	}
	userIDs := make([]uint, 0, len(comments))
	seen := make(map[uint]struct{}, len(comments))
	for _, c := range comments {
		if c.CreatedBy > 0 {
			if _, ok := seen[c.CreatedBy]; !ok {
				seen[c.CreatedBy] = struct{}{}
				userIDs = append(userIDs, c.CreatedBy)
			}
		}
	}
	if len(userIDs) == 0 {
		return nil
	}
	var users []model.UserOption
	if err := r.db.Table("users").
		Select("id, username, display_name").
		Where("id IN ?", userIDs).
		Find(&users).Error; err != nil {
		return err
	}
	userMap := make(map[uint]model.UserOption, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}
	for i := range comments {
		if u, ok := userMap[comments[i].CreatedBy]; ok {
			comments[i].CreatorUsername = u.Username
			if u.DisplayName != "" {
				comments[i].CreatorName = u.DisplayName
			} else {
				comments[i].CreatorName = u.Username
			}
		}
	}
	return nil
}

// CreateAttachment records a new bug attachment metadata record.
func (r *BugRepository) CreateAttachment(att *model.ProjectBugAttachment) error {
	return r.db.Create(att).Error
}

// FindAttachmentByID retrieves a bug attachment by ID with storage metadata and creator info.
func (r *BugRepository) FindAttachmentByID(id uint) (*model.ProjectBugAttachment, error) {
	var att model.ProjectBugAttachment
	if err := r.db.First(&att, id).Error; err != nil {
		return nil, err
	}
	atts := []model.ProjectBugAttachment{att}
	if err := r.attachAttachmentDetails(atts); err != nil {
		return nil, err
	}
	return &atts[0], nil
}

// ListAttachments retrieves all attachments associated with a bug.
func (r *BugRepository) ListAttachments(bugID uint) ([]model.ProjectBugAttachment, error) {
	var atts []model.ProjectBugAttachment
	if err := r.db.Where("bug_id = ?", bugID).Order("created_at ASC, id ASC").Find(&atts).Error; err != nil {
		return nil, err
	}
	if err := r.attachAttachmentDetails(atts); err != nil {
		return nil, err
	}
	return atts, nil
}

// DeleteAttachment removes a bug attachment metadata record.
func (r *BugRepository) DeleteAttachment(id uint) error {
	return r.db.Delete(&model.ProjectBugAttachment{}, id).Error
}

func (r *BugRepository) attachAttachmentDetails(atts []model.ProjectBugAttachment) error {
	if len(atts) == 0 {
		return nil
	}
	userIDs := make([]uint, 0, len(atts))
	seenUsers := make(map[uint]struct{}, len(atts))
	storageIDs := make([]uint, 0, len(atts))
	seenStorage := make(map[uint]struct{}, len(atts))

	for _, a := range atts {
		if a.CreatedBy > 0 {
			if _, ok := seenUsers[a.CreatedBy]; !ok {
				seenUsers[a.CreatedBy] = struct{}{}
				userIDs = append(userIDs, a.CreatedBy)
			}
		}
		if a.StorageObjectID > 0 {
			if _, ok := seenStorage[a.StorageObjectID]; !ok {
				seenStorage[a.StorageObjectID] = struct{}{}
				storageIDs = append(storageIDs, a.StorageObjectID)
			}
		}
	}

	if len(userIDs) > 0 {
		var users []model.UserOption
		if err := r.db.Table("users").
			Select("id, username, display_name").
			Where("id IN ?", userIDs).
			Find(&users).Error; err != nil {
			return err
		}
		userMap := make(map[uint]model.UserOption, len(users))
		for _, u := range users {
			userMap[u.ID] = u
		}
		for i := range atts {
			if u, ok := userMap[atts[i].CreatedBy]; ok {
				atts[i].CreatorUsername = u.Username
				if u.DisplayName != "" {
					atts[i].CreatorName = u.DisplayName
				} else {
					atts[i].CreatorName = u.Username
				}
			}
		}
	}

	if len(storageIDs) > 0 {
		type storageMeta struct {
			ID          uint   `gorm:"column:id"`
			Size        int64  `gorm:"column:size"`
			ContentType string `gorm:"column:content_type"`
		}
		var metas []storageMeta
		if err := r.db.Table("storage_objects").
			Select("id, size, content_type").
			Where("id IN ?", storageIDs).
			Find(&metas).Error; err != nil {
			return err
		}
		metaMap := make(map[uint]storageMeta, len(metas))
		for _, m := range metas {
			metaMap[m.ID] = m
		}
		for i := range atts {
			if m, ok := metaMap[atts[i].StorageObjectID]; ok {
				atts[i].FileSize = m.Size
				atts[i].ContentType = m.ContentType
			}
		}
	}

	return nil
}
