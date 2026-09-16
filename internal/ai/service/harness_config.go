package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"bedrock/internal/ai/model"
	harnessservice "bedrock/internal/harness/service"
)

const harnessLoopbackTokenBytes = 32

// harnessProviderSource supplies the enabled provider catalog and decrypts
// API keys for the harness BYOK config (satisfied by ProviderService).
type harnessProviderSource interface {
	ListEnabledProvidersWithModels() ([]model.AiProvider, error)
	DecryptAPIKey(providerID uint) (string, error)
}

// HarnessConfigService renders the enabled ai providers/models into
// per-directory opencode.json files (BYOK): the agent workspace before every
// run (with the agent's default reasoning effort on its model), the chat
// user directory before every session/catalog read, and the workspace root
// as the anchor for the agent-form model catalog.
//
// Each bedrock-p* provider points at the local ChatProxy (loopback baseURL +
// process token); upstream API keys stay in the platform DB for forwarding.
type HarnessConfigService struct {
	providers    harnessProviderSource
	root         string
	proxyBaseURL string
	proxyAPIKey  string
	log          *zap.Logger
}

// GenerateHarnessLoopbackToken returns a bearer token for opencode →
// ChatProxy calls on 127.0.0.1/::1 only.
func GenerateHarnessLoopbackToken() (string, error) {
	b := make([]byte, harnessLoopbackTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "br_harness_" + hex.EncodeToString(b), nil
}

// IsBedrockHarnessProvider reports whether providerID is a platform BYOK key.
func IsBedrockHarnessProvider(providerID string) bool {
	return strings.HasPrefix(strings.TrimSpace(providerID), "bedrock-p")
}

// NewHarnessConfigService builds the service. root is the bedrock workspace
// root (catalog anchor for GET /ai/models). proxyBaseURL is the OpenAI SDK
// base (e.g. http://127.0.0.1:{port}/api/v1/ai); proxyAPIKey is the loopback
// bearer token written into opencode.json instead of upstream keys.
func NewHarnessConfigService(providers harnessProviderSource, workspaceRoot, proxyBaseURL, proxyAPIKey string, log *zap.Logger) *HarnessConfigService {
	if log == nil {
		log = zap.NewNop()
	}
	return &HarnessConfigService{
		providers:    providers,
		root:         workspaceRoot,
		proxyBaseURL: strings.TrimRight(strings.TrimSpace(proxyBaseURL), "/"),
		proxyAPIKey:  strings.TrimSpace(proxyAPIKey),
		log:          log,
	}
}

// WorkspaceRoot returns the anchor directory backing GET /ai/models.
func (s *HarnessConfigService) WorkspaceRoot() string {
	return s.root
}

// HarnessProviderKey is the stable opencode provider id of an ai provider
// row (stable across renames; the provider name rides the display field).
func HarnessProviderKey(providerID uint) string {
	return fmt.Sprintf("bedrock-p%d", providerID)
}

// Refresh rewrites the workspace-root config; called at boot and after
// provider/model CRUD.
func (s *HarnessConfigService) Refresh() error {
	spec, err := s.buildSpec()
	if err != nil {
		return err
	}
	if err := harnessservice.SyncWorkspaceProviderConfig(s.root, spec); err != nil {
		return err
	}
	s.log.Info("harness provider config refreshed",
		zap.String("root", s.root), zap.Int("providers", len(spec.Providers)))
	return nil
}

// DefaultModelRef returns the catalog default "provider/model" split into
// its opencode ref parts, or empty strings when no provider is enabled. It
// pins sessions without an explicit model to a bedrock provider so a cold
// opencode instance cannot fall back to a foreign host-level provider.
func (s *HarnessConfigService) DefaultModelRef() (providerID, modelID string) {
	spec, err := s.buildSpec()
	if err != nil || spec.DefaultModel == "" {
		return "", ""
	}
	key, id, ok := strings.Cut(spec.DefaultModel, "/")
	if !ok {
		return "", ""
	}
	return key, id
}

// EnsureDirectoryConfig checks and fixes the opencode.json of one session
// directory right before harness sessions or catalog reads target it.
func (s *HarnessConfigService) EnsureDirectoryConfig(directory string) error {
	spec, err := s.buildSpec()
	if err != nil {
		return err
	}
	return harnessservice.SyncWorkspaceProviderConfig(directory, spec)
}

// EnsureAgentDirectoryConfig writes the config of an agent workspace: like
// EnsureDirectoryConfig, plus the agent's default reasoning effort applied
// to its selected model's request options (opencode ignores agent-level
// model options, so the effort must ride the per-directory model entry).
func (s *HarnessConfigService) EnsureAgentDirectoryConfig(directory, modelProvider, modelID, reasoningEffort string) error {
	spec, err := s.buildSpec()
	if err != nil {
		return err
	}
	if effort := strings.TrimSpace(reasoningEffort); effort != "" {
		for i := range spec.Providers {
			if spec.Providers[i].Key != modelProvider {
				continue
			}
			for j := range spec.Providers[i].Models {
				if spec.Providers[i].Models[j].ID == modelID {
					spec.Providers[i].Models[j].ReasoningEffort = effort
				}
			}
		}
	}
	return harnessservice.SyncWorkspaceProviderConfig(directory, spec)
}

// buildSpec loads the enabled catalog and returns the rendered spec.
// Providers without enabled models or incomplete name/url are skipped; the
// default model is the first enabled model in catalog order.
func (s *HarnessConfigService) buildSpec() (harnessservice.ProviderConfigSpec, error) {
	spec := harnessservice.ProviderConfigSpec{}
	providers, err := s.providers.ListEnabledProvidersWithModels()
	if err != nil {
		return spec, err
	}
	for i := range providers {
		p := &providers[i]
		if len(p.Models) == 0 {
			continue
		}
		name := strings.TrimSpace(p.Name)
		baseURL := strings.TrimSpace(p.APIURL)
		if name == "" || baseURL == "" {
			s.log.Warn("harness provider config skips incomplete provider",
				zap.Uint("provider_id", p.ID), zap.String("name", name), zap.String("api_url", baseURL))
			continue
		}
		entry := harnessservice.ProviderSpec{
			Key:     HarnessProviderKey(p.ID),
			Name:    name,
			BaseURL: s.proxyBaseURL,
			APIKey:  s.proxyAPIKey,
		}
		for _, m := range p.Models {
			entry.Models = append(entry.Models, harnessservice.ProviderModelSpec{
				ID: strings.TrimSpace(m.ModelID), Name: strings.TrimSpace(m.Name),
			})
		}
		if spec.DefaultModel == "" {
			spec.DefaultModel = entry.Key + "/" + strings.TrimSpace(p.Models[0].ModelID)
		}
		spec.Providers = append(spec.Providers, entry)
	}
	return spec, nil
}
