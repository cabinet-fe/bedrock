package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	authservice "bedrock/internal/auth/service"
	"bedrock/internal/harness/provider"
	"bedrock/internal/harness/service"
	"bedrock/internal/middleware"
	rbacservice "bedrock/internal/rbac/service"
)

// wsWriteWait bounds one WS frame write before the connection is dropped.
const wsWriteWait = 10 * time.Second

// WSHandler serves /ws/harness/sessions/:id/events: one unified-frame stream
// per connection, replaying the bridge ring buffer after a seq baseline,
// re-sending still-pending asks, then streaming live frames.
type WSHandler struct {
	auth     *authservice.AuthService
	perm     *rbacservice.PermissionService
	sessions *service.SessionService
	streams  *service.StreamService
	cfg      HandlerConfig
	cors     middleware.CORSConfig
	log      *zap.Logger
}

// NewWSHandler builds the WS handler. A nil logger becomes a no-op logger.
func NewWSHandler(
	auth *authservice.AuthService,
	perm *rbacservice.PermissionService,
	sessions *service.SessionService,
	streams *service.StreamService,
	cfg HandlerConfig,
	cors middleware.CORSConfig,
	log *zap.Logger,
) *WSHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &WSHandler{auth: auth, perm: perm, sessions: sessions, streams: streams, cfg: cfg, cors: cors, log: log}
}

// RegisterRoutes registers the WS endpoint on the engine root (path prefix
// /ws, outside /api/v1).
func (h *WSHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/ws/harness/sessions/:id/events", h.HandleSessionEvents)
}

// HandleSessionEvents upgrades one connection and streams unified frames:
// replay baseline (durable frames with seq > after), still-pending
// permission/question asks, a settled-idle status snapshot for sessions the
// bridge believes are not running, then the live fan-out. Every connection
// gets its own subscription of the shared bridge; frames are never
// duplicated within a connection because replay and live are snapshotted
// atomically by Subscribe and durable frames are seq-filtered.
func (h *WSHandler) HandleSessionEvents(c *gin.Context) {
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
	if err := h.perm.CheckAccess(claims.UserID, claims.IsSuperAdmin, "harness_chat:view"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	if !h.cfg.Enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": unavailableMessage})
		return
	}
	sessionID := c.Param("id")
	if _, err := h.sessions.GetSession(c.Request.Context(), sessionID); err != nil {
		status, message := h.wsProviderError(err)
		c.JSON(status, gin.H{"error": message})
		return
	}
	after, err := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	if err != nil || after < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid after"})
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

	replay, live, stop := h.streams.Subscribe(sessionID)
	defer stop()
	pending := h.streams.PendingRequests(sessionID)
	// Snapshot the settled state when the replay baseline itself asserts a
	// running session: replay never carries transient status frames (seq 0),
	// so a client joining after the last turn would otherwise stay on the
	// running state asserted by the replayed step frames forever. The
	// bridge's busy view and the replay are snapshotted atomically.
	settled := !h.streams.SessionBusy(sessionID) && replayAssertsRunning(replay, after)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.pump(ctx, sessionID, conn, replay, pending, live, after, settled)

	// Drain inbound messages until the peer disconnects; the stream is
	// read-only and answers go through the REST endpoints.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	cancel()
	_ = conn.Close()
}

// pump writes the replay baseline, the pending asks, the settled-idle
// snapshot and then the live feed. It returns when ctx is cancelled, the
// peer disconnects, or a write fails.
func (h *WSHandler) pump(ctx context.Context, sessionID string, conn *websocket.Conn, replay []provider.Frame, pending []service.PendingRequest, live <-chan *provider.Frame, after int64, settled bool) {
	for i := range replay {
		// Transient frames (seq 0) are never replayed; durable frames are
		// filtered by the caller's seq baseline.
		if replay[i].Seq <= 0 || replay[i].Seq <= after {
			continue
		}
		if !writeFrame(ctx, conn, &replay[i]) {
			return
		}
	}
	for _, req := range pending {
		frame := req.Frame
		if !writeFrame(ctx, conn, &frame) {
			return
		}
	}
	if settled {
		if !writeFrame(ctx, conn, &provider.Frame{
			SessionID: sessionID,
			Kind:      provider.FrameStatus,
			Status:    &provider.StatusFrame{Name: provider.StatusIdle},
		}) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-live:
			if !ok {
				return
			}
			if !writeFrame(ctx, conn, frame) {
				return
			}
		}
	}
}

// replayAssertsRunning reports whether the durable status frames above the
// seq baseline leave the session in the running state (a prompt or step
// start after the last idle/error/step-failure), mirroring how the client
// fold resolves them.
func replayAssertsRunning(replay []provider.Frame, after int64) bool {
	running := false
	for i := range replay {
		f := &replay[i]
		if f.Kind != provider.FrameStatus || f.Status == nil || f.Seq <= 0 || f.Seq <= after {
			continue
		}
		switch f.Status.Name {
		case provider.StatusPromptAdmitted, provider.StatusPrompted, provider.StatusStepStarted:
			running = true
		case provider.StatusIdle, provider.StatusError, provider.StatusStepFailed:
			running = false
		}
	}
	return running
}

// writeFrame marshals and sends one unified frame under a write deadline.
func writeFrame(ctx context.Context, conn *websocket.Conn, frame *provider.Frame) bool {
	if ctx.Err() != nil {
		return false
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return conn.WriteMessage(websocket.TextMessage, payload) == nil
}

// wsProviderError maps provider errors onto the pre-upgrade HTTP shape.
func (h *WSHandler) wsProviderError(err error) (int, string) {
	switch {
	case errors.Is(err, provider.ErrNotFound):
		return http.StatusNotFound, "harness-session-not-found"
	case errors.Is(err, provider.ErrUnavailable):
		return http.StatusServiceUnavailable, unavailableMessage
	default:
		h.log.Warn("harness provider call failed", zap.Error(err))
		return http.StatusInternalServerError, err.Error()
	}
}
