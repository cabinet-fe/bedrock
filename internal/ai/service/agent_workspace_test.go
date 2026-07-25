package service_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
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

func waitWorkspaceStatus(t *testing.T, agents *service.AgentService, agentID uint, want string) *model.AiAgent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := agents.GetAgent(agentID)
		if err != nil {
			t.Fatal(err)
		}
		if got.WorkspaceStatus == want {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := agents.GetAgent(agentID)
	t.Fatalf("workspace_status=%q want=%q err=%q", got.WorkspaceStatus, want, got.WorkspaceError)
	return nil
}

func setupAgentWorkspace(t *testing.T) (*service.AgentService, *service.SkillService, *resourcerepo.CLIRepository, string) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "ai-ws.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	repo := repository.NewAIRepository(gdb)
	cliRepo := resourcerepo.NewCLIRepository(gdb)
	cli := resourceservice.NewCLIService(cliRepo)
	storageRoot := filepath.Join(t.TempDir(), "storage")
	storageSvc, err := storageservice.NewStorageService(storagerepo.NewStorageRepository(gdb), storageRoot, storageservice.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	skills := service.NewSkillService(repo, storageSvc)
	work := filepath.Join(t.TempDir(), "work")
	logs := filepath.Join(t.TempDir(), "logs")
	agents := service.NewAgentService(repo, cli, skills, nil, zap.NewNop(), work, logs)
	agents.SetGitCheckout(stubGitCheckout)
	agents.Start()
	t.Cleanup(agents.Shutdown)
	return agents, skills, cliRepo, work
}

func TestAgentWorkspaceSyncSkillsAndRepoCheckouts(t *testing.T) {
	agents, skills, _, work := setupAgentWorkspace(t)

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
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)

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
	checkout := filepath.Join(root, fmt.Sprintf("repo-%d", repoID))
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
	agents, _, _, _ := setupAgentWorkspace(t)
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
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	if len(agent.RepoBindings) != 1 || agent.RepoBindings[0].Branch != "main" {
		t.Fatalf("expected default main, got %#v", agent.RepoBindings)
	}

	_, err = agents.CreateAgent(1, service.AgentInput{
		Name: "dup", CliKey: "claude_code",
		RepoBindings: []model.RepoBinding{
			{RepositoryID: repoID, Branch: "a"},
			{RepositoryID: repoID, Branch: "b"},
		},
		TimeoutSec: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestAgentWorkspaceRemovesStaleJobLinksAndUnboundRepos(t *testing.T) {
	agents, _, _, work := setupAgentWorkspace(t)
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
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
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
	updated = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	if len(updated.RepoBindings) != 1 {
		t.Fatalf("bindings=%v", updated.RepoBindings)
	}
	if _, err := os.Lstat(legacyJob); !os.IsNotExist(err) {
		t.Fatalf("legacy job link should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "repo-2")); !os.IsNotExist(err) {
		t.Fatalf("unbound repo dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "repo-1")); err != nil {
		t.Fatalf("kept repo missing: %v", err)
	}
}

func TestAgentWorkspaceDeleteRemovesDir(t *testing.T) {
	agents, _, _, work := setupAgentWorkspace(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "del", CliKey: "claude_code", TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	root := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	if err := agents.DeleteAgent(agent.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("workspace should be removed, err=%v", err)
	}
}

func TestAgentRunsReusePersistentWorkspace(t *testing.T) {
	agents, _, repo, work := setupAgentWorkspace(t)
	t.Setenv("BEDROCK_AGENT_OUTPUT", "/must-not-leak")

	script := filepath.Join(t.TempDir(), "fake-cli.sh")
	content := `#!/bin/sh
if [ -z "$BEDROCK_AGENT_OUTPUT" ]; then
  echo "BEDROCK_AGENT_OUTPUT missing"
  exit 23
fi
case "$BEDROCK_AGENT_OUTPUT" in
  /must-not-leak) echo "parent BEDROCK_AGENT_OUTPUT leaked"; exit 24 ;;
esac
if [ ! -d "$BEDROCK_AGENT_OUTPUT" ]; then
  echo "output dir missing"
  exit 25
fi
if [ -f "$BEDROCK_AGENT_WORKDIR/note.txt" ]; then
  # Output dir is preserved across runs so caches can be reused.
  if [ ! -f "$BEDROCK_AGENT_OUTPUT/result.txt" ]; then
    echo "output dir was cleared"
    exit 26
  fi
  printf 'second' > "$BEDROCK_AGENT_OUTPUT/result.txt"
else
  printf 'first' > "$BEDROCK_AGENT_OUTPUT/result.txt"
  printf 'workspace-note' > "$BEDROCK_AGENT_WORKDIR/note.txt"
fi
echo "persistent output"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	cli, err := repo.FindByKey("claude_code")
	if err != nil {
		t.Fatal(err)
	}
	cli.InstalledPath = script
	cli.DefaultArgs = ""
	cli.InstallStatus = "installed"
	cli.Healthy = true
	if err := repo.Update(cli); err != nil {
		t.Fatal(err)
	}

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "persistent", CliKey: "claude_code", SystemPrompt: "x",
		OutputDir: "deliverables", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	if agent.OutputDir != "deliverables" {
		t.Fatalf("output_dir=%q", agent.OutputDir)
	}
	wantWork := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID))
	wantOutput := filepath.Join(wantWork, "deliverables")
	keepPath := filepath.Join(wantWork, "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitRun := func(runID uint) *model.AgentRun {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			got, err := agents.GetRun(runID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status == model.JobSuccess || got.Status == model.JobFailed {
				return got
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("run did not finish")
		return nil
	}
	var finishedRuns []*model.AgentRun
	for range 2 {
		run, err := agents.ManualRun(agent.ID, 1)
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
		finishedRuns = append(finishedRuns, waitRun(run.ID))
	}
	for _, finished := range finishedRuns {
		if finished.Status != model.JobSuccess {
			t.Fatalf("status=%s err=%s", finished.Status, finished.ErrorMessage)
		}
		if finished.WorkDir != wantWork {
			t.Fatalf("work_dir=%q want=%q", finished.WorkDir, wantWork)
		}
		if !strings.Contains(finished.OutputText, "persistent output") {
			t.Fatalf("output_text=%q", finished.OutputText)
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
	agents, _, _, work := setupAgentWorkspace(t)
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
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	cfgPath := filepath.Join(work, "agents", fmt.Sprintf("agent-%d", agent.ID), "opencode.json")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("opencode.json should not be written, err=%v", err)
	}
}

func TestAgentManualRunRejectedWhileWorkspacePending(t *testing.T) {
	agents, _, _, _ := setupAgentWorkspace(t)
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
	_, err = agents.ManualRun(agent.ID, 1)
	if err == nil || !strings.Contains(err.Error(), "工作区未初始化完成") {
		t.Fatalf("expected pending gate error, got %v", err)
	}
	close(block)
	waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
}

func TestAgentRunPassesFullPermissionFlagsAndScopeHint(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh required")
	}
	agents, _, repo, _ := setupAgentWorkspace(t)
	repoID := uint(9)
	agents.SetRepoCheckoutDeps(&stubRepoFinder{
		repos: map[uint]*resourcemodel.Repository{
			repoID: {ID: repoID, Name: "r", RepoURL: "https://example.com/r.git", AuthType: "none"},
		},
	}, nil)

	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	script := filepath.Join(t.TempDir(), "fake-cli-fullperm.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	cli, err := repo.FindByKey("claude_code")
	if err != nil {
		t.Fatal(err)
	}
	cli.InstalledPath = script
	cli.DefaultArgs = "--print"
	cli.InstallStatus = "installed"
	cli.Healthy = true
	if err := repo.Update(cli); err != nil {
		t.Fatal(err)
	}

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "fullperm", CliKey: "claude_code", SystemPrompt: "do work",
		RepoBindings: []model.RepoBinding{{RepositoryID: repoID, Branch: "main"}},
		TimeoutSec:   30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	run, err := agents.ManualRun(agent.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := agents.GetRun(run.ID)
		if got != nil && (got.Status == model.JobSuccess || got.Status == model.JobFailed) {
			if got.Status != model.JobSuccess {
				t.Fatalf("status=%s err=%s", got.Status, got.ErrorMessage)
			}
			raw, err := os.ReadFile(argvFile)
			if err != nil {
				t.Fatal(err)
			}
			joined := string(raw)
			for _, want := range []string{
				"--print",
				"--dangerously-skip-permissions",
				"$BEDROCK_AGENT_WORKDIR",
				"./repo-{id}",
				"禁止访问该目录之外的任意路径",
			} {
				if !strings.Contains(joined, want) {
					t.Fatalf("argv missing %q; got:\n%s", want, raw)
				}
			}
			if strings.Contains(joined, "--add-dir") {
				t.Fatalf("argv should not include --add-dir; got:\n%s", raw)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestAgentRunNonInteractiveCLIArgs(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh required")
	}
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
			agents, _, repo, _ := setupAgentWorkspace(t)
			argvFile := filepath.Join(t.TempDir(), "argv.txt")
			script := filepath.Join(t.TempDir(), "fake-cli.sh")
			content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n"
			if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			cli, err := repo.FindByKey(tc.cliKey)
			if err != nil {
				t.Fatal(err)
			}
			cli.InstalledPath = script
			cli.DefaultArgs = tc.defaultArg
			cli.InstallStatus = "installed"
			cli.Healthy = true
			if err := repo.Update(cli); err != nil {
				t.Fatal(err)
			}
			agent, err := agents.CreateAgent(1, service.AgentInput{
				Name: "args-" + tc.cliKey, CliKey: tc.cliKey, SystemPrompt: "do work", TimeoutSec: 30,
			})
			if err != nil {
				t.Fatal(err)
			}
			agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
			run, err := agents.ManualRun(agent.ID, 1)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				got, _ := agents.GetRun(run.ID)
				if got != nil && (got.Status == model.JobSuccess || got.Status == model.JobFailed) {
					if got.Status != model.JobSuccess {
						t.Fatalf("status=%s err=%s", got.Status, got.ErrorMessage)
					}
					raw, err := os.ReadFile(argvFile)
					if err != nil {
						t.Fatal(err)
					}
					joined := string(raw)
					for _, want := range tc.wantParts {
						if !strings.Contains(joined, want) {
							t.Fatalf("argv missing %q; got:\n%s", want, raw)
						}
					}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("timeout")
		})
	}
}

func TestAgentRunStreamOutputCLIArgs(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh required")
	}
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
			agents, _, repo, _ := setupAgentWorkspace(t)
			argvFile := filepath.Join(t.TempDir(), "argv.txt")
			script := filepath.Join(t.TempDir(), "fake-cli.sh")
			content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n"
			if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			cli, err := repo.FindByKey(tc.cliKey)
			if err != nil {
				t.Fatal(err)
			}
			cli.InstalledPath = script
			cli.DefaultArgs = tc.defaultArg
			cli.InstallStatus = "installed"
			cli.Healthy = true
			if err := repo.Update(cli); err != nil {
				t.Fatal(err)
			}
			stream := true
			agent, err := agents.CreateAgent(1, service.AgentInput{
				Name: "stream-" + tc.cliKey, CliKey: tc.cliKey, SystemPrompt: "do work",
				StreamOutput: &stream, TimeoutSec: 30,
			})
			if err != nil {
				t.Fatal(err)
			}
			agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
			run, err := agents.ManualRun(agent.ID, 1)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				got, _ := agents.GetRun(run.ID)
				if got != nil && (got.Status == model.JobSuccess || got.Status == model.JobFailed) {
					if got.Status != model.JobSuccess {
						t.Fatalf("status=%s err=%s", got.Status, got.ErrorMessage)
					}
					raw, err := os.ReadFile(argvFile)
					if err != nil {
						t.Fatal(err)
					}
					joined := string(raw)
					lines := strings.Fields(strings.ReplaceAll(joined, "\n", " "))
					hasArg := func(flag string) bool {
						for line := range strings.SplitSeq(joined, "\n") {
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
								t.Fatalf("argv should not contain %q; got:\n%s", bad, raw)
							}
						case "stream-json", "--json":
							if strings.Contains(joined, bad) {
								t.Fatalf("argv should not contain %q; got:\n%s", bad, raw)
							}
						case "--format":
							if hasArg("--format") || strings.Contains(joined, "--format json") {
								t.Fatalf("argv should not contain json format flag; got:\n%s", raw)
							}
						default:
							if strings.Contains(strings.Join(lines, " "), bad) {
								t.Fatalf("argv should not contain %q; got:\n%s", bad, raw)
							}
						}
					}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("timeout")
		})
	}
}

func TestAgentRunNonStreamOutputCLIArgs(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh required")
	}
	agents, _, repo, _ := setupAgentWorkspace(t)
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	script := filepath.Join(t.TempDir(), "fake-cli.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	cli, err := repo.FindByKey("reasonix")
	if err != nil {
		t.Fatal(err)
	}
	cli.InstalledPath = script
	cli.DefaultArgs = "run"
	cli.InstallStatus = "installed"
	cli.Healthy = true
	if err := repo.Update(cli); err != nil {
		t.Fatal(err)
	}
	stream := false
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "non-stream-rx", CliKey: "reasonix", SystemPrompt: "do work",
		StreamOutput: &stream, TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	run, err := agents.ManualRun(agent.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := agents.GetRun(run.ID)
		if got != nil && (got.Status == model.JobSuccess || got.Status == model.JobFailed) {
			if got.Status != model.JobSuccess {
				t.Fatalf("status=%s err=%s", got.Status, got.ErrorMessage)
			}
			raw, err := os.ReadFile(argvFile)
			if err != nil {
				t.Fatal(err)
			}
			hasArg := false
			for line := range strings.SplitSeq(string(raw), "\n") {
				if strings.TrimSpace(line) == "-p" {
					hasArg = true
					break
				}
			}
			if !hasArg {
				t.Fatalf("reasonix non-stream should pass -p; got:\n%s", raw)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout")
}
