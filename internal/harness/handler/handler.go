// Package handler exposes the harness session domain: REST endpoints for
// sessions, messages, permission/question replies, export and catalogs, plus
// the WS unified-frame endpoint (ws_handler.go). Replies go through the
// stream bridge's pending registry and are written to the operation log.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/service"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	systemservice "bedrock/internal/system/service"
)

// unavailableMessage is the fixed envelope message for a disabled or dead
// session backend (mirrors dsh-unavailable).
const unavailableMessage = "harness-unavailable"

// HandlerConfig gates the domain and is shared with the WS handler.
type HandlerConfig struct {
	// Enabled reflects harness.enabled; false answers every endpoint 503.
	Enabled bool
}

// Handler serves /api/v1/harness.
type Handler struct {
	sessions *service.SessionService
	streams  *service.StreamService
	perm     *rbacservice.PermissionService
	audit    *systemservice.AuditService
	cfg      HandlerConfig
	log      *zap.Logger
}

// NewHandler builds the REST handler. A nil audit disables reply logging
// (tests); a nil logger becomes a no-op logger.
func NewHandler(
	sessions *service.SessionService,
	streams *service.StreamService,
	perm *rbacservice.PermissionService,
	audit *systemservice.AuditService,
	cfg HandlerConfig,
	log *zap.Logger,
) *Handler {
	if log == nil {
		log = zap.NewNop()
	}
	return &Handler{sessions: sessions, streams: streams, perm: perm, audit: audit, cfg: cfg, log: log}
}

// RegisterRoutes registers the REST endpoints on the /api/v1 group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	hs := rg.Group("/harness", authMW)
	hs.GET("/sessions", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.ListSessions)
	hs.POST("/sessions", rbacmw.RequirePermission(h.perm, "harness_chat:send"), h.CreateSession)
	hs.GET("/sessions/:id", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.GetSession)
	hs.GET("/sessions/:id/messages", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.ListMessages)
	hs.POST("/sessions/:id/messages", rbacmw.RequirePermission(h.perm, "harness_chat:send"), h.SendMessage)
	hs.POST("/sessions/:id/interrupt", rbacmw.RequirePermission(h.perm, "harness_chat:send"), h.Interrupt)
	hs.POST("/sessions/:id/permissions/:reqId", rbacmw.RequirePermission(h.perm, "harness_chat:approve"), h.ReplyPermission)
	hs.POST("/sessions/:id/questions/:reqId", rbacmw.RequirePermission(h.perm, "harness_chat:approve"), h.ReplyQuestion)
	hs.GET("/sessions/:id/export", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.ExportSession)
	hs.GET("/models", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.ListModels)
	hs.GET("/agents", rbacmw.RequirePermission(h.perm, "harness_chat:view"), h.ListAgents)
}

// gate answers 503 when the harness backend is disabled.
func (h *Handler) gate(c *gin.Context) bool {
	if h.cfg.Enabled {
		return false
	}
	pkg.Error(c, http.StatusServiceUnavailable, unavailableMessage)
	return true
}

// writeProviderError maps provider errors onto the contract's HTTP shape.
func (h *Handler) writeProviderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, provider.ErrNotFound):
		pkg.Error(c, http.StatusNotFound, "harness-session-not-found")
	case errors.Is(err, service.ErrRequestNotPending):
		pkg.Error(c, http.StatusConflict, "harness-pending-not-found")
	case errors.Is(err, service.ErrInvalidModelProvider):
		pkg.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, provider.ErrUnavailable):
		pkg.Error(c, http.StatusServiceUnavailable, unavailableMessage)
	default:
		h.log.Warn("harness provider call failed", zap.Error(err))
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}

type modelRefRequest struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// createSessionRequest is the allow-list of POST /harness/sessions; any
// directory-ish field in the body is rejected before it can reach a provider.
type createSessionRequest struct {
	Agent string           `json:"agent"`
	Model *modelRefRequest `json:"model"`
}

func (h *Handler) CreateSession(c *gin.Context) {
	if h.gate(c) {
		return
	}
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	for _, key := range []string{"directory", "cwd", "dir", "path", "location"} {
		if _, ok := raw[key]; ok {
			pkg.Error(c, http.StatusBadRequest, "请求不允许指定目录")
			return
		}
	}
	var input createSessionRequest
	if body, err := json.Marshal(raw); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	} else if err := json.Unmarshal(body, &input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	prov := provider.CreateSessionInput{Agent: input.Agent}
	if input.Model != nil {
		prov.Model = &provider.ModelRef{ProviderID: input.Model.Provider, ID: input.Model.ID}
	}
	session, err := h.sessions.CreateChatSession(c.Request.Context(), authmiddleware.GetUserID(c), prov)
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	pkg.Created(c, newSessionResponse(*session))
}

func (h *Handler) ListSessions(c *gin.Context) {
	if h.gate(c) {
		return
	}
	q := pkg.ParseListQuery(c)
	sessions, err := h.sessions.ListChatSessions(c.Request.Context(), authmiddleware.GetUserID(c))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	start := q.Offset()
	if start > len(sessions) {
		start = len(sessions)
	}
	end := start + q.PageSize
	if end > len(sessions) {
		end = len(sessions)
	}
	items := make([]sessionInfoResponse, 0, end-start)
	for _, info := range sessions[start:end] {
		items = append(items, newSessionInfoResponse(info))
	}
	pkg.PageSuccess(c, items, int64(len(sessions)), q)
}

func (h *Handler) GetSession(c *gin.Context) {
	if h.gate(c) {
		return
	}
	info, err := h.sessions.GetSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	pkg.Success(c, newSessionInfoResponse(*info))
}

type sendMessageRequest struct {
	Text     string `json:"text"`
	Delivery string `json:"delivery"`
}

func (h *Handler) SendMessage(c *gin.Context) {
	if h.gate(c) {
		return
	}
	var input sendMessageRequest
	if err := c.ShouldBindJSON(&input); err != nil || input.Text == "" {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	delivery := provider.Delivery(input.Delivery)
	switch delivery {
	case "", provider.DeliveryQueue, provider.DeliverySteer:
	default:
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	ack, err := h.sessions.Prompt(c.Request.Context(), c.Param("id"), provider.PromptInput{Text: input.Text, Delivery: delivery})
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: newPromptAckResponse(*ack)})
}

func (h *Handler) Interrupt(c *gin.Context) {
	if h.gate(c) {
		return
	}
	if err := h.sessions.Interrupt(c.Request.Context(), c.Param("id")); err != nil {
		h.writeProviderError(c, err)
		return
	}
	pkg.Success(c, nil)
}

func (h *Handler) ListMessages(c *gin.Context) {
	if h.gate(c) {
		return
	}
	messages, err := h.sessions.History(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	q := pkg.ParseListQuery(c)
	start := q.Offset()
	if start > len(messages) {
		start = len(messages)
	}
	end := start + q.PageSize
	if end > len(messages) {
		end = len(messages)
	}
	items := make([]messageResponse, 0, end-start)
	for _, m := range messages[start:end] {
		items = append(items, newMessageResponse(m))
	}
	pkg.PageSuccess(c, items, int64(len(messages)), q)
}

func (h *Handler) ExportSession(c *gin.Context) {
	if h.gate(c) {
		return
	}
	data, err := h.sessions.Export(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/x-ndjson", data)
}

type permissionReplyRequest struct {
	Reply string `json:"reply"`
}

func (h *Handler) ReplyPermission(c *gin.Context) {
	if h.gate(c) {
		return
	}
	var input permissionReplyRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	reply := provider.PermissionReply(input.Reply)
	switch reply {
	case provider.PermissionOnce, provider.PermissionAlways, provider.PermissionReject:
	default:
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	sessionID, reqID := c.Param("id"), c.Param("reqId")
	if err := h.streams.ReplyPermission(c.Request.Context(), sessionID, reqID, reply); err != nil {
		h.writeProviderError(c, err)
		return
	}
	h.writeAudit(c, "harness_permission_reply", sessionID, "reqId="+reqID+" reply="+string(reply))
	pkg.Success(c, nil)
}

type questionReplyRequest struct {
	Answers [][]string `json:"answers"`
}

func (h *Handler) ReplyQuestion(c *gin.Context) {
	if h.gate(c) {
		return
	}
	var input questionReplyRequest
	if err := c.ShouldBindJSON(&input); err != nil || input.Answers == nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	sessionID, reqID := c.Param("id"), c.Param("reqId")
	if err := h.streams.ReplyQuestion(c.Request.Context(), sessionID, reqID, provider.QuestionAnswers{Answers: input.Answers}); err != nil {
		h.writeProviderError(c, err)
		return
	}
	h.writeAudit(c, "harness_question_reply", sessionID, "reqId="+reqID)
	pkg.Success(c, nil)
}

func (h *Handler) ListModels(c *gin.Context) {
	if h.gate(c) {
		return
	}
	models, err := h.sessions.ListChatModels(c.Request.Context(), authmiddleware.GetUserID(c))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	items := make([]modelInfoResponse, 0, len(models))
	for _, m := range models {
		if !strings.HasPrefix(strings.TrimSpace(m.ProviderID), "bedrock-p") {
			continue
		}
		items = append(items, newModelInfoResponse(m))
	}
	pkg.Success(c, items)
}

func (h *Handler) ListAgents(c *gin.Context) {
	if h.gate(c) {
		return
	}
	agents, err := h.sessions.ListChatAgents(c.Request.Context(), authmiddleware.GetUserID(c))
	if err != nil {
		h.writeProviderError(c, err)
		return
	}
	items := make([]agentInfoResponse, 0, len(agents))
	for _, a := range agents {
		items = append(items, newAgentInfoResponse(a))
	}
	pkg.Success(c, items)
}

// writeAudit records a successful permission/question reply in the operation
// log; audit failures never fail the reply.
func (h *Handler) writeAudit(c *gin.Context, action, sessionID, details string) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Write(authmiddleware.GetUserID(c), authmiddleware.GetUsername(c), action, "harness_session", sessionID, details, c.ClientIP()); err != nil {
		h.log.Warn("harness audit write failed", zap.String("action", action), zap.Error(err))
	}
}

// --- response shapes (contract snake_case; provider types stay camelCase) ---

type sessionResponse struct {
	ID        string            `json:"id"`
	Directory string            `json:"directory"`
	Title     string            `json:"title,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Model     *modelRefResponse `json:"model,omitempty"`
}

type sessionInfoResponse struct {
	ID         string            `json:"id"`
	Title      string            `json:"title,omitempty"`
	Directory  string            `json:"directory,omitempty"`
	Agent      string            `json:"agent,omitempty"`
	Model      *modelRefResponse `json:"model,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
	ArchivedAt *string           `json:"archived_at,omitempty"`
}

type modelRefResponse struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type promptAckResponse struct {
	ID          string `json:"id"`
	AdmittedSeq int64  `json:"admitted_seq"`
}

func newPromptAckResponse(ack provider.PromptAck) promptAckResponse {
	return promptAckResponse{ID: ack.MessageID, AdmittedSeq: ack.AdmittedSeq}
}

type messageResponse struct {
	ID      string            `json:"id"`
	Role    string            `json:"role"`
	Agent   string            `json:"agent,omitempty"`
	Model   *modelRefResponse `json:"model,omitempty"`
	Content json.RawMessage   `json:"content,omitempty"`
}

type modelInfoResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
	Family   string `json:"family,omitempty"`
}

type agentInfoResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Native      bool   `json:"native"`
	Hidden      bool   `json:"hidden"`
}

func newSessionResponse(s provider.Session) sessionResponse {
	return sessionResponse{
		ID:        s.ID,
		Directory: s.Directory,
		Title:     s.Title,
		Agent:     s.Agent,
		Model:     modelRefResponseFrom(s.Model),
	}
}

func modelRefResponseFrom(m *provider.ModelRef) *modelRefResponse {
	if m == nil {
		return nil
	}
	return &modelRefResponse{Provider: m.ProviderID, ID: m.ID}
}

func newSessionInfoResponse(info provider.SessionInfo) sessionInfoResponse {
	out := sessionInfoResponse{
		ID:        info.ID,
		Title:     info.Title,
		Directory: info.Directory,
		Agent:     info.Agent,
		Model:     modelRefResponseFrom(info.Model),
		CreatedAt: info.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt: info.UpdatedAt.UTC().Format(timeFormat),
	}
	if info.ArchivedAt != nil {
		t := info.ArchivedAt.UTC().Format(timeFormat)
		out.ArchivedAt = &t
	}
	return out
}

func newMessageResponse(m provider.Message) messageResponse {
	return messageResponse{
		ID:      m.ID,
		Role:    m.Role,
		Agent:   m.Agent,
		Model:   modelRefResponseFrom(m.Model),
		Content: m.Content,
	}
}

func newModelInfoResponse(m provider.ModelInfo) modelInfoResponse {
	return modelInfoResponse{ID: m.ID, Provider: m.ProviderID, Name: m.Name, Family: m.Family}
}

func newAgentInfoResponse(a provider.AgentInfo) agentInfoResponse {
	return agentInfoResponse{
		Name:        a.Name,
		Description: a.Description,
		Mode:        a.Mode,
		Native:      a.Native,
		Hidden:      a.Hidden,
	}
}

// timeFormat is RFC 3339; provider times are UTC.
const timeFormat = "2006-01-02T15:04:05Z07:00"
