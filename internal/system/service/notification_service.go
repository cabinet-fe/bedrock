package service

import (
	"encoding/json"
	"fmt"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/ws"
)

// NotificationService persists inbox rows and pushes them on WS channel notifications:{userId}.
type NotificationService struct {
	repo *repository.NotificationRepository
	hub  *ws.Hub
}

func NewNotificationService(repo *repository.NotificationRepository, hub *ws.Hub) *NotificationService {
	return &NotificationService{repo: repo, hub: hub}
}

type PushInput struct {
	UserID     uint
	Type       string
	Title      string
	Message    string
	BuildRunID *uint
	AgentRunID *uint
}

func (s *NotificationService) Push(in PushInput) (*model.Notification, error) {
	n := &model.Notification{
		UserID:     in.UserID,
		Type:       in.Type,
		Title:      in.Title,
		Message:    in.Message,
		BuildRunID: in.BuildRunID,
		AgentRunID: in.AgentRunID,
	}
	if err := s.repo.Create(n); err != nil {
		return nil, err
	}
	s.broadcast(n)
	return n, nil
}

// NotifyBuildRun implements engine.TerminalNotifier.
func (s *NotificationService) NotifyBuildRun(userID uint, buildRunID uint, buildNumber int, status, message string) {
	if userID == 0 {
		return
	}
	runID := buildRunID
	title := fmt.Sprintf("构建 #%d %s", buildNumber, statusLabel(status))
	msg := message
	if msg == "" {
		msg = title
	}
	_, _ = s.Push(PushInput{
		UserID:     userID,
		Type:       "build_run_" + status,
		Title:      title,
		Message:    msg,
		BuildRunID: &runID,
	})
}

// NotifyAgentRun pushes an AgentRun terminal notification (and keeps ai-run log channel separate).
func (s *NotificationService) NotifyAgentRun(userID uint, agentRunID, agentID uint, status string) {
	if userID == 0 {
		return
	}
	runID := agentRunID
	title := fmt.Sprintf("智能体运行 #%d %s", agentRunID, statusLabel(status))
	_, _ = s.Push(PushInput{
		UserID:     userID,
		Type:       "agent_run_" + status,
		Title:      title,
		Message:    fmt.Sprintf("agent_id=%d status=%s", agentID, status),
		AgentRunID: &runID,
	})
}

// ListByUser 分页查询用户通知；isRead 非 nil 时按已读状态过滤。
func (s *NotificationService) ListByUser(userID uint, isRead *bool, q pkg.ListQuery) ([]model.Notification, int64, error) {
	return s.repo.ListByUser(userID, isRead, q)
}

func (s *NotificationService) MarkRead(id, userID uint) error {
	return s.repo.MarkRead(id, userID)
}

func (s *NotificationService) MarkAllRead(userID uint) error {
	return s.repo.MarkAllRead(userID)
}

func (s *NotificationService) CountUnread(userID uint) (int64, error) {
	return s.repo.CountUnread(userID)
}

func (s *NotificationService) broadcast(n *model.Notification) {
	if s.hub == nil || n == nil {
		return
	}
	payload, err := json.Marshal(n)
	if err != nil {
		return
	}
	s.hub.BroadcastToChannel(fmt.Sprintf("notifications:%d", n.UserID), payload)
}

func statusLabel(status string) string {
	switch status {
	case "success":
		return "成功"
	case "failed":
		return "失败"
	case "cancelled":
		return "已取消"
	case "interrupted":
		return "已中断"
	default:
		return status
	}
}
