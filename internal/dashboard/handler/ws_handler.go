package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	authmiddleware "bedrock/internal/auth/middleware"
	authservice "bedrock/internal/auth/service"
	"bedrock/internal/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/ws"
)

// DashboardWSHandler 推送仪表盘运行变更与系统状态（多频道订阅）。
type DashboardWSHandler struct {
	auth   *authservice.AuthService
	pat    authmiddleware.PATValidator
	perm   *rbacservice.PermissionService
	hub    *ws.Hub
	cors   middleware.CORSConfig
}

func NewDashboardWSHandler(
	auth *authservice.AuthService,
	pat authmiddleware.PATValidator,
	perm *rbacservice.PermissionService,
	hub *ws.Hub,
	cors middleware.CORSConfig,
) *DashboardWSHandler {
	return &DashboardWSHandler{auth: auth, pat: pat, perm: perm, hub: hub, cors: cors}
}

func (h *DashboardWSHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/ws/dashboard", h.HandleDashboard)
}

func (h *DashboardWSHandler) HandleDashboard(c *gin.Context) {
	user, err := authmiddleware.ResolveQueryToken(h.auth, h.pat, c.Query("token"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
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

	client := &ws.Client{
		Conn:   conn,
		Send:   make(chan []byte, 256),
		UserID: user.UserID,
	}
	h.hub.Register(client)
	hasDashboardView := h.perm.CheckAccess(user.UserID, user.IsSuperAdmin, "dashboard:view") == nil
	if hasDashboardView {
		h.hub.Subscribe(client, ws.ChannelDashboardRuns)
	}
	if hasDashboardView && h.perm.CheckAccess(user.UserID, user.IsSuperAdmin, "dashboard:system_status") == nil {
		h.hub.Subscribe(client, ws.ChannelDashboardSystemStatus)
	}
	go ws.WritePump(client, h.hub)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.hub.Unregister(client)
}
