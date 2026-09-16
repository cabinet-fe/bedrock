package service_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
	"bedrock/internal/harness/harnesstest"
	"bedrock/internal/harness/provider"
	"bedrock/internal/pkg"
	resourcemodel "bedrock/internal/resource/model"
)

type stubRepoFinder struct {
	repos map[uint]*resourcemodel.Repository
}

func (f *stubRepoFinder) FindByID(id uint) (*resourcemodel.Repository, error) {
	repo, ok := f.repos[id]
	if !ok {
		return nil, fmt.Errorf("repo %d not found", id)
	}
	return repo, nil
}

func stubGitCheckout(_ context.Context, workDir, _repoURL, _authType, _username, _password, branch string, _logFn func(string)) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "BRANCH"), []byte(branch), 0o644)
}

func readRunLog(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		return "<empty log path>"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "<read log: " + err.Error() + ">"
	}
	const max = 2000
	if len(data) > max {
		return string(data[:max]) + "...(truncated)"
	}
	return string(data)
}

// waitWorkspaceAsync polls only for intentionally async workspace init tests.
func waitWorkspaceAsync(t *testing.T, agents *service.AgentService, agentID uint, want string) *model.AiAgent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := agents.GetAgent(agentID)
		if err != nil {
			t.Fatal(err)
		}
		if got.WorkspaceStatus == want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := agents.GetAgent(agentID)
	t.Fatalf("workspace_status=%q want=%q err=%q", got.WorkspaceStatus, want, got.WorkspaceError)
	return nil
}

func TestAgentWorkspaceSyncSkillsAndRepoCheckouts(t *testing.T) {
	agents, _, skills, work, _ := setupAgentWorkspace(t)

	z := zipBytes(t, map[string]string{"SKILL.md": "# workspace-skill"})
	skill, err := skills.Create(service.SkillUploadInput{
		Name: "ws", Visibility: model.SkillPublic, Filename: "ws.zip",
		Size: int64(len(z)), Source: bytes.NewReader(z), UserID: 1, IsSuperAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repoID := uint(7)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "demo", RepoURL: "https://example.com/demo.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "ws-agent", SystemPrompt: "hello workspace",
		SkillIDs:     []uint{skill.ID},
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "develop"}},
		TimeoutSec:   30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.WorkspaceStatus != model.WorkspacePending {
		t.Fatalf("create should return pending, got %q", agent.WorkspaceStatus)
	}
	if len(agent.RepoBindings) != 1 || agent.RepoBindings[0].Branch != "develop" {
		t.Fatalf("repo_bindings=%v", agent.RepoBindings)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	skillMD := filepath.Join(root, ".agents", "skills", skill.Name, "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("skill not extracted: %v", err)
	}
	// The harness agent definition compiles into the workspace (customized
	// agent → bedrock-agent-{id}.md under .opencode/agents/).
	defMD := filepath.Join(root, ".opencode", "agents", fmt.Sprintf("bedrock-agent-%d.md", agent.ID))
	if _, err := os.Stat(defMD); err != nil {
		t.Fatalf("compiled agent definition missing: %v", err)
	}
	nestedByID := filepath.Join(root, ".agents", "skills", fmt.Sprintf("%d", skill.ID), "SKILL.md")
	if _, err := os.Stat(nestedByID); err == nil {
		t.Fatalf("skill must not be nested under id folder %q", nestedByID)
	}
	prompt := filepath.Join(root, "SYSTEM_PROMPT.md")
	data, err := os.ReadFile(prompt)
	if err != nil || string(data) != "hello workspace" {
		t.Fatalf("SYSTEM_PROMPT.md: %v %q", err, data)
	}
	checkout := filepath.Join(root, fmt.Sprintf("repo-%d-develop", repoID))
	branchFile, err := os.ReadFile(filepath.Join(checkout, "BRANCH"))
	if err != nil {
		t.Fatalf("repo checkout missing: %v", err)
	}
	if string(branchFile) != "develop" {
		t.Fatalf("branch=%q", branchFile)
	}
	if _, err := os.Lstat(filepath.Join(root, "job-1")); !os.IsNotExist(err) {
		t.Fatalf("legacy job softlink must not exist, err=%v", err)
	}
}

// TestSyncAgentWorkspaceSkillNormalizationAndEnvPermissions covers the P4
// harness integration acceptance on the ai side: bound skills land in
// normalized lowercase-hyphen dirs with aligned SKILL.md frontmatter, and
// the decrypted .env is written 0600.
func TestSyncAgentWorkspaceSkillNormalizationAndEnvPermissions(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	agents, _, skills, work, _ := setupAgentWorkspace(t)

	z := zipBytes(t, map[string]string{"SKILL.md": "---\nname: Old Name\n---\n\n# deploy body\n"})
	skill, err := skills.Create(service.SkillUploadInput{
		Name: "Deploy Helper", Visibility: model.SkillPublic, Filename: "deploy.zip",
		Size: int64(len(z)), Source: bytes.NewReader(z), UserID: 1, IsSuperAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := "secret-token"
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "norm-agent", SystemPrompt: "sp",
		SkillIDs:   []uint{skill.ID},
		EnvVars:    []service.EnvVarInput{{Key: "DEPLOY_TOKEN", Value: &token}},
		TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))

	skillMD := filepath.Join(root, ".agents", "skills", "deploy-helper", "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("normalized skill dir missing: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\nname: deploy-helper\n") {
		t.Fatalf("SKILL.md frontmatter not aligned:\n%s", data)
	}
	if !strings.Contains(string(data), "deploy body") {
		t.Fatalf("SKILL.md body lost:\n%s", data)
	}

	envPath := filepath.Join(root, ".env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf(".env mode = %o, want 600", perm)
	}
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(envData)) != "DEPLOY_TOKEN=secret-token" {
		t.Fatalf(".env content = %q", envData)
	}
}

func TestAgentWorkspaceDefaultBranchAndDuplicateRejected(t *testing.T) {
	agents, _, _, work, _ := setupAgentWorkspace(t)
	repoID := uint(3)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name:         "defaults",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID}},
		TimeoutSec:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	if len(agent.RepoBindings) != 1 || agent.RepoBindings[0].Branch != "main" {
		t.Fatalf("expected default main, got %#v", agent.RepoBindings)
	}
	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("repo-%d-main", repoID))); err != nil {
		t.Fatalf("default branch checkout missing: %v", err)
	}

	multi, err := agents.CreateAgent(1, service.AgentInput{
		Name: "multi-branch",
		RepoBindings: []model.RepoBinding{
			{RepositoryID: repoID, Branch: "a"},
			{RepositoryID: repoID, Branch: "b"},
		},
		TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	multi = requireWorkspaceReady(t, agents, multi.ID)
	if len(multi.RepoBindings) != 2 {
		t.Fatalf("expected 2 bindings, got %#v", multi.RepoBindings)
	}
	multiRoot := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", multi.ID))
	dirA := filepath.Join(multiRoot, fmt.Sprintf("repo-%d-a", repoID))
	dirB := filepath.Join(multiRoot, fmt.Sprintf("repo-%d-b", repoID))
	if dirA == dirB {
		t.Fatalf("same-repo branches must use different dirs: %q", dirA)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Fatalf("branch a checkout missing: %v", err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Fatalf("branch b checkout missing: %v", err)
	}

	_, err = agents.CreateAgent(1, service.AgentInput{
		Name: "dup",
		RepoBindings: []model.RepoBinding{
			{RepositoryID: repoID, Branch: "main"},
			{RepositoryID: repoID, Branch: "main"},
		},
		TimeoutSec: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestAgentWorkspaceRemovesStaleJobLinksAndUnboundRepos(t *testing.T) {
	agents, _, _, work, _ := setupAgentWorkspace(t)
	repoKeep := uint(1)
	repoDrop := uint(2)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoKeep: {ID: repoKeep, Name: "keep", RepoURL: "https://example.com/keep.git", AuthType: "none"},
			repoDrop: {ID: repoDrop, Name: "drop", RepoURL: "https://example.com/drop.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "cleanup",
		RepoBindings: []model.RepoBinding{
			{RepositoryID: repoKeep, Branch: "main"},
			{RepositoryID: repoDrop, Branch: "main"},
		},
		TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	legacyJob := filepath.Join(root, "job-99")
	if err := os.Symlink(root, legacyJob); err != nil {
		t.Fatal(err)
	}

	updated, err := agents.UpdateAgent(agent.ID, 1, service.AgentInput{
		Name: "cleanup",
		RepoBindings: []model.RepoBinding{
			{RepositoryID: repoKeep, Branch: "main"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.WorkspaceStatus != model.WorkspacePending {
		t.Fatalf("update should return pending, got %q", updated.WorkspaceStatus)
	}
	updated = requireWorkspaceReady(t, agents, agent.ID)
	if len(updated.RepoBindings) != 1 {
		t.Fatalf("bindings=%v", updated.RepoBindings)
	}
	if _, err := os.Lstat(legacyJob); !os.IsNotExist(err) {
		t.Fatalf("legacy job link should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("repo-%d-main", repoDrop))); !os.IsNotExist(err) {
		t.Fatalf("unbound repo dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("repo-%d-main", repoKeep))); err != nil {
		t.Fatalf("kept repo missing: %v", err)
	}
}

func TestAgentWorkspaceDeleteRemovesDir(t *testing.T) {
	agents, _, _, work, arts := setupAgentWorkspace(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "del", TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	artRoot := filepath.Join(arts, fmt.Sprintf("agent-%d", agent.ID))
	if err := os.MkdirAll(artRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artRoot, "run-1.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	if err := agents.DeleteAgent(agent.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("workspace should be removed, err=%v", err)
	}
	if _, err := os.Stat(artRoot); !os.IsNotExist(err) {
		t.Fatalf("artifact dir should be removed, err=%v", err)
	}
}

func TestAgentRunsReusePersistentWorkspace(t *testing.T) {
	agents, fake, _, work, arts := setupAgentWorkspace(t)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "persistent", SystemPrompt: "x",
		OutputDir: "deliverables", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	if agent.OutputDir != "deliverables" {
		t.Fatalf("output_dir=%q", agent.OutputDir)
	}
	wantWork := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	wantOutput := filepath.Join(wantWork, "deliverables")
	keepPath := filepath.Join(wantWork, "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The session script plays the agent: deliverables go into the fixed
	// output directory, the workspace note persists across runs.
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
		note := filepath.Join(sess.Directory, "note.txt")
		result := filepath.Join(wantOutput, "result.txt")
		if _, err := os.Stat(note); err == nil {
			if _, err := os.Stat(result); err != nil {
				_ = f.Emit(provider.Frame{
					SessionID: sess.ID, Kind: provider.FrameStatus,
					Status: &provider.StatusFrame{Name: provider.StatusError, Error: "output dir was cleared"},
				})
				return
			}
			_ = os.WriteFile(result, []byte("second"), 0o644)
		} else {
			_ = os.WriteFile(result, []byte("first"), 0o644)
			_ = os.WriteFile(note, []byte("workspace-note"), 0o644)
		}
		f.Complete(sess.ID, "persistent output")
	})

	var finishedRuns []*model.AgentRun
	for range 2 {
		run, err := agents.ManualRun(agent.ID, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(run.SnapshotJSON, `"output_dir":"deliverables"`) {
			t.Fatalf("snapshot missing output_dir: %s", run.SnapshotJSON)
		}
		if !strings.Contains(run.SnapshotJSON, `"repo_bindings"`) {
			t.Fatalf("snapshot missing repo_bindings: %s", run.SnapshotJSON)
		}
		for _, removed := range []string{"artifact_format", "max_artifacts", "artifact_path", "build_job_ids", "cli_key"} {
			if strings.Contains(run.SnapshotJSON, removed) {
				t.Fatalf("snapshot contains removed field %q: %s", removed, run.SnapshotJSON)
			}
		}
		finishedRuns = append(finishedRuns, requireRunStatus(t, agents, run.ID, model.JobSuccess))
	}
	for _, finished := range finishedRuns {
		if finished.WorkDir != wantWork {
			t.Fatalf("work_dir=%q want=%q", finished.WorkDir, wantWork)
		}
		if finished.HarnessSessionID == nil || *finished.HarnessSessionID == "" {
			t.Fatalf("harness_session_id missing: %+v", finished)
		}
		if !strings.Contains(finished.FinalOutput, "persistent output") {
			t.Fatalf("final_output=%q log=%s", finished.FinalOutput, readRunLog(t, finished.LogPath))
		}
		if !strings.Contains(finished.OutputText, "persistent output") {
			t.Fatalf("output_text=%q log=%s", finished.OutputText, readRunLog(t, finished.LogPath))
		}
		wantArt := filepath.Join(arts, fmt.Sprintf("agent-%d", agent.ID), fmt.Sprintf("run-%d.zip", finished.ID))
		if finished.ArtifactPath != wantArt {
			t.Fatalf("artifact_path=%q want=%q", finished.ArtifactPath, wantArt)
		}
		if finished.ArtifactKind != "archive" {
			t.Fatalf("artifact_kind=%q", finished.ArtifactKind)
		}
		if _, err := os.Stat(wantArt); err != nil {
			t.Fatalf("artifact missing: %v", err)
		}
		path, name, err := agents.ArtifactPath(finished.ID)
		if err != nil || path != wantArt || name != filepath.Base(wantArt) {
			t.Fatalf("ArtifactPath=%q name=%q err=%v", path, name, err)
		}
	}
	// One run maps to exactly one harness session; two runs, two sessions.
	if n := len(fake.Sessions()); n != 2 {
		t.Fatalf("sessions=%d want 2", n)
	}
	for _, path := range []string{keepPath, filepath.Join(wantWork, "note.txt"), filepath.Join(wantOutput, "result.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persistent file missing %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(wantOutput, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("output result=%q want second", data)
	}
	if _, err := os.Lstat(filepath.Join(wantWork, "runs")); !os.IsNotExist(err) {
		t.Fatalf("per-run workspace must not exist, err=%v", err)
	}
}

func TestAgentWorkspaceNoOpenCodeExternalDirs(t *testing.T) {
	agents, _, _, work, _ := setupAgentWorkspace(t)
	repoID := uint(2)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name:         "oc",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "main"}},
		TimeoutSec:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	cfgPath := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID), "opencode.json")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("opencode.json should not be written, err=%v", err)
	}
}

func TestAgentManualRunRejectedWhileWorkspacePending(t *testing.T) {
	agents, _, _, _, _ := setupAgentWorkspace(t)
	agents.SetSyncWorkspaceInit(false)
	block := make(chan struct{})
	agents.SetGitCheckout(func(ctx context.Context, workDir, repoURL, authType, username, password, branch string, logFn func(string)) error {
		<-block
		return stubGitCheckout(ctx, workDir, repoURL, authType, username, password, branch, logFn)
	})
	repoID := uint(11)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name:         "pending-run",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "main"}},
		TimeoutSec:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.WorkspaceStatus != model.WorkspacePending {
		t.Fatalf("status=%q", agent.WorkspaceStatus)
	}
	_, err = agents.ManualRun(agent.ID, 1, "")
	if err == nil || !strings.Contains(err.Error(), "工作区未初始化完成") {
		t.Fatalf("expected pending gate error, got %v", err)
	}
	close(block)
	waitWorkspaceAsync(t, agents, agent.ID, model.WorkspaceReady)
}

func TestAgentRunPromptCarriesWorkspaceScope(t *testing.T) {
	agents, fake, _, work, _ := setupAgentWorkspace(t)
	repoID := uint(9)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "scope", SystemPrompt: "do work",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "main"}},
		TimeoutSec:   30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	run, err := agents.ManualRun(agent.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	finished := requireRunStatus(t, agents, run.ID, model.JobSuccess)
	prompt := waitRunPrompt(t, agents, fake, run.ID)
	wantRoot := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	for _, want := range []string{
		filepath.Join(wantRoot, "output"),
		"./repo-{id}-{branch}",
		"禁止访问该目录之外的任意路径",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q; got:\n%s", want, prompt)
		}
	}
	// The system prompt rides the compiled opencode agent definition, not
	// the run prompt.
	defData, err := os.ReadFile(filepath.Join(wantRoot, ".opencode", "agents",
		fmt.Sprintf("bedrock-agent-%d.md", agent.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defData), "do work") {
		t.Fatalf("compiled agent def missing system prompt:\n%s", defData)
	}
	if strings.Contains(prompt, "do work") {
		t.Fatalf("run prompt must not duplicate the system prompt:\n%s", prompt)
	}
	if finished.HarnessSessionID == nil {
		t.Fatal("harness_session_id missing on finished run")
	}
	if fake.Delivery(*finished.HarnessSessionID) != provider.DeliveryQueue {
		t.Fatalf("delivery=%q want queue", fake.Delivery(*finished.HarnessSessionID))
	}
}

func TestCancelRunAbortsWorkspaceSync(t *testing.T) {
	agents, _, _, _, _ := setupAgentWorkspace(t)
	repoID := uint(21)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name:         "cancel-sync",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "main"}},
		TimeoutSec:   30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	agents.SetInlineExec(false)
	var calls atomic.Int32
	started := make(chan struct{})
	agents.SetGitCheckout(func(ctx context.Context, workDir, repoURL, authType, username, password, branch string, logFn func(string)) error {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return stubGitCheckout(ctx, workDir, repoURL, authType, username, password, branch, logFn)
	})

	hung, err := agents.ManualRun(agent.ID, 1, "hang")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first run never entered git checkout")
	}

	queued, err := agents.ManualRun(agent.ID, 1, "next")
	if err != nil {
		t.Fatal(err)
	}
	if err := agents.CancelRun(hung.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, agents, hung.ID, model.JobCancelled)
	waitRunStatus(t, agents, queued.ID, model.JobSuccess)
}
