package repository

import (
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"

	"gorm.io/gorm"
)

type IssueRepository struct {
	db *gorm.DB
}

func NewIssueRepository(db *gorm.DB) *IssueRepository {
	return &IssueRepository{db: db}
}

// IssueFilter encapsulates query parameters for issue searches.
type IssueFilter struct {
	Keyword    string
	ProjectID  *uint
	Type       string
	Status     string
	Severity   string
	Priority   string
	AssigneeID *uint
	IterationID *uint
	// ExcludeClosed drops the closed/rejected terminal statuses (legacy
	// exclude_closed semantics keep only `closed` out for bugs).
	ExcludeClosed bool
	// ExcludeTerminal drops all terminal statuses (kanban default).
	ExcludeTerminal bool
}

func (r *IssueRepository) Create(issue *model.ProjectIssue) error {
	return r.db.Create(issue).Error
}

func (r *IssueRepository) FindByID(id uint) (*model.ProjectIssue, error) {
	var issue model.ProjectIssue
	if err := r.db.First(&issue, id).Error; err != nil {
		return nil, err
	}
	if err := r.AttachIssueDetails(&issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

func (r *IssueRepository) Update(issue *model.ProjectIssue) error {
	return r.db.Save(issue).Error
}

// Delete removes an issue and its comments/attachments/activities/watchers.
func (r *IssueRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("issue_id = ?", id).Delete(&model.ProjectIssueComment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id = ?", id).Delete(&model.ProjectIssueAttachment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id = ?", id).Delete(&model.ProjectIssueActivity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("issue_id = ?", id).Delete(&model.ProjectIssueWatcher{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ProjectIssue{}, id).Error
	})
}

func issueOrderExpr(table string) string {
	return table + ".updated_at DESC, " + table + ".id DESC"
}

func applyIssueFilter(db *gorm.DB, filter IssueFilter) *gorm.DB {
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("project_issues.title LIKE ? OR project_issues.description LIKE ?", like, like)
	}
	if t := strings.TrimSpace(filter.Type); t != "" {
		db = db.Where("project_issues.type = ?", t)
	}
	if st := strings.TrimSpace(filter.Status); st != "" {
		db = db.Where("project_issues.status = ?", st)
	}
	if sev := strings.TrimSpace(filter.Severity); sev != "" {
		db = db.Where("project_issues.severity = ?", sev)
	}
	if pri := strings.TrimSpace(filter.Priority); pri != "" {
		db = db.Where("project_issues.priority = ?", pri)
	}
	if filter.AssigneeID != nil {
		db = db.Where("project_issues.assignee_id = ?", *filter.AssigneeID)
	}
	if filter.IterationID != nil {
		db = db.Where("project_issues.iteration_id = ?", *filter.IterationID)
	}
	if filter.ExcludeClosed {
		db = db.Where("project_issues.status <> ?", model.BugStatusClosed)
	}
	if filter.ExcludeTerminal {
		db = db.Where("project_issues.status NOT IN ?", model.TerminalStatuses())
	}
	return db
}

func issueOrderMap() map[string]string {
	return map[string]string{
		"title":      "project_issues.title",
		"type":       "project_issues.type",
		"status":     "project_issues.status",
		"severity":   "project_issues.severity",
		"priority":   "project_issues.priority",
		"created_at": "project_issues.created_at",
		"updated_at": "project_issues.updated_at",
	}
}

// ListByProject queries issues within a specific project.
func (r *IssueRepository) ListByProject(projectID uint, filter IssueFilter, q pkg.ListQuery) ([]model.ProjectIssue, int64, error) {
	db := r.db.Model(&model.ProjectIssue{}).Where("project_issues.project_id = ?", projectID)
	db = applyIssueFilter(db, filter)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := pkg.OrderBy(q.Sort, issueOrderMap(), "project_issues.id", issueOrderExpr("project_issues"))
	var issues []model.ProjectIssue
	if err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&issues).Error; err != nil {
		return nil, 0, err
	}
	if err := r.AttachIssueListDetails(issues); err != nil {
		return nil, 0, err
	}
	return issues, total, nil
}

// ListAcrossProjects queries issues across projects, optionally scoped to accessible projectIDs.
func (r *IssueRepository) ListAcrossProjects(projectIDs []uint, filter IssueFilter, q pkg.ListQuery) ([]model.ProjectIssue, int64, error) {
	db := r.db.Model(&model.ProjectIssue{})
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return []model.ProjectIssue{}, 0, nil
		}
		db = db.Where("project_issues.project_id IN ?", projectIDs)
	}
	if filter.ProjectID != nil {
		db = db.Where("project_issues.project_id = ?", *filter.ProjectID)
	}
	db = applyIssueFilter(db, filter)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := pkg.OrderBy(q.Sort, issueOrderMap(), "project_issues.id", issueOrderExpr("project_issues"))
	var issues []model.ProjectIssue
	if err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&issues).Error; err != nil {
		return nil, 0, err
	}
	if err := r.AttachIssueListDetails(issues); err != nil {
		return nil, 0, err
	}
	return issues, total, nil
}

// ListForKanban loads issues of a type for board rendering; the caller sets
// ExcludeTerminal for the default "terminal statuses stay off the board".
func (r *IssueRepository) ListForKanban(projectIDs []uint, filter IssueFilter) ([]model.ProjectIssue, error) {
	db := r.db.Model(&model.ProjectIssue{})
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return []model.ProjectIssue{}, nil
		}
		db = db.Where("project_issues.project_id IN ?", projectIDs)
	}
	db = applyIssueFilter(db, filter)
	var issues []model.ProjectIssue
	if err := db.Find(&issues).Error; err != nil {
		return nil, err
	}
	if err := r.AttachIssueListDetails(issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// CountByStatus returns issue counts grouped by status for a type in a project.
func (r *IssueRepository) CountByStatus(projectID uint, issueType string) (map[string]int64, error) {
	type statusCount struct {
		Status string
		Count  int64
	}
	db := r.db.Model(&model.ProjectIssue{}).
		Select("status, count(*) as count").
		Where("project_id = ?", projectID)
	if issueType != "" {
		db = db.Where("type = ?", issueType)
	}
	var results []statusCount
	if err := db.Group("status").Scan(&results).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, sc := range results {
		counts[sc.Status] = sc.Count
	}
	return counts, nil
}

// CountByStatusAndIteration returns per-status issue counts for an iteration.
func (r *IssueRepository) CountByStatusAndIteration(iterationID uint) (map[string]int64, error) {
	type statusCount struct {
		Status string
		Count  int64
	}
	var results []statusCount
	if err := r.db.Model(&model.ProjectIssue{}).
		Select("status, count(*) as count").
		Where("iteration_id = ?", iterationID).
		Group("status").Scan(&results).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, sc := range results {
		counts[sc.Status] = sc.Count
	}
	return counts, nil
}

// Activities -------------------------------------------------------------

func (r *IssueRepository) RecordActivity(activity *model.ProjectIssueActivity) error {
	return r.db.Create(activity).Error
}

func (r *IssueRepository) ListActivities(issueID uint) ([]model.ProjectIssueActivity, error) {
	var activities []model.ProjectIssueActivity
	if err := r.db.Where("issue_id = ?", issueID).Order("created_at ASC, id ASC").Find(&activities).Error; err != nil {
		return nil, err
	}
	if err := r.attachActivityUsers(activities); err != nil {
		return nil, err
	}
	return activities, nil
}

// ListStatusChanges returns the status_change + create activities of issues in
// a project (optionally of one type), used to reconstruct per-day remaining
// counts for burndown charts.
func (r *IssueRepository) ListStatusTransitions(projectID uint, since string) ([]model.ProjectIssueActivity, error) {
	db := r.db.Model(&model.ProjectIssueActivity{}).
		Select("project_issue_activities.*").
		Joins("JOIN project_issues ON project_issues.id = project_issue_activities.issue_id").
		Where("project_issues.project_id = ?", projectID).
		Where("project_issue_activities.action IN ?", []string{model.IssueActivityCreate, model.IssueActivityStatusChange})
	if since != "" {
		db = db.Where("project_issue_activities.created_at >= ?", since)
	}
	var activities []model.ProjectIssueActivity
	if err := db.Order("project_issue_activities.created_at ASC, project_issue_activities.id ASC").Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

// Comments ----------------------------------------------------------------

func (r *IssueRepository) CreateComment(comment *model.ProjectIssueComment) error {
	return r.db.Create(comment).Error
}

func (r *IssueRepository) FindCommentByID(id uint) (*model.ProjectIssueComment, error) {
	var comment model.ProjectIssueComment
	if err := r.db.First(&comment, id).Error; err != nil {
		return nil, err
	}
	comments := []model.ProjectIssueComment{comment}
	if err := r.attachCommentExtras(comments); err != nil {
		return nil, err
	}
	return &comments[0], nil
}

func (r *IssueRepository) ListComments(issueID uint) ([]model.ProjectIssueComment, error) {
	var comments []model.ProjectIssueComment
	if err := r.db.Where("issue_id = ?", issueID).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	if err := r.attachCommentExtras(comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func (r *IssueRepository) UpdateComment(comment *model.ProjectIssueComment) error {
	return r.db.Save(comment).Error
}

func (r *IssueRepository) DeleteComment(id uint) error {
	return r.db.Delete(&model.ProjectIssueComment{}, id).Error
}

func (r *IssueRepository) attachCommentUsers(comments []model.ProjectIssueComment) error {
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
	return attachUserNames(r.db, userIDs, func(id uint, u model.UserOption) {
		for i := range comments {
			if comments[i].CreatedBy == id {
				fillCreatorView(&comments[i].CreatorName, &comments[i].CreatorUsername, u)
			}
		}
	})
}

// attachCommentExtras attaches creator info and comment-scoped attachments.
func (r *IssueRepository) attachCommentExtras(comments []model.ProjectIssueComment) error {
	if err := r.attachCommentUsers(comments); err != nil {
		return err
	}
	return r.attachCommentAttachments(comments)
}

func (r *IssueRepository) attachCommentAttachments(comments []model.ProjectIssueComment) error {
	if len(comments) == 0 {
		return nil
	}
	commentIDs := make([]uint, 0, len(comments))
	for _, c := range comments {
		commentIDs = append(commentIDs, c.ID)
	}
	var atts []model.ProjectIssueAttachment
	if err := r.db.Where("comment_id IN ?", commentIDs).Order("created_at ASC, id ASC").Find(&atts).Error; err != nil {
		return err
	}
	if err := r.attachAttachmentDetails(atts); err != nil {
		return err
	}
	grouped := make(map[uint][]model.ProjectIssueAttachment, len(comments))
	for _, a := range atts {
		if a.CommentID != nil {
			grouped[*a.CommentID] = append(grouped[*a.CommentID], a)
		}
	}
	for i := range comments {
		if list := grouped[comments[i].ID]; len(list) > 0 {
			comments[i].Attachments = list
		}
	}
	return nil
}

// Attachments ---------------------------------------------------------------

func (r *IssueRepository) CreateAttachment(att *model.ProjectIssueAttachment) error {
	return r.db.Create(att).Error
}

func (r *IssueRepository) FindAttachmentByID(id uint) (*model.ProjectIssueAttachment, error) {
	var att model.ProjectIssueAttachment
	if err := r.db.First(&att, id).Error; err != nil {
		return nil, err
	}
	atts := []model.ProjectIssueAttachment{att}
	if err := r.attachAttachmentDetails(atts); err != nil {
		return nil, err
	}
	return &atts[0], nil
}

func (r *IssueRepository) ListAttachments(issueID uint) ([]model.ProjectIssueAttachment, error) {
	var atts []model.ProjectIssueAttachment
	if err := r.db.Where("issue_id = ?", issueID).Order("created_at ASC, id ASC").Find(&atts).Error; err != nil {
		return nil, err
	}
	if err := r.attachAttachmentDetails(atts); err != nil {
		return nil, err
	}
	return atts, nil
}

func (r *IssueRepository) DeleteAttachment(id uint) error {
	return r.db.Delete(&model.ProjectIssueAttachment{}, id).Error
}

func (r *IssueRepository) ListAttachmentsByCommentID(commentID uint) ([]model.ProjectIssueAttachment, error) {
	var atts []model.ProjectIssueAttachment
	if err := r.db.Where("comment_id = ?", commentID).Find(&atts).Error; err != nil {
		return nil, err
	}
	return atts, nil
}

// ListAttachmentsByProject returns attachments of all issues in a project.
func (r *IssueRepository) ListAttachmentsByProject(projectID uint) ([]model.ProjectIssueAttachment, error) {
	var atts []model.ProjectIssueAttachment
	err := r.db.Model(&model.ProjectIssueAttachment{}).
		Joins("JOIN project_issues ON project_issues.id = project_issue_attachments.issue_id").
		Where("project_issues.project_id = ?", projectID).
		Find(&atts).Error
	return atts, err
}

// ListByIteration returns all issues assigned to an iteration.
func (r *IssueRepository) ListByIteration(iterationID uint) ([]model.ProjectIssue, error) {
	var issues []model.ProjectIssue
	if err := r.db.Where("iteration_id = ?", iterationID).Find(&issues).Error; err != nil {
		return nil, err
	}
	return issues, nil
}

// ClearIteration unassigns all issues from an iteration (back to the backlog).
func (r *IssueRepository) ClearIteration(iterationID uint) error {
	return r.db.Model(&model.ProjectIssue{}).Where("iteration_id = ?", iterationID).Update("iteration_id", nil).Error
}

// Watchers ------------------------------------------------------------------

func (r *IssueRepository) AddWatcher(issueID, userID uint) error {
	watcher := model.ProjectIssueWatcher{IssueID: issueID, UserID: userID}
	return r.db.Where("issue_id = ? AND user_id = ?", issueID, userID).FirstOrCreate(&watcher).Error
}

func (r *IssueRepository) RemoveWatcher(issueID, userID uint) error {
	return r.db.Where("issue_id = ? AND user_id = ?", issueID, userID).Delete(&model.ProjectIssueWatcher{}).Error
}

func (r *IssueRepository) ListWatcherIDs(issueID uint) ([]uint, error) {
	var watchers []model.ProjectIssueWatcher
	if err := r.db.Where("issue_id = ?", issueID).Find(&watchers).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(watchers))
	for _, w := range watchers {
		ids = append(ids, w.UserID)
	}
	return ids, nil
}

func (r *IssueRepository) IsWatching(issueID, userID uint) (bool, error) {
	var count int64
	if err := r.db.Model(&model.ProjectIssueWatcher{}).Where("issue_id = ? AND user_id = ?", issueID, userID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// View-field attach helpers --------------------------------------------------

func (r *IssueRepository) AttachIssueDetails(issue *model.ProjectIssue) error {
	if issue == nil {
		return nil
	}
	wrapper := []model.ProjectIssue{*issue}
	if err := r.AttachIssueListDetails(wrapper); err != nil {
		return err
	}
	*issue = wrapper[0]
	return nil
}

// AttachIssueListDetails batch-loads user/project/repository names and comment
// counts onto the issues (slice is modified in place via index).
func (r *IssueRepository) AttachIssueListDetails(issues []model.ProjectIssue) error {
	if len(issues) == 0 {
		return nil
	}
	ptrs := make([]*model.ProjectIssue, len(issues))
	for i := range issues {
		ptrs[i] = &issues[i]
	}

	userIDs := make([]uint, 0, len(issues)*2)
	seenUsers := make(map[uint]struct{})
	attach := func(id uint) {
		if id > 0 {
			if _, ok := seenUsers[id]; !ok {
				seenUsers[id] = struct{}{}
				userIDs = append(userIDs, id)
			}
		}
	}
	projectIDs := make([]uint, 0, len(issues))
	seenProjects := make(map[uint]struct{})
	repoIDs := make([]uint, 0, len(issues))
	seenRepos := make(map[uint]struct{})
	for _, iss := range issues {
		attach(iss.CreatedBy)
		if iss.AssigneeID != nil {
			attach(*iss.AssigneeID)
		}
		if iss.ProjectID > 0 {
			if _, ok := seenProjects[iss.ProjectID]; !ok {
				seenProjects[iss.ProjectID] = struct{}{}
				projectIDs = append(projectIDs, iss.ProjectID)
			}
		}
		if iss.RepositoryID != nil && *iss.RepositoryID > 0 {
			if _, ok := seenRepos[*iss.RepositoryID]; !ok {
				seenRepos[*iss.RepositoryID] = struct{}{}
				repoIDs = append(repoIDs, *iss.RepositoryID)
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
		for _, iss := range ptrs {
			if u, ok := userMap[iss.CreatedBy]; ok {
				fillCreatorView(&iss.CreatorName, &iss.CreatorUsername, u)
			}
			if iss.AssigneeID != nil {
				if u, ok := userMap[*iss.AssigneeID]; ok {
					fillCreatorView(&iss.AssigneeName, &iss.AssigneeUsername, u)
				}
			}
		}
	}

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
		for _, iss := range ptrs {
			if name, ok := projMap[iss.ProjectID]; ok {
				iss.ProjectName = name
			}
		}
	}

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
		for _, iss := range ptrs {
			if iss.RepositoryID != nil {
				if name, ok := repoMap[*iss.RepositoryID]; ok {
					iss.RepositoryName = name
				}
			}
		}
	}

	// Comment counts per issue.
	issueIDs := make([]uint, 0, len(issues))
	for _, iss := range issues {
		issueIDs = append(issueIDs, iss.ID)
	}
	type commentCount struct {
		IssueID uint
		Count   int64
	}
	var counts []commentCount
	if err := r.db.Model(&model.ProjectIssueComment{}).
		Select("issue_id, count(*) as count").
		Where("issue_id IN ?", issueIDs).
		Group("issue_id").Scan(&counts).Error; err != nil {
		return err
	}
	countMap := make(map[uint]int64, len(counts))
	for _, c := range counts {
		countMap[c.IssueID] = c.Count
	}
	for _, iss := range ptrs {
		iss.CommentCount = countMap[iss.ID]
	}
	return nil
}

func (r *IssueRepository) attachActivityUsers(activities []model.ProjectIssueActivity) error {
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
	return attachUserNames(r.db, userIDs, func(id uint, u model.UserOption) {
		for i := range activities {
			if activities[i].CreatedBy == id {
				fillCreatorView(&activities[i].CreatorName, &activities[i].CreatorUsername, u)
			}
		}
	})
}

func (r *IssueRepository) attachAttachmentDetails(atts []model.ProjectIssueAttachment) error {
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

	if err := attachUserNames(r.db, userIDs, func(id uint, u model.UserOption) {
		for i := range atts {
			if atts[i].CreatedBy == id {
				fillCreatorView(&atts[i].CreatorName, &atts[i].CreatorUsername, u)
			}
		}
	}); err != nil {
		return err
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

// attachUserNames loads usernames once and applies them via apply.
func attachUserNames(db *gorm.DB, userIDs []uint, apply func(id uint, u model.UserOption)) error {
	if len(userIDs) == 0 {
		return nil
	}
	var users []model.UserOption
	if err := db.Table("users").
		Select("id, username, display_name").
		Where("id IN ?", userIDs).
		Find(&users).Error; err != nil {
		return err
	}
	userMap := make(map[uint]model.UserOption, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}
	for id, u := range userMap {
		apply(id, u)
	}
	return nil
}

func fillCreatorView(name *string, username *string, u model.UserOption) {
	*username = u.Username
	if u.DisplayName != "" {
		*name = u.DisplayName
	} else {
		*name = u.Username
	}
}
