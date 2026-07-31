package handler

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	authservice "bedrock/internal/auth/service"
	cicdservice "bedrock/internal/cicd/service"
	"bedrock/internal/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/ws"
)

// WSHandler streams build-run / script-run logs over WebSocket (query token auth + RBAC).
type WSHandler struct {
	auth       *authservice.AuthService
	perm       *rbacservice.PermissionService
	runs       *cicdservice.BuildRunService
	scriptRuns *cicdservice.ScriptRunService
	hub        *ws.Hub
	cors       middleware.CORSConfig
}

func NewWSHandler(
	auth *authservice.AuthService,
	perm *rbacservice.PermissionService,
	runs *cicdservice.BuildRunService,
	hub *ws.Hub,
	cors middleware.CORSConfig,
) *WSHandler {
	return &WSHandler{auth: auth, perm: perm, runs: runs, hub: hub, cors: cors}
}

// SetScriptRuns wires script-run log streaming (optional additive).
func (h *WSHandler) SetScriptRuns(runs *cicdservice.ScriptRunService) {
	h.scriptRuns = runs
}

func (h *WSHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/ws/build-runs/:id/logs", h.HandleBuildRunLogs)
	r.GET("/ws/script-runs/:id/logs", h.HandleScriptRunLogs)
}

func (h *WSHandler) HandleBuildRunLogs(c *gin.Context) {
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
	if err := h.perm.CheckAccess(claims.UserID, claims.IsSuperAdmin, "cicd_build_runs:view"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	scope, err := h.perm.ResolveDataScope(claims.UserID, claims.IsSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
		return
	}

	runID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	run, err := h.runs.Get(uint(runID), claims.UserID, scope)
	if err != nil {
		if cicdservice.IsForbidden(err) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
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

	channel := fmt.Sprintf("build-run:%d", run.ID)
	client := &ws.Client{
		Conn:    conn,
		Send:    make(chan []byte, 256),
		Channel: channel,
		UserID:  claims.UserID,
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
			f.Close()
		}
	}

	// Keep connection alive until client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.hub.Unregister(client)
}

func (h *WSHandler) HandleScriptRunLogs(c *gin.Context) {
	if h.scriptRuns == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
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
	if err := h.perm.CheckAccess(claims.UserID, claims.IsSuperAdmin, "cicd_script_runs:view"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	scope, err := h.perm.ResolveDataScope(claims.UserID, claims.IsSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
		return
	}

	runID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	run, err := h.scriptRuns.Get(uint(runID), claims.UserID, scope)
	if err != nil {
		if cicdservice.IsForbidden(err) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
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

	channel := fmt.Sprintf("script-run:%d", run.ID)
	client := &ws.Client{
		Conn:    conn,
		Send:    make(chan []byte, 256),
		Channel: channel,
		UserID:  claims.UserID,
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
			f.Close()
		}
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.hub.Unregister(client)
}
