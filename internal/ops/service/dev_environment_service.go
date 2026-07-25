package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	"bedrock/internal/pkg"
)

var (
	ErrBuiltinImmutable = errors.New("内置开发环境不可删除")
	ErrMissingScript    = errors.New("该开发环境未配置此操作脚本")
	ErrInvalidOperation = errors.New("不支持的开发环境操作")
	secretFlagName      = `(?:token|password|secret|api[_-]?(?:key|token)|access[_-]?token|auth(?:orization)?(?:[_-]?token)?|credential(?:s)?|client[_-]?secret|private[_-]?key)`
	secretAssignment    = regexp.MustCompile(`(?i)((?:^|[^a-z0-9])` + secretFlagName + `\s*=\s*)[^\s&'"]+`)
	secretSpacedFlag    = regexp.MustCompile(`(?i)((?:^|[^a-z0-9_-])(?:--` + secretFlagName + `|-t)\s+)(?:"[^"]*"|'[^']*'|[^\s&'"]+)`)
	secretBearer        = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s&'"]+`)
	secretURLUserInfo   = regexp.MustCompile(`(?i)(https?://[^/\s:@]+:)[^@/\s]+@`)
)

// AuditWriter is the small audit boundary used by asynchronous operations.
// Worker events do not have an HTTP request, so username and IP are empty.
type AuditWriter interface {
	Write(userID uint, username, action, resourceType, resourceID, details, ip string) error
}

type DevEnvironmentInput struct {
	Name            string `json:"name"`
	Executable      string `json:"executable"`
	Description     string `json:"description"`
	DetectScript    string `json:"detect_script"`
	InstallScript   string `json:"install_script"`
	UpgradeScript   string `json:"upgrade_script"`
	UninstallScript string `json:"uninstall_script"`
	VersionsScript  string `json:"versions_script"`
	SwitchScript    string `json:"switch_script"`
	DefaultVersion  string `json:"default_version"`
}

type SourceInput struct {
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type JobInput struct {
	Version string `json:"version"`
}

type DetectResult struct {
	Detected bool   `json:"detected"`
	Output   string `json:"output"`
}

type DevEnvironmentService struct {
	repo  *repository.OpsRepository
	audit AuditWriter

	jobs    chan uint
	stop    chan struct{}
	wg      sync.WaitGroup
	startMu sync.Mutex
	started bool
}

func NewDevEnvironmentService(repo *repository.OpsRepository, audit ...AuditWriter) *DevEnvironmentService {
	service := &DevEnvironmentService{
		repo: repo,
		jobs: make(chan uint, 128),
		stop: make(chan struct{}),
	}
	if len(audit) > 0 {
		service.audit = audit[0]
	}
	return service
}

func (s *DevEnvironmentService) Start() {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.started {
		return
	}
	s.started = true
	s.wg.Add(1)
	go s.worker()
}

func (s *DevEnvironmentService) Shutdown() {
	s.startMu.Lock()
	if !s.started {
		s.startMu.Unlock()
		return
	}
	s.started = false
	close(s.stop)
	s.startMu.Unlock()
	s.wg.Wait()
}

// RecoverOnStartup implements DESIGN D18: active work is marked interrupted,
// while never-started queued jobs are submitted again.
func (s *DevEnvironmentService) RecoverOnStartup() error {
	if _, err := s.repo.MarkRunningInterrupted(); err != nil {
		return err
	}
	queued, err := s.repo.ListJobsByStatuses(model.JobQueued)
	if err != nil {
		return err
	}
	for _, job := range queued {
		if err := s.submit(job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *DevEnvironmentService) ListEnvironments() ([]model.DevEnvironment, error) {
	return s.repo.ListEnvironments()
}

func (s *DevEnvironmentService) CreateCustom(input DevEnvironmentInput, createdBy uint) (*model.DevEnvironment, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Executable) == "" {
		return nil, errors.New("名称和可执行文件不能为空")
	}
	item := &model.DevEnvironment{
		Name: input.Name, Kind: model.DevEnvCustom, Executable: input.Executable,
		Description: input.Description, DetectScript: input.DetectScript,
		InstallScript: input.InstallScript, UpgradeScript: input.UpgradeScript,
		UninstallScript: input.UninstallScript, VersionsScript: input.VersionsScript,
		SwitchScript: input.SwitchScript, DefaultVersion: input.DefaultVersion, CreatedBy: createdBy,
	}
	if err := s.repo.CreateEnvironment(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *DevEnvironmentService) UpdateCustom(id uint, input DevEnvironmentInput) (*model.DevEnvironment, error) {
	item, err := s.repo.FindEnvironment(id)
	if err != nil {
		return nil, err
	}
	if item.Kind != model.DevEnvCustom {
		return nil, ErrBuiltinImmutable
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Executable) == "" {
		return nil, errors.New("名称和可执行文件不能为空")
	}
	item.Name, item.Executable, item.Description = input.Name, input.Executable, input.Description
	item.DetectScript, item.InstallScript, item.UpgradeScript = input.DetectScript, input.InstallScript, input.UpgradeScript
	item.UninstallScript, item.VersionsScript, item.SwitchScript = input.UninstallScript, input.VersionsScript, input.SwitchScript
	item.DefaultVersion = input.DefaultVersion
	if err := s.repo.UpdateEnvironment(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *DevEnvironmentService) DeleteCustom(id uint) error {
	item, err := s.repo.FindEnvironment(id)
	if err != nil {
		return err
	}
	if item.Kind != model.DevEnvCustom {
		return ErrBuiltinImmutable
	}
	return s.repo.DeleteEnvironment(id)
}

func (s *DevEnvironmentService) Detect(id uint) (*DetectResult, error) {
	item, err := s.repo.FindEnvironment(id)
	if err != nil {
		return nil, err
	}
	command := item.DetectScript
	if command == "" {
		command = item.Executable + " --version"
	}
	output, err := executeCommand(context.Background(), command)
	return &DetectResult{Detected: err == nil, Output: redact(output)}, nil
}

func (s *DevEnvironmentService) Enqueue(id uint, operation string, input JobInput, createdBy uint) (*model.DevEnvJob, error) {
	item, err := s.repo.FindEnvironment(id)
	if err != nil {
		return nil, err
	}
	if _, err := commandScript(item, operation); err != nil {
		return nil, err
	}
	job := &model.DevEnvJob{
		EnvironmentID: id, Operation: operation, RequestedVersion: strings.TrimSpace(input.Version),
		Status: model.JobQueued, CreatedBy: createdBy,
	}
	if err := s.repo.CreateJob(job); err != nil {
		return nil, err
	}
	s.auditJob(job, "dev_env_job_enqueued", "", false)
	if err := s.submit(job.ID); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *DevEnvironmentService) Retry(environmentID, jobID uint, createdBy uint) (*model.DevEnvJob, error) {
	old, err := s.repo.FindJobInEnvironment(environmentID, jobID)
	if err != nil {
		return nil, err
	}
	if old.Status == model.JobQueued || old.Status == model.JobRunning {
		return nil, errors.New("任务仍在执行")
	}
	return s.Enqueue(old.EnvironmentID, old.Operation, JobInput{Version: old.RequestedVersion}, createdBy)
}

func (s *DevEnvironmentService) ListSources(environmentID uint) ([]model.DevEnvInstallSource, error) {
	if _, err := s.repo.FindEnvironment(environmentID); err != nil {
		return nil, err
	}
	return s.repo.ListSources(environmentID)
}

func (s *DevEnvironmentService) CreateSource(environmentID uint, input SourceInput) (*model.DevEnvInstallSource, error) {
	if _, err := s.repo.FindEnvironment(environmentID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" {
		return nil, errors.New("名称和地址不能为空")
	}
	item := &model.DevEnvInstallSource{
		EnvironmentID: environmentID, Name: input.Name, BaseURL: input.BaseURL,
		Priority: input.Priority, Enabled: input.Enabled,
	}
	if err := s.repo.CreateSource(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *DevEnvironmentService) UpdateSource(environmentID, sourceID uint, input SourceInput) (*model.DevEnvInstallSource, error) {
	item, err := s.repo.FindSourceInEnvironment(environmentID, sourceID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" {
		return nil, errors.New("名称和地址不能为空")
	}
	item.Name, item.BaseURL, item.Priority, item.Enabled = input.Name, input.BaseURL, input.Priority, input.Enabled
	if err := s.repo.UpdateSource(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *DevEnvironmentService) DeleteSource(environmentID, sourceID uint) error {
	if _, err := s.repo.FindSourceInEnvironment(environmentID, sourceID); err != nil {
		return err
	}
	return s.repo.DeleteSource(sourceID)
}

func (s *DevEnvironmentService) PingSource(environmentID, sourceID uint) (bool, string, error) {
	source, err := s.repo.FindSourceInEnvironment(environmentID, sourceID)
	if err != nil {
		return false, "", err
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodHead, source.BaseURL, nil)
	if err != nil {
		return false, "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false, err.Error(), nil
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 400, response.Status, nil
}

func (s *DevEnvironmentService) ListJobs(environmentID uint, q pkg.ListQuery, status string) ([]model.DevEnvJob, int64, error) {
	if _, err := s.repo.FindEnvironment(environmentID); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repo.ListJobs(environmentID, q, status)
	for i := range items {
		sanitizeJob(&items[i])
	}
	return items, total, err
}

func (s *DevEnvironmentService) GetJob(environmentID, jobID uint) (*model.DevEnvJob, error) {
	job, err := s.repo.FindJobInEnvironment(environmentID, jobID)
	if err != nil {
		return nil, err
	}
	sanitizeJob(job)
	return job, nil
}

func (s *DevEnvironmentService) JobLogs(environmentID, jobID uint) (string, error) {
	job, err := s.repo.FindJobInEnvironment(environmentID, jobID)
	if err != nil {
		return "", err
	}
	return redact(job.LogText), nil
}

func (s *DevEnvironmentService) submit(id uint) error {
	s.startMu.Lock()
	started := s.started
	s.startMu.Unlock()
	if !started {
		return errors.New("开发环境任务调度器未启动")
	}
	select {
	case s.jobs <- id:
		return nil
	default:
		s.jobs <- id
		return nil
	}
}

func (s *DevEnvironmentService) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case id := <-s.jobs:
			s.ExecuteJob(context.Background(), id)
		}
	}
}

// ExecuteJob is exported for deterministic integration tests and worker use.
func (s *DevEnvironmentService) ExecuteJob(ctx context.Context, id uint) {
	job, err := s.repo.FindJob(id)
	if err != nil || job.Status != model.JobQueued {
		return
	}
	now := time.Now().UTC()
	job.Status, job.StartedAt = model.JobRunning, &now
	job.LogText += fmt.Sprintf("%s job started: %s\n", now.Format(time.RFC3339), job.Operation)
	if err := s.repo.UpdateJob(job); err != nil {
		return
	}
	s.auditJob(job, "dev_env_job_started", "", false)

	env, err := s.repo.FindEnvironment(job.EnvironmentID)
	if err != nil {
		s.fail(job, err, "", false)
		return
	}
	script, err := commandScript(env, job.Operation)
	if err != nil {
		s.fail(job, err, "", false)
		return
	}

	if needsSource(job.Operation, script) {
		sources, err := s.repo.ListEnabledSources(job.EnvironmentID)
		if err != nil {
			s.fail(job, err, "", false)
			return
		}
		if len(sources) == 0 {
			s.fail(job, errors.New("没有可用安装源"), "", false)
			return
		}
		for index, source := range sources {
			command := renderCommand(script, *env, job.RequestedVersion, source.BaseURL)
			job.CommandSnapshot = redactSourceURL(command, source.BaseURL)
			job.LogText += fmt.Sprintf("trying source %q (priority %d)\n", source.Name, source.Priority)
			output, runErr := executeCommand(ctx, command)
			job.LogText += redactSourceURL(output, source.BaseURL)
			if runErr == nil {
				sourceID := source.ID
				job.SourceID = &sourceID
				job.LogText += fmt.Sprintf("source %q succeeded\n", source.Name)
				s.succeed(job, env, source.Name, index > 0)
				return
			}
			job.LogText += fmt.Sprintf("source %q failed: %s\n", source.Name, redact(runErr.Error()))
		}
		s.fail(job, errors.New("所有安装源均失败"), "", len(sources) > 1)
		return
	}

	command := renderCommand(script, *env, job.RequestedVersion, "")
	job.CommandSnapshot = redact(command)
	output, runErr := executeCommand(ctx, command)
	job.LogText += redact(output)
	if runErr != nil {
		s.fail(job, runErr, "", false)
		return
	}
	s.succeed(job, env, "", false)
}

func (s *DevEnvironmentService) succeed(
	job *model.DevEnvJob,
	env *model.DevEnvironment,
	sourceName string,
	sourceFallback bool,
) {
	now := time.Now().UTC()
	job.Status, job.FinishedAt, job.ErrorMessage = model.JobSuccess, &now, ""
	job.LogText += fmt.Sprintf("%s job succeeded\n", now.Format(time.RFC3339))
	job.CommandSnapshot, job.LogText = redact(job.CommandSnapshot), redact(job.LogText)
	if err := s.repo.UpdateJob(job); err != nil {
		return
	}
	if job.Operation == "switch" && job.RequestedVersion != "" {
		env.DefaultVersion = job.RequestedVersion
		_ = s.repo.UpdateEnvironment(env)
	}
	s.auditJob(job, "dev_env_job_completed", sourceName, sourceFallback)
}

func (s *DevEnvironmentService) fail(
	job *model.DevEnvJob,
	err error,
	sourceName string,
	sourceFallback bool,
) {
	now := time.Now().UTC()
	job.Status, job.FinishedAt, job.ErrorMessage = model.JobFailed, &now, redact(err.Error())
	job.LogText += fmt.Sprintf("%s job failed: %s\n", now.Format(time.RFC3339), job.ErrorMessage)
	job.CommandSnapshot, job.LogText = redact(job.CommandSnapshot), redact(job.LogText)
	if err := s.repo.UpdateJob(job); err != nil {
		return
	}
	s.auditJob(job, "dev_env_job_completed", sourceName, sourceFallback)
}

func commandScript(item *model.DevEnvironment, operation string) (string, error) {
	var command string
	switch operation {
	case "install":
		command = item.InstallScript
	case "upgrade":
		command = item.UpgradeScript
	case "uninstall":
		command = item.UninstallScript
	case "switch":
		command = item.SwitchScript
	default:
		return "", ErrInvalidOperation
	}
	if strings.TrimSpace(command) == "" {
		return "", ErrMissingScript
	}
	return command, nil
}

func needsSource(operation, script string) bool {
	return (operation == "install" || operation == "upgrade") && strings.Contains(script, "{{source_url}}")
}

func renderCommand(script string, env model.DevEnvironment, version, sourceURL string) string {
	replacer := strings.NewReplacer(
		"{{name}}", env.Name,
		"{{executable}}", env.Executable,
		"{{version}}", version,
		"{{source_url}}", sourceURL,
	)
	return replacer.Replace(script)
}

func executeCommand(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.String(), err
}

func redact(value string) string {
	value = secretAssignment.ReplaceAllString(value, "$1[REDACTED]")
	value = secretSpacedFlag.ReplaceAllString(value, "$1[REDACTED]")
	value = secretBearer.ReplaceAllString(value, "$1[REDACTED]")
	return secretURLUserInfo.ReplaceAllString(value, "$1[REDACTED]@")
}

func sanitizeJob(job *model.DevEnvJob) {
	job.CommandSnapshot = redact(job.CommandSnapshot)
	job.LogText = redact(job.LogText)
	job.ErrorMessage = redact(job.ErrorMessage)
	job.RequestedVersion = redact(job.RequestedVersion)
	if job.Source != nil {
		job.Source.BaseURL = "[REDACTED]"
	}
	job.Environment.DetectScript = redact(job.Environment.DetectScript)
	job.Environment.InstallScript = redact(job.Environment.InstallScript)
	job.Environment.UpgradeScript = redact(job.Environment.UpgradeScript)
	job.Environment.UninstallScript = redact(job.Environment.UninstallScript)
	job.Environment.VersionsScript = redact(job.Environment.VersionsScript)
	job.Environment.SwitchScript = redact(job.Environment.SwitchScript)
}

func redactSourceURL(value, sourceURL string) string {
	if sourceURL != "" {
		value = strings.ReplaceAll(value, sourceURL, "[SOURCE_URL_REDACTED]")
	}
	return redact(value)
}

func (s *DevEnvironmentService) auditJob(
	job *model.DevEnvJob,
	action, sourceName string,
	sourceFallback bool,
) {
	if s.audit == nil {
		return
	}
	details := redact(fmt.Sprintf(
		"operation=%s status=%s source=%s source_fallback=%t",
		job.Operation,
		job.Status,
		sourceName,
		sourceFallback,
	))
	_ = s.audit.Write(
		job.CreatedBy,
		"",
		action,
		"dev_env_job",
		strconv.FormatUint(uint64(job.ID), 10),
		details,
		"",
	)
}
