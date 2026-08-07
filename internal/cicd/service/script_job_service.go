package service

import (
	"encoding/json"
	"sort"
	"strings"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	projectrepo "bedrock/internal/project/repository"
	rbacmodel "bedrock/internal/rbac/model"
)

type ScriptJobService struct {
	jobs         *repository.ScriptJobRepository
	projects     *projectrepo.ProjectRepository
	cron         ScriptCronRegistrar
	workspaceDir string
}

// ScriptCronRegistrar updates in-process cron entries when script jobs change.
type ScriptCronRegistrar interface {
	Add(job model.ScriptJob) error
	Remove(jobID uint)
}

func NewScriptJobService(jobs *repository.ScriptJobRepository, projects *projectrepo.ProjectRepository) *ScriptJobService {
	return &ScriptJobService{jobs: jobs, projects: projects}
}

func (s *ScriptJobService) SetCron(c ScriptCronRegistrar) { s.cron = c }

func (s *ScriptJobService) SetWorkspaceDir(dir string) { s.workspaceDir = strings.TrimSpace(dir) }

type CreateScriptJobInput struct {
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Enabled        *bool         `json:"enabled"`
	ScriptType     string        `json:"script_type"`
	Script         string        `json:"script"`
	WorkDir        string        `json:"work_dir"`
	EnvVarNames    []string      `json:"env_var_names"`
	EnvVars        []EnvVarInput `json:"env_vars"`
	TriggerManual  *bool         `json:"trigger_manual"`
	TriggerWebhook *bool         `json:"trigger_webhook"`
	TriggerCron    *bool         `json:"trigger_cron"`
	CronExpression string        `json:"cron_expression"`
	CronTimezone   string        `json:"cron_timezone"`
	WebhookType    string        `json:"webhook_type"`
	IsPublic       *bool         `json:"is_public"`
	ProjectID      *uint         `json:"project_id"`
}

type UpdateScriptJobInput struct {
	Name           *string        `json:"name"`
	Description    *string        `json:"description"`
	Enabled        *bool          `json:"enabled"`
	ScriptType     *string        `json:"script_type"`
	Script         *string        `json:"script"`
	WorkDir        *string        `json:"work_dir"`
	EnvVarNames    *[]string      `json:"env_var_names"`
	EnvVars        *[]EnvVarInput `json:"env_vars"`
	TriggerManual  *bool          `json:"trigger_manual"`
	TriggerWebhook *bool          `json:"trigger_webhook"`
	TriggerCron    *bool          `json:"trigger_cron"`
	CronExpression *string        `json:"cron_expression"`
	CronTimezone   *string        `json:"cron_timezone"`
	WebhookType    *string        `json:"webhook_type"`
	IsPublic       *bool          `json:"is_public"`
	ProjectID      *uint          `json:"project_id"`
}

func (s *ScriptJobService) Create(createdBy uint, in CreateScriptJobInput) (*model.ScriptJob, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errorsNew("名称不能为空")
	}
	projectID, err := resolveProjectID(s.projects, in.ProjectID)
	if err != nil {
		return nil, err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, err
	}
	whType := strings.TrimSpace(in.WebhookType)
	if whType == "" {
		whType = "generic"
	}
	job := &model.ScriptJob{
		Name:           name,
		Description:    strings.TrimSpace(in.Description),
		Enabled:        boolOr(in.Enabled, true),
		ScriptType:     stringOr(in.ScriptType, "bash"),
		Script:         in.Script,
		WorkDir:        strings.TrimSpace(in.WorkDir),
		TriggerManual:  boolOr(in.TriggerManual, true),
		TriggerWebhook: boolOr(in.TriggerWebhook, false),
		TriggerCron:    boolOr(in.TriggerCron, false),
		WebhookSecret:  secret,
		WebhookType:    whType,
		CronExpression: strings.TrimSpace(in.CronExpression),
		CronTimezone:   stringOr(in.CronTimezone, "UTC"),
		IsPublic:       boolOr(in.IsPublic, false),
		ProjectID:      projectID,
		CreatedBy:      createdBy,
	}
	if err := validateOptionalRelPath(job.WorkDir, "工作目录"); err != nil {
		return nil, err
	}
	if err := encodeScriptEnvNames(job, in.EnvVarNames); err != nil {
		return nil, err
	}
	if in.EnvVars != nil {
		if err := applyScriptJobEnvVarsInput(job, in.EnvVars); err != nil {
			return nil, err
		}
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	out, err := s.Get(job.ID, createdBy, rbacmodel.DataScopeAll)
	if err != nil {
		return nil, err
	}
	if err := s.syncCron(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ScriptJobService) Update(id uint, userID uint, dataScope string, in UpdateScriptJobInput) (*model.ScriptJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobWrite(job, userID, dataScope); err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errorsNew("名称不能为空")
		}
		job.Name = name
	}
	if in.Description != nil {
		job.Description = strings.TrimSpace(*in.Description)
	}
	if in.Enabled != nil {
		job.Enabled = *in.Enabled
	}
	if in.ScriptType != nil {
		job.ScriptType = stringOr(*in.ScriptType, "bash")
	}
	if in.Script != nil {
		job.Script = *in.Script
	}
	if in.WorkDir != nil {
		job.WorkDir = strings.TrimSpace(*in.WorkDir)
		if err := validateOptionalRelPath(job.WorkDir, "工作目录"); err != nil {
			return nil, err
		}
	}
	if in.EnvVarNames != nil {
		if err := encodeScriptEnvNames(job, *in.EnvVarNames); err != nil {
			return nil, err
		}
	}
	if in.EnvVars != nil {
		if err := applyScriptJobEnvVarsInput(job, *in.EnvVars); err != nil {
			return nil, err
		}
	}
	if in.TriggerManual != nil {
		job.TriggerManual = *in.TriggerManual
	}
	if in.TriggerWebhook != nil {
		job.TriggerWebhook = *in.TriggerWebhook
	}
	if in.TriggerCron != nil {
		job.TriggerCron = *in.TriggerCron
	}
	if in.CronExpression != nil {
		job.CronExpression = strings.TrimSpace(*in.CronExpression)
	}
	if in.CronTimezone != nil {
		job.CronTimezone = stringOr(*in.CronTimezone, "UTC")
	}
	if in.WebhookType != nil {
		job.WebhookType = stringOr(*in.WebhookType, "generic")
	}
	if in.IsPublic != nil {
		job.IsPublic = *in.IsPublic
	}
	if in.ProjectID != nil {
		projectID, err := resolveProjectID(s.projects, in.ProjectID)
		if err != nil {
			return nil, err
		}
		job.ProjectID = projectID
	}
	if err := s.jobs.Update(job); err != nil {
		return nil, err
	}
	out, err := s.Get(id, userID, dataScope)
	if err != nil {
		return nil, err
	}
	if err := s.syncCron(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ScriptJobService) Delete(id uint, userID uint, dataScope string) error {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobWrite(job, userID, dataScope); err != nil {
		return err
	}
	if err := s.jobs.Delete(id); err != nil {
		return err
	}
	if s.cron != nil {
		s.cron.Remove(id)
	}
	return nil
}

func (s *ScriptJobService) Get(id uint, userID uint, dataScope string) (*model.ScriptJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobRead(job, userID, dataScope); err != nil {
		return nil, err
	}
	hydrateScriptJobEnv(job)
	out := publicScriptJob(job, false)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *ScriptJobService) GetWithSecret(id uint, userID uint, dataScope string) (*model.ScriptJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobWrite(job, userID, dataScope); err != nil {
		return nil, err
	}
	hydrateScriptJobEnv(job)
	out := publicScriptJob(job, true)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *ScriptJobService) RotateWebhookSecret(id uint, userID uint, dataScope string) (*model.ScriptJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobWrite(job, userID, dataScope); err != nil {
		return nil, err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, err
	}
	job.WebhookSecret = secret
	if err := s.jobs.Update(job); err != nil {
		return nil, err
	}
	hydrateScriptJobEnv(job)
	out := publicScriptJob(job, true)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *ScriptJobService) List(q pkg.ListQuery, keyword string, projectID *uint, userID uint, dataScope string) ([]model.ScriptJob, int64, error) {
	var createdBy *uint
	// D3: 带 project_id 时跳过 created_by/is_public 数据范围过滤
	if projectID == nil && dataScope != rbacmodel.DataScopeAll {
		createdBy = &userID
	}
	items, total, err := s.jobs.List(q, keyword, createdBy, projectID)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		hydrateScriptJobEnv(&items[i])
		pub := publicScriptJob(&items[i], false)
		s.attachWorkspacePath(pub)
		items[i] = *pub
	}
	return items, total, nil
}

func (s *ScriptJobService) syncCron(job *model.ScriptJob) error {
	if s.cron == nil || job == nil {
		return nil
	}
	return s.cron.Add(*job)
}

func (s *ScriptJobService) attachWorkspacePath(job *model.ScriptJob) {
	if job == nil || job.ID == 0 || s.workspaceDir == "" {
		return
	}
	abs, err := engine.AbsoluteScriptWorkspace(s.workspaceDir, job.ID)
	if err != nil {
		job.WorkspacePath = engine.ScriptWorkspace(s.workspaceDir, job.ID)
		return
	}
	job.WorkspacePath = abs
}

func encodeScriptEnvNames(job *model.ScriptJob, names []string) error {
	if names == nil {
		names = []string{}
	}
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			cleaned = append(cleaned, n)
		}
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	job.EnvVarNamesJSON = string(b)
	job.EnvVarNames = cleaned
	return nil
}

func decodeScriptEnvNames(job *model.ScriptJob) {
	if job.EnvVarNamesJSON == "" {
		job.EnvVarNames = []string{}
		return
	}
	var names []string
	if err := json.Unmarshal([]byte(job.EnvVarNamesJSON), &names); err != nil {
		job.EnvVarNames = []string{}
		return
	}
	job.EnvVarNames = names
}

func hydrateScriptJobEnv(job *model.ScriptJob) {
	decodeScriptEnvNames(job)
	projectScriptJobEnvVars(job)
}

func projectScriptJobEnvVars(job *model.ScriptJob) {
	if job == nil {
		return
	}
	vars, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		job.EnvVars = []model.EnvVarView{}
		return
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]model.EnvVarView, 0, len(keys))
	for _, k := range keys {
		out = append(out, model.EnvVarView{Key: k, HasValue: true})
	}
	job.EnvVars = out
}

func applyScriptJobEnvVarsInput(job *model.ScriptJob, inputs []EnvVarInput) error {
	existing, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		return err
	}
	merged, err := mergeJobEnvVars(existing, inputs)
	if err != nil {
		return err
	}
	cipher, err := encryptJobEnvVars(merged)
	if err != nil {
		return err
	}
	job.EnvVarsCipher = cipher
	return nil
}

func scriptJobEnvVarKeys(job *model.ScriptJob) []string {
	if job == nil {
		return []string{}
	}
	vars, err := decryptJobEnvVars(job.EnvVarsCipher)
	if err != nil {
		return []string{}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func publicScriptJob(job *model.ScriptJob, revealSecret bool) *model.ScriptJob {
	cp := *job
	if !revealSecret {
		cp.WebhookSecret = ""
	}
	return &cp
}

func requireScriptJobRead(job *model.ScriptJob, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || job.IsPublic || job.CreatedBy == userID {
		return nil
	}
	// D3: 已关联项目的资源对具备 view 权限的用户可读
	if job.ProjectID != nil {
		return nil
	}
	return NewForbidden("无权访问该脚本任务")
}

func requireScriptJobWrite(job *model.ScriptJob, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || job.CreatedBy == userID {
		return nil
	}
	return NewForbidden("无权访问该脚本任务")
}
