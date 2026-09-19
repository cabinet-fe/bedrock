package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	authservice "bedrock/internal/auth/service"
	harnesshandler "bedrock/internal/harness/handler"
	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/service"
	"bedrock/internal/middleware"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

// wsEnv is a real HTTP server exposing the harness WS endpoint with a real
// auth + RBAC stack over sqlite.
type wsEnv struct {
	server     *httptest.Server
	fake       *fakeProvider
	stream     *service.StreamService
	auth       *authservice.AuthService
	gdb        *gorm.DB
	token      string // super-admin JWT
	plainToken string // non-super-admin JWT
}

func setupWS(t *testing.T, enabled bool) *wsEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: t.TempDir() + "/harness_ws_test.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration up: %v", err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("seed rbac resources: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	permSvc := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)
	authSvc, err := authservice.NewAuthService(&config.Config{JWT: config.JWTConfig{Secret: "ws-test-secret"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authSvc.GenerateTokenPair(&authmodel.User{ID: 1, Username: "admin", IsSuperAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	plainToken, _, err := authSvc.GenerateTokenPair(&authmodel.User{ID: 2, Username: "plain"})
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeProvider()
	sessions := service.NewSessionService(fake, service.SessionConfig{WorkspaceRoot: t.TempDir()}, nil)
	streams := service.NewStreamService(fake, service.StreamConfig{}, nil)
	streamCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go streams.Run(streamCtx)

	r := gin.New()
	ws := harnesshandler.NewWSHandler(authSvc, permSvc, sessions, streams, harnesshandler.HandlerConfig{Enabled: enabled}, middleware.DefaultCORSConfig(), nil)
	ws.RegisterRoutes(r)

	fake.sessions["ses_1"] = provider.SessionInfo{ID: "ses_1", Directory: "/tmp/x"}

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &wsEnv{server: server, fake: fake, stream: streams, auth: authSvc, gdb: gdb, token: token, plainToken: plainToken}
}

func dialWS(t *testing.T, env *wsEnv, path string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	dialer := &websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	return dialer.Dial("ws"+strings.TrimPrefix(env.server.URL, "http")+path, nil)
}

func readFrame(t *testing.T, conn *websocket.Conn) provider.Frame {
	t.Helper()
	var frame provider.Frame
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

func TestWSHandler_AuthAndGate(t *testing.T) {
	env := setupWS(t, true)

	// Missing token.
	if _, resp, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events"); err == nil {
		t.Fatal("expected dial failure for missing token")
	} else if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Invalid token.
	if _, resp, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token=bad"); err == nil {
		t.Fatal("expected dial failure for invalid token")
	} else if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", resp.StatusCode)
	}

	// Valid token, no RBAC permission (non-super-admin user without roles).
	if _, resp, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.plainToken); err == nil {
		t.Fatal("expected dial failure for user without harness_chat:view")
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for user without harness_chat:view, got %d", resp.StatusCode)
	}

	// Unknown session.
	if _, resp, err := dialWS(t, env, "/ws/harness/sessions/ses_missing/events?token="+env.token); err == nil {
		t.Fatal("expected dial failure for unknown session")
	} else if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	// Disabled backend answers 503 before upgrading.
	disabled := setupWS(t, false)
	if _, resp, err := dialWS(t, disabled, "/ws/harness/sessions/ses_1/events?token="+disabled.token); err == nil {
		t.Fatal("expected dial failure when disabled")
	} else if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when disabled, got %d", resp.StatusCode)
	}
}

// A non-super-admin user holding the seeded harness_chat:view must upgrade
// and receive frames (guards seed/handler code drift).
func TestWSHandler_GrantedUserUpgrades(t *testing.T) {
	env := setupWS(t, true)

	users := authrepo.NewUserRepository(env.gdb)
	roleSvc := rbacservice.NewRoleService(rbacrepo.NewRoleRepository(env.gdb), rbacrepo.NewResourceRepository(env.gdb))
	granted := &authmodel.User{Username: "harness_ws_user", PasswordHash: "hash", IsActive: true}
	if err := users.Create(granted); err != nil {
		t.Fatal(err)
	}
	role, err := roleSvc.Create("会话用户", "harness_chat_user", "", "", []string{"harness_chat:view"})
	if err != nil {
		t.Fatalf("create role with harness_chat:view (seed must expose it): %v", err)
	}
	if err := roleSvc.SetUserRoles(granted.ID, []uint{role.ID}); err != nil {
		t.Fatal(err)
	}
	grantedToken, _, err := env.auth.GenerateTokenPair(granted)
	if err != nil {
		t.Fatal(err)
	}

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+grantedToken)
	if err != nil {
		t.Fatalf("expected upgrade for granted user, got %v", err)
	}
	defer conn.Close()

	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t1", Text: "ok"}}
	frame := readFrame(t, conn)
	if frame.Seq != 1 || frame.MessageText.Text != "ok" {
		t.Fatalf("expected live frame seq=1 for granted user, got %+v", frame)
	}
}

func TestWSHandler_ReplayPendingAndLive(t *testing.T) {
	env := setupWS(t, true)

	// Fill the ring buffer: two durable frames and one pending permission ask.
	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t1", Text: "hello"}}
	env.fake.bus <- &provider.Frame{Seq: 2, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t2", Text: "world"}}
	env.fake.bus <- &provider.Frame{EventID: "p1", SessionID: "ses_1", Kind: provider.FramePermission,
		Permission: &provider.PermissionFrame{RequestID: "req_1", Action: "bash"}}
	waitFor(t, "ring buffer filled", func() bool {
		replay, _, _ := env.stream.Subscribe("ses_1")
		return len(replay) == 3
	})

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	first := readFrame(t, conn)
	if first.Seq != 1 || first.MessageText.Text != "hello" {
		t.Fatalf("expected replay frame seq=1, got %+v", first)
	}
	second := readFrame(t, conn)
	if second.Seq != 2 || second.MessageText.Text != "world" {
		t.Fatalf("expected replay frame seq=2, got %+v", second)
	}
	pending := readFrame(t, conn)
	if pending.Kind != provider.FramePermission || pending.Permission.RequestID != "req_1" {
		t.Fatalf("expected pending permission frame, got %+v", pending)
	}

	// Live frames after the replay baseline: a transient message delta, a
	// durable tool call and a transient question ask must all arrive.
	env.fake.bus <- &provider.Frame{EventID: "d1", SessionID: "ses_1", Kind: provider.FrameMessageDelta,
		MessageDelta: &provider.MessageDelta{TextID: "t3", Delta: "he"}}
	delta := readFrame(t, conn)
	if delta.Kind != provider.FrameMessageDelta || delta.MessageDelta.Delta != "he" {
		t.Fatalf("expected live message_delta frame, got %+v", delta)
	}
	env.fake.bus <- &provider.Frame{Seq: 3, SessionID: "ses_1", Kind: provider.FrameToolCall,
		ToolCall: &provider.ToolCall{CallID: "c1", Tool: "bash"}}
	tool := readFrame(t, conn)
	if tool.Seq != 3 || tool.ToolCall.Tool != "bash" {
		t.Fatalf("expected live tool_call frame seq=3, got %+v", tool)
	}
	env.fake.bus <- &provider.Frame{EventID: "q1", SessionID: "ses_1", Kind: provider.FrameQuestion,
		Question: &provider.QuestionFrame{RequestID: "q_9", Questions: []provider.Question{{Question: "ok?"}}}}
	ask := readFrame(t, conn)
	if ask.Kind != provider.FrameQuestion || ask.Question.RequestID != "q_9" {
		t.Fatalf("expected live question frame, got %+v", ask)
	}
}

func TestWSHandler_ReplayAfterBaselineFiltersDurableFrames(t *testing.T) {
	env := setupWS(t, true)

	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t1", Text: "one"}}
	env.fake.bus <- &provider.Frame{Seq: 2, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t2", Text: "two"}}
	waitFor(t, "ring buffer filled", func() bool {
		replay, _, _ := env.stream.Subscribe("ses_1")
		return len(replay) == 2
	})

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token+"&after=1")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	frame := readFrame(t, conn)
	if frame.Seq != 2 || frame.MessageText.Text != "two" {
		t.Fatalf("expected only seq=2 after baseline 1, got %+v", frame)
	}

	env.fake.bus <- &provider.Frame{Seq: 3, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t3", Text: "three"}}
	frame = readFrame(t, conn)
	if frame.Seq != 3 {
		t.Fatalf("expected live seq=3, got %+v", frame)
	}
}

func TestWSHandler_MultiConnectionNoDuplication(t *testing.T) {
	env := setupWS(t, true)

	conn1, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer conn1.Close()
	conn2, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer conn2.Close()

	// One bus event must arrive exactly once per connection.
	env.fake.bus <- &provider.Frame{Seq: 5, SessionID: "ses_1", Kind: provider.FrameToolCall,
		ToolCall: &provider.ToolCall{CallID: "c1", Tool: "bash"}}

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		frame := readFrame(t, conn)
		if frame.Seq != 5 || frame.Kind != provider.FrameToolCall {
			t.Fatalf("connection %d: expected one tool_call frame seq=5, got %+v", i+1, frame)
		}
		// The very next read must block: no duplicate delivery. Peek with a
		// short deadline instead of a plain read to fail fast.
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		var extra provider.Frame
		if err := conn.ReadJSON(&extra); err == nil {
			t.Fatalf("connection %d: unexpected duplicate frame %+v", i+1, extra)
		}
		_ = conn.SetReadDeadline(time.Time{})
	}
}

// waitForBusy polls the bridge busy flag with a longer horizon than the
// default settle delay needs.
func waitForBusy(t *testing.T, what string, env *wsEnv, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for env.stream.SessionBusy("ses_1") != want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A client joining after a turn whose replayed durable frames assert a
// running session (prompt/step starts, no replayable idle) gets the
// settled-idle snapshot after the baseline, so it does not stay stuck on
// the running state forever.
func TestWSHandler_SettledIdleSnapshotAfterRunningReplay(t *testing.T) {
	env := setupWS(t, true)

	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusPrompted}}
	env.fake.bus <- &provider.Frame{Seq: 2, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusStepStarted}}
	env.fake.bus <- &provider.Frame{Seq: 3, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusStepEnded}}
	waitFor(t, "ring buffer filled", func() bool {
		replay, _, _ := env.stream.Subscribe("ses_1")
		return len(replay) == 3
	})
	waitForBusy(t, "bridge settle", env, false)

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for seq := int64(1); seq <= 3; seq++ {
		if frame := readFrame(t, conn); frame.Seq != seq {
			t.Fatalf("replay frame seq = %d, want %d", frame.Seq, seq)
		}
	}
	snapshot := readFrame(t, conn)
	if snapshot.Kind != provider.FrameStatus || snapshot.Status == nil || snapshot.Status.Name != provider.StatusIdle {
		t.Fatalf("expected settled-idle snapshot, got %+v", snapshot)
	}
}

// A still-running session gets no snapshot: the live frames continue.
func TestWSHandler_NoSnapshotWhileBusy(t *testing.T) {
	env := setupWS(t, true)

	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusPrompted}}
	waitFor(t, "ring buffer filled", func() bool {
		replay, _, _ := env.stream.Subscribe("ses_1")
		return len(replay) == 1
	})
	waitForBusy(t, "bridge busy", env, true)

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if frame := readFrame(t, conn); frame.Seq != 1 {
		t.Fatalf("replay frame seq = %d, want 1", frame.Seq)
	}
	env.fake.bus <- &provider.Frame{Seq: 2, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusStepStarted}}
	if frame := readFrame(t, conn); frame.Seq != 2 || frame.Status.Name != provider.StatusStepStarted {
		t.Fatalf("expected live step_started, got %+v", frame)
	}
}

// A replay that already ends settled (error state) asserts no running, so
// no snapshot is needed.
func TestWSHandler_NoSnapshotWhenReplaySettled(t *testing.T) {
	env := setupWS(t, true)

	env.fake.bus <- &provider.Frame{Seq: 1, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusPrompted}}
	env.fake.bus <- &provider.Frame{Seq: 2, SessionID: "ses_1", Kind: provider.FrameStatus,
		Status: &provider.StatusFrame{Name: provider.StatusStepFailed, Error: "boom"}}
	waitFor(t, "ring buffer filled", func() bool {
		replay, _, _ := env.stream.Subscribe("ses_1")
		return len(replay) == 2
	})

	conn, _, err := dialWS(t, env, "/ws/harness/sessions/ses_1/events?token="+env.token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for seq := int64(1); seq <= 2; seq++ {
		if frame := readFrame(t, conn); frame.Seq != seq {
			t.Fatalf("replay frame seq = %d, want %d", frame.Seq, seq)
		}
	}
	env.fake.bus <- &provider.Frame{Seq: 3, SessionID: "ses_1", Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: "t9", Text: "after"}}
	if frame := readFrame(t, conn); frame.Seq != 3 {
		t.Fatalf("expected live frame seq=3 right after replay (no snapshot), got %+v", frame)
	}
}
