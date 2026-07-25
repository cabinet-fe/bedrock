package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bedrock/internal/cicd/model"
	"bedrock/internal/deployer"
	resourcemodel "bedrock/internal/resource/model"
)

func (p *Pipeline) runDistributions(
	ctx context.Context,
	run *model.BuildRun,
	job *model.BuildJob,
	sourceDir string,
	writeLine func(string),
	filterIDs []uint,
) {
	targets, err := p.jobs.ListDeployTargets(job.ID)
	if err != nil {
		writeLine("ERROR: load deploy targets: " + err.Error())
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
			"stage":                "idle",
			"distribution_summary": "all_failed",
		})
		p.broadcastRunRefresh(run.ID)
		return
	}
	targets = filterDeployTargets(targets, filterIDs)
	if len(targets) == 0 {
		writeLine("=== No deploy targets ===")
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
			"stage":                "idle",
			"distribution_summary": "none",
		})
		p.broadcastRunRefresh(run.ID)
		return
	}

	batchNo, err := p.runs.NextBatchNo(run.ID)
	if err != nil || batchNo < 1 {
		batchNo = 1
	}

	p.setStageKeepSuccess(run, "distributing")
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"distribution_summary": "running"})
	p.broadcastRunRefresh(run.ID)
	writeLine(fmt.Sprintf("=== Stage: Distributing (batch %d) ===", batchNo))

	var nOK, nFail int
	for i := range targets {
		if ctx.Err() != nil {
			for j := i; j < len(targets); j++ {
				p.recordAttemptCancelled(run, batchNo, &targets[j])
			}
			writeLine("ERROR: cancelled")
			break
		}
		t := targets[i]
		snap, _ := json.Marshal(t)
		attempt := &model.BuildDeployAttempt{
			BuildRunID:         run.ID,
			BatchNo:            batchNo,
			DeployTargetID:     &t.ID,
			TargetSnapshotJSON: string(snap),
			Status:             "running",
			StartedAt:          new(time.Now()),
		}
		_ = p.runs.CreateAttempt(attempt)
		p.broadcastRunRefresh(run.ID)
		writeLine(fmt.Sprintf("--- Target #%d (%s → %s) ---", t.ID, t.Method, t.RemotePath))
		err := p.deployOneTarget(ctx, &t, sourceDir, NormalizeArtifactFormat(job.ArtifactFormat), writeLine)
		fin := time.Now()
		attempt.FinishedAt = &fin
		if err != nil {
			if ctx.Err() != nil {
				attempt.Status = "cancelled"
				attempt.ErrorMessage = "cancelled"
			} else {
				attempt.Status = "failed"
				attempt.ErrorMessage = err.Error()
			}
			_ = p.runs.UpdateAttempt(attempt)
			p.broadcastRunRefresh(run.ID)
			writeLine("ERROR: " + err.Error())
			nFail++
			continue
		}
		attempt.Status = "success"
		attempt.ErrorMessage = ""
		_ = p.runs.UpdateAttempt(attempt)
		p.broadcastRunRefresh(run.ID)
		nOK++
	}

	summary := "all_success"
	if ctx.Err() != nil {
		summary = "cancelled"
	} else if nFail > 0 && nOK > 0 {
		summary = "partial"
	} else if nFail > 0 && nOK == 0 {
		summary = "all_failed"
	}

	// Never change status away from success due to distribution outcome.
	_ = p.runs.UpdateFields(run.ID, map[string]interface{}{
		"status":               "success",
		"stage":                "idle",
		"distribution_summary": summary,
	})
	p.broadcastRunRefresh(run.ID)
	writeLine(fmt.Sprintf("=== Distribution phase finished (%s) ===", summary))
	if p.agentHook != nil {
		job, err := p.jobs.FindByID(run.BuildJobID)
		if err == nil {
			// Job may override default artifact_ready to distribution_finished.
			p.agentHook.OnBuildEvent("distribution_finished", job, run)
		}
	}
}

func (p *Pipeline) recordAttemptCancelled(run *model.BuildRun, batchNo int, t *model.DeployTarget) {
	snap, _ := json.Marshal(t)
	id := t.ID
	_ = p.runs.CreateAttempt(&model.BuildDeployAttempt{
		BuildRunID:         run.ID,
		BatchNo:            batchNo,
		DeployTargetID:     &id,
		TargetSnapshotJSON: string(snap),
		Status:             "cancelled",
		ErrorMessage:       "cancelled",
		FinishedAt:         new(time.Now()),
	})
	p.broadcastRunRefresh(run.ID)
}

func filterDeployTargets(all []model.DeployTarget, ids []uint) []model.DeployTarget {
	if len(ids) == 0 {
		return all
	}
	want := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]model.DeployTarget, 0, len(ids))
	for _, t := range all {
		if _, ok := want[t.ID]; ok {
			out = append(out, t)
		}
	}
	return out
}

func (p *Pipeline) deployOneTarget(
	ctx context.Context,
	t *model.DeployTarget,
	sourceDir, artifactFormat string,
	writeLine func(string),
) error {
	method := strings.TrimSpace(strings.ToLower(t.Method))
	if method == "" {
		method = "rsync"
	}
	isLocal := method == "local"
	deployPath := strings.TrimSpace(t.RemotePath)

	if isLocal {
		if deployPath == "" {
			return fmt.Errorf("分发未配置路径")
		}
		if !filepath.IsAbs(deployPath) {
			return fmt.Errorf("本机分发路径须为绝对路径")
		}
	} else {
		if t.ServerID == nil || deployPath == "" {
			return fmt.Errorf("分发未配置服务器或路径")
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	opts := deployer.DeployOptions{
		SourceDir:     sourceDir,
		ArchiveFormat: artifactFormat,
		RemotePath:    deployPath,
		Logger:        writeLine,
	}
	if !isLocal {
		server, err := p.servers.FindByID(*t.ServerID)
		if err != nil {
			return fmt.Errorf("服务器不存在")
		}
		password, privateKey, agentToken, err := p.resolveServerSecrets(server)
		if err != nil {
			return err
		}
		username := server.Username
		if username == "" {
			// Prefer credential username when server username empty
			if server.CredentialID != nil {
				_, u, _, _, _ := p.secrets.Resolve(*server.CredentialID)
				username = u
			}
		}
		opts.Server = deployer.ServerInfo{
			Host:       server.Host,
			Port:       server.Port,
			OSType:     server.OSType,
			Username:   username,
			AuthType:   server.AuthType,
			Password:   password,
			PrivateKey: privateKey,
			AgentURL:   server.AgentURL,
			AgentToken: agentToken,
		}
	}

	dpl := deployer.NewDeployer(method)
	if err := dpl.Deploy(ctx, opts); err != nil {
		return fmt.Errorf("分发失败: %w", err)
	}
	writeLine("Distribution completed successfully")

	if strings.TrimSpace(t.PostDeployScript) != "" {
		writeLine("=== Executing post-deploy script ===")
		var err error
		if isLocal {
			err = deployer.ExecuteLocalScriptInDir(ctx, opts.RemotePath, t.PostDeployScript, writeLine)
		} else {
			err = deployer.ExecuteRemoteScriptInDir(ctx, opts.Server, opts.RemotePath, t.PostDeployScript, writeLine)
		}
		if err != nil {
			return fmt.Errorf("部署后脚本失败: %w", err)
		}
		writeLine("Post-deploy script completed")
	}
	return nil
}

func (p *Pipeline) resolveServerSecrets(server *resourcemodel.Server) (password, privateKey, agentToken string, err error) {
	if server.CredentialID != nil && *server.CredentialID > 0 {
		typ, _, secret, passphrase, rerr := p.secrets.Resolve(*server.CredentialID)
		if rerr != nil {
			return "", "", "", rerr
		}
		switch strings.ToLower(typ) {
		case "ssh_key":
			privateKey = secret
			_ = passphrase // passphrase not threaded into deployer.ServerInfo in current API
		default:
			password = secret
		}
	}
	if server.AgentCredentialID != nil && *server.AgentCredentialID > 0 {
		_, _, token, _, rerr := p.secrets.Resolve(*server.AgentCredentialID)
		if rerr != nil {
			return "", "", "", rerr
		}
		agentToken = token
	}
	return password, privateKey, agentToken, nil
}

func (p *Pipeline) executeRedeployOnly(ctx context.Context, run *model.BuildRun, job *model.BuildJob, writeLine func(string)) {
	artifactPath := strings.TrimSpace(run.ArtifactPath)
	if artifactPath == "" {
		writeLine("ERROR: no artifact_path")
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"distribution_summary": "all_failed", "stage": "idle"})
		p.broadcastRunRefresh(run.ID)
		return
	}
	if !filepath.IsAbs(artifactPath) {
		artifactPath = filepath.Join(p.artifact, artifactPath)
	}
	if _, err := os.Stat(artifactPath); err != nil {
		writeLine("ERROR: " + err.Error())
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"distribution_summary": "all_failed", "stage": "idle"})
		p.broadcastRunRefresh(run.ID)
		return
	}
	writeLine("=== Redeploy: using existing artifact ===")
	writeLine("Artifact: " + artifactPath)

	tmpDir, err := os.MkdirTemp("", "bedrock-redeploy-*")
	if err != nil {
		writeLine("ERROR: mkdir temp: " + err.Error())
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"distribution_summary": "all_failed", "stage": "idle"})
		p.broadcastRunRefresh(run.ID)
		return
	}
	defer os.RemoveAll(tmpDir)

	format := NormalizeArtifactFormat(job.ArtifactFormat)
	if err := extractArtifactArchive(artifactPath, tmpDir, format); err != nil {
		writeLine("ERROR: " + err.Error())
		_ = p.runs.UpdateFields(run.ID, map[string]interface{}{"distribution_summary": "all_failed", "stage": "idle"})
		p.broadcastRunRefresh(run.ID)
		return
	}

	filter := parseTargetFilterFromSnapshot(run.SnapshotJSON)
	if ctx.Err() != nil {
		p.cancelRun(run)
		return
	}
	p.runDistributions(ctx, run, job, tmpDir, writeLine, filter)
}

// parseTargetFilterFromSnapshot reads optional redeploy_target_ids from snapshot_json.
func parseTargetFilterFromSnapshot(snapshotJSON string) []uint {
	if strings.TrimSpace(snapshotJSON) == "" {
		return nil
	}
	var snap map[string]interface{}
	if err := json.Unmarshal([]byte(snapshotJSON), &snap); err != nil {
		return nil
	}
	raw, ok := snap["redeploy_target_ids"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var ids []uint
	for _, v := range arr {
		switch n := v.(type) {
		case float64:
			ids = append(ids, uint(n))
		}
	}
	return ids
}
