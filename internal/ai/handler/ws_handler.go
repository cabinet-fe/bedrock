package handler

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"bedrock/internal/ai/service"
	authservice "bedrock/internal/auth/service"
	"bedrock/internal/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/ws"
)

type WSHandler struct {
	auth   *authservice.AuthService
	perm   *rbacservice.PermissionService
	agents *service.AgentService
	hub    *ws.Hub
	cors   middleware.CORSConfig
}

func NewWSHandler(
	auth *authservice.AuthService,
	perm *rbacservice.PermissionService,
	agents *service.AgentService,
	hub *ws.Hub,
	cors middleware.CORSConfig,
) *WSHandler {
	return &WSHandler{auth: auth, perm: perm, agents: agents, hub: hub, cors: cors}
}

func (h *WSHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/ws/ai/runs/:id/logs", h.HandleAgentRunLogs)
}

func (h *WSHandler) HandleAgentRunLogs(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}
	claims, err := h.auth.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}
	if err := h.perm.CheckAccess(claims.UserID, claims.IsSuperAdmin, "ai_runs:view"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	runID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	scope, err := h.perm.ResolveDataScope(claims.UserID, claims.IsSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	run, err := h.agents.RequireRunAccess(uint(runID), service.AgentActor{UserID: claims.UserID, DataScope: scope})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return middleware.WebSocketCheckOrigin(h.cors, r)
		},
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	channel := fmt.Sprintf("ai-run:%d", run.ID)
	client := &ws.Client{
		Conn: conn, Send: make(chan []byte, 256), Channel: channel, UserID: claims.UserID,
	}
	h.hub.Register(client)
	go ws.WritePump(client, h.hub)

	if run.LogPath != "" {
		if f, err := os.Open(run.LogPath); err == nil {
			scanner := bufio.NewScanner(f)
			buf := make([]byte, 0, 64*1024)
			scanner.Buffer(buf, 1024*1024)
			for scanner.Scan() {
				select {
				case client.Send <- []byte(scanner.Text()):
				default:
				}
			}
			_ = f.Close()
		}
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.hub.Unregister(client)
}
