package model

import (
	"encoding/json"
	"time"
)

// Standard chat message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ChatSession represents an AI conversation session.
type ChatSession struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"not null;index"`
	Title     string    `json:"title" gorm:"size:200;not null"`
	ModelID   string    `json:"model_id" gorm:"size:100"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at" gorm:"index"`
}

func (ChatSession) TableName() string { return "ai_chat_sessions" }

// ChatMessage represents a single message in an AI conversation session.
type ChatMessage struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	SessionID        uint      `json:"session_id" gorm:"not null;index"`
	UserID           uint      `json:"user_id" gorm:"not null;index"`
	Role             string    `json:"role" gorm:"size:20;not null"`
	Content          string    `json:"content" gorm:"type:text;not null"`
	ReasoningContent string    `json:"reasoning_content,omitempty" gorm:"type:text"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (ChatMessage) TableName() string { return "ai_chat_messages" }

// ChatSessionInput represents the input for creating or updating a session.
type ChatSessionInput struct {
	Title   string `json:"title"`
	ModelID string `json:"model_id"`
}

// ChatMessageInput represents the input for creating a message in a session.
type ChatMessageInput struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ChatCompletionMessage represents an OpenAI-compatible message object.
type ChatCompletionMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ChatCompletionRequest represents an OpenAI-compatible chat completions request.
type ChatCompletionRequest struct {
	Model           string                  `json:"model"`
	Messages        []ChatCompletionMessage `json:"messages"`
	Stream          *bool                   `json:"stream,omitempty"`
	ReasoningEffort string                  `json:"reasoning_effort,omitempty"`
	SessionID       *uint                   `json:"session_id,omitempty"`
	Extra           map[string]any          `json:"-"`
}

// UnmarshalJSON unmarshals known fields and captures any remaining fields into Extra.
func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type Alias ChatCompletionRequest
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	delete(raw, "model")
	delete(raw, "messages")
	delete(raw, "stream")
	delete(raw, "reasoning_effort")
	delete(raw, "session_id")

	r.Extra = raw
	return nil
}
