package model

import "time"

// Layout is a per-user dashboard card configuration. CardsJSON is kept as a
// JSON array so a new card type does not require a schema migration.
type Layout struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"not null;uniqueIndex"`
	CardsJSON string    `json:"-" gorm:"type:text;not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Layout) TableName() string { return "dashboard_layouts" }

// CardLayout controls one known dashboard card. Card IDs are server-defined;
// clients cannot register arbitrary cards through the layout endpoint.
// X/Y/W/H are 12-column grid geometry (GridStack); omitted in legacy JSON.
type CardLayout struct {
	ID      string `json:"id"`
	Visible bool   `json:"visible"`
	Order   int    `json:"order"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
}

// LayoutResponse is intentionally stable for the dashboard editor.
type LayoutResponse struct {
	Cards []CardLayout `json:"cards"`
}

type BuildSummary struct {
	Running     int64       `json:"running"`
	Queued      int64       `json:"queued"`
	SuccessRate float64     `json:"success_rate"`
	Recent      []RecentRun `json:"recent"`
}

type RecentRun struct {
	ID          uint      `json:"id"`
	BuildJobID  uint      `json:"build_job_id"`
	BuildNumber int       `json:"build_number"`
	Status      string    `json:"status"`
	Branch      string    `json:"branch"`
	CreatedAt   time.Time `json:"created_at"`
}

type AgentRunSummary struct {
	Running     int64            `json:"running"`
	Queued      int64            `json:"queued"`
	SuccessRate float64          `json:"success_rate"`
	Recent      []RecentAgentRun `json:"recent"`
}

type RecentAgentRun struct {
	ID          uint      `json:"id"`
	AgentID     uint      `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	TriggerType string    `json:"trigger_type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ScriptRunSummary struct {
	Running     int64             `json:"running"`
	Queued      int64             `json:"queued"`
	SuccessRate float64           `json:"success_rate"`
	Recent      []RecentScriptRun `json:"recent"`
}

type RecentScriptRun struct {
	ID          uint      `json:"id"`
	ScriptJobID uint      `json:"script_job_id"`
	JobName     string    `json:"job_name"`
	RunNumber   int       `json:"run_number"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type PipelineRunSummary struct {
	Running     int64               `json:"running"`
	Queued      int64               `json:"queued"`
	SuccessRate float64             `json:"success_rate"`
	Recent      []RecentPipelineRun `json:"recent"`
}

type RecentPipelineRun struct {
	ID              uint      `json:"id"`
	BuildPipelineID uint      `json:"build_pipeline_id"`
	PipelineName    string    `json:"pipeline_name"`
	RunNumber       int       `json:"run_number"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

// TaskOverview 按权限分项返回任务总数；无权限项为 null。
type TaskOverview struct {
	BuildJobs  *int64 `json:"build_jobs"`
	ScriptJobs *int64 `json:"script_jobs"`
	Pipelines  *int64 `json:"pipelines"`
}

type MyProject struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
	MyRole string `json:"my_role"`
}

type SystemInfo struct {
	Version   string    `json:"version"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Runtime   string    `json:"runtime"`
	Hostname  string    `json:"hostname"`
	StartTime time.Time `json:"start_time"`
}

// DirectoryUsage is the on-disk size of one configured data directory.
type DirectoryUsage struct {
	Path      string `json:"path"`
	UsedBytes uint64 `json:"used_bytes"`
}

type SystemStatus struct {
	CPUUsagePercent    float64          `json:"cpu_usage_percent"`
	MemoryUsedBytes    uint64           `json:"memory_used_bytes"`
	MemoryTotalBytes   uint64           `json:"memory_total_bytes"`
	MemoryUsagePercent float64          `json:"memory_usage_percent"`
	DiskUsedBytes      uint64           `json:"disk_used_bytes"`
	DiskTotalBytes     uint64           `json:"disk_total_bytes"`
	DiskFreeBytes      uint64           `json:"disk_free_bytes"`
	DiskUsagePercent   float64          `json:"disk_usage_percent"`
	Health             string           `json:"health"`
	Directories        []DirectoryUsage `json:"directories"`
	CollectedAt        time.Time        `json:"collected_at"`
}
