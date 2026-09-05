package service

import (
	"errors"
	"strings"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
)

type ChatService struct {
	repo *repository.ChatRepository
}

func NewChatService(repo *repository.ChatRepository) *ChatService {
	return &ChatService{repo: repo}
}

// CreateSession creates a new session for the current user.
func (s *ChatService) CreateSession(userID uint, in model.ChatSessionInput) (*model.ChatSession, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "新对话"
	}
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}

	session := &model.ChatSession{
		UserID:  userID,
		Title:   title,
		ModelID: strings.TrimSpace(in.ModelID),
	}

	if err := s.repo.CreateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// UpdateSession updates session title and/or model ID for the current user.
func (s *ChatService) UpdateSession(id, userID uint, in model.ChatSessionInput) (*model.ChatSession, error) {
	session, err := s.repo.FindSession(id, userID)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(in.Title)
	if title != "" {
		if len([]rune(title)) > 200 {
			title = string([]rune(title)[:200])
		}
		session.Title = title
	}

	modelID := strings.TrimSpace(in.ModelID)
	if modelID != "" {
		session.ModelID = modelID
	}

	if err := s.repo.UpdateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// DeleteSession deletes a session and all its messages.
func (s *ChatService) DeleteSession(id, userID uint) error {
	return s.repo.DeleteSession(id, userID)
}

// GetSession retrieves a single session for the current user.
func (s *ChatService) GetSession(id, userID uint) (*model.ChatSession, error) {
	return s.repo.FindSession(id, userID)
}

// ListSessions returns a paginated list of sessions for the current user.
func (s *ChatService) ListSessions(userID uint, page, pageSize int) ([]model.ChatSession, int64, error) {
	return s.repo.ListSessions(userID, page, pageSize)
}

// ListMessages returns all messages in a session for the current user.
func (s *ChatService) ListMessages(sessionID, userID uint) ([]model.ChatMessage, error) {
	return s.repo.ListMessagesBySession(sessionID, userID)
}

// CreateMessage creates a single message in a session for the current user.
func (s *ChatService) CreateMessage(sessionID, userID uint, in model.ChatMessageInput) (*model.ChatMessage, error) {
	// Verify session ownership
	if _, err := s.repo.FindSession(sessionID, userID); err != nil {
		return nil, err
	}

	role := strings.TrimSpace(in.Role)
	if role == "" {
		return nil, errors.New("消息角色不能为空")
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, errors.New("消息内容不能为空")
	}

	msg := &model.ChatMessage{
		SessionID:        sessionID,
		UserID:           userID,
		Role:             role,
		Content:          content,
		ReasoningContent: strings.TrimSpace(in.ReasoningContent),
	}

	if err := s.repo.CreateMessage(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// SaveExchange saves both user question and assistant answer upon completion of a turn.
func (s *ChatService) SaveExchange(sessionID, userID uint, userContent, assistantContent, reasoningContent string) error {
	session, err := s.repo.FindSession(sessionID, userID)
	if err != nil {
		return err
	}

	// If session title is default and userContent is available, auto-name the session
	trimmedUser := strings.TrimSpace(userContent)
	if session.Title == "新对话" && trimmedUser != "" {
		newTitle := trimmedUser
		if len([]rune(newTitle)) > 30 {
			newTitle = string([]rune(newTitle)[:30]) + "..."
		}
		session.Title = newTitle
		_ = s.repo.UpdateSession(session)
	}

	if trimmedUser != "" {
		userMsg := &model.ChatMessage{
			SessionID: sessionID,
			UserID:    userID,
			Role:      model.RoleUser,
			Content:   trimmedUser,
		}
		if err := s.repo.CreateMessage(userMsg); err != nil {
			return err
		}
	}

	trimmedAssistant := strings.TrimSpace(assistantContent)
	if trimmedAssistant != "" || strings.TrimSpace(reasoningContent) != "" {
		assistantMsg := &model.ChatMessage{
			SessionID:        sessionID,
			UserID:           userID,
			Role:             model.RoleAssistant,
			Content:          trimmedAssistant,
			ReasoningContent: strings.TrimSpace(reasoningContent),
		}
		if err := s.repo.CreateMessage(assistantMsg); err != nil {
			return err
		}
	}

	return nil
}
