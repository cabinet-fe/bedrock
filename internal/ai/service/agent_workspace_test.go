package service_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
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
	deadline := time.Now().Add(2 * time.Second)
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
	agents, skills, _, work, _ := setupAgentWorkspace(t)

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
		Name: "ws-agent", CliKey: "claude_code", SystemPrompt: "hello workspace",
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

func TestAgentWorkspaceDefaultBranchAndDuplicateRejected(t *testing.T) {
	agents, _, _, work, _ := setupAgentWorkspace(t)
	repoID := uint(3)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "defaults", CliKey: "claude_code",
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
		Name: "multi-branch", CliKey: "claude_code",
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
		Name: "dup", CliKey: "claude_code",
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
		Name: "cleanup", CliKey: "claude_code",
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
		Name: "del", CliKey: "claude_code", TimeoutSec: 10,
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
	agents, _, repo, work, arts := setupAgentWorkspace(t)
	t.Setenv("BEDROCK_AGENT_OUTPUT", "/must-not-leak")
	markCLIInstalled(t, repo, "claude_code", "")

	agents.SetCLIRunner(func(_ context.Context, req service.CLIRunRequest) (string, error) {
		output := envValue(req.Env, "BEDROCK_AGENT_OUTPUT")
		workdir := envValue(req.Env, "BEDROCK_AGENT_WORKDIR")
		if output == "" {
			return "", fmt.Errorf("BEDROCK_AGENT_OUTPUT missing")
		}
		if output == "/must-not-leak" {
			return "", fmt.Errorf("parent BEDROCK_AGENT_OUTPUT leaked")
		}
		if st, err := os.Stat(output); err != nil || !st.IsDir() {
			return "", fmt.Errorf("output dir missing")
		}
		note := filepath.Join(workdir, "note.txt")
		result := filepath.Join(output, "result.txt")
		if _, err := os.Stat(note); err == nil {
			if _, err := os.Stat(result); err != nil {
				return "", fmt.Errorf("output dir was cleared")
			}
			if err := os.WriteFile(result, []byte("second"), 0o644); err != nil {
				return "", err
			}
		} else {
			if err := os.WriteFile(result, []byte("first"), 0o644); err != nil {
				return "", err
			}
			if err := os.WriteFile(note, []byte("workspace-note"), 0o644); err != nil {
				return "", err
			}
		}
		return "persistent output\n", nil
	})

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "persistent", CliKey: "claude_code", SystemPrompt: "x",
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
		for _, removed := range []string{"artifact_format", "max_artifacts", "artifact_path", "build_job_ids"} {
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
		Name: "oc", CliKey: "opencode",
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
		Name: "pending-run", CliKey: "claude_code",
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

func TestAgentRunPassesFullPermissionFlagsAndScopeHint(t *testing.T) {
	agents, _, repo, _, _ := setupAgentWorkspace(t)
	repoID := uint(9)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)
	markCLIInstalled(t, repo, "claude_code", "--print")

	var last service.CLIRunRequest
	agents.SetCLIRunner(recordingCLIRunner(&last))

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "fullperm", CliKey: "claude_code", SystemPrompt: "do work",
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
	requireRunStatus(t, agents, run.ID, model.JobSuccess)
	joined := strings.Join(last.Args, "\n")
	for _, want := range []string{
		"--print",
		"--dangerously-skip-permissions",
		"$BEDROCK_AGENT_WORKDIR",
		"./repo-{id}-{branch}",
		"禁止访问该目录之外的任意路径",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv missing %q; got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--add-dir") {
		t.Fatalf("argv should not include --add-dir; got:\n%s", joined)
	}
}

func TestAgentRunNonInteractiveCLIArgs(t *testing.T) {
	cases := []struct {
		cliKey     string
		defaultArg string
		wantParts  []string
	}{
		{
			cliKey: "claude_code", defaultArg: "--print",
			wantParts: []string{"--print", "--dangerously-skip-permissions"},
		},
		{
			cliKey: "codex", defaultArg: "exec",
			wantParts: []string{"exec", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			cliKey: "opencode", defaultArg: "run",
			wantParts: []string{"run", "--dangerously-skip-permissions"},
		},
		{
			cliKey: "reasonix", defaultArg: "run",
			wantParts: []string{"run", "--permission-mode", "bypassPermissions"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.cliKey, func(t *testing.T) {
			agents, _, repo, _, _ := setupAgentWorkspace(t)
			markCLIInstalled(t, repo, tc.cliKey, tc.defaultArg)
			var last service.CLIRunRequest
			agents.SetCLIRunner(recordingCLIRunner(&last))
			agent, err := agents.CreateAgent(1, service.AgentInput{
				Name: "args-" + tc.cliKey, CliKey: tc.cliKey, SystemPrompt: "do work", TimeoutSec: 30,
			})
			if err != nil {
				t.Fatal(err)
			}
			agent = requireWorkspaceReady(t, agents, agent.ID)
			run, err := agents.ManualRun(agent.ID, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			requireRunStatus(t, agents, run.ID, model.JobSuccess)
			joined := strings.Join(last.Args, "\n")
			for _, want := range tc.wantParts {
				if !strings.Contains(joined, want) {
					t.Fatalf("argv missing %q; got:\n%s", want, joined)
				}
			}
		})
	}
}

func TestAgentRunStreamOutputCLIArgs(t *testing.T) {
	cases := []struct {
		cliKey     string
		defaultArg string
		forbidden  []string
	}{
		{
			cliKey: "claude_code", defaultArg: "--print",
			forbidden: []string{"stream-json", "--json", "--format", "json"},
		},
		{cliKey: "codex", defaultArg: "exec", forbidden: []string{"--json", "stream-json"}},
		{cliKey: "opencode", defaultArg: "run", forbidden: []string{"--format", "json", "stream-json"}},
		{cliKey: "reasonix", defaultArg: "run", forbidden: []string{"stream-json", "--json", "-p"}},
	}
	for _, tc := range cases {
		t.Run(tc.cliKey, func(t *testing.T) {
			agents, _, repo, _, _ := setupAgentWorkspace(t)
			markCLIInstalled(t, repo, tc.cliKey, tc.defaultArg)
			var last service.CLIRunRequest
			agents.SetCLIRunner(recordingCLIRunner(&last))
			stream := true
			agent, err := agents.CreateAgent(1, service.AgentInput{
				Name: "stream-" + tc.cliKey, CliKey: tc.cliKey, SystemPrompt: "do work",
				StreamOutput: &stream, TimeoutSec: 30,
			})
			if err != nil {
				t.Fatal(err)
			}
			agent = requireWorkspaceReady(t, agents, agent.ID)
			run, err := agents.ManualRun(agent.ID, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			requireRunStatus(t, agents, run.ID, model.JobSuccess)
			joined := strings.Join(last.Args, "\n")
			hasArg := func(flag string) bool {
				for _, line := range last.Args {
					if strings.TrimSpace(line) == flag {
						return true
					}
				}
				return false
			}
			for _, bad := range tc.forbidden {
				switch bad {
				case "-p", "--print":
					if hasArg(bad) {
						t.Fatalf("argv should not contain %q; got:\n%s", bad, joined)
					}
				case "stream-json", "--json":
					if strings.Contains(joined, bad) {
						t.Fatalf("argv should not contain %q; got:\n%s", bad, joined)
					}
				case "--format":
					if hasArg("--format") || strings.Contains(joined, "--format json") {
						t.Fatalf("argv should not contain json format flag; got:\n%s", joined)
					}
				case "json":
					// covered by --format / --json cases
				default:
					if strings.Contains(joined, bad) {
						t.Fatalf("argv should not contain %q; got:\n%s", bad, joined)
					}
				}
			}
		})
	}
}

func TestAgentRunNonStreamOutputCLIArgs(t *testing.T) {
	agents, _, repo, _, _ := setupAgentWorkspace(t)
	markCLIInstalled(t, repo, "reasonix", "run")
	var last service.CLIRunRequest
	agents.SetCLIRunner(recordingCLIRunner(&last))
	stream := false
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "non-stream-rx", CliKey: "reasonix", SystemPrompt: "do work",
		StreamOutput: &stream, TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	run, err := agents.ManualRun(agent.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	requireRunStatus(t, agents, run.ID, model.JobSuccess)
	hasArg := false
	for _, arg := range last.Args {
		if strings.TrimSpace(arg) == "-p" {
			hasArg = true
			break
		}
	}
	if !hasArg {
		t.Fatalf("reasonix non-stream should pass -p; got:\n%s", strings.Join(last.Args, "\n"))
	}
}
