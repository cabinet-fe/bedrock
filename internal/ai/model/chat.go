package model

import (
	"encoding/json"
	"strings"
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
	Role             string         `json:"role"`
	Content          any            `json:"content"`
	Name             string         `json:"name,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ToolCalls        any            `json:"tool_calls,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Extra            map[string]any `json:"-"`
}

func (m *ChatCompletionMessage) UnmarshalJSON(data []byte) error {
	type Alias ChatCompletionMessage
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	delete(raw, "role")
	delete(raw, "content")
	delete(raw, "name")
	delete(raw, "tool_call_id")
	delete(raw, "tool_calls")
	delete(raw, "reasoning_content")

	m.Extra = raw
	return nil
}

func (m ChatCompletionMessage) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(m.Extra)+6)
	for k, v := range m.Extra {
		out[k] = v
	}

	out["role"] = m.Role
	out["content"] = m.Content
	if m.Name != "" {
		out["name"] = m.Name
	}
	if m.ToolCallID != "" {
		out["tool_call_id"] = m.ToolCallID
	}
	if m.ToolCalls != nil {
		out["tool_calls"] = m.ToolCalls
	}
	if m.ReasoningContent != "" {
		out["reasoning_content"] = m.ReasoningContent
	}

	return json.Marshal(out)
}

// StringContent returns the message content as a plain text string.
func (m ChatCompletionMessage) StringContent() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if pm, ok := part.(map[string]any); ok {
				if t, ok := pm["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	default:
		if v != nil {
			b, _ := json.Marshal(v)
			return string(b)
		}
		return ""
	}
}

// ChatCompletionRequest represents an OpenAI-compatible chat completions request.
type ChatCompletionRequest struct {
	Model           string                  `json:"model"`
	Messages        []ChatCompletionMessage `json:"messages"`
	RawMessages     []json.RawMessage       `json:"-"`
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

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if rawMessagesBytes, ok := raw["messages"]; ok && len(rawMessagesBytes) > 0 {
		var rawList []json.RawMessage
		if err := json.Unmarshal(rawMessagesBytes, &rawList); err == nil {
			r.RawMessages = rawList
		}
	}

	delete(raw, "model")
	delete(raw, "messages")
	delete(raw, "stream")
	delete(raw, "reasoning_effort")
	delete(raw, "session_id")

	extra := make(map[string]any, len(raw))
	for k, v := range raw {
		var val any
		if err := json.Unmarshal(v, &val); err == nil {
			extra[k] = val
		}
	}

	r.Extra = extra
	return nil
}

