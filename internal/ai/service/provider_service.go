package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/pkg"
)

type ProviderService struct {
	repo *repository.ProviderRepository
}

func NewProviderService(repo *repository.ProviderRepository) *ProviderService {
	return &ProviderService{repo: repo}
}

// CreateProvider handles creating a new provider with AES-GCM encrypted API key.
func (s *ProviderService) CreateProvider(userID uint, in model.ProviderInput) (*model.AiProvider, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("服务商名称不能为空")
	}
	apiURL := strings.TrimSpace(in.APIURL)
	if apiURL == "" {
		return nil, errors.New("API 地址不能为空")
	}

	if existing, err := s.repo.FindProviderByName(name); err == nil && existing != nil {
		return nil, errors.New("服务商名称已存在")
	}

	var apiKeyCipher string
	rawKey := strings.TrimSpace(in.APIKey)
	if rawKey != "" {
		enc, err := pkg.Encrypt(rawKey)
		if err != nil {
			return nil, fmt.Errorf("加密 API Key 失败: %w", err)
		}
		apiKeyCipher = enc
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	provider := &model.AiProvider{
		Name:         name,
		APIURL:       apiURL,
		APIKeyCipher: apiKeyCipher,
		Enabled:      enabled,
		Notes:        strings.TrimSpace(in.Notes),
		CreatedBy:    userID,
	}

	if err := s.repo.CreateProvider(provider); err != nil {
		return nil, err
	}

	maskProvider(provider)
	return provider, nil
}

// UpdateProvider updates provider info. An empty API key preserves the existing cipher.
func (s *ProviderService) UpdateProvider(id uint, in model.ProviderInput) (*model.AiProvider, error) {
	existing, err := s.repo.FindProvider(id)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name != "" && name != existing.Name {
		if other, err := s.repo.FindProviderByName(name); err == nil && other != nil && other.ID != id {
			return nil, errors.New("服务商名称已存在")
		}
		existing.Name = name
	}

	apiURL := strings.TrimSpace(in.APIURL)
	if apiURL != "" {
		existing.APIURL = apiURL
	}

	rawKey := strings.TrimSpace(in.APIKey)
	if rawKey != "" {
		enc, err := pkg.Encrypt(rawKey)
		if err != nil {
			return nil, fmt.Errorf("加密 API Key 失败: %w", err)
		}
		existing.APIKeyCipher = enc
	}

	if in.Enabled != nil {
		existing.Enabled = *in.Enabled
	}

	if in.Notes != "" || in.Name != "" || in.APIURL != "" {
		existing.Notes = strings.TrimSpace(in.Notes)
	}

	if err := s.repo.UpdateProvider(existing); err != nil {
		return nil, err
	}

	maskProvider(existing)
	return existing, nil
}

// DeleteProvider deletes a provider and cascades all associated models.
func (s *ProviderService) DeleteProvider(id uint) error {
	return s.repo.DeleteProvider(id)
}

// GetProvider retrieves a provider by ID with masked API key.
func (s *ProviderService) GetProvider(id uint) (*model.AiProvider, error) {
	p, err := s.repo.FindProvider(id)
	if err != nil {
		return nil, err
	}
	maskProvider(p)
	return p, nil
}

// ListProviders returns a paginated list of providers with masked API keys.
func (s *ProviderService) ListProviders(page, pageSize int) ([]model.AiProvider, int64, error) {
	items, total, err := s.repo.ListProviders(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		maskProvider(&items[i])
	}
	return items, total, nil
}

// DecryptAPIKey returns the decrypted API key for internal use (e.g. chat completions proxy).
func (s *ProviderService) DecryptAPIKey(id uint) (string, error) {
	p, err := s.repo.FindProvider(id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.APIKeyCipher) == "" {
		return "", nil
	}
	return pkg.Decrypt(p.APIKeyCipher)
}

// CreateModel validates and persists a new model under a provider.
func (s *ProviderService) CreateModel(providerID uint, in model.ModelInput) (*model.AiModel, error) {
	if _, err := s.repo.FindProvider(providerID); err != nil {
		return nil, errors.New("服务商不存在")
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("模型名称不能为空")
	}
	modelID := strings.TrimSpace(in.ModelID)
	if modelID == "" {
		return nil, errors.New("模型标识不能为空")
	}

	if existing, err := s.repo.FindModelByProviderAndModelID(providerID, modelID); err == nil && existing != nil {
		return nil, errors.New("该服务商下已存在相同标识的模型")
	}

	effortsJSON, err := parseReasoningEfforts(in.ReasoningEfforts)
	if err != nil {
		return nil, err
	}

	paramsJSON, err := parseDefaultParams(in.DefaultParams)
	if err != nil {
		return nil, err
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	sortOrder := 0
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}

	aiModel := &model.AiModel{
		ProviderID:           providerID,
		Name:                 name,
		ModelID:              modelID,
		Enabled:              enabled,
		SortOrder:            sortOrder,
		ReasoningEffortsJSON: effortsJSON,
		DefaultParamsJSON:    paramsJSON,
		Notes:                strings.TrimSpace(in.Notes),
	}

	if err := s.repo.CreateModel(aiModel); err != nil {
		return nil, err
	}

	projectModel(aiModel)
	return aiModel, nil
}

// UpdateModel updates an existing model.
func (s *ProviderService) UpdateModel(id uint, in model.ModelInput) (*model.AiModel, error) {
	existing, err := s.repo.FindModel(id)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name != "" {
		existing.Name = name
	}

	modelID := strings.TrimSpace(in.ModelID)
	if modelID != "" && modelID != existing.ModelID {
		if other, err := s.repo.FindModelByProviderAndModelID(existing.ProviderID, modelID); err == nil && other != nil && other.ID != id {
			return nil, errors.New("该服务商下已存在相同标识的模型")
		}
		existing.ModelID = modelID
	}

	if in.ReasoningEfforts != nil {
		effortsJSON, err := parseReasoningEfforts(in.ReasoningEfforts)
		if err != nil {
			return nil, err
		}
		existing.ReasoningEffortsJSON = effortsJSON
	}

	if in.DefaultParams != nil {
		paramsJSON, err := parseDefaultParams(in.DefaultParams)
		if err != nil {
			return nil, err
		}
		existing.DefaultParamsJSON = paramsJSON
	}

	if in.Enabled != nil {
		existing.Enabled = *in.Enabled
	}
	if in.SortOrder != nil {
		existing.SortOrder = *in.SortOrder
	}
	if in.Notes != "" || in.Name != "" || in.ModelID != "" {
		existing.Notes = strings.TrimSpace(in.Notes)
	}

	if err := s.repo.UpdateModel(existing); err != nil {
		return nil, err
	}

	projectModel(existing)
	return existing, nil
}

// DeleteModel deletes a model by ID.
func (s *ProviderService) DeleteModel(id uint) error {
	return s.repo.DeleteModel(id)
}

// GetModel retrieves a model by ID.
func (s *ProviderService) GetModel(id uint) (*model.AiModel, error) {
	m, err := s.repo.FindModel(id)
	if err != nil {
		return nil, err
	}
	projectModel(m)
	return m, nil
}

// ListModelsByProvider retrieves models for a given provider.
func (s *ProviderService) ListModelsByProvider(providerID uint, page, pageSize int) ([]model.AiModel, int64, error) {
	items, total, err := s.repo.ListModelsByProvider(providerID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		projectModel(&items[i])
	}
	return items, total, nil
}

// ListModels retrieves all models optionally filtered by provider and enabled.
func (s *ProviderService) ListModels(providerID *uint, enabled *bool) ([]model.AiModel, error) {
	items, err := s.repo.ListModels(providerID, enabled)
	if err != nil {
		return nil, err
	}
	for i := range items {
		projectModel(&items[i])
	}
	return items, nil
}

// FindEnabledModelWithProvider retrieves the active model and its active provider.
func (s *ProviderService) FindEnabledModelWithProvider(modelID string) (*model.AiModel, *model.AiProvider, error) {
	m, p, err := s.repo.FindEnabledModelWithProvider(modelID)
	if err != nil {
		return nil, nil, err
	}
	projectModel(m)
	return m, p, nil
}

// ListAvailableModels retrieves all enabled models whose providers are also enabled.
func (s *ProviderService) ListAvailableModels() ([]model.AiModel, error) {
	items, err := s.repo.ListEnabledModelsWithProviders()
	if err != nil {
		return nil, err
	}
	for i := range items {
		projectModel(&items[i])
	}
	return items, nil
}

func maskProvider(p *model.AiProvider) {
	if p == nil {
		return
	}
	p.HasAPIKey = strings.TrimSpace(p.APIKeyCipher) != ""
	p.APIKeyCipher = ""
}

func projectModel(m *model.AiModel) {
	if m == nil {
		return
	}
	if strings.TrimSpace(m.ReasoningEffortsJSON) != "" {
		var opts []model.ReasoningEffortOption
		if err := json.Unmarshal([]byte(m.ReasoningEffortsJSON), &opts); err == nil {
			m.ReasoningEfforts = opts
		}
	}
	if m.ReasoningEfforts == nil {
		m.ReasoningEfforts = []model.ReasoningEffortOption{}
	}

	if strings.TrimSpace(m.DefaultParamsJSON) != "" {
		var params map[string]any
		if err := json.Unmarshal([]byte(m.DefaultParamsJSON), &params); err == nil {
			m.DefaultParams = params
		}
	}
	if m.DefaultParams == nil {
		m.DefaultParams = map[string]any{}
	}
}

func parseDefaultParams(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	switch val := v.(type) {
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return "{}", nil
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
			return "", errors.New("默认参数必须为合法 JSON 对象格式")
		}
		b, _ := json.Marshal(obj)
		return string(b), nil
	case map[string]any:
		b, err := json.Marshal(val)
		if err != nil {
			return "", errors.New("默认参数必须为合法 JSON 对象格式")
		}
		return string(b), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", errors.New("默认参数必须为合法 JSON 对象格式")
		}
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			return "", errors.New("默认参数必须为合法 JSON 对象格式")
		}
		return string(b), nil
	}
}

func parseReasoningEfforts(opts []model.ReasoningEffortOption) (string, error) {
	if len(opts) == 0 {
		return "[]", nil
	}
	seen := make(map[string]struct{}, len(opts))
	cleaned := make([]model.ReasoningEffortOption, 0, len(opts))
	for _, opt := range opts {
		val := strings.TrimSpace(opt.Value)
		if val == "" {
			return "", errors.New("推理等级档位英文值不能为空")
		}
		if _, dup := seen[val]; dup {
			return "", fmt.Errorf("推理等级档位值重复: %s", val)
		}
		seen[val] = struct{}{}
		lbl := strings.TrimSpace(opt.Label)
		if lbl == "" {
			lbl = val
		}
		cleaned = append(cleaned, model.ReasoningEffortOption{
			Value: val,
			Label: lbl,
		})
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
