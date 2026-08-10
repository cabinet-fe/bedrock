package repository

import (
	"bedrock/internal/dashboard/model"

	"gorm.io/gorm"
)

type DashboardRepository struct{ db *gorm.DB }

func NewDashboardRepository(db *gorm.DB) *DashboardRepository {
	return &DashboardRepository{db: db}
}

func (r *DashboardRepository) FindLayoutByUserID(userID uint) (*model.Layout, error) {
	var layout model.Layout
	if err := r.db.Where("user_id = ?", userID).First(&layout).Error; err != nil {
		return nil, err
	}
	return &layout, nil
}

func (r *DashboardRepository) CreateLayout(layout *model.Layout) error {
	return r.db.Create(layout).Error
}

func (r *DashboardRepository) UpdateLayout(layout *model.Layout) error {
	return r.db.Model(&model.Layout{}).Where("user_id = ?", layout.UserID).
		Updates(map[string]interface{}{"cards_json": layout.CardsJSON}).Error
}

func (r *DashboardRepository) CountRunsByStatus(status string) (int64, error) {
	var total int64
	err := r.db.Table("build_runs").Where("status = ?", status).Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountFinishedRuns() (total, success int64, err error) {
	err = r.db.Table("build_runs").
		Where("status IN ?", []string{"success", "failed", "cancelled", "interrupted"}).
		Count(&total).Error
	if err != nil {
		return 0, 0, err
	}
	err = r.db.Table("build_runs").Where("status = ?", "success").Count(&success).Error
	return total, success, err
}

func (r *DashboardRepository) ListRecentRuns(limit int) ([]model.RecentRun, error) {
	var rows []model.RecentRun
	err := r.db.Table("build_runs").
		Select("id, build_job_id, build_number, status, branch, created_at").
		Order("id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *DashboardRepository) CountAgentRunsByStatus(status string) (int64, error) {
	var total int64
	err := r.db.Table("agent_runs").Where("status = ?", status).Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountAgentRunsByStatuses(statuses []string) (int64, error) {
	var total int64
	err := r.db.Table("agent_runs").Where("status IN ?", statuses).Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountFinishedAgentRuns() (total, success int64, err error) {
	err = r.db.Table("agent_runs").
		Where("status IN ?", []string{"success", "failed", "cancelled", "interrupted"}).
		Count(&total).Error
	if err != nil {
		return 0, 0, err
	}
	err = r.db.Table("agent_runs").Where("status = ?", "success").Count(&success).Error
	return total, success, err
}

func (r *DashboardRepository) ListRecentAgentRuns(limit int) ([]model.RecentAgentRun, error) {
	var rows []model.RecentAgentRun
	err := r.db.Table("agent_runs").
		Select("agent_runs.id, agent_runs.agent_id, ai_agents.name AS agent_name, agent_runs.trigger_type, agent_runs.status, agent_runs.created_at").
		Joins("LEFT JOIN ai_agents ON ai_agents.id = agent_runs.agent_id").
		Order("agent_runs.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *DashboardRepository) CountScriptRunsByStatus(status string) (int64, error) {
	var total int64
	err := r.db.Table("script_runs").Where("status = ?", status).Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountFinishedScriptRuns() (total, success int64, err error) {
	err = r.db.Table("script_runs").
		Where("status IN ?", []string{"success", "failed", "cancelled", "interrupted"}).
		Count(&total).Error
	if err != nil {
		return 0, 0, err
	}
	err = r.db.Table("script_runs").Where("status = ?", "success").Count(&success).Error
	return total, success, err
}

func (r *DashboardRepository) ListRecentScriptRuns(limit int) ([]model.RecentScriptRun, error) {
	var rows []model.RecentScriptRun
	err := r.db.Table("script_runs").
		Select("script_runs.id, script_runs.script_job_id, script_jobs.name AS job_name, script_runs.run_number, script_runs.status, script_runs.created_at").
		Joins("LEFT JOIN script_jobs ON script_jobs.id = script_runs.script_job_id").
		Order("script_runs.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *DashboardRepository) CountPipelineRunsByStatus(status string) (int64, error) {
	var total int64
	err := r.db.Table("pipeline_runs").Where("status = ?", status).Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountFinishedPipelineRuns() (total, success int64, err error) {
	err = r.db.Table("pipeline_runs").
		Where("status IN ?", []string{"success", "failed", "cancelled"}).
		Count(&total).Error
	if err != nil {
		return 0, 0, err
	}
	err = r.db.Table("pipeline_runs").Where("status = ?", "success").Count(&success).Error
	return total, success, err
}

func (r *DashboardRepository) ListRecentPipelineRuns(limit int) ([]model.RecentPipelineRun, error) {
	var rows []model.RecentPipelineRun
	err := r.db.Table("pipeline_runs").
		Select("pipeline_runs.id, pipeline_runs.build_pipeline_id, build_pipelines.name AS pipeline_name, pipeline_runs.run_number, pipeline_runs.status, pipeline_runs.created_at").
		Joins("LEFT JOIN build_pipelines ON build_pipelines.id = pipeline_runs.build_pipeline_id").
		Order("pipeline_runs.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *DashboardRepository) CountBuildJobs() (int64, error) {
	var total int64
	err := r.db.Table("build_jobs").Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountScriptJobs() (int64, error) {
	var total int64
	err := r.db.Table("script_jobs").Count(&total).Error
	return total, err
}

func (r *DashboardRepository) CountPipelines() (int64, error) {
	var total int64
	err := r.db.Table("build_pipelines").Count(&total).Error
	return total, err
}

// ListMyProjects 返回用户作为成员或创建者的项目，按 updated_at 倒序。
func (r *DashboardRepository) ListMyProjects(userID uint, limit int) ([]model.MyProject, error) {
	var rows []model.MyProject
	err := r.db.Table("product_projects AS p").
		Select("p.id, p.name, p.slug, p.status, COALESCE(pm.role, '') AS my_role").
		Joins("LEFT JOIN project_members AS pm ON pm.project_id = p.id AND pm.user_id = ?", userID).
		Where("p.deleted_at IS NULL AND (pm.user_id = ? OR p.created_by = ?)", userID, userID).
		Order("p.updated_at DESC, p.id DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}
