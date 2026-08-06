package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
	resourcerepo "bedrock/internal/resource/repository"
)

type BuildJobService struct {
	jobs         *repository.BuildJobRepository
	repos        *resourcerepo.RepositoryRepository
	cron         CronRegistrar
	workspaceDir string
}

// CronRegistrar updates in-process cron entries when jobs change.
type CronRegistrar interface {
	Add(job model.BuildJob) error
	Remove(jobID uint)
}

func NewBuildJobService(jobs *repository.BuildJobRepository, repos *resourcerepo.RepositoryRepository) *BuildJobService {
	return &BuildJobService{jobs: jobs, repos: repos}
}

func (s *BuildJobService) SetCron(c CronRegistrar) { s.cron = c }

// SetWorkspaceDir configures build.workspace_dir for computing workspace_path.
func (s *BuildJobService) SetWorkspaceDir(dir string) { s.workspaceDir = strings.TrimSpace(dir) }

type DeployTargetInput struct {
	ServerID         *uint  `json:"server_id"`
	RemotePath       string `json:"remote_path"`
	Method           string `json:"method"`
	PostDeployScript string `json:"post_deploy_script"`
	Mirror           bool   `json:"mirror"`
	SortOrder        int    `json:"sort_order"`
}

type CreateBuildJobInput struct {
	RepositoryID       uint                `json:"repository_id"`
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	Tags               string              `json:"tags"`
	Enabled            *bool               `json:"enabled"`
	Branch             string              `json:"branch"`
	ShallowClone       *bool               `json:"shallow_clone"`
	BuildScriptType    string              `json:"build_script_type"`
	BuildScript        string              `json:"build_script"`
	PostBuildScript    string              `json:"post_build_script"`
	WorkDir            string              `json:"work_dir"`
	OutputDir          string              `json:"output_dir"` // deprecated: prefer artifact_paths
	ArtifactPaths      []string            `json:"artifact_paths"`
	CachePaths         string              `json:"cache_paths"`
	EnvVarNames        []string            `json:"env_var_names"`
	EnvVars            []EnvVarInput       `json:"env_vars"`
	TriggerManual      *bool               `json:"trigger_manual"`
	TriggerWebhook     *bool               `json:"trigger_webhook"`
	TriggerCron        *bool               `json:"trigger_cron"`
	CronExpression     string              `json:"cron_expression"`
	CronTimezone       string              `json:"cron_timezone"`
	MaxArtifacts       int                 `json:"max_artifacts"`
	ArtifactFormat     string              `json:"artifact_format"`
	AgentTriggerEvent  string              `json:"agent_trigger_event"`
	AgentIDs           []uint              `json:"agent_ids"`
	WebhookType        string              `json:"webhook_type"`
	WebhookRefPath     string              `json:"webhook_ref_path"`
	WebhookCommitPath  string              `json:"webhook_commit_path"`
	WebhookMessagePath string              `json:"webhook_message_path"`
	IsPublic           *bool               `json:"is_public"`
	DeployTargets      []DeployTargetInput `json:"deploy_targets"`
}

type UpdateBuildJobInput struct {
	Name               *string              `json:"name"`
	Description        *string              `json:"description"`
	Tags               *string              `json:"tags"`
	Enabled            *bool                `json:"enabled"`
	Branch             *string              `json:"branch"`
	ShallowClone       *bool                `json:"shallow_clone"`
	BuildScriptType    *string              `json:"build_script_type"`
	BuildScript        *string              `json:"build_script"`
	PostBuildScript    *string              `json:"post_build_script"`
	WorkDir            *string              `json:"work_dir"`
	OutputDir          *string              `json:"output_dir"` // deprecated: prefer artifact_paths
	ArtifactPaths      *[]string            `json:"artifact_paths"`
	CachePaths         *string              `json:"cache_paths"`
	EnvVarNames        *[]string            `json:"env_var_names"`
	EnvVars            *[]EnvVarInput       `json:"env_vars"`
	TriggerManual      *bool                `json:"trigger_manual"`
	TriggerWebhook     *bool                `json:"trigger_webhook"`
	TriggerCron        *bool                `json:"trigger_cron"`
	CronExpression     *string              `json:"cron_expression"`
	CronTimezone       *string              `json:"cron_timezone"`
	MaxArtifacts       *int                 `json:"max_artifacts"`
	ArtifactFormat     *string              `json:"artifact_format"`
	AgentTriggerEvent  *string              `json:"agent_trigger_event"`
	AgentIDs           *[]uint              `json:"agent_ids"`
	WebhookType        *string              `json:"webhook_type"`
	WebhookRefPath     *string              `json:"webhook_ref_path"`
	WebhookCommitPath  *string              `json:"webhook_commit_path"`
	WebhookMessagePath *string              `json:"webhook_message_path"`
	IsPublic           *bool                `json:"is_public"`
	DeployTargets      *[]DeployTargetInput `json:"deploy_targets"`
}

func (s *BuildJobService) Create(createdBy uint, in CreateBuildJobInput) (*model.BuildJob, error) {
	if _, err := s.repos.FindByID(in.RepositoryID); err != nil {
		return nil, errorsNew("所属仓库不存在")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errorsNew("名称不能为空")
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, err
	}
	whType := strings.TrimSpace(in.WebhookType)
	if whType == "" {
		whType = "auto"
	}
	job := &model.BuildJob{
		RepositoryID:       in.RepositoryID,
		Name:               name,
		Description:        strings.TrimSpace(in.Description),
		Tags:               strings.TrimSpace(in.Tags),
		Enabled:            boolOr(in.Enabled, true),
		Branch:             stringOr(in.Branch, "main"),
		ShallowClone:       boolOr(in.ShallowClone, true),
		BuildScriptType:    stringOr(in.BuildScriptType, "bash"),
		BuildScript:        in.BuildScript,
		PostBuildScript:    in.PostBuildScript,
		WorkDir:            strings.TrimSpace(in.WorkDir),
		CachePaths:         in.CachePaths,
		TriggerManual:      boolOr(in.TriggerManual, true),
		TriggerWebhook:     boolOr(in.TriggerWebhook, false),
		TriggerCron:        boolOr(in.TriggerCron, false),
		WebhookSecret:      secret,
		WebhookType:        whType,
		WebhookRefPath:     strings.TrimSpace(in.WebhookRefPath),
		WebhookCommitPath:  strings.TrimSpace(in.WebhookCommitPath),
		WebhookMessagePath: strings.TrimSpace(in.WebhookMessagePath),
		CronExpression:     strings.TrimSpace(in.CronExpression),
		CronTimezone:       stringOr(in.CronTimezone, "UTC"),
		MaxArtifacts:       intOr(in.MaxArtifacts, 5),
		ArtifactFormat:     normalizeArtifactFormat(in.ArtifactFormat),
		AgentTriggerEvent:  normalizeAgentEvent(in.AgentTriggerEvent),
		AgentIDs:           cleanAgentIDs(in.AgentIDs),
		IsPublic:           boolOr(in.IsPublic, false),
		CreatedBy:          createdBy,
	}
	if err := validateOptionalRelPath(job.WorkDir, "工作目录"); err != nil {
		return nil, err
	}
	if err := validateJobCachePaths(job.CachePaths); err != nil {
		return nil, err
	}
	if err := encodeArtifactPaths(job, resolveArtifactPathsInput(in.ArtifactPaths, in.OutputDir)); err != nil {
		return nil, err
	}
	if err := encodeEnvNames(job, in.EnvVarNames); err != nil {
		return nil, err
	}
	if in.EnvVars != nil {
		if err := applyJobEnvVarsInput(job, in.EnvVars); err != nil {
			return nil, err
		}
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	if len(in.DeployTargets) > 0 {
		targets, err := mapDeployTargets(in.DeployTargets)
		if err != nil {
			return nil, err
		}
		if err := s.jobs.ReplaceDeployTargets(job.ID, targets); err != nil {
			return nil, err
		}
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

func (s *BuildJobService) Update(id uint, userID uint, dataScope string, in UpdateBuildJobInput) (*model.BuildJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if err := requireJobAccess(job, userID, dataScope); err != nil {
		return nil, err
	}
	if in.Name != nil {
		job.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		job.Description = strings.TrimSpace(*in.Description)
	}
	if in.Tags != nil {
		job.Tags = strings.TrimSpace(*in.Tags)
	}
	if in.Enabled != nil {
		job.Enabled = *in.Enabled
	}
	if in.Branch != nil {
		job.Branch = strings.TrimSpace(*in.Branch)
	}
	if in.ShallowClone != nil {
		job.ShallowClone = *in.ShallowClone
	}
	if in.BuildScriptType != nil {
		job.BuildScriptType = strings.TrimSpace(*in.BuildScriptType)
	}
	if in.BuildScript != nil {
		job.BuildScript = *in.BuildScript
	}
	if in.PostBuildScript != nil {
		job.PostBuildScript = *in.PostBuildScript
	}
	if in.WorkDir != nil {
		job.WorkDir = strings.TrimSpace(*in.WorkDir)
		if err := validateOptionalRelPath(job.WorkDir, "工作目录"); err != nil {
			return nil, err
		}
	}
	// Explicit artifact_paths (including empty) wins; do not fall back to output_dir.
	if in.ArtifactPaths != nil {
		if err := encodeArtifactPaths(job, resolveArtifactPathsInput(*in.ArtifactPaths, "")); err != nil {
			return nil, err
		}
	} else if in.OutputDir != nil {
		if err := encodeArtifactPaths(job, resolveArtifactPathsInput(nil, *in.OutputDir)); err != nil {
			return nil, err
		}
	}
	if in.CachePaths != nil {
		if err := validateJobCachePaths(*in.CachePaths); err != nil {
			return nil, err
		}
		job.CachePaths = *in.CachePaths
	}
	if in.EnvVarNames != nil {
		if err := encodeEnvNames(job, *in.EnvVarNames); err != nil {
			return nil, err
		}
	}
	if in.EnvVars != nil {
		if err := applyJobEnvVarsInput(job, *in.EnvVars); err != nil {
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
	if in.WebhookType != nil {
		job.WebhookType = strings.TrimSpace(*in.WebhookType)
	}
	if in.WebhookRefPath != nil {
		job.WebhookRefPath = strings.TrimSpace(*in.WebhookRefPath)
	}
	if in.WebhookCommitPath != nil {
		job.WebhookCommitPath = strings.TrimSpace(*in.WebhookCommitPath)
	}
	if in.WebhookMessagePath != nil {
		job.WebhookMessagePath = strings.TrimSpace(*in.WebhookMessagePath)
	}
	if in.IsPublic != nil {
		job.IsPublic = *in.IsPublic
	}
	if in.CronExpression != nil {
		job.CronExpression = strings.TrimSpace(*in.CronExpression)
	}
	if in.CronTimezone != nil {
		job.CronTimezone = stringOr(*in.CronTimezone, "UTC")
	}
	if in.MaxArtifacts != nil {
		job.MaxArtifacts = intOr(*in.MaxArtifacts, 5)
	}
	if in.ArtifactFormat != nil {
		job.ArtifactFormat = normalizeArtifactFormat(*in.ArtifactFormat)
	}
	if in.AgentTriggerEvent != nil {
		job.AgentTriggerEvent = normalizeAgentEvent(*in.AgentTriggerEvent)
	}
	if in.AgentIDs != nil {
		job.AgentIDs = cleanAgentIDs(*in.AgentIDs)
	}
	if job.Name == "" {
		return nil, errorsNew("名称不能为空")
	}
	if err := s.jobs.Update(job); err != nil {
		return nil, err
	}
	if in.DeployTargets != nil {
		targets, err := mapDeployTargets(*in.DeployTargets)
		if err != nil {
			return nil, err
		}
		if err := s.jobs.ReplaceDeployTargets(job.ID, targets); err != nil {
			return nil, err
		}
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

func (s *BuildJobService) Delete(id uint, userID uint, dataScope string) error {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return NewNotFound("构建任务不存在")
	}
	if err := requireJobAccess(job, userID, dataScope); err != nil {
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

func (s *BuildJobService) syncCron(job *model.BuildJob) error {
	if s.cron == nil || job == nil {
		return nil
	}
	return s.cron.Add(*job)
}

func (s *BuildJobService) Get(id uint, userID uint, dataScope string) (*model.BuildJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if err := requireJobRead(job, userID, dataScope); err != nil {
		return nil, err
	}
	hydrateJobEnv(job)
	out := publicJob(job, false)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *BuildJobService) GetWithSecret(id uint, userID uint, dataScope string) (*model.BuildJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if err := requireJobWrite(job, userID, dataScope); err != nil {
		return nil, err
	}
	hydrateJobEnv(job)
	out := publicJob(job, true)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *BuildJobService) RotateWebhookSecret(id uint, userID uint, dataScope string) (*model.BuildJob, error) {
	job, err := s.jobs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if err := requireJobAccess(job, userID, dataScope); err != nil {
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
	hydrateJobEnv(job)
	out := publicJob(job, true)
	s.attachWorkspacePath(out)
	return out, nil
}

func (s *BuildJobService) List(q pkg.ListQuery, repositoryID *uint, keyword, tag string, userID uint, dataScope string) ([]model.BuildJob, int64, error) {
	var createdBy *uint
	if dataScope != rbacmodel.DataScopeAll {
		createdBy = &userID
	}
	items, total, err := s.jobs.List(q, repositoryID, keyword, tag, createdBy)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		hydrateJobEnv(&items[i])
		pub := publicJob(&items[i], false)
		s.attachWorkspacePath(pub)
		items[i] = *pub
	}
	return items, total, nil
}

func mapDeployTargets(in []DeployTargetInput) ([]model.DeployTarget, error) {
	out := make([]model.DeployTarget, 0, len(in))
	for i, t := range in {
		method := normalizeDeployMethod(t.Method)
		if method == "" {
			return nil, errorsNew("部署方法无效")
		}
		if method != "local" && (t.ServerID == nil || *t.ServerID == 0) {
			return nil, errorsNew("非 local 部署必须指定 server_id")
		}
		order := t.SortOrder
		if order == 0 {
			order = i
		}
		out = append(out, model.DeployTarget{
			ServerID:         nilIfZero(t.ServerID),
			RemotePath:       strings.TrimSpace(t.RemotePath),
			Method:           method,
			PostDeployScript: t.PostDeployScript,
			Mirror:           t.Mirror,
			SortOrder:        order,
		})
	}
	return out, nil
}

func encodeEnvNames(job *model.BuildJob, names []string) error {
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

func decodeEnvNames(job *model.BuildJob) {
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

func hydrateJobEnv(job *model.BuildJob) {
	decodeEnvNames(job)
	decodeArtifactPaths(job)
	projectJobEnvVars(job)
}

func normalizeArtifactFormat(f string) string {
	if strings.ToLower(strings.TrimSpace(f)) == "zip" {
		return "zip"
	}
	return "gzip"
}

func normalizeAgentEvent(e string) string {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "distribution_finished", "none":
		return strings.ToLower(strings.TrimSpace(e))
	default:
		return "artifact_ready"
	}
}

// cleanAgentIDs drops zero/duplicate IDs and always returns a non-nil list.
func cleanAgentIDs(ids []uint) model.UintList {
	out := make(model.UintList, 0, len(ids))
	seen := map[uint]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func normalizeDeployMethod(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "rsync", "sftp", "scp", "agent", "local":
		return strings.ToLower(strings.TrimSpace(m))
	default:
		return ""
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func stringOr(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func publicJob(job *model.BuildJob, revealSecret bool) *model.BuildJob {
	cp := *job
	if !revealSecret {
		cp.WebhookSecret = ""
	}
	return &cp
}

func (s *BuildJobService) attachWorkspacePath(job *model.BuildJob) {
	if job == nil || job.ID == 0 || s.workspaceDir == "" {
		return
	}
	abs, err := engine.AbsoluteJobWorkspace(s.workspaceDir, job.ID)
	if err != nil {
		job.WorkspacePath = engine.JobWorkspace(s.workspaceDir, job.ID)
		return
	}
	job.WorkspacePath = abs
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func requireJobRead(job *model.BuildJob, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || job.IsPublic || job.CreatedBy == userID {
		return nil
	}
	return NewForbidden("无权访问该构建任务")
}

func requireJobWrite(job *model.BuildJob, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || job.CreatedBy == userID {
		return nil
	}
	return NewForbidden("无权访问该构建任务")
}

// requireJobAccess keeps write semantics for mutate/execute paths.
func requireJobAccess(job *model.BuildJob, userID uint, dataScope string) error {
	return requireJobWrite(job, userID, dataScope)
}
