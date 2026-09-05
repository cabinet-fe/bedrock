package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
)

type ChatHandler struct {
	chatSvc     *service.ChatService
	chatProxy   *service.ChatProxy
	providerSvc *service.ProviderService
}

func NewChatHandler(
	chatSvc *service.ChatService,
	chatProxy *service.ChatProxy,
	providerSvc *service.ProviderService,
) *ChatHandler {
	return &ChatHandler{
		chatSvc:     chatSvc,
		chatProxy:   chatProxy,
		providerSvc: providerSvc,
	}
}

// ListSessions lists chat sessions belonging to the current authenticated user.
func (h *ChatHandler) ListSessions(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	items, total, err := h.chatSvc.ListSessions(userID, page, pageSize)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Paginated(c, items, total, page, pageSize)
}

// CreateSession creates a new chat session for the current user.
func (h *ChatHandler) CreateSession(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	var in model.ChatSessionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求参数")
		return
	}

	session, err := h.chatSvc.CreateSession(userID, in)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, session)
}

// UpdateSession updates a session title or model for the current user.
func (h *ChatHandler) UpdateSession(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效会话 ID")
		return
	}

	var in model.ChatSessionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求参数")
		return
	}

	session, err := h.chatSvc.UpdateSession(uint(id), userID, in)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pkg.Error(c, http.StatusNotFound, "会话不存在")
			return
		}
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, session)
}

// DeleteSession deletes a session and all its messages.
func (h *ChatHandler) DeleteSession(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效会话 ID")
		return
	}

	if err := h.chatSvc.DeleteSession(uint(id), userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pkg.Error(c, http.StatusNotFound, "会话不存在")
			return
		}
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

// ListMessages returns history messages for a session belonging to the current user.
func (h *ChatHandler) ListMessages(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效会话 ID")
		return
	}

	items, err := h.chatSvc.ListMessages(uint(id), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pkg.Error(c, http.StatusNotFound, "会话不存在")
			return
		}
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, items)
}

// CreateMessage adds a single message to a session.
func (h *ChatHandler) CreateMessage(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效会话 ID")
		return
	}

	var in model.ChatMessageInput
	if err := c.ShouldBindJSON(&in); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求参数")
		return
	}

	msg, err := h.chatSvc.CreateMessage(uint(id), userID, in)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pkg.Error(c, http.StatusNotFound, "会话不存在")
			return
		}
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, msg)
}

// ListAvailableModels returns all enabled models whose providers are enabled.
func (h *ChatHandler) ListAvailableModels(c *gin.Context) {
	items, err := h.providerSvc.ListAvailableModels()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, items)
}

// ChatCompletions proxies chat completions request with SSE streaming.
func (h *ChatHandler) ChatCompletions(c *gin.Context) {
	userID := authmiddleware.GetUserID(c)

	var req model.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求参数")
		return
	}

	// Fallback to query or header if session_id is not in request body
	if req.SessionID == nil {
		if sidStr := c.Query("session_id"); sidStr != "" {
			if sid, err := strconv.ParseUint(sidStr, 10, 64); err == nil && sid > 0 {
				u := uint(sid)
				req.SessionID = &u
			}
		} else if sidStr := c.GetHeader("X-Session-ID"); sidStr != "" {
			if sid, err := strconv.ParseUint(sidStr, 10, 64); err == nil && sid > 0 {
				u := uint(sid)
				req.SessionID = &u
			}
		}
	}

	if err := h.chatProxy.ProxyCompletions(c, userID, req); err != nil {
		if !c.Writer.Written() {
			var upstreamErr *service.UpstreamHTTPError
			if errors.As(err, &upstreamErr) {
				pkg.Error(c, upstreamErr.StatusCode, upstreamErr.Error())
				return
			}
			pkg.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}
}
