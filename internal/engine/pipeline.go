package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	resourcemodel "bedrock/internal/resource/model"
	"bedrock/internal/ws"
)

// AgentEventHook receives build lifecycle events for async AgentRun creation.
// Implementations must not mutate BuildRun.status.
type AgentEventHook interface {
	OnBuildEvent(event string, job *model.BuildJob, run *model.BuildRun)
}

// TerminalNotifier persists + pushes per-user inbox notifications on BuildRun terminal.
type TerminalNotifier interface {
	NotifyBuildRun(userID uint, buildRunID uint, buildNumber int, status, message string)
}

// Pipeline executes BuildRun: clone → build → archive → success → distribute.
// Distribution failure never sets status=failed (DESIGN §5.2).
// Sync AI Agent stage is intentionally absent (P4 async AgentRun only).
type Pipeline struct {
	runs      RunStore
	jobs      JobStore
	repos     RepoStore
	servers   ServerStore
	secrets   SecretResolver
	hub       *ws.Hub
	logger    *zap.Logger
	workspace string
	artifact  string
	logDir    string
	cacheDir  string
	agentHook AgentEventHook
	notifier  TerminalNotifier
}

// SetAgentEventHook wires P4 async AgentRun creation from build events.
func (p *Pipeline) SetAgentEventHook(h AgentEventHook) {
	p.agentHook = h
}

// SetTerminalNotifier wires DESIGN §12 in-app notifications for build terminal states.
func (p *Pipeline) SetTerminalNotifier(n TerminalNotifier) {
	p.notifier = n
}

func NewPipeline(
	runs RunStore,
	jobs JobStore,
	repos RepoStore,
	servers ServerStore,
	secrets SecretResolver,
	hub *ws.Hub,
	logger *zap.Logger,
	workspaceDir, artifactDir, logDir, cacheDir string,
) *Pipeline {
	return &Pipeline{
		runs:      runs,
		jobs:      jobs,
		repos:     repos,
		servers:   servers,
		secrets:   secrets,
		hub:       hub,
		logger:    logger,
		workspace: workspaceDir,
		artifact:  artifactDir,
		logDir:    logDir,
		cacheDir:  cacheDir,
	}
}

func (p *Pipeline) Execute(ctx context.Context, runID uint) {
	defer func() {
		if r := recover(); r != nil {
			if p.logger != nil {
				p.logger.Error("pipeline panic recovered", zap.Uint("run_id", runID), zap.Any("panic", r))
			}
			run, err := p.runs.FindByID(runID)
			if err == nil && run.Status == "success" {
				return
			}
			p.failRun(&model.BuildRun{ID: runID}, fmt.Sprintf("internal panic: %v", r))
		}
	}()

	run, err := p.runs.FindByID(runID)
	if err != nil {
		if p.logger != nil {
			p.logger.Error("build run not found", zap.Uint("id", runID))
		}
		return
	}
	if run.Status == "cancelled" || run.Status == "interrupted" {
		return
	}
	job, err := p.jobs.FindByID(run.BuildJobID)
	if err != nil {
		p.failRun(run, "build job not found")
		return
	}
	repo, err := p.repos.FindByID(job.RepositoryID)
	if err != nil {
		p.failRun(run, "repository not found")
		return
	}
	decodeJobEnvNames(job)

	now := time.Now()
	redeployOnly := run.TriggerType == "redeploy"
	if !redeployOnly || run.Status != "success" {
		run.StartedAt = &now
	}

	if redeployOnly {
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
			"status":               "success",
			"stage":                "distributing",
			"distribution_summary": "running",
		})
		run.Status = "success"
		run.Stage = "distributing"
		p.broadcastRunRefresh(run.ID)
	} else {
		p.setRunning(run, "cloning")
	}

	logDir := filepath.Join(p.logDir, fmt.Sprintf("job-%d", job.ID))
	_ = os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, fmt.Sprintf("run-%03d.log", run.BuildNumber))
	var logFile *os.File
	if redeployOnly && run.LogPath != "" {
		logPath = run.LogPath
		logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		logFile, err = os.Create(logPath)
	}
	if err != nil {
		p.failRun(run, "无法创建日志文件: "+err.Error())
		return
	}
	defer logFile.Close()
	run.LogPath = logPath
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"log_path":   logPath,
		"started_at": run.StartedAt,
		"stage":      run.Stage,
		"status":     run.Status,
	})
	p.broadcastRunRefresh(run.ID)

	channel := fmt.Sprintf("build-run:%d", run.ID)
	var logMu sync.Mutex
	writeLine := func(line string) {
		logMu.Lock()
		defer logMu.Unlock()
		_, _ = logFile.WriteString(line + "\n")
		if p.hub != nil {
			p.hub.BroadcastToChannel(channel, []byte(line))
		}
	}

	if redeployOnly {
		p.executeRedeployOnly(ctx, run, job, writeLine)
		return
	}

	writeLine("=== Stage: Cloning ===")
	writeLine("NOTE: Build scripts run as the same OS user as Bedrock (no sandbox isolation).")
	workDir := filepath.Join(p.workspace, fmt.Sprintf("repo-%d", repo.ID), fmt.Sprintf("job-%d", job.ID))

	authType, username, password, err := p.resolveRepoGitAuth(repo)
	if err != nil {
		p.failRun(run, "仓库凭证错误: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}

	branch := job.Branch
	if run.Branch != "" {
		branch = run.Branch
	}

	err = GitCloneOrPull(ctx, workDir, repo.RepoURL, authType, username, password, branch, writeLine)
	if err != nil {
		if ctx.Err() != nil {
			p.cancelRun(run)
			return
		}
		p.failRun(run, "Git操作失败: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}

	if run.CommitHash != "" {
		writeLine("Checking out commit: " + run.CommitHash)
		if err := runGit(ctx, workDir, writeLine, "checkout", run.CommitHash); err != nil {
			if ctx.Err() != nil {
				p.cancelRun(run)
				return
			}
			p.failRun(run, "Checkout commit 失败: "+err.Error())
			writeLine("ERROR: " + err.Error())
			return
		}
	} else {
		// Capture HEAD for snapshot enrichment
		if out, err := runGitOutput(ctx, workDir, "rev-parse", "HEAD"); err == nil {
			hash := strings.TrimSpace(out)
			run.CommitHash = hash
			_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"commit_hash": hash})
			p.broadcastRunRefresh(run.ID)
		}
	}

	cachePaths := parseCachePaths(job.CachePaths)
	if len(cachePaths) > 0 && p.cacheDir != "" {
		writeLine("=== Stage: Restoring Cache ===")
		jobCacheDir := filepath.Join(p.cacheDir, fmt.Sprintf("job-%d", job.ID))
		restored := 0
		for _, cp := range cachePaths {
			src := filepath.Join(jobCacheDir, cp)
			dst := filepath.Join(workDir, cp)
			if _, err := os.Stat(src); err == nil {
				_ = os.MkdirAll(filepath.Dir(dst), 0755)
				if err := copyDir(src, dst); err != nil {
					writeLine(fmt.Sprintf("WARNING: 恢复缓存 %s 失败: %s", cp, err.Error()))
				} else {
					restored++
					writeLine(fmt.Sprintf("Restored cache: %s", cp))
				}
			}
		}
		if restored == 0 {
			writeLine("No cache found (first build or cache cleared)")
		}
	}

	if ctx.Err() != nil {
		p.cancelRun(run)
		return
	}
	p.setRunning(run, "building")
	writeLine("=== Stage: Building ===")

	buildDir := workDir
	if strings.TrimSpace(job.WorkDir) != "" {
		buildDir = filepath.Join(workDir, job.WorkDir)
	}
	envVars := os.Environ()
	for _, name := range job.EnvVarNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if v, ok := os.LookupEnv(name); ok {
			envVars = append(envVars, name+"="+v)
		}
	}

	cmd, cleanupScript, err := newBuildScriptCommand(ctx, buildDir, job.BuildScriptType, job.BuildScript)
	if err != nil {
		p.failRun(run, "构建脚本配置无效: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}
	defer cleanupScript()
	cmd.Dir = buildDir
	cmd.Env = envVars
	configureBuildCmdProc(cmd)
	cmd.Cancel = func() error { return killBuildCmdProcess(cmd) }

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		p.failRun(run, "启动构建脚本失败: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}
	defer func() { _ = killBuildCmdProcess(cmd) }()

	var scanWg sync.WaitGroup
	scanWg.Go(func() {
		scanLines(stdout, writeLine)
	})
	scanLines(stderr, writeLine)
	scanWg.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			p.cancelRun(run)
			return
		}
		p.failRun(run, "构建失败: "+err.Error())
		writeLine("ERROR: Build failed with " + err.Error())
		return
	}
	writeLine("=== Build completed successfully ===")

	if len(cachePaths) > 0 && p.cacheDir != "" {
		writeLine("=== Stage: Saving Cache ===")
		jobCacheDir := filepath.Join(p.cacheDir, fmt.Sprintf("job-%d", job.ID))
		for _, cp := range cachePaths {
			src := filepath.Join(workDir, cp)
			dst := filepath.Join(jobCacheDir, cp)
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				_ = os.MkdirAll(filepath.Dir(dst), 0755)
				_ = os.RemoveAll(dst)
				if err := copyDir(src, dst); err != nil {
					writeLine(fmt.Sprintf("WARNING: 保存缓存 %s 失败: %s", cp, err.Error()))
				} else {
					writeLine(fmt.Sprintf("Saved cache: %s", cp))
				}
			}
		}
	}

	p.setRunning(run, "archiving")
	sourceDir := workDir
	if strings.TrimSpace(job.OutputDir) != "" {
		sourceDir = filepath.Join(workDir, job.OutputDir)
		writeLine("=== Stage: Collecting Artifact ===")
		artifactDir := filepath.Join(p.artifact, fmt.Sprintf("job-%d", job.ID))
		_ = os.MkdirAll(artifactDir, 0755)
		artifactFormat := NormalizeArtifactFormat(job.ArtifactFormat)
		artifactPath := filepath.Join(artifactDir, artifactArchiveName(run.BuildNumber, artifactFormat))
		if err := CreateArtifactArchive(artifactPath, sourceDir, artifactFormat); err != nil {
			writeLine("WARNING: 打包构建产物失败: " + err.Error())
		} else {
			run.ArtifactPath = artifactPath
			_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"artifact_path": artifactPath})
			p.broadcastRunRefresh(run.ID)
			writeLine("Artifact saved: " + artifactPath)
		}
		p.cleanupArtifacts(job)
	} else {
		writeLine("=== Stage: Archiving (no output_dir; skip archive file) ===")
	}

	targets, _ := p.jobs.ListDeployTargets(job.ID)
	hasDist := len(targets) > 0
	p.markArtifactSuccess(run, writeLine, hasDist)
	if ctx.Err() != nil {
		p.cancelRun(run)
		return
	}
	// Agent sync stage intentionally omitted — P4 creates AgentRun asynchronously.
	if hasDist {
		p.setStageKeepSuccess(run, "distributing")
		p.runDistributions(ctx, run, job, sourceDir, writeLine, nil)
	} else {
		p.setStageKeepSuccess(run, "idle")
	}
}

func (p *Pipeline) resolveRepoGitAuth(repo *resourcemodel.Repository) (authType, username, password string, err error) {
	switch strings.ToLower(strings.TrimSpace(repo.AuthType)) {
	case "", "none":
		return "none", "", "", nil
	case "credential":
		if repo.CredentialID == nil || *repo.CredentialID == 0 {
			return "", "", "", fmt.Errorf("repository credential is empty")
		}
		typ, user, secret, _, err := p.secrets.Resolve(*repo.CredentialID)
		if err != nil {
			return "", "", "", err
		}
		authType = "password"
		if strings.EqualFold(typ, "token") || strings.EqualFold(typ, "api_key") {
			authType = "token"
		}
		return authType, user, secret, nil
	default:
		return "none", "", "", nil
	}
}

func (p *Pipeline) broadcastRunRefresh(runID uint) {
	if p.hub == nil {
		return
	}
	p.hub.BroadcastToChannel(fmt.Sprintf("build-run:%d", runID), []byte("__REFRESH__"))
}

func (p *Pipeline) setRunning(run *model.BuildRun, stage string) {
	run.Status = "running"
	run.Stage = stage
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"status": "running",
		"stage":  stage,
	})
	p.broadcastRunRefresh(run.ID)
}

func (p *Pipeline) setStageKeepSuccess(run *model.BuildRun, stage string) {
	run.Stage = stage
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"status": "success",
		"stage":  stage,
	})
	p.broadcastRunRefresh(run.ID)
}

func (p *Pipeline) failRun(run *model.BuildRun, errMsg string) {
	latest, err := p.runs.FindByID(run.ID)
	if err == nil && latest.Status == "success" {
		return
	}
	finished := time.Now()
	fields := map[string]interface{}{
		"status":        "failed",
		"error_message": errMsg,
		"finished_at":   finished,
		"stage":         run.Stage,
	}
	if run.StartedAt != nil {
		fields["duration_ms"] = finished.Sub(*run.StartedAt).Milliseconds()
	}
	_ = p.runs.UpdateFields(run.ID, fields)
	p.broadcastRunRefresh(run.ID)
	p.notifyTerminal(run, "failed", errMsg)
}

func (p *Pipeline) cancelRun(run *model.BuildRun) {
	latest, err := p.runs.FindByID(run.ID)
	if err == nil && latest.Status == "success" {
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
			"stage":                "idle",
			"distribution_summary": "cancelled",
		})
		p.broadcastRunRefresh(run.ID)
		return
	}
	finished := time.Now()
	fields := map[string]interface{}{
		"status":      "cancelled",
		"finished_at": finished,
		"stage":       run.Stage,
	}
	if run.StartedAt != nil {
		fields["duration_ms"] = finished.Sub(*run.StartedAt).Milliseconds()
	}
	_ = p.runs.UpdateFields(run.ID, fields)
	p.broadcastRunRefresh(run.ID)
	p.notifyTerminal(run, "cancelled", "")
}

func (p *Pipeline) markArtifactSuccess(run *model.BuildRun, writeLine func(string), hasDist bool) {
	finished := time.Now()
	run.FinishedAt = &finished
	if run.StartedAt != nil {
		run.DurationMs = finished.Sub(*run.StartedAt).Milliseconds()
	}
	run.Status = "success"
	run.ErrorMessage = ""
	summary := "none"
	stage := "idle"
	if hasDist {
		summary = "running"
		stage = "distributing"
	}
	run.Stage = stage
	run.DistributionSummary = summary
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"finished_at":          run.FinishedAt,
		"duration_ms":          run.DurationMs,
		"stage":                stage,
		"status":               "success",
		"error_message":        "",
		"distribution_summary": summary,
	})
	p.broadcastRunRefresh(run.ID)
	writeLine(fmt.Sprintf("=== Build phase succeeded in %dms (artifact ready) ===", run.DurationMs))
	p.notifyTerminal(run, "success", "")
	if p.agentHook != nil && run.ArtifactPath != "" {
		// Default event: artifact_ready (archive succeeded with a usable artifact path).
		job, err := p.jobs.FindByID(run.BuildJobID)
		if err == nil {
			p.agentHook.OnBuildEvent("artifact_ready", job, run)
		}
	}
}

func (p *Pipeline) notifyTerminal(run *model.BuildRun, status, message string) {
	if run == nil || run.TriggeredBy == 0 {
		return
	}
	if p.notifier != nil {
		p.notifier.NotifyBuildRun(run.TriggeredBy, run.ID, run.BuildNumber, status, message)
		return
	}
	// Fallback for tests without a notifier: push raw JSON on notifications channel only.
	if p.hub == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"type":         "build_run_" + status,
		"build_run_id": run.ID,
		"build_job_id": run.BuildJobID,
		"build_number": run.BuildNumber,
		"status":       status,
		"message":      message,
	})
	p.hub.BroadcastToChannel(fmt.Sprintf("notifications:%d", run.TriggeredBy), payload)
}

func (p *Pipeline) cleanupArtifacts(job *model.BuildJob) {
	builds, _ := p.runs.ListArtifactsByJob(job.ID)
	maxArtifacts := job.MaxArtifacts
	if maxArtifacts <= 0 {
		maxArtifacts = 5
	}
	if len(builds) <= maxArtifacts {
		return
	}
	for _, b := range builds[maxArtifacts:] {
		if b.ArtifactPath != "" {
			_ = os.Remove(b.ArtifactPath)
			_ = p.runs.UpdateFields(b.ID, map[string]interface{}{"artifact_path": ""})
		}
	}
}

func scanLines(r io.Reader, fn func(string)) {
	scanner := bufio.NewScanner(r)
	// Allow long build log lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		fn(scanner.Text())
	}
}

func decodeJobEnvNames(job *model.BuildJob) {
	if len(job.EnvVarNames) > 0 {
		return
	}
	if strings.TrimSpace(job.EnvVarNamesJSON) == "" {
		job.EnvVarNames = []string{}
		return
	}
	_ = json.Unmarshal([]byte(job.EnvVarNamesJSON), &job.EnvVarNames)
	if job.EnvVarNames == nil {
		job.EnvVarNames = []string{}
	}
}
