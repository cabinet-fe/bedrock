package oc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceConfigFileName is the project-level opencode config written into
// every bedrock-managed session directory; opencode loads it per location.
const WorkspaceConfigFileName = "opencode.json"

// openAICompatiblePackage wires custom providers through the OpenAI-compatible
// AI SDK (all bedrock providers expose /chat/completions). opencode 2.x
// prefixes AI SDK package ids with "aisdk:".
const openAICompatiblePackage = "aisdk:@ai-sdk/openai-compatible"

// ModelConfigEntry is one model of a rendered provider. ReasoningEffort, when
// set, rides the model's settings and reaches the upstream API as
// `reasoning_effort` on every call (verified against opencode 2.2.0).
type ModelConfigEntry struct {
	ID              string
	Name            string
	ReasoningEffort string // "" = model default
}

// ProviderConfigEntry is one rendered BYOK provider. APIKey carries the
// literal key: the config file must be written 0600 (same same-UID threat
// model as the workspace .env).
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
	// explicit model; "" omits it. It also pins the title-generation model
	// (opencode 2.x: agents.title.model) so auxiliary calls stay on BYOK
	// providers instead of falling back to opencode's shared free models.
	DefaultModel string
}

// workspaceConfigDoc mirrors the opencode.json (v2 schema) subset bedrock
// generates. Field order follows the struct; provider/model maps marshal
// sorted.
type workspaceConfigDoc struct {
	Schema   string                            `json:"$schema,omitempty"`
	Model    string                            `json:"model,omitempty"`
	Agents   *workspaceAgents                  `json:"agents,omitempty"`
	Provider map[string]workspaceProviderEntry `json:"providers,omitempty"`
}

type workspaceAgents struct {
	Title *workspaceAgentRef `json:"title,omitempty"`
}

type workspaceAgentRef struct {
	ProviderID string `json:"providerID"`
	Model      string `json:"model"`
}

type workspaceProviderEntry struct {
	Package  string                           `json:"package"`
	Name     string                           `json:"name,omitempty"`
	Settings workspaceProviderSettings        `json:"settings"`
	Models   map[string]workspaceModelOptions `json:"models"`
}

type workspaceProviderSettings struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
}

type workspaceModelOptions struct {
	Name     string                  `json:"name"`
	Settings *workspaceModelSettings `json:"settings,omitempty"`
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
		providerID, modelID, ok := strings.Cut(in.DefaultModel, "/")
		if !ok || providerID == "" || modelID == "" {
			return nil, fmt.Errorf("oc config: default model %q must be provider/model", in.DefaultModel)
		}
		doc.Model = in.DefaultModel
		doc.Agents = &workspaceAgents{
			Title: &workspaceAgentRef{ProviderID: providerID, Model: modelID},
		}
	}
	for _, p := range in.Providers {
		if p.Key == "" {
			return nil, fmt.Errorf("oc config: provider key is required")
		}
		entry := workspaceProviderEntry{
			Package:  openAICompatiblePackage,
			Name:     p.Name,
			Settings: workspaceProviderSettings{BaseURL: p.BaseURL, APIKey: p.APIKey},
			Models:   map[string]workspaceModelOptions{},
		}
		for _, m := range p.Models {
			if m.ID == "" {
				return nil, fmt.Errorf("oc config: model id is required for provider %s", p.Key)
			}
			model := workspaceModelOptions{Name: m.Name}
			if m.ReasoningEffort != "" {
				model.Settings = &workspaceModelSettings{ReasoningEffort: m.ReasoningEffort}
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
