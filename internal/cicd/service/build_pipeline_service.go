package service

import (
	"strings"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
)

// PipelineCronRegistrar updates in-process cron entries when pipelines change.
type PipelineCronRegistrar interface {
	Add(p model.BuildPipeline) error
	Remove(pipelineID uint)
}

type BuildPipelineService struct {
	pipelines   *repository.BuildPipelineRepository
	jobs        *repository.BuildJobRepository
	scriptJobs  *repository.ScriptJobRepository
	agentExists func(id uint) bool
	cron        PipelineCronRegistrar
}

func NewBuildPipelineService(
	pipelines *repository.BuildPipelineRepository,
	jobs *repository.BuildJobRepository,
	scriptJobs *repository.ScriptJobRepository,
) *BuildPipelineService {
	return &BuildPipelineService{pipelines: pipelines, jobs: jobs, scriptJobs: scriptJobs}
}

func (s *BuildPipelineService) SetCron(c PipelineCronRegistrar) { s.cron = c }

// SetAgentExists wires the existence check for agent nodes (ai domain).
func (s *BuildPipelineService) SetAgentExists(fn func(id uint) bool) { s.agentExists = fn }

type CreateBuildPipelineInput struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	Enabled            *bool  `json:"enabled"`
	GraphJSON          string `json:"graph_json"`
	TriggerManual      *bool  `json:"trigger_manual"`
	TriggerWebhook     *bool  `json:"trigger_webhook"`
	TriggerCron        *bool  `json:"trigger_cron"`
	CronExpression     string `json:"cron_expression"`
	CronTimezone       string `json:"cron_timezone"`
	WebhookType        string `json:"webhook_type"`
	WebhookRefPath     string `json:"webhook_ref_path"`
	WebhookCommitPath  string `json:"webhook_commit_path"`
	WebhookMessagePath string `json:"webhook_message_path"`
	IsPublic           *bool  `json:"is_public"`
}

type UpdateBuildPipelineInput struct {
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	Enabled            *bool   `json:"enabled"`
	GraphJSON          *string `json:"graph_json"`
	TriggerManual      *bool   `json:"trigger_manual"`
	TriggerWebhook     *bool   `json:"trigger_webhook"`
	TriggerCron        *bool   `json:"trigger_cron"`
	CronExpression     *string `json:"cron_expression"`
	CronTimezone       *string `json:"cron_timezone"`
	WebhookType        *string `json:"webhook_type"`
	WebhookRefPath     *string `json:"webhook_ref_path"`
	WebhookCommitPath  *string `json:"webhook_commit_path"`
	WebhookMessagePath *string `json:"webhook_message_path"`
	IsPublic           *bool   `json:"is_public"`
}

func (s *BuildPipelineService) Create(createdBy uint, in CreateBuildPipelineInput) (*model.BuildPipeline, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errorsNew("名称不能为空")
	}
	graphJSON := strings.TrimSpace(in.GraphJSON)
	if graphJSON == "" {
		graphJSON = `{"nodes":[],"edges":[]}`
	}
	graphJSON, err := EncryptGraphEnvVars(graphJSON, "")
	if err != nil {
		return nil, err
	}
	if err := s.validateGraphJSON(graphJSON); err != nil {
		return nil, err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	manual := true
	if in.TriggerManual != nil {
		manual = *in.TriggerManual
	}
	p := &model.BuildPipeline{
		Name:               name,
		Description:        strings.TrimSpace(in.Description),
		Enabled:            enabled,
		GraphJSON:          graphJSON,
		TriggerManual:      manual,
		TriggerWebhook:     in.TriggerWebhook != nil && *in.TriggerWebhook,
		TriggerCron:        in.TriggerCron != nil && *in.TriggerCron,
		WebhookSecret:      secret,
		WebhookType:        defaultStr(in.WebhookType, "generic"),
		WebhookRefPath:     in.WebhookRefPath,
		WebhookCommitPath:  in.WebhookCommitPath,
		WebhookMessagePath: in.WebhookMessagePath,
		CronExpression:     strings.TrimSpace(in.CronExpression),
		CronTimezone:       defaultStr(in.CronTimezone, "UTC"),
		IsPublic:           in.IsPublic != nil && *in.IsPublic,
		CreatedBy:          createdBy,
	}
	if err := s.pipelines.Create(p); err != nil {
		return nil, err
	}
	s.syncCron(p)
	return publicPipeline(p, false), nil
}

func (s *BuildPipelineService) Update(id, userID uint, dataScope string, in UpdateBuildPipelineInput) (*model.BuildPipeline, error) {
	p, err := s.pipelines.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, userID, dataScope); err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errorsNew("名称不能为空")
		}
		p.Name = name
	}
	if in.Description != nil {
		p.Description = strings.TrimSpace(*in.Description)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.GraphJSON != nil {
		graphJSON := strings.TrimSpace(*in.GraphJSON)
		if graphJSON == "" {
			graphJSON = `{"nodes":[],"edges":[]}`
		}
		graphJSON, err := EncryptGraphEnvVars(graphJSON, p.GraphJSON)
		if err != nil {
			return nil, err
		}
		if err := s.validateGraphJSON(graphJSON); err != nil {
			return nil, err
		}
		p.GraphJSON = graphJSON
	}
	if in.TriggerManual != nil {
		p.TriggerManual = *in.TriggerManual
	}
	if in.TriggerWebhook != nil {
		p.TriggerWebhook = *in.TriggerWebhook
	}
	if in.TriggerCron != nil {
		p.TriggerCron = *in.TriggerCron
	}
	if in.CronExpression != nil {
		p.CronExpression = strings.TrimSpace(*in.CronExpression)
	}
	if in.CronTimezone != nil {
		p.CronTimezone = defaultStr(*in.CronTimezone, "UTC")
	}
	if in.WebhookType != nil {
		p.WebhookType = defaultStr(*in.WebhookType, "generic")
	}
	if in.WebhookRefPath != nil {
		p.WebhookRefPath = *in.WebhookRefPath
	}
	if in.WebhookCommitPath != nil {
		p.WebhookCommitPath = *in.WebhookCommitPath
	}
	if in.WebhookMessagePath != nil {
		p.WebhookMessagePath = *in.WebhookMessagePath
	}
	if in.IsPublic != nil {
		p.IsPublic = *in.IsPublic
	}
	if err := s.pipelines.Update(p); err != nil {
		return nil, err
	}
	s.syncCron(p)
	return publicPipeline(p, false), nil
}

func (s *BuildPipelineService) Delete(id, userID uint, dataScope string) error {
	p, err := s.pipelines.FindByID(id)
	if err != nil {
		return NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, userID, dataScope); err != nil {
		return err
	}
	if s.cron != nil {
		s.cron.Remove(id)
	}
	return s.pipelines.Delete(id)
}

func (s *BuildPipelineService) Get(id, userID uint, dataScope string) (*model.BuildPipeline, error) {
	p, err := s.pipelines.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineRead(p, userID, dataScope); err != nil {
		return nil, err
	}
	return publicPipeline(p, false), nil
}

func (s *BuildPipelineService) GetWithSecret(id, userID uint, dataScope string) (*model.BuildPipeline, error) {
	p, err := s.pipelines.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, userID, dataScope); err != nil {
		return nil, err
	}
	return publicPipeline(p, true), nil
}

func (s *BuildPipelineService) RotateWebhookSecret(id, userID uint, dataScope string) (*model.BuildPipeline, error) {
	p, err := s.pipelines.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, userID, dataScope); err != nil {
		return nil, err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, err
	}
	p.WebhookSecret = secret
	if err := s.pipelines.Update(p); err != nil {
		return nil, err
	}
	return publicPipeline(p, true), nil
}

func (s *BuildPipelineService) List(q pkg.ListQuery, keyword string, userID uint, dataScope string) ([]model.BuildPipeline, int64, error) {
	var createdBy *uint
	if dataScope != rbacmodel.DataScopeAll {
		createdBy = &userID
	}
	items, total, err := s.pipelines.List(q, keyword, createdBy)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.BuildPipeline, len(items))
	for i := range items {
		out[i] = *publicPipeline(&items[i], false)
	}
	return out, total, nil
}

func (s *BuildPipelineService) validateGraphJSON(graphJSON string) error {
	g, err := ParsePipelineGraph(graphJSON)
	if err != nil {
		return errorsNew(err.Error())
	}
	// Allow empty graph on create (editor seeds start/end); reject non-empty invalid DAGs.
	if len(g.Nodes) == 0 {
		if len(g.Edges) > 0 {
			return errorsNew("空节点图不能包含边")
		}
		return nil
	}
	return ValidatePipelineDAG(g, PipelineRefChecker{
		BuildJobExists: func(id uint) bool {
			_, err := s.jobs.FindByID(id)
			return err == nil
		},
		ScriptJobExists: func(id uint) bool {
			if s.scriptJobs == nil {
				return false
			}
			_, err := s.scriptJobs.FindByID(id)
			return err == nil
		},
		AgentExists: s.agentExists,
	})
}

func (s *BuildPipelineService) syncCron(p *model.BuildPipeline) {
	if s.cron == nil || p == nil {
		return
	}
	_ = s.cron.Add(*p)
}

func publicPipeline(p *model.BuildPipeline, revealSecret bool) *model.BuildPipeline {
	cp := *p
	if !revealSecret {
		cp.WebhookSecret = ""
	}
	cp.GraphJSON = SanitizeGraphEnvVars(cp.GraphJSON)
	return &cp
}

func defaultStr(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func requirePipelineRead(p *model.BuildPipeline, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || p.IsPublic || p.CreatedBy == userID {
		return nil
	}
	return NewForbidden("无权访问该流水线")
}

func requirePipelineWrite(p *model.BuildPipeline, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll || p.CreatedBy == userID {
		return nil
	}
	return NewForbidden("无权访问该流水线")
}
