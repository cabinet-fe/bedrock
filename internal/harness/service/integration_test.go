package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/provider/oc"
)

// integrationProvider returns the opencode adapter for a real
// `opencode serve`, skipping when the opt-in environment is absent (same
// convention as the oc contract suite):
//
//	HARNESS_OC_TEST_ADDR (plus optional HARNESS_OC_TEST_PASSWORD / USERNAME)
func integrationProvider(t *testing.T) *oc.Adapter {
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

// pollUntil retries check every 500ms until it returns true or the deadline
// passes (agent/skill discovery is asynchronous in a fresh directory).
func pollUntil(t *testing.T, ctx context.Context, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatalf("%s: ctx done: %v", what, ctx.Err())
		}
		if check() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s: not observed within 60s", what)
}

// TestIntegrationAgentDefCompileAndSkillInjection covers the P4 acceptance
// against a real serve: compiled bedrock-* agent definitions and injected
// skills are visible for the session's location, the session binds to the
// compiled definition, the .env artifact keeps its 0600 permission, and the
// permission.skill rule reflects approval_mode (auto=allow, manual=ask).
func TestIntegrationAgentDefCompileAndSkillInjection(t *testing.T) {
	adapter := integrationProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	svc := NewSessionService(adapter, SessionConfig{WorkspaceRoot: root}, nil)

	// Build a skill source dir; inject it into the auto-approval workspace.
	srcSkill := filepath.Join(root, "skill-src", "deploy-helper")
	if err := os.MkdirAll(srcSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	skillBody := "---\nname: deploy-helper\ndescription: deploy ops\n---\n\n# deploy-helper\n"
	if err := os.WriteFile(filepath.Join(srcSkill, "SKILL.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}

	autoAgent := AgentSpec{
		ID: 11, Name: "Deploy", Description: "deploys things",
		SystemPrompt: "你是部署助手。", SkillIDs: []uint{1}, ApprovalMode: oc.ApprovalModeAuto,
	}
	manualAgent := AgentSpec{
		ID: 12, Name: "Review", Description: "reviews things",
		SystemPrompt: "你是评审助手。", ApprovalMode: oc.ApprovalModeManual,
	}
	for _, agent := range []AgentSpec{autoAgent, manualAgent} {
		dir := AgentSessionDirectory(root, agent.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := SyncAgentDefinition(dir, agent); err != nil {
			t.Fatalf("compile agent %d: %v", agent.ID, err)
		}
		// .env is written by SyncAgentWorkspace at 0600; mirror it here.
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEPLOY_TOKEN=tok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	autoDir := AgentSessionDirectory(root, autoAgent.ID)
	if err := SyncAgentSkills(autoDir, []SkillSource{{Name: "Deploy Helper", Dir: srcSkill}}); err != nil {
		t.Fatal(err)
	}

	// Create sessions bound to the compiled definitions.
	autoSession, err := svc.CreateAgentSession(ctx, autoAgent, 1)
	if err != nil {
		t.Fatalf("create auto session: %v", err)
	}
	if autoSession.Agent != "bedrock-agent-11" {
		t.Fatalf("auto session agent = %q", autoSession.Agent)
	}
	manualSession, err := svc.CreateAgentSession(ctx, manualAgent, 1)
	if err != nil {
		t.Fatalf("create manual session: %v", err)
	}
	if manualSession.Agent != "bedrock-agent-12" {
		t.Fatalf("manual session agent = %q", manualSession.Agent)
	}

	// The .env artifact stays 0600 next to the session directory.
	info, err := os.Stat(filepath.Join(autoDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf(".env mode = %o, want 600", perm)
	}

	// Agent catalog: the compiled defs show up in their own directories
	// (discovery is async in a fresh directory, so poll) with the expected
	// permission.skill rules.
	waitForDefRules := func(directory, def string) []oc.AgentPermissionRule {
		var rules []oc.AgentPermissionRule
		pollUntil(t, ctx, def+" in catalog", func() bool {
			entries, err := adapter.Client().ListAgentCatalog(ctx, directory)
			if err != nil {
				t.Logf("list agents: %v", err)
				return false
			}
			for _, e := range entries {
				if e.Name == def {
					rules = e.Permissions
				}
			}
			return rules != nil
		})
		return rules
	}
	autoRules := waitForDefRules(autoDir, "bedrock-agent-11")
	manualRules := waitForDefRules(AgentSessionDirectory(root, manualAgent.ID), "bedrock-agent-12")
	if !hasSkillAllow(autoRules) {
		t.Fatalf("auto agent must carry permission.skill allow, got %+v", autoRules)
	}
	if hasSkillAllow(manualRules) {
		t.Fatalf("manual agent must not carry permission.skill allow, got %+v", manualRules)
	}

	// Skills: the injected skill is visible for the session location.
	pollUntil(t, ctx, "injected skill visible in catalog", func() bool {
		skills, err := adapter.Client().ListSkills(ctx, autoDir)
		if err != nil {
			t.Logf("list skills: %v", err)
			return false
		}
		for _, s := range skills {
			if s.Name == "deploy-helper" {
				return true
			}
		}
		return false
	})
}

// hasSkillAllow reports whether the resolved ruleset allows skills.
func hasSkillAllow(rules []oc.AgentPermissionRule) bool {
	for _, r := range rules {
		if r.Action == "skill" && (r.Effect == "allow" || r.Effect == "") {
			return true
		}
	}
	return false
}

// TestIntegrationBridgeSynthesizesIdleAfterTurn drives one real model turn
// through the stream bridge and pins the fix for the stuck "running" session
// view: opencode 1.18.x ends a turn with the last durable step_ended and
// never broadcasts session.idle, so the bridge's active probe must
// synthesize the idle frame and clear the busy flag.
func TestIntegrationBridgeSynthesizesIdleAfterTurn(t *testing.T) {
	prov := integrationProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	modelID := os.Getenv("HARNESS_OC_TEST_MODEL")
	if modelID == "" {
		modelID = "opencode/muse-spark-1.3-contributor-free"
	}
	parts := strings.SplitN(modelID, "/", 2)
	model := &provider.ModelRef{ProviderID: parts[0], ID: parts[1]}

	streams := NewStreamService(prov, StreamConfig{
		SettleDelay: 500 * time.Millisecond,
		SettlePoll:  500 * time.Millisecond,
	}, nil)
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	go streams.Run(sctx)

	session, err := prov.CreateSession(ctx, provider.CreateSessionInput{
		Directory: t.TempDir(),
		Model:     model,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, live, stop := streams.Subscribe(session.ID)
	defer stop()

	if _, err := prov.Prompt(ctx, session.ID, provider.PromptInput{
		Text: "只回复两个字母：ok", Delivery: provider.DeliveryQueue,
	}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	// Drain live frames until the turn's last durable step_ended passes,
	// then require the synthesized idle within the settle window.
	idleDeadline := time.Now().Add(90 * time.Second)
	stepEnded := false
	for time.Now().Before(idleDeadline) {
		select {
		case frame, ok := <-live:
			if !ok {
				t.Fatal("bridge stopped")
			}
			if frame.Kind == provider.FrameStatus && frame.Status != nil {
				switch frame.Status.Name {
				case provider.StatusStepEnded:
					stepEnded = true
				case provider.StatusIdle:
					if !stepEnded {
						t.Fatal("backend broadcast idle; this pin no longer holds")
					}
					if streams.SessionBusy(session.ID) {
						t.Fatal("busy flag must clear with the synthesized idle")
					}
					return
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(2 * time.Second):
			if stepEnded && !streams.SessionBusy(session.ID) {
				t.Fatal("session settled busy=false without an idle frame")
			}
		}
	}
	t.Fatal("no idle frame within 90s of the turn")
}
