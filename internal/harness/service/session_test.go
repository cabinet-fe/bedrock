package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

// fakeProvider implements the provider surface the session service uses;
// unimplemented methods panic via the nil embed.
type fakeProvider struct {
	provider.Provider

	nextSeq    int
	sessions   map[string]provider.SessionInfo
	archived   []string
	listErr    error
	archiveErr error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{nextSeq: 1, sessions: map[string]provider.SessionInfo{}}
}

func (f *fakeProvider) CreateSession(_ context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	id := fmt.Sprintf("ses_%d", f.nextSeq)
	f.nextSeq++
	now := time.Now()
	f.sessions[id] = provider.SessionInfo{
		ID: id, Directory: input.Directory, Agent: input.Agent,
		Model: input.Model, CreatedAt: now, UpdatedAt: now,
	}
	return &provider.Session{ID: id, Directory: input.Directory, Agent: input.Agent, Model: input.Model}, nil
}

func (f *fakeProvider) ListSessions(_ context.Context, _ string) ([]provider.SessionInfo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]provider.SessionInfo, 0, len(f.sessions))
	for _, info := range f.sessions {
		out = append(out, info)
	}
	return out, nil
}

func (f *fakeProvider) GetSession(_ context.Context, sessionID string) (*provider.SessionInfo, error) {
	info, ok := f.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return &info, nil
}

func (f *fakeProvider) ArchiveSession(_ context.Context, sessionID string) error {
	if f.archiveErr != nil {
		return f.archiveErr
	}
	info, ok := f.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	at := time.Now()
	info.ArchivedAt = &at
	info.UpdatedAt = at
	f.sessions[sessionID] = info
	f.archived = append(f.archived, sessionID)
	return nil
}

func TestAgentDefName(t *testing.T) {
	tests := []struct {
		name  string
		agent AgentSpec
		want  string
	}{
		{name: "no customization uses builtin build", agent: AgentSpec{ID: 7}, want: "build"},
		{name: "system prompt compiles", agent: AgentSpec{ID: 7, SystemPrompt: "p"}, want: "bedrock-agent-7"},
		{name: "skills compile", agent: AgentSpec{ID: 7, SkillIDs: []uint{3}}, want: "bedrock-agent-7"},
		{name: "model override compiles", agent: AgentSpec{ID: 7, ModelID: "m"}, want: "bedrock-agent-7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentDefName(tt.agent); got != tt.want {
				t.Fatalf("AgentDefName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateAgentSession(t *testing.T) {
	root := t.TempDir()
	fake := newFakeProvider()
	svc := NewSessionService(fake, SessionConfig{WorkspaceRoot: root}, nil)

	session, err := svc.CreateAgentSession(context.Background(), AgentSpec{
		ID: 7, SystemPrompt: "p", ModelProvider: "prov", ModelID: "model",
	}, 42)
	if err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	wantDir := filepath.Join(root, "agents", "agent-7")
	if fi, err := os.Stat(wantDir); err != nil || !fi.IsDir() {
		t.Fatalf("agent workspace dir not created: %v", err)
	}
	if session.Directory != wantDir {
		t.Fatalf("directory = %q, want %q", session.Directory, wantDir)
	}
	if session.Agent != "bedrock-agent-7" {
		t.Fatalf("agent = %q, want bedrock-agent-7", session.Agent)
	}
	if session.Model == nil || session.Model.ProviderID != "prov" || session.Model.ID != "model" {
		t.Fatalf("model = %+v, want prov/model", session.Model)
	}

	plain, err := svc.CreateAgentSession(context.Background(), AgentSpec{ID: 8}, 1)
	if err != nil {
		t.Fatalf("create plain agent session: %v", err)
	}
	if plain.Agent != "build" {
		t.Fatalf("agent = %q, want build", plain.Agent)
	}
	if plain.Model != nil {
		t.Fatalf("model = %+v, want nil", plain.Model)
	}
}

func TestSessionLimitArchivesOldest(t *testing.T) {
	root := t.TempDir()
	fake := newFakeProvider()
	svc := NewSessionService(fake, SessionConfig{WorkspaceRoot: root, MaxSessionsPerDir: 3}, nil)
	dir := filepath.Join(root, "agents", "agent-1")

	var first string
	for i := range 5 {
		session, err := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: dir})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if i == 0 {
			first = session.ID
		}
		// Distinct, increasing timestamps so newest-first ordering is stable.
		info := fake.sessions[session.ID]
		info.CreatedAt = time.UnixMilli(int64(1000 + i))
		fake.sessions[session.ID] = info
	}

	active, err := svc.ListActiveSessions(context.Background(), dir)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("active = %d, want 3", len(active))
	}
	if len(fake.archived) != 2 || fake.archived[0] != first {
		t.Fatalf("archived = %v, want oldest two starting with %q", fake.archived, first)
	}
}

func TestSessionLimitKeepsArchivedOutOfCount(t *testing.T) {
	root := t.TempDir()
	fake := newFakeProvider()
	svc := NewSessionService(fake, SessionConfig{WorkspaceRoot: root, MaxSessionsPerDir: 2}, nil)
	dir := filepath.Join(root, "agents", "agent-1")

	a, _ := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: dir})
	b, _ := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: dir})
	if err := svc.ArchiveSession(context.Background(), a.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: dir}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// b + the new session stay active; the already-archived a is not counted.
	if len(fake.archived) != 1 {
		t.Fatalf("archived = %v, want only %q", fake.archived, a.ID)
	}
	if _, err := fake.GetSession(context.Background(), b.ID); err != nil {
		t.Fatalf("active session disappeared: %v", err)
	}
}

func TestLazyRecoveryAcrossRestart(t *testing.T) {
	root := t.TempDir()
	fake := newFakeProvider()
	first := NewSessionService(fake, SessionConfig{WorkspaceRoot: root}, nil)
	session, err := first.CreateAgentSession(context.Background(), AgentSpec{ID: 9, SystemPrompt: "p"}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A fresh service instance (bedrock restart) resolves the session purely
	// from the provider store.
	second := NewSessionService(fake, SessionConfig{WorkspaceRoot: root}, nil)
	info, err := second.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if info.ID != session.ID || info.Directory != session.Directory {
		t.Fatalf("info = %+v", info)
	}
	active, err := second.ListActiveSessions(context.Background(), filepath.Join(root, "agents", "agent-9"))
	if err != nil || len(active) != 1 || active[0].ID != session.ID {
		t.Fatalf("list after restart = %v (err %v)", active, err)
	}
}

func TestEnforceSessionLimitFailuresAreLoggedNotReturned(t *testing.T) {
	root := t.TempDir()
	fake := newFakeProvider()
	fake.listErr = errors.New("boom")
	svc := NewSessionService(fake, SessionConfig{WorkspaceRoot: root, MaxSessionsPerDir: 1}, nil)
	if _, err := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: root}); err != nil {
		t.Fatalf("create must not fail on limit-check error: %v", err)
	}

	fake.listErr = nil
	fake.archiveErr = errors.New("boom")
	if _, err := svc.CreateSession(context.Background(), provider.CreateSessionInput{Directory: root}); err != nil {
		t.Fatalf("create must not fail on archive error: %v", err)
	}
}

func TestParseAgentSessionDirectory(t *testing.T) {
	root := t.TempDir()
	if id, ok := ParseAgentSessionDirectory(root, AgentSessionDirectory(root, 42)); !ok || id != 42 {
		t.Fatalf("agent dir parse = (%d, %v), want (42, true)", id, ok)
	}
	for _, dir := range []string{
		filepath.Join(root, "agents", "agent-"),
		filepath.Join(root, "agents", "agent-x"),
		filepath.Join(root, "agents", "agent-0"),
		filepath.Join(root, "agents", "other-1"),
		filepath.Join(root, "harness", "users", "user-1"),
		filepath.Join(root, "agents"),
	} {
		if id, ok := ParseAgentSessionDirectory(root, dir); ok {
			t.Fatalf("parse(%q) = (%d, true), want unresolved", dir, id)
		}
	}
	// A different root's agent directory does not resolve.
	other := t.TempDir()
	if _, ok := ParseAgentSessionDirectory(other, AgentSessionDirectory(root, 3)); ok {
		t.Fatal("foreign root must not resolve")
	}
}
