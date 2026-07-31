package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg/scripttmpl"
	"bedrock/internal/ws"
)

// ScriptJobStore loads ScriptJob for execution.
type ScriptJobStore interface {
	FindByID(id uint) (*model.ScriptJob, error)
	ListCronEnabled() ([]model.ScriptJob, error)
}

// ScriptRunStore is the ScriptRun persistence surface used by ScriptPipeline/ScriptScheduler.
type ScriptRunStore interface {
	FindByID(id uint) (*model.ScriptRun, error)
	UpdateFields(id uint, fields map[string]interface{}) error
	ListByStatuses(statuses ...string) ([]model.ScriptRun, error)
	MarkRunningInterrupted() (int64, error)
	HasNonTerminal(jobID uint) (bool, error)
}

// ScriptRunEnqueuer creates queued ScriptRuns (used by Cron/Webhook).
type ScriptRunEnqueuer interface {
	EnqueueInternal(jobID, triggeredBy uint, triggerType string) (*model.ScriptRun, error)
}

// ScriptRunTerminalHook is invoked for every ScriptRun terminal status
// (success|failed|cancelled|interrupted), mirroring BuildRunTerminalHook.
// Used by PipelineOrchestrator to advance DAG stages.
type ScriptRunTerminalHook interface {
	OnScriptRunTerminal(run *model.ScriptRun, status string)
}

// ScriptPipeline executes ScriptRuns: workspace → template expand → run script → terminal.
type ScriptPipeline struct {
	runs         ScriptRunStore
	jobs         ScriptJobStore
	hub          *ws.Hub
	logger       *zap.Logger
	workspaceDir string
	logDir       string
	termHook     ScriptRunTerminalHook
}

func NewScriptPipeline(
	runs ScriptRunStore,
	jobs ScriptJobStore,
	hub *ws.Hub,
	logger *zap.Logger,
	workspaceDir, logDir string,
) *ScriptPipeline {
	return &ScriptPipeline{
		runs:         runs,
		jobs:         jobs,
		hub:          hub,
		logger:       logger,
		workspaceDir: workspaceDir,
		logDir:       logDir,
	}
}

// SetTerminalHook wires PipelineOrchestrator (or tests) on ScriptRun terminal.
func (p *ScriptPipeline) SetTerminalHook(h ScriptRunTerminalHook) {
	p.termHook = h
}

// Execute runs a ScriptRun to completion (or cancellation).
func (p *ScriptPipeline) Execute(ctx context.Context, runID uint) {
	run, err := p.runs.FindByID(runID)
	if err != nil || run == nil {
		return
	}
	if run.Status != "queued" && run.Status != "running" {
		return
	}

	job, err := p.jobs.FindByID(run.ScriptJobID)
	if err != nil || job == nil {
		p.failRun(run, "脚本任务不存在")
		return
	}
	decodeScriptEnvNames(job)

	now := time.Now()
	run.StartedAt = &now
	run.Status = "running"
	run.Stage = "running"
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"status":     "running",
		"stage":      "running",
		"started_at": now,
	})
	p.broadcastRefresh(run.ID)

	wsRoot := ScriptWorkspace(p.workspaceDir, job.ID)
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		p.failRun(run, "创建工作区失败: "+err.Error())
		return
	}
	absWS, err := filepath.Abs(wsRoot)
	if err != nil {
		absWS = wsRoot
	}

	workDir := absWS
	if rel := strings.TrimSpace(job.WorkDir); rel != "" {
		workDir = filepath.Join(absWS, filepath.Clean(rel))
		if !isSubpath(absWS, workDir) {
			p.failRun(run, "工作目录越界")
			return
		}
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			p.failRun(run, "创建工作目录失败: "+err.Error())
			return
		}
	}
	if err := validateBuildWorkDir(workDir); err != nil {
		p.failRun(run, err.Error())
		return
	}

	logDir := filepath.Join(p.logDir, fmt.Sprintf("script-%d", job.ID))
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		p.failRun(run, "创建日志目录失败: "+err.Error())
		return
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("run-%03d.log", run.RunNumber))
	run.LogPath = logPath
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"log_path": logPath})

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		p.failRun(run, "打开日志失败: "+err.Error())
		return
	}
	defer logFile.Close()

	channel := fmt.Sprintf("script-run:%d", run.ID)
	writeLine := func(line string) {
		_, _ = logFile.WriteString(line + "\n")
		if p.hub != nil {
			p.hub.BroadcastToChannel(channel, []byte(line))
		}
	}

	writeLine("=== Script run started ===")
	writeLine("Workspace: " + absWS)
	writeLine("Work dir: " + workDir)

	kvEnv, err := decryptJobEnvVarsCipher(job.EnvVarsCipher)
	if err != nil {
		p.failRun(run, "解密环境变量失败: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}
	if err := applyRunEnvOverrides(kvEnv, run.EnvOverridesCipher); err != nil {
		p.failRun(run, "解密运行变量覆盖失败: "+err.Error())
		writeLine("ERROR: " + err.Error())
		return
	}
	envVars := mergeBuildEnv(job.EnvVarNames, kvEnv)
	tmplVars := buildScriptJobTemplateVars(job, run, absWS, kvEnv)

	if err := p.runScript(ctx, run, workDir, job.ScriptType, job.Script, envVars, tmplVars, writeLine); err != nil {
		return
	}

	finished := time.Now()
	dur := int64(0)
	if run.StartedAt != nil {
		dur = finished.Sub(*run.StartedAt).Milliseconds()
	}
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"status":        "success",
		"stage":         "idle",
		"finished_at":   finished,
		"duration_ms":   dur,
		"error_message": "",
	})
	p.broadcastRefresh(run.ID)
	writeLine(fmt.Sprintf("=== Script succeeded in %dms ===", dur))
	p.notifyTerminal(run, "success")
}

func (p *ScriptPipeline) runScript(
	ctx context.Context,
	run *model.ScriptRun,
	workDir, scriptType, script string,
	envVars []string,
	tmplVars map[string]string,
	writeLine func(string),
) error {
	expanded, err := scripttmpl.Expand(script, tmplVars)
	if err != nil {
		msg := "脚本模板替换失败: " + err.Error()
		p.failRun(run, msg)
		writeLine("ERROR: " + err.Error())
		return err
	}

	cmd, cleanup, err := newBuildScriptCommand(ctx, workDir, scriptType, expanded)
	if err != nil {
		msg := "脚本配置无效: " + err.Error()
		p.failRun(run, msg)
		writeLine("ERROR: " + err.Error())
		return err
	}
	defer cleanup()
	cmd.Dir = workDir
	cmd.Env = envVars
	configureBuildCmdProc(cmd)
	cmd.Cancel = func() error { return killBuildCmdProcess(cmd) }

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		msg := "启动脚本失败: " + err.Error()
		p.failRun(run, msg)
		writeLine("ERROR: " + err.Error())
		return err
	}
	defer func() { _ = killBuildCmdProcess(cmd) }()

	var scanWg sync.WaitGroup
	scanWg.Go(func() { scanLines(stdout, writeLine) })
	scanLines(stderr, writeLine)
	scanWg.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			p.cancelRun(run)
			return ctx.Err()
		}
		msg := "脚本失败: " + err.Error()
		p.failRun(run, msg)
		writeLine("ERROR: script failed with " + err.Error())
		return err
	}
	return nil
}

func (p *ScriptPipeline) broadcastRefresh(runID uint) {
	if p.hub == nil {
		return
	}
	p.hub.BroadcastToChannel(fmt.Sprintf("script-run:%d", runID), []byte("__REFRESH__"))
}

func (p *ScriptPipeline) failRun(run *model.ScriptRun, errMsg string) {
	finished := time.Now()
	fields := map[string]interface{}{
		"status":        "failed",
		"error_message": errMsg,
		"finished_at":   finished,
		"stage":         "idle",
	}
	if run.StartedAt != nil {
		fields["duration_ms"] = finished.Sub(*run.StartedAt).Milliseconds()
	}
	_ = p.runs.UpdateFields(run.ID, fields)
	p.broadcastRefresh(run.ID)
	p.notifyTerminal(run, "failed")
}

func (p *ScriptPipeline) cancelRun(run *model.ScriptRun) {
	finished := time.Now()
	fields := map[string]interface{}{
		"status":      "cancelled",
		"finished_at": finished,
		"stage":       "idle",
	}
	if run.StartedAt != nil {
		fields["duration_ms"] = finished.Sub(*run.StartedAt).Milliseconds()
	}
	_ = p.runs.UpdateFields(run.ID, fields)
	p.broadcastRefresh(run.ID)
	p.notifyTerminal(run, "cancelled")
}

func (p *ScriptPipeline) notifyTerminal(run *model.ScriptRun, status string) {
	if p.termHook == nil || run == nil {
		return
	}
	run.Status = status
	p.termHook.OnScriptRunTerminal(run, status)
}

// applyRunEnvOverrides overlays run-level env overrides (same AES-GCM JSON map
// format as job env_vars) onto the job env; override keys win.
func applyRunEnvOverrides(kvEnv map[string]string, overridesCipher string) error {
	if kvEnv == nil {
		return nil
	}
	overrides, err := decryptJobEnvVarsCipher(overridesCipher)
	if err != nil {
		return err
	}
	for k, v := range overrides {
		kvEnv[k] = v
	}
	return nil
}

func buildScriptJobTemplateVars(
	job *model.ScriptJob,
	run *model.ScriptRun,
	workspace string,
	kvEnv map[string]string,
) map[string]string {
	absWS := workspace
	if a, err := filepath.Abs(workspace); err == nil {
		absWS = a
	}
	vars := map[string]string{
		"job.id":    fmt.Sprintf("%d", job.ID),
		"job.name":  job.Name,
		"run.id":    fmt.Sprintf("%d", run.ID),
		"workspace": absWS,
	}
	for _, name := range job.EnvVarNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if v, ok := os.LookupEnv(name); ok {
			vars["env."+name] = v
		}
	}
	for k, v := range kvEnv {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		vars["env."+k] = v
	}
	return vars
}

func decodeScriptEnvNames(job *model.ScriptJob) {
	if job == nil {
		return
	}
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

// isSubpath reports whether child is equal to or under parent (after Clean).
func isSubpath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(child, parent+sep)
}
