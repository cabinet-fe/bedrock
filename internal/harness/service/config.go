package service

import (
	"os"
	"path/filepath"

	"bedrock/internal/harness/provider/oc"
)

// ProviderModelSpec is one model of a rendered BYOK provider. ReasoningEffort
// rides the model's request options (per-workspace default reasoning).
type ProviderModelSpec struct {
	ID              string
	Name            string
	ReasoningEffort string // "" = model default
}

// ProviderSpec is one BYOK provider projected from the ai domain. APIKey is
// the literal key: opencode 1.18.x does not expand `{file:}`/`{env:}`
// references in custom provider options, so workspace configs are written
// 0600 under the same same-UID threat model as the workspace .env.
type ProviderSpec struct {
	Key     string // opencode provider id, e.g. "bedrock-p3"
	Name    string
	BaseURL string
	APIKey  string
	Models  []ProviderModelSpec
}

// ProviderConfigSpec is the BYOK provider projection the ai domain renders
// into every session directory (opencode loads config per location).
type ProviderConfigSpec struct {
	Providers []ProviderSpec
	// DefaultModel is the fallback "provider/model" for sessions without an
	// explicit model (also covers title generation); "" omits it.
	DefaultModel string
}

// SyncWorkspaceProviderConfig atomically writes the rendered opencode.json
// into directory; an empty spec removes any stale file instead so opencode
// falls back to its own global config.
func SyncWorkspaceProviderConfig(directory string, spec ProviderConfigSpec) error {
	if len(spec.Providers) == 0 {
		return removeWorkspaceConfig(directory)
	}
	input := oc.ProviderConfigInput{
		DefaultModel: spec.DefaultModel,
	}
	for _, p := range spec.Providers {
		entry := oc.ProviderConfigEntry{
			Key: p.Key, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		}
		for _, m := range p.Models {
			entry.Models = append(entry.Models, oc.ModelConfigEntry{
				ID: m.ID, Name: m.Name, ReasoningEffort: m.ReasoningEffort,
			})
		}
		input.Providers = append(input.Providers, entry)
	}
	_, err := oc.WriteWorkspaceConfig(directory, input)
	return err
}

func removeWorkspaceConfig(directory string) error {
	err := os.Remove(filepath.Join(directory, oc.WorkspaceConfigFileName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
