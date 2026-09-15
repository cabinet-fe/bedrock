package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	harnesshandler "bedrock/internal/harness/handler"
	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	systemrepo "bedrock/internal/system/repository"
	systemservice "bedrock/internal/system/service"
)

// fakeProvider implements the provider surface the handlers consume; the
// nil-embedded interface panics on anything the tests never call.
type fakeProvider struct {
	provider.Provider

	bus          chan *provider.Frame
	sessions     map[string]provider.SessionInfo
	createDirs   []string
	prompts      chan promptCall
	interrupts   chan string
	permReplies  chan permReplyCall
	permErr      error
	questReplies chan questionReplyCall
}

type promptCall struct {
	sessionID string
	input     provider.PromptInput
}

type permReplyCall struct {
	sessionID string
	requestID string
	reply     provider.PermissionReply
}

type questionReplyCall struct {
	sessionID string
	requestID string
	answers   provider.QuestionAnswers
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		bus:          make(chan *provider.Frame, 64),
		sessions:     map[string]provider.SessionInfo{},
		prompts:      make(chan promptCall, 8),
		interrupts:   make(chan string, 8),
		permReplies:  make(chan permReplyCall, 8),
		questReplies: make(chan questionReplyCall, 8),
	}
}

func (f *fakeProvider) notFound(sessionID string) error {
	return fmt.Errorf("session %s: %w: no such session", sessionID, provider.ErrNotFound)
}

func (f *fakeProvider) CreateSession(_ context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	id := fmt.Sprintf("ses_%d", len(f.sessions)+1)
	f.createDirs = append(f.createDirs, input.Directory)
	now := time.Now()
	f.sessions[id] = provider.SessionInfo{ID: id, Directory: input.Directory, Agent: input.Agent, CreatedAt: now, UpdatedAt: now}
	return &provider.Session{ID: id, Directory: input.Directory, Agent: input.Agent}, nil
}

func (f *fakeProvider) ListSessions(_ context.Context, directory string) ([]provider.SessionInfo, error) {
	var out []provider.SessionInfo
	for _, info := range f.sessions {
		if info.Directory == directory {
			out = append(out, info)
		}
	}
	return out, nil
}

func (f *fakeProvider) GetSession(_ context.Context, sessionID string) (*provider.SessionInfo, error) {
	info, ok := f.sessions[sessionID]
	if !ok {
		return nil, f.notFound(sessionID)
	}
	return &info, nil
}

func (f *fakeProvider) ArchiveSession(_ context.Context, sessionID string) error { return nil }

func (f *fakeProvider) Prompt(_ context.Context, sessionID string, input provider.PromptInput) (*provider.PromptAck, error) {
	if _, ok := f.sessions[sessionID]; !ok {
		return nil, f.notFound(sessionID)
	}
	f.prompts <- promptCall{sessionID: sessionID, input: input}
	return &provider.PromptAck{MessageID: "msg_1", AdmittedSeq: 7}, nil
}

func (f *fakeProvider) History(_ context.Context, sessionID string) ([]provider.Message, error) {
	if _, ok := f.sessions[sessionID]; !ok {
		return nil, f.notFound(sessionID)
	}
	return []provider.Message{
		{ID: "m1", Role: "user", Content: json.RawMessage(`[{"type":"text","text":"hi"}]`)},
		{ID: "m2", Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hello"}]`)},
	}, nil
}

func (f *fakeProvider) Interrupt(_ context.Context, sessionID string) error {
	if _, ok := f.sessions[sessionID]; !ok {
		return f.notFound(sessionID)
	}
	f.interrupts <- sessionID
	return nil
}

func (f *fakeProvider) ReplyPermission(_ context.Context, sessionID, requestID string, reply provider.PermissionReply) error {
	if f.permErr != nil {
		return f.permErr
	}
	f.permReplies <- permReplyCall{sessionID: sessionID, requestID: requestID, reply: reply}
	return nil
}

func (f *fakeProvider) ReplyQuestion(_ context.Context, sessionID, requestID string, answers provider.QuestionAnswers) error {
	f.questReplies <- questionReplyCall{sessionID: sessionID, requestID: requestID, answers: answers}
	return nil
}

func (f *fakeProvider) RejectQuestion(_ context.Context, _, _ string) error { return nil }

func (f *fakeProvider) ListModels(_ context.Context, _ string) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "claude-sonnet", ProviderID: "anthropic", Name: "Sonnet"}}, nil
}

func (f *fakeProvider) ListAgents(_ context.Context, _ string) ([]provider.AgentInfo, error) {
	return []provider.AgentInfo{{Name: "build", Native: true}, {Name: "bedrock-agent-1"}}, nil
}

func (f *fakeProvider) Export(_ context.Context, sessionID string) ([]byte, error) {
	if _, ok := f.sessions[sessionID]; !ok {
		return nil, f.notFound(sessionID)
	}
	return []byte("{\"id\":\"m1\",\"role\":\"user\"}\n"), nil
}

func (f *fakeProvider) BusStream(context.Context) (provider.Stream, error) {
	return chanStream{ch: f.bus}, nil
}

type chanStream struct{ ch <-chan *provider.Frame }

func (c chanStream) Next(ctx context.Context) (*provider.Frame, error) {
	select {
	case f := <-c.ch:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (chanStream) Close() error { return nil }

type testEnv struct {
	router *gin.Engine
	fake   *fakeProvider
	stream *service.StreamService
	audit  *systemservice.AuditService
	gdb    *gorm.DB
}

// setup builds a router with a real RBAC stack over sqlite and the mock auth
// middleware (X-Test-User-ID / X-Test-Is-Admin headers), mirroring the ai
// handler tests.
func setup(t *testing.T, enabled bool) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}

	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "harness_handler_test.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration up: %v", err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("seed rbac resources: %v", err)
	}

	permSvc := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)
	auditSvc := systemservice.NewAuditService(systemrepo.NewOperationLogRepository(gdb))

	fake := newFakeProvider()
	root := t.TempDir()
	sessions := service.NewSessionService(fake, service.SessionConfig{WorkspaceRoot: root}, nil)
	streams := service.NewStreamService(fake, service.StreamConfig{}, nil)
	streamCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go streams.Run(streamCtx)

	h := harnesshandler.NewHandler(sessions, streams, permSvc, auditSvc, harnesshandler.HandlerConfig{Enabled: enabled}, nil)

	r := gin.New()
	api := r.Group("/api/v1")
	authMW := func(c *gin.Context) {
		uid := c.GetHeader("X-Test-User-ID")
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
			return
		}
		var userID uint
		if _, err := fmt.Sscanf(uid, "%d", &userID); err != nil || userID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
			return
		}
		c.Set("user_id", userID)
		c.Set("username", "tester")
		c.Set("is_super_admin", c.GetHeader("X-Test-Is-Admin") == "true")
		c.Next()
	}
	h.RegisterRoutes(api, authMW)
	return &testEnv{router: r, fake: fake, stream: streams, audit: auditSvc, gdb: gdb}
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandler_ForbiddenWithoutPermission(t *testing.T) {
	env := setup(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/harness/sessions", nil)
	req.Header.Set("X-Test-User-ID", "2")
	req.Header.Set("X-Test-Is-Admin", "false")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user without harness_chat:view, got %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/harness/sessions", strings.NewReader(`{}`))
	req.Header.Set("X-Test-User-ID", "2")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user without harness_chat:send, got %d body=%s", w.Code, w.Body.String())
	}
}

// A non-super-admin user holding the seeded harness_chat codes must pass the
// enforced permission gates (guards seed/handler code drift).
func TestHandler_AllowedWithGrantedPermission(t *testing.T) {
	env := setup(t, true)

	users := authrepo.NewUserRepository(env.gdb)
	roleSvc := rbacservice.NewRoleService(rbacrepo.NewRoleRepository(env.gdb), rbacrepo.NewResourceRepository(env.gdb))
	granted := &authmodel.User{Username: "harness_user", PasswordHash: "hash", IsActive: true}
	if err := users.Create(granted); err != nil {
		t.Fatal(err)
	}
	role, err := roleSvc.Create("会话用户", "harness_chat_user", "", "",
		[]string{"harness_chat:view", "harness_chat:send", "harness_chat:approve"})
	if err != nil {
		t.Fatalf("create role with harness_chat permissions (seed must expose them): %v", err)
	}
	if err := roleSvc.SetUserRoles(granted.ID, []uint{role.ID}); err != nil {
		t.Fatal(err)
	}

	doAs := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body == "" {
			reader = bytes.NewReader(nil)
		} else {
			reader = bytes.NewReader([]byte(body))
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("X-Test-User-ID", fmt.Sprintf("%d", granted.ID))
		req.Header.Set("X-Test-Is-Admin", "false")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		return w
	}

	if w := doAs(http.MethodGet, "/api/v1/harness/sessions", ""); w.Code != http.StatusOK {
		t.Fatalf("granted user list: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if w := doAs(http.MethodPost, "/api/v1/harness/sessions", `{}`); w.Code != http.StatusCreated {
		t.Fatalf("granted user create: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if w := doAs(http.MethodGet, "/api/v1/harness/models", ""); w.Code != http.StatusOK {
		t.Fatalf("granted user models: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_DisabledReturns503(t *testing.T) {
	env := setup(t, false)
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/harness/sessions", `{}`},
		{http.MethodPost, "/api/v1/harness/sessions/ses_1/messages", `{"text":"hi"}`},
		{http.MethodPost, "/api/v1/harness/sessions/ses_1/interrupt", ""},
		{http.MethodPost, "/api/v1/harness/sessions/ses_1/permissions/req_1", `{"reply":"once"}`},
		{http.MethodPost, "/api/v1/harness/sessions/ses_1/questions/req_1", `{"answers":[["yes"]]}`},
		{http.MethodGet, "/api/v1/harness/sessions", ""},
	}
	for _, tc := range cases {
		w := do(t, env.router, tc.method, tc.path, tc.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: expected 503 when disabled, got %d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "harness-unavailable") {
			t.Fatalf("%s %s: expected harness-unavailable message, got %s", tc.method, tc.path, w.Body.String())
		}
	}
}

func TestHandler_CreateSessionRejectsDirectoryInput(t *testing.T) {
	env := setup(t, true)
	for _, key := range []string{"directory", "cwd", "location"} {
		w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions", fmt.Sprintf(`{%q:"/etc"}`, key))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("request with %s: expected 400, got %d body=%s", key, w.Code, w.Body.String())
		}
	}
	if len(env.fake.createDirs) != 0 {
		t.Fatalf("expected no provider create for rejected bodies, got %v", env.fake.createDirs)
	}
}

func TestHandler_CreateSessionUsesServerDirectory(t *testing.T) {
	env := setup(t, true)
	w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions", `{"agent":"build"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if len(env.fake.createDirs) != 1 {
		t.Fatalf("expected one provider create, got %v", env.fake.createDirs)
	}
	dir := env.fake.createDirs[0]
	if !strings.HasSuffix(dir, filepath.Join("harness", "users", "user-1")) {
		t.Fatalf("expected server-derived user directory, got %s", dir)
	}
}

func TestHandler_ListAndGetSessions(t *testing.T) {
	env := setup(t, true)
	do(t, env.router, http.MethodPost, "/api/v1/harness/sessions", `{}`)

	w := do(t, env.router, http.MethodGet, "/api/v1/harness/sessions?page=1&page_size=20", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var page struct {
		Data struct {
			Items []struct {
				ID        string `json:"id"`
				CreatedAt string `json:"created_at"`
				Directory string `json:"directory"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if page.Data.Total == 0 || len(page.Data.Items) == 0 {
		t.Fatalf("expected one session, got total=%d items=%d", page.Data.Total, len(page.Data.Items))
	}
	if page.Data.Items[0].CreatedAt == "" {
		t.Fatal("expected snake_case created_at field")
	}

	w = do(t, env.router, http.MethodGet, "/api/v1/harness/sessions/ses_1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, env.router, http.MethodGet, "/api/v1/harness/sessions/ses_missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_MessagesPromptInterrupt(t *testing.T) {
	env := setup(t, true)
	do(t, env.router, http.MethodPost, "/api/v1/harness/sessions", `{}`)

	w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/messages", `{"text":"hi","delivery":"steer"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	select {
	case call := <-env.fake.prompts:
		if call.input.Text != "hi" || call.input.Delivery != provider.DeliverySteer {
			t.Fatalf("unexpected prompt call: %+v", call.input)
		}
	default:
		t.Fatal("expected provider prompt call")
	}

	if w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/messages", `{"text":"","delivery":"bogus"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty text / bad delivery, got %d", w.Code)
	}

	w = do(t, env.router, http.MethodGet, "/api/v1/harness/sessions/ses_1/messages", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("expected two messages, got %s", w.Body.String())
	}

	if w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/interrupt", ""); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	select {
	case id := <-env.fake.interrupts:
		if id != "ses_1" {
			t.Fatalf("unexpected interrupt target %s", id)
		}
	default:
		t.Fatal("expected provider interrupt call")
	}
}

func TestHandler_ReplyPermissionAudited(t *testing.T) {
	env := setup(t, true)
	env.fake.bus <- &provider.Frame{
		EventID: "e1", SessionID: "ses_1", Kind: provider.FramePermission,
		Permission: &provider.PermissionFrame{RequestID: "req_1", Action: "bash"},
	}
	waitFor(t, "pending permission", func() bool {
		return len(env.stream.PendingRequests("ses_1")) == 1
	})

	w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/permissions/req_1", `{"reply":"once"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	select {
	case call := <-env.fake.permReplies:
		if call.reply != provider.PermissionOnce {
			t.Fatalf("unexpected reply %s", call.reply)
		}
	default:
		t.Fatal("expected provider reply call")
	}

	logs, total, err := env.audit.List(systemrepo.OperationLogFilters{ListQuery: pkg.ListQuery{Page: 1, PageSize: 10}, Action: "harness_permission_reply"})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected one audit row, got total=%d rows=%d", total, len(logs))
	}
	if logs[0].ResourceID != "ses_1" || !strings.Contains(logs[0].Details, "req_1") {
		t.Fatalf("unexpected audit row: %+v", logs[0])
	}

	// The pending entry is consumed: a second reply is a 409.
	if w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/permissions/req_1", `{"reply":"once"}`); w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for non-pending reply, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_ReplyQuestionAudited(t *testing.T) {
	env := setup(t, true)
	env.fake.bus <- &provider.Frame{
		EventID: "e2", SessionID: "ses_1", Kind: provider.FrameQuestion,
		Question: &provider.QuestionFrame{RequestID: "q_1", Questions: []provider.Question{{Question: "proceed?"}}},
	}
	waitFor(t, "pending question", func() bool {
		return len(env.stream.PendingRequests("ses_1")) == 1
	})

	w := do(t, env.router, http.MethodPost, "/api/v1/harness/sessions/ses_1/questions/q_1", `{"answers":[["yes"]]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	select {
	case call := <-env.fake.questReplies:
		if len(call.answers.Answers) != 1 || call.answers.Answers[0][0] != "yes" {
			t.Fatalf("unexpected answers %+v", call.answers)
		}
	default:
		t.Fatal("expected provider question reply call")
	}

	logs, total, err := env.audit.List(systemrepo.OperationLogFilters{ListQuery: pkg.ListQuery{Page: 1, PageSize: 10}, Action: "harness_question_reply"})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected one audit row, got total=%d", total)
	}
	if logs[0].ResourceID != "ses_1" {
		t.Fatalf("unexpected audit row: %+v", logs[0])
	}
}

func TestHandler_CatalogsAndExport(t *testing.T) {
	env := setup(t, true)
	do(t, env.router, http.MethodPost, "/api/v1/harness/sessions", `{}`)

	w := do(t, env.router, http.MethodGet, "/api/v1/harness/models", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "claude-sonnet") {
		t.Fatalf("models: expected 200 with catalog, got %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, env.router, http.MethodGet, "/api/v1/harness/agents", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "bedrock-agent-1") {
		t.Fatalf("agents: expected 200 with catalog, got %d body=%s", w.Code, w.Body.String())
	}
	w = do(t, env.router, http.MethodGet, "/api/v1/harness/sessions/ses_1/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/x-ndjson") {
		t.Fatalf("export: expected ndjson content type, got %s", ct)
	}
	w = do(t, env.router, http.MethodGet, "/api/v1/harness/sessions/ses_missing/export", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("export missing: expected 404, got %d", w.Code)
	}
}

// waitFor polls cond until it holds or the deadline elapses.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
