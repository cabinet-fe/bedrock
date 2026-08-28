package service

// DSH JSON-RPC methods used by this module (design §4.3). Do not add
// goals.*, workspace.*, subagent.*, credentials.*, or settings.*.
const (
	MethodHostDescribe       = "host.describe"
	MethodSessionCreate      = "session.create"
	MethodSessionList        = "session.list"
	MethodSessionHistory     = "session.history"
	MethodSessionPrompt      = "session.prompt"
	MethodSessionCancel      = "session.cancel"
	MethodSessionModels      = "session.models"
	MethodSessionSelectModel = "session.selectModel"
	MethodAgentPresetsList   = "agentPresets.list"
	MethodAgentPresetsSelect = "agentPresets.select"
	MethodLLMProviders       = "llm.providers"
	MethodLLMModels          = "llm.models"

	PromptModeQueue = "queue"
	PromptModeSteer = "steer"
)

var rpcMethods = []string{
	MethodHostDescribe,
	MethodSessionCreate,
	MethodSessionList,
	MethodSessionHistory,
	MethodSessionPrompt,
	MethodSessionCancel,
	MethodSessionModels,
	MethodSessionSelectModel,
	MethodAgentPresetsList,
	MethodAgentPresetsSelect,
	MethodLLMProviders,
	MethodLLMModels,
}

type SessionCreatePayload struct {
	Cwd         string `json:"cwd"`
	SessionID   string `json:"sessionId,omitempty"`
	AgentPreset string `json:"agentPreset,omitempty"`
}

type SessionHistoryPayload struct {
	SessionID   string `json:"sessionId"`
	BeforeSeq   *int64 `json:"beforeSeq,omitempty"`
	MaxMessages *int   `json:"maxMessages,omitempty"`
}

type SessionPromptPayload struct {
	SessionID string          `json:"sessionId"`
	Mode      string          `json:"mode"`
	Content   []PromptContent `json:"content"`
}

type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type SessionCancelPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionCancelValue struct {
	Accepted bool `json:"accepted"`
}
