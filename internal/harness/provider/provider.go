// Package provider defines the harness session-backend abstraction: a
// Provider drives conversational agent sessions (create, prompt, stream,
// approve, interrupt) behind a provider-neutral frame model. The opencode
// adapter lives under provider/oc; future backends implement the same
// interface.
package provider

import (
	"context"
	"encoding/json"
	"errors"
)

// Delivery selects how a prompt joins a busy session.
type Delivery string

const (
	// DeliveryQueue waits for the current agent loop to finish first.
	DeliveryQueue Delivery = "queue"
	// DeliverySteer redirects the agent once the running step completes.
	DeliverySteer Delivery = "steer"
)

// PermissionReply answers a pending permission request.
type PermissionReply string

const (
	PermissionOnce   PermissionReply = "once"
	PermissionAlways PermissionReply = "always"
	PermissionReject PermissionReply = "reject"
)

// ModelRef identifies a model on a provider.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant,omitempty"`
}

// CreateSessionInput creates a session bound to a workspace directory.
type CreateSessionInput struct {
	// Directory is the working directory of the session (required).
	Directory string `json:"-"`
	// Agent selects an agent definition (e.g. "build" or "bedrock-<key>").
	Agent string `json:"agent,omitempty"`
	// Model pins the session model; zero value uses the agent/default model.
	Model *ModelRef `json:"model,omitempty"`
}

// Session is a created conversation.
type Session struct {
	ID        string    `json:"id"`
	Directory string    `json:"directory"`
	Title     string    `json:"title,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	Model     *ModelRef `json:"model,omitempty"`
}

// PromptInput sends one user message.
type PromptInput struct {
	Text     string   `json:"text"`
	Delivery Delivery `json:"-"`
}

// PromptAck is the admission result of a prompt.
type PromptAck struct {
	MessageID   string `json:"id"`
	AdmittedSeq int64  `json:"admittedSeq"`
}

// Message is one message in a session transcript.
type Message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"` // "user" | "assistant"
	Agent   string          `json:"agent,omitempty"`
	Model   *ModelRef       `json:"model,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

// FrameKind classifies a unified frame.
type FrameKind string

const (
	// FrameStatus marks lifecycle transitions: prompt admitted/prompted,
	// step started/ended/failed, session errors and idle.
	FrameStatus FrameKind = "status"
	// FrameMessageDelta is an incremental text fragment of an assistant
	// message (transient: only delivered on the live bus, never replayed).
	FrameMessageDelta FrameKind = "message_delta"
	// FrameMessageText is the full text of a finished assistant text part
	// (durable: always replayable).
	FrameMessageText FrameKind = "message_text"
	// FrameReasoningDelta is an incremental reasoning fragment.
	FrameReasoningDelta FrameKind = "reasoning_delta"
	// FrameToolCall reports a tool invocation with its input.
	FrameToolCall FrameKind = "tool_call"
	// FrameToolResult reports tool success (output) or failure (error).
	FrameToolResult FrameKind = "tool_result"
	// FramePermission requests a tool-permission decision.
	FramePermission FrameKind = "permission"
	// FrameQuestion requests structured user input.
	FrameQuestion FrameKind = "question"
)

// Frame is the provider-neutral event unit delivered by EventStream and the
// service bus. Exactly one payload field is set, matching Kind.
type Frame struct {
	// Seq is the durable sequence number (0 for transient frames such as
	// deltas, permissions and questions).
	Seq int64 `json:"seq"`
	// EventID is the backend event id (diagnostics/resumption aid).
	EventID string `json:"eventId,omitempty"`
	// SessionID scopes the frame to a session.
	SessionID string `json:"sessionId"`
	// Kind selects the payload below.
	Kind FrameKind `json:"kind"`

	Status         *StatusFrame     `json:"status,omitempty"`
	MessageDelta   *MessageDelta    `json:"messageDelta,omitempty"`
	MessageText    *MessageText     `json:"messageText,omitempty"`
	ReasoningDelta *ReasoningDelta  `json:"reasoningDelta,omitempty"`
	ToolCall       *ToolCall        `json:"toolCall,omitempty"`
	ToolResult     *ToolResult      `json:"toolResult,omitempty"`
	Permission     *PermissionFrame `json:"permission,omitempty"`
	Question       *QuestionFrame   `json:"question,omitempty"`
}

// StatusName enumerates lifecycle transitions reported via FrameStatus.
type StatusName string

const (
	StatusPromptAdmitted StatusName = "prompt_admitted"
	StatusPrompted       StatusName = "prompted"
	StatusStepStarted    StatusName = "step_started"
	StatusStepEnded      StatusName = "step_ended"
	StatusStepFailed     StatusName = "step_failed"
	StatusError          StatusName = "error"
	StatusIdle           StatusName = "idle"
)

// StatusFrame carries a lifecycle transition.
type StatusFrame struct {
	Name StatusName `json:"name"`
	// MessageID identifies the user message of prompt transitions.
	MessageID string `json:"messageId,omitempty"`
	// AssistantMessageID identifies the assistant message of step/tool
	// transitions.
	AssistantMessageID string    `json:"assistantMessageId,omitempty"`
	Delivery           Delivery  `json:"delivery,omitempty"`
	Finish             string    `json:"finish,omitempty"` // step end reason, e.g. "stop"
	Agent              string    `json:"agent,omitempty"`
	Model              *ModelRef `json:"model,omitempty"`
	Error              string    `json:"error,omitempty"`
}

// MessageDelta is one text fragment.
type MessageDelta struct {
	AssistantMessageID string `json:"assistantMessageId"`
	TextID             string `json:"textId"`
	Delta              string `json:"delta"`
}

// MessageText is the complete text of a finished text part.
type MessageText struct {
	AssistantMessageID string `json:"assistantMessageId"`
	TextID             string `json:"textId"`
	Text               string `json:"text"`
}

// ReasoningDelta is one reasoning fragment.
type ReasoningDelta struct {
	AssistantMessageID string `json:"assistantMessageId"`
	Delta              string `json:"delta"`
}

// ToolCall reports a tool invocation.
type ToolCall struct {
	AssistantMessageID string          `json:"assistantMessageId"`
	CallID             string          `json:"callId"`
	Tool               string          `json:"tool"`
	Input              json.RawMessage `json:"input,omitempty"`
}

// ToolResult reports the outcome of a tool call.
type ToolResult struct {
	AssistantMessageID string          `json:"assistantMessageId"`
	CallID             string          `json:"callId"`
	Tool               string          `json:"tool,omitempty"`
	Output             json.RawMessage `json:"output,omitempty"`
	Error              string          `json:"error,omitempty"`
}

// PermissionFrame requests a permission decision; answer via Provider.
type PermissionFrame struct {
	RequestID string   `json:"requestId"`
	Action    string   `json:"action"` // e.g. "bash"
	Resources []string `json:"resources,omitempty"`
	Save      []string `json:"save,omitempty"`
}

// QuestionFrame requests structured input; answer via Provider.
type QuestionFrame struct {
	RequestID string     `json:"requestId"`
	Questions []Question `json:"questions"`
}

// Question is one structured question with selectable options.
type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header,omitempty"`
	Options  []string `json:"options,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

// QuestionAnswers answers a QuestionFrame; one answer list per question.
type QuestionAnswers struct {
	Answers [][]string `json:"answers"`
}

// Stream delivers unified frames until closed or the context ends.
type Stream interface {
	// Next blocks until the next frame. It returns io.EOF when the stream
	// ended and the context error when ctx is done.
	Next(ctx context.Context) (*Frame, error)
	// Close releases the underlying transport.
	Close() error
}

// Provider is the session-backend contract implemented by adapters
// (opencode first, dsh later).
type Provider interface {
	// CreateSession creates a session bound to input.Directory.
	CreateSession(ctx context.Context, input CreateSessionInput) (*Session, error)
	// Prompt sends one user message with the given delivery mode.
	Prompt(ctx context.Context, sessionID string, input PromptInput) (*PromptAck, error)
	// SelectModel switches the model of an existing session.
	SelectModel(ctx context.Context, sessionID string, model ModelRef) error
	// History lists the messages of a session, oldest first.
	History(ctx context.Context, sessionID string) ([]Message, error)
	// EventStream subscribes to a session, replaying durable events after
	// the given sequence (0 replays everything) then streaming live.
	EventStream(ctx context.Context, sessionID string, after int64) (Stream, error)
	// Wait blocks until the session agent loop becomes idle.
	Wait(ctx context.Context, sessionID string) error
	// Interrupt cancels the running agent loop.
	Interrupt(ctx context.Context, sessionID string) error
	// ReplyPermission answers a pending permission request.
	ReplyPermission(ctx context.Context, sessionID, requestID string, reply PermissionReply) error
	// ReplyQuestion answers a pending question request.
	ReplyQuestion(ctx context.Context, sessionID, requestID string, answers QuestionAnswers) error
	// ListModels lists available models for a workspace directory.
	ListModels(ctx context.Context, directory string) ([]ModelInfo, error)
	// ListAgents lists agent definitions visible in a workspace directory.
	ListAgents(ctx context.Context, directory string) ([]AgentInfo, error)
	// Export returns the session transcript as JSONL (one message per line).
	Export(ctx context.Context, sessionID string) ([]byte, error)
}

// ModelInfo describes one selectable model.
type ModelInfo struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name,omitempty"`
	Family     string `json:"family,omitempty"`
}

// AgentInfo describes one agent definition.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode,omitempty"` // primary | subagent | all
	Native      bool   `json:"native"`
	Hidden      bool   `json:"hidden"`
}

// ErrWaitUnavailable reports that the backend cannot serve Wait in the
// current version (observed on opencode 1.18.29).
var ErrWaitUnavailable = errors.New("provider: session wait is not available")
