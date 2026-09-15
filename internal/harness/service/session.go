// Package service hosts the bedrock-side harness session services. The
// session service wraps a provider with bedrock policy: agent-bound session
// creation, an archive/count limit per workspace directory, and lazy recovery
// after restarts — the provider store is the only session registry, so every
// read goes to it on demand and nothing is rebuilt at boot.
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.uber.org/zap"

	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/provider/oc"
)

// DefaultMaxSessionsPerDir is the default cap of non-archived sessions kept
// per workspace directory; older ones are archived when a new session is
// created in the same directory.
const DefaultMaxSessionsPerDir = 20

// SessionConfig configures the SessionService.
type SessionConfig struct {
	// WorkspaceRoot is the bedrock agent workspace root. Agent sessions are
	// bound to {WorkspaceRoot}/agents/agent-{id}.
	WorkspaceRoot string
	// MaxSessionsPerDir overrides DefaultMaxSessionsPerDir (0 = default).
	MaxSessionsPerDir int
}

// AgentSpec is the agent projection CreateAgentSession needs. The ai domain
// maps its persisted agent onto it; the harness domain must not depend on ai.
type AgentSpec struct {
	ID            uint
	Name          string
	Description   string
	SystemPrompt  string
	SkillIDs      []uint
	ModelProvider string
	ModelID       string
	ApprovalMode  string // manual | auto (see provider/oc compile modes)
}

// AgentSessionDirectory returns the workspace directory an agent's sessions
// are bound to.
func AgentSessionDirectory(workspaceRoot string, agentID uint) string {
	return filepath.Join(workspaceRoot, "agents", fmt.Sprintf("agent-%d", agentID))
}

// AgentDefKey returns the compiled-artifact key of an agent
// (e.g. "agent-3" -> file bedrock-agent-3.md).
func AgentDefKey(agentID uint) string {
	return fmt.Sprintf("agent-%d", agentID)
}

// hasCustomization reports whether the agent needs a compiled definition;
// the predicate lives in one place (oc.NeedsAgentDef) and is shared with the
// compiler.
func (a AgentSpec) hasCustomization() bool {
	return oc.NeedsAgentDef(oc.AgentDefInput{
		SystemPrompt:  a.SystemPrompt,
		HasSkills:     len(a.SkillIDs) > 0,
		ModelProvider: a.ModelProvider,
		ModelID:       a.ModelID,
	})
}

// AgentDefName returns the opencode agent definition an agent's sessions
// use: the compiled bedrock-<key> artifact, or the builtin build agent when
// the agent has no customization (no prompt, no skills, no model override).
func AgentDefName(agent AgentSpec) string {
	if !agent.hasCustomization() {
		return "build"
	}
	return oc.AgentDefPrefix + AgentDefKey(agent.ID)
}

// SessionService is the provider-facing session facade with bedrock policy:
// CRUD passthrough, archive/count limit, and the agent-internal entry.
type SessionService struct {
	provider provider.Provider
	cfg      SessionConfig
	log      *zap.Logger
}

// NewSessionService builds a session service. A nil logger is replaced by a
// no-op logger.
func NewSessionService(prov provider.Provider, cfg SessionConfig, log *zap.Logger) *SessionService {
	if log == nil {
		log = zap.NewNop()
	}
	return &SessionService{provider: prov, cfg: cfg, log: log}
}

func (s *SessionService) maxSessionsPerDir() int {
	if s.cfg.MaxSessionsPerDir > 0 {
		return s.cfg.MaxSessionsPerDir
	}
	return DefaultMaxSessionsPerDir
}

// CreateSession passes a session create through to the provider, then
// enforces the per-directory archive limit.
func (s *SessionService) CreateSession(ctx context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	session, err := s.provider.CreateSession(ctx, input)
	if err != nil {
		return nil, err
	}
	s.enforceSessionLimit(ctx, input.Directory)
	return session, nil
}

// CreateAgentSession creates a session for an agent run or agent chat in the
// agent's workspace directory. userID is recorded for attribution (audit
// wiring lands with the REST layer).
func (s *SessionService) CreateAgentSession(ctx context.Context, agent AgentSpec, userID uint) (*provider.Session, error) {
	directory := AgentSessionDirectory(s.cfg.WorkspaceRoot, agent.ID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("harness session dir: %w", err)
	}
	input := provider.CreateSessionInput{
		Directory: directory,
		Agent:     AgentDefName(agent),
	}
	if agent.ModelProvider != "" && agent.ModelID != "" {
		input.Model = &provider.ModelRef{ProviderID: agent.ModelProvider, ID: agent.ModelID}
	}
	session, err := s.CreateSession(ctx, input)
	if err != nil {
		return nil, err
	}
	s.log.Info("harness agent session created",
		zap.Uint("agent_id", agent.ID),
		zap.Uint("user_id", userID),
		zap.String("session_id", session.ID),
		zap.String("agent_def", input.Agent),
	)
	return session, nil
}

// ListActiveSessions lists the non-archived sessions of a directory, newest
// first. Archived sessions are excluded; the provider stays the only store.
func (s *SessionService) ListActiveSessions(ctx context.Context, directory string) ([]provider.SessionInfo, error) {
	sessions, err := s.provider.ListSessions(ctx, directory)
	if err != nil {
		return nil, err
	}
	return activeSessions(sessions), nil
}

// GetSession fetches one session by id. This is also the lazy-recovery path:
// a session id (e.g. agent_runs.harness_session_id) resolves after any
// bedrock restart because the read always targets the provider store.
func (s *SessionService) GetSession(ctx context.Context, sessionID string) (*provider.SessionInfo, error) {
	return s.provider.GetSession(ctx, sessionID)
}

// ArchiveSession marks a session archived.
func (s *SessionService) ArchiveSession(ctx context.Context, sessionID string) error {
	return s.provider.ArchiveSession(ctx, sessionID)
}

// activeSessions filters archived entries and sorts the rest newest first.
func activeSessions(sessions []provider.SessionInfo) []provider.SessionInfo {
	out := make([]provider.SessionInfo, 0, len(sessions))
	for _, info := range sessions {
		if info.ArchivedAt == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// enforceSessionLimit archives the oldest non-archived sessions of the
// directory until at most MaxSessionsPerDir remain. Failures are logged and
// never fail the caller's create: archival is hygiene, not correctness.
func (s *SessionService) enforceSessionLimit(ctx context.Context, directory string) {
	sessions, err := s.provider.ListSessions(ctx, directory)
	if err != nil {
		s.log.Warn("harness session limit check failed",
			zap.String("directory", directory), zap.Error(err))
		return
	}
	active := activeSessions(sessions)
	if len(active) <= s.maxSessionsPerDir() {
		return
	}
	for _, info := range active[s.maxSessionsPerDir():] {
		if err := s.provider.ArchiveSession(ctx, info.ID); err != nil {
			s.log.Warn("harness session archive failed",
				zap.String("session_id", info.ID), zap.Error(err))
		}
	}
}
