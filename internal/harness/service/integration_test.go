package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/provider/oc"
)

// integrationProvider returns the provider for a real `opencode serve`,
// skipping when the opt-in environment is absent (same convention as the oc
// contract suite):
//
//	HARNESS_OC_TEST_ADDR (plus optional HARNESS_OC_TEST_PASSWORD / USERNAME)
func integrationProvider(t *testing.T) provider.Provider {
	t.Helper()
	addr := os.Getenv("HARNESS_OC_TEST_ADDR")
	if addr == "" {
		t.Skip("set HARNESS_OC_TEST_ADDR (and optionally HARNESS_OC_TEST_PASSWORD) to run integration tests against a real opencode serve")
	}
	adapter := oc.New(oc.Config{
		BaseURL:  addr,
		Password: os.Getenv("HARNESS_OC_TEST_PASSWORD"),
		Username: os.Getenv("HARNESS_OC_TEST_USERNAME"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := adapter.Client().Health(ctx); err != nil {
		t.Skipf("serve at %s unhealthy: %v", addr, err)
	}
	return adapter
}

// TestIntegrationSessionCRUDAndLazyRecovery covers the P3 acceptance against
// a real serve: agent-session create, active list, detail, archive limit,
// and lazy recovery through a fresh service instance.
func TestIntegrationSessionCRUDAndLazyRecovery(t *testing.T) {
	prov := integrationProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := t.TempDir()
	svc := NewSessionService(prov, SessionConfig{WorkspaceRoot: root, MaxSessionsPerDir: 2}, nil)

	session, err := svc.CreateAgentSession(ctx, AgentSpec{ID: 3, SystemPrompt: "p"}, 7)
	if err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	dir := filepath.Join(root, "agents", "agent-3")
	if session.Directory != dir || session.Agent != "bedrock-agent-3" {
		t.Fatalf("session = %+v", session)
	}

	// Create beyond the limit: the oldest sessions must get archived.
	for range 2 {
		if _, err := svc.CreateAgentSession(ctx, AgentSpec{ID: 3, SystemPrompt: "p"}, 7); err != nil {
			t.Fatalf("create follow-up session: %v", err)
		}
	}
	active, err := svc.ListActiveSessions(ctx, dir)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active = %d, want 2 (archive limit)", len(active))
	}

	// The first (oldest) session is archived but still resolvable by id.
	info, err := svc.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get archived session: %v", err)
	}
	if info.ArchivedAt == nil {
		t.Fatalf("oldest session not archived: %+v", info)
	}

	// Lazy recovery: a fresh service instance (bedrock restart) resolves the
	// sessions purely from the provider store.
	restarted := NewSessionService(prov, SessionConfig{WorkspaceRoot: root}, nil)
	recovered, err := restarted.GetSession(ctx, active[0].ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if recovered.Directory != dir {
		t.Fatalf("recovered directory = %q, want %q", recovered.Directory, dir)
	}
	if restartedActive, err := restarted.ListActiveSessions(ctx, dir); err != nil || len(restartedActive) != 2 {
		t.Fatalf("list after restart = %v (err %v)", restartedActive, err)
	}
}
