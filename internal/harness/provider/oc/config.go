package oc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspaceConfigFileName is the project-level opencode config written into
// every bedrock-managed session directory; opencode loads it per location.
const WorkspaceConfigFileName = "opencode.json"

// openAICompatibleNPMPackage wires custom providers through the
// OpenAI-compatible AI SDK (all bedrock providers expose /chat/completions).
const openAICompatibleNPMPackage = "@ai-sdk/openai-compatible"

// ModelConfigEntry is one model of a rendered provider. ReasoningEffort, when
// set, rides the model's request options and reaches the upstream API as
// `reasoning_effort` on every call (verified against opencode 1.18.x).
type ModelConfigEntry struct {
	ID              string
	Name            string
	ReasoningEffort string // "" = model default
}

// ProviderConfigEntry is one rendered BYOK provider. APIKey carries the
// literal key: opencode 1.18.x does not expand `{file:}`/`{env:}` references
// in custom provider options, so the config file must be written 0600 (same
// same-UID threat model as the workspace .env).
type ProviderConfigEntry struct {
	Key     string // opencode provider id, e.g. "bedrock-p3"
	Name    string // display name in catalogs
	BaseURL string
	APIKey  string // "" omits apiKey (keyless gateways)
	Models  []ModelConfigEntry
}

// ProviderConfigInput is the neutral provider projection the ai domain
// renders into workspace configs; the harness domain stays ai-free.
type ProviderConfigInput struct {
	Providers []ProviderConfigEntry
	// DefaultModel is the fallback "provider/model" for sessions without an
	// explicit model (also used for title generation); "" omits it.
	DefaultModel string
}

// workspaceConfigDoc mirrors the opencode.json subset bedrock generates.
// Field order follows the struct; provider/model maps marshal sorted.
type workspaceConfigDoc struct {
	Schema     string                            `json:"$schema,omitempty"`
	Model      string                            `json:"model,omitempty"`
	SmallModel string                            `json:"small_model,omitempty"`
	Provider   map[string]workspaceProviderEntry `json:"provider,omitempty"`
}

type workspaceProviderEntry struct {
	NPM     string                           `json:"npm"`
	Name    string                           `json:"name,omitempty"`
	Options workspaceProviderOptions         `json:"options"`
	Models  map[string]workspaceModelOptions `json:"models"`
}

type workspaceProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
}

type workspaceModelOptions struct {
	Name    string                  `json:"name"`
	Options *workspaceModelSettings `json:"options,omitempty"`
}

type workspaceModelSettings struct {
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// RenderWorkspaceConfig renders the opencode.json document for the input.
func RenderWorkspaceConfig(in ProviderConfigInput) ([]byte, error) {
	doc := workspaceConfigDoc{
		Schema:   "https://opencode.ai/config.json",
		Provider: map[string]workspaceProviderEntry{},
	}
	if in.DefaultModel != "" {
		doc.Model = in.DefaultModel
		doc.SmallModel = in.DefaultModel
	}
	for _, p := range in.Providers {
		if p.Key == "" {
			return nil, fmt.Errorf("oc config: provider key is required")
		}
		entry := workspaceProviderEntry{
			NPM:     openAICompatibleNPMPackage,
			Name:    p.Name,
			Options: workspaceProviderOptions{BaseURL: p.BaseURL, APIKey: p.APIKey},
			Models:  map[string]workspaceModelOptions{},
		}
		for _, m := range p.Models {
			if m.ID == "" {
				return nil, fmt.Errorf("oc config: model id is required for provider %s", p.Key)
			}
			model := workspaceModelOptions{Name: m.Name}
			if m.ReasoningEffort != "" {
				model.Options = &workspaceModelSettings{ReasoningEffort: m.ReasoningEffort}
			}
			entry.Models[m.ID] = model
		}
		doc.Provider[p.Key] = entry
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("oc config: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// WriteWorkspaceConfig atomically writes the rendered opencode.json into dir
// (tmp + rename, 0600: the file carries provider keys in plain text).
func WriteWorkspaceConfig(dir string, in ProviderConfigInput) (string, error) {
	data, err := RenderWorkspaceConfig(in)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("oc config: dir: %w", err)
	}
	path := filepath.Join(dir, WorkspaceConfigFileName)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", fmt.Errorf("oc config %s: %w", path, err)
	}
	return path, nil
}
