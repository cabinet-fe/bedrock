package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"bedrock/internal/ai/model"
	"bedrock/internal/engine"
	resourcemodel "bedrock/internal/resource/model"
)

// RepositoryFinder loads code repositories for agent workspace checkouts.
type RepositoryFinder interface {
	FindByID(id uint) (*resourcemodel.Repository, error)
}

// SecretResolver decrypts credentials for git auth (never exposed via API).
type SecretResolver interface {
	Resolve(id uint) (typ, username, secret, passphrase string, err error)
}

// GitCheckoutFunc clones or updates a repository into workDir at branch.
type GitCheckoutFunc func(ctx context.Context, workDir, repoURL, authType, username, password, branch string, logFn func(string)) error

// SetRepoCheckoutDeps wires repository + credential resolution for SyncAgentWorkspace.
func (s *AgentService) SetRepoCheckoutDeps(repos RepositoryFinder, secrets SecretResolver) {
	s.repos = repos
	s.secrets = secrets
}

// SetGitCheckout overrides the git clone/pull implementation (tests).
func (s *AgentService) SetGitCheckout(fn GitCheckoutFunc) {
	s.gitCheckout = fn
}

func (s *AgentService) agentRoot(agentID uint) string {
	return filepath.Join(s.workDir, "agents", fmt.Sprintf("agent-%d", agentID))
}

// enqueueWorkspaceInit starts async SyncAgentWorkspace for an agent.
// Concurrent inits for the same agent are serialized by generation: only the
// latest completion may write ready/failed status.
func (s *AgentService) enqueueWorkspaceInit(agentID, userID uint) {
	s.wsInitMu.Lock()
	s.wsInitGen[agentID]++
	gen := s.wsInitGen[agentID]
	s.wsInitMu.Unlock()
	s.wsInitWg.Add(1)
	go func() {
		defer s.wsInitWg.Done()
		s.initAgentWorkspace(agentID, userID, gen)
	}()
}

func (s *AgentService) initAgentWorkspace(agentID, userID uint, gen uint64) {
	agent, err := s.repo.FindAgent(agentID)
	if err != nil {
		return
	}
	decodeSkillIDs(agent)
	if err := s.attachRepoBindings(agent); err != nil {
		s.finishWorkspaceInit(agentID, gen, err)
		return
	}
	_, _, err = s.SyncAgentWorkspace(agent, userID, true)
	s.finishWorkspaceInit(agentID, gen, err)
}

func (s *AgentService) finishWorkspaceInit(agentID uint, gen uint64, syncErr error) {
	s.wsInitMu.Lock()
	current := s.wsInitGen[agentID]
	s.wsInitMu.Unlock()
	if gen != current {
		return
	}
	fields := map[string]any{
		"workspace_error":  "",
		"workspace_status": model.WorkspaceReady,
	}
	if syncErr != nil {
		fields["workspace_status"] = model.WorkspaceFailed
		fields["workspace_error"] = syncErr.Error()
		if s.logger != nil {
			s.logger.Warn("agent workspace init failed",
				zap.Uint("agent_id", agentID), zap.Error(syncErr))
		}
	}
	_ = s.repo.UpdateAgentFields(agentID, fields)
}

// repoDirName returns the checkout directory name for a repository+branch binding:
// repo-{repositoryID}-{sanitizedBranch}.
func repoDirName(repositoryID uint, branch string) string {
	return fmt.Sprintf("repo-%d-%s", repositoryID, sanitizeBranchForDir(branch))
}

// sanitizeBranchForDir turns a git branch name into a safe single path segment.
func sanitizeBranchForDir(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	var b strings.Builder
	prevDash := false
	for _, r := range branch {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_'
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "branch"
	}
	const maxLen = 100
	if len(s) > maxLen {
		s = strings.Trim(s[:maxLen], "-")
		if s == "" {
			s = "branch"
		}
	}
	return s
}

// SyncAgentWorkspace ensures the persistent agent directory layout:
// skills under .agents/skills, repo-{id}-{branch} checkouts for bindings, SYSTEM_PROMPT.md.
// repoDirs are absolute paths of successfully synced repository checkouts (for run logs).
func (s *AgentService) SyncAgentWorkspace(agent *model.AiAgent, userID uint, isSuperAdmin bool) (digests map[uint]string, repoDirs []string, err error) {
	if agent == nil {
		return nil, nil, fmt.Errorf("agent is nil")
	}
	decodeSkillIDs(agent)
	if err := s.attachRepoBindings(agent); err != nil {
		return nil, nil, err
	}

	root := s.agentRoot(agent.ID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, nil, err
	}

	skillsRoot := filepath.Join(root, ".agents", "skills")
	_ = os.RemoveAll(skillsRoot)
	digests = map[uint]string{}
	if s.skills != nil {
		digests, err = s.skills.InjectSkills(root, agent.SkillIDs, userID, isSuperAdmin)
		if err != nil {
			return nil, nil, err
		}
	}

	repoDirs, err = s.syncRepoCheckouts(root, agent.RepoBindings)
	if err != nil {
		return nil, nil, err
	}
	// Drop legacy per-job OpenCode external_directory configs from the prior approach.
	_ = os.Remove(filepath.Join(root, "opencode.json"))

	promptPath := filepath.Join(root, "SYSTEM_PROMPT.md")
	if err := os.WriteFile(promptPath, []byte(agent.SystemPrompt), 0o644); err != nil {
		return nil, nil, err
	}
	// 解密写入工作区 .env（同 UID 可见）；Run 时还会注入 cmd.Env。
	if _, _, err := s.writeAgentEnvFile(agent, root); err != nil {
		return nil, nil, err
	}
	return digests, repoDirs, nil
}

func (s *AgentService) syncRepoCheckouts(agentRoot string, bindings []model.RepoBinding) ([]string, error) {
	wanted := map[string]bool{}
	for _, b := range bindings {
		wanted[repoDirName(b.RepositoryID, b.Branch)] = true
	}

	entries, err := os.ReadDir(agentRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "job-") {
			_ = os.RemoveAll(filepath.Join(agentRoot, name))
			continue
		}
		if strings.HasPrefix(name, "repo-") && !wanted[name] {
			_ = os.RemoveAll(filepath.Join(agentRoot, name))
		}
	}

	if len(bindings) == 0 {
		return nil, nil
	}
	if s.repos == nil {
		return nil, fmt.Errorf("repository finder not configured")
	}

	checkout := s.gitCheckout
	if checkout == nil {
		checkout = engine.GitCloneOrPull
	}

	var synced []string
	for _, b := range bindings {
		repo, err := s.repos.FindByID(b.RepositoryID)
		if err != nil {
			return nil, fmt.Errorf("仓库 %d 不存在: %w", b.RepositoryID, err)
		}
		authType, username, password, err := s.resolveRepoGitAuth(repo)
		if err != nil {
			return nil, fmt.Errorf("仓库 %d 凭证错误: %w", b.RepositoryID, err)
		}
		dest := filepath.Join(agentRoot, repoDirName(b.RepositoryID, b.Branch))
		logFn := func(line string) {
			if s.logger != nil {
				s.logger.Info("agent git", zap.Uint("repository_id", b.RepositoryID), zap.String("line", line))
			}
		}
		if err := checkout(context.Background(), dest, repo.RepoURL, authType, username, password, b.Branch, logFn); err != nil {
			return nil, fmt.Errorf("同步仓库 %d (%s) 失败: %w", b.RepositoryID, b.Branch, err)
		}
		absDest, err := filepath.Abs(dest)
		if err != nil {
			absDest = dest
		}
		synced = append(synced, absDest)
	}
	return synced, nil
}

func (s *AgentService) resolveRepoGitAuth(repo *resourcemodel.Repository) (authType, username, password string, err error) {
	switch strings.ToLower(strings.TrimSpace(repo.AuthType)) {
	case "", "none":
		return "none", "", "", nil
	case "credential":
		if repo.CredentialID == nil || *repo.CredentialID == 0 {
			return "", "", "", fmt.Errorf("repository credential is empty")
		}
		if s.secrets == nil {
			return "", "", "", fmt.Errorf("secret resolver not configured")
		}
		typ, user, secret, _, err := s.secrets.Resolve(*repo.CredentialID)
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

// appendNonStreamingOutputArgs prefers final/summary output for CLIs that support it.
// Human-readable incremental streaming is the default for non-interactive runs; do not
// add JSON/NDJSON flags here — those are machine-oriented and look ugly in log UIs.
func appendNonStreamingOutputArgs(cliKey string, args []string) []string {
	switch cliKey {
	case "reasonix":
		return append(args, "-p")
	default:
		return args
	}
}

// appendFullPermissionArgs enables each CLI's broad / bypass-sandbox mode so
// nested repo-* checkouts under the agent workspace are fully usable. Scope is
// enforced via prompt splicing (agentWorkspaceScopeHint), not per-directory allow lists.
func appendFullPermissionArgs(cliKey string, args []string) []string {
	switch cliKey {
	case "claude_code", "opencode":
		return append(args, "--dangerously-skip-permissions")
	case "reasonix":
		// reasonix run accepts --permission-mode, not --dangerously-skip-permissions.
		return append(args, "--permission-mode", "bypassPermissions")
	case "codex":
		return append(args, "--dangerously-bypass-approvals-and-sandbox")
	default:
		return args
	}
}

// agentWorkspaceScopeHint asks the CLI to stay inside the persistent
// BEDROCK_AGENT_WORKDIR and write deliverables into BEDROCK_AGENT_OUTPUT.
func agentWorkspaceScopeHint() string {
	return "你的工作目录是 $BEDROCK_AGENT_WORKDIR（agents 下本智能体目录）。" +
		"该目录是跨 Run 复用的持久工作区；不要删除其中已有文件，除非明确需要。" +
		"只能在该目录内读写；通过 ./repo-{id}-{branch} 访问绑定仓库代码。" +
		"禁止访问该目录之外的任意路径。" +
		"请将需交付的文件写入 $BEDROCK_AGENT_OUTPUT（本智能体固定产出目录，默认 ./output；跨 Run 保留，不清空）。" +
		" Your working directory is $BEDROCK_AGENT_WORKDIR (this agent under agents/)." +
		" This persistent workspace is reused across runs; do not delete existing files unless required." +
		" Read/write only inside it; access bound repository code via ./repo-{id}-{branch}." +
		" Do not access any path outside this directory." +
		" Write deliverable files into $BEDROCK_AGENT_OUTPUT (this agent's fixed output directory; preserved across runs)."
}

func (s *AgentService) removeAgentWorkspace(agentID uint) {
	_ = os.RemoveAll(s.agentRoot(agentID))
}

func dirHasRegularFiles(dir string) (bool, error) {
	has := false
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info != nil && info.Mode().IsRegular() {
			has = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return has, nil
}

// resolveAgentOutputDir returns the fixed per-agent output directory under the
// persistent agent root. It never creates per-run subdirectories.
func resolveAgentOutputDir(agentRoot, outputDir string) (string, error) {
	rel := strings.TrimSpace(outputDir)
	if rel == "" {
		rel = "output"
	}
	rel = filepath.Clean(rel)
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("无效的 output_dir: %q", outputDir)
	}
	out := filepath.Join(agentRoot, rel)
	relToRoot, err := filepath.Rel(agentRoot, out)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("output_dir 越出 Agent 工作区: %q", outputDir)
	}
	return out, nil
}

// prepareAgentOutputDir ensures the fixed output directory exists. Previous
// contents are preserved across runs so agents can reuse caches and
// incremental deliverables. The agent root itself is never cleared either.
func prepareAgentOutputDir(outputDir string) error {
	return os.MkdirAll(outputDir, 0o755)
}

func (s *AgentService) attachRepoBindings(agent *model.AiAgent) error {
	if agent == nil {
		return nil
	}
	rows, err := s.repo.ListAgentRepoBindings(agent.ID)
	if err != nil {
		return err
	}
	bindings := make([]model.RepoBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, model.RepoBinding{
			RepositoryID: row.RepositoryID, Branch: row.Branch,
		})
	}
	agent.RepoBindings = bindings
	return nil
}

func (s *AgentService) normalizeRepoBindings(in []model.RepoBinding) ([]model.RepoBinding, error) {
	if in == nil {
		return []model.RepoBinding{}, nil
	}
	type bindingKey struct {
		repoID uint
		branch string
	}
	seen := map[bindingKey]struct{}{}
	out := make([]model.RepoBinding, 0, len(in))
	for _, b := range in {
		if b.RepositoryID == 0 {
			return nil, fmt.Errorf("repository_id 不能为空")
		}
		branch := strings.TrimSpace(b.Branch)
		if branch == "" {
			branch = "main"
		}
		key := bindingKey{repoID: b.RepositoryID, branch: branch}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("同一智能体内仓库与分支不能重复绑定")
		}
		seen[key] = struct{}{}
		if s.repos != nil {
			if _, err := s.repos.FindByID(b.RepositoryID); err != nil {
				return nil, fmt.Errorf("仓库不存在: %d", b.RepositoryID)
			}
		}
		out = append(out, model.RepoBinding{RepositoryID: b.RepositoryID, Branch: branch})
	}
	return out, nil
}
