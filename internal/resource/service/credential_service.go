package service

import (
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
)

type CredentialService struct {
	repo *repository.CredentialRepository
}

func NewCredentialService(repo *repository.CredentialRepository) *CredentialService {
	return &CredentialService{repo: repo}
}

type CreateCredentialInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Username    string `json:"username"`
	Secret      string `json:"secret"`
	Passphrase  string `json:"passphrase"`
	Description string `json:"description"`
}

type UpdateCredentialInput struct {
	Name        *string `json:"name"`
	Type        *string `json:"type"`
	Username    *string `json:"username"`
	Secret      *string `json:"secret"`     // empty/omit = keep
	Passphrase  *string `json:"passphrase"` // empty/omit = keep; null not distinguished in JSON omitempty callers
	Description *string `json:"description"`
}

func (s *CredentialService) Create(createdBy uint, in CreateCredentialInput) (*model.Credential, error) {
	name := strings.TrimSpace(in.Name)
	typ := normalizeCredentialType(in.Type)
	if name == "" {
		return nil, errorsNew("名称不能为空")
	}
	if typ == "" {
		return nil, errorsNew("凭证类型无效")
	}
	secret := strings.TrimSpace(in.Secret)
	if secret == "" {
		return nil, errorsNew("secret 不能为空")
	}
	enc, err := pkg.Encrypt(secret)
	if err != nil {
		return nil, err
	}
	c := &model.Credential{
		Name:         name,
		Type:         typ,
		Username:     strings.TrimSpace(in.Username),
		SecretCipher: enc,
		Description:  strings.TrimSpace(in.Description),
		CreatedBy:    createdBy,
	}
	if pp := strings.TrimSpace(in.Passphrase); pp != "" {
		ppEnc, err := pkg.Encrypt(pp)
		if err != nil {
			return nil, err
		}
		c.PassphraseCipher = ppEnc
	}
	if err := s.repo.Create(c); err != nil {
		return nil, err
	}
	return maskCredential(c), nil
}

func (s *CredentialService) Update(id uint, in UpdateCredentialInput) (*model.Credential, error) {
	existing, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("凭证不存在")
	}
	if in.Name != nil {
		existing.Name = strings.TrimSpace(*in.Name)
	}
	if in.Type != nil {
		existing.Type = normalizeCredentialType(*in.Type)
	}
	if in.Username != nil {
		existing.Username = strings.TrimSpace(*in.Username)
	}
	if in.Description != nil {
		existing.Description = strings.TrimSpace(*in.Description)
	}
	if existing.Name == "" {
		return nil, errorsNew("名称不能为空")
	}
	if existing.Type == "" {
		return nil, errorsNew("凭证类型无效")
	}
	// Empty secret on update = keep existing ciphertext.
	if in.Secret != nil && strings.TrimSpace(*in.Secret) != "" {
		enc, err := pkg.Encrypt(strings.TrimSpace(*in.Secret))
		if err != nil {
			return nil, err
		}
		existing.SecretCipher = enc
	}
	if in.Passphrase != nil && strings.TrimSpace(*in.Passphrase) != "" {
		enc, err := pkg.Encrypt(strings.TrimSpace(*in.Passphrase))
		if err != nil {
			return nil, err
		}
		existing.PassphraseCipher = enc
	}
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	return maskCredential(existing), nil
}

func (s *CredentialService) Delete(id uint) error {
	if _, err := s.repo.FindByID(id); err != nil {
		return NewNotFound("凭证不存在")
	}
	n1, err := s.repo.CountByRepoRefs(id)
	if err != nil {
		return err
	}
	n2, err := s.repo.CountByServerRefs(id)
	if err != nil {
		return err
	}
	if n1+n2 > 0 {
		return NewConflict("该凭证仍被仓库或服务器引用，无法删除")
	}
	return s.repo.Delete(id)
}

func (s *CredentialService) Get(id uint) (*model.Credential, error) {
	c, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("凭证不存在")
	}
	return maskCredential(c), nil
}

// GetDecrypted returns plaintext for internal use (git fetch / SSH test). Never expose via API.
func (s *CredentialService) GetDecrypted(id uint) (*model.Credential, string, string, error) {
	c, err := s.repo.FindByID(id)
	if err != nil {
		return nil, "", "", NewNotFound("凭证不存在")
	}
	secret, err := pkg.Decrypt(c.SecretCipher)
	if err != nil {
		return nil, "", "", err
	}
	passphrase, err := pkg.Decrypt(c.PassphraseCipher)
	if err != nil {
		return nil, "", "", err
	}
	return c, secret, passphrase, nil
}

func (s *CredentialService) List(q pkg.ListQuery, keyword string) ([]model.Credential, int64, error) {
	items, total, err := s.repo.List(q, keyword)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.Credential, 0, len(items))
	for i := range items {
		out = append(out, *maskCredential(&items[i]))
	}
	return out, total, nil
}

func maskCredential(c *model.Credential) *model.Credential {
	cp := *c
	cp.HasSecret = c.SecretCipher != ""
	cp.HasPassphrase = c.PassphraseCipher != ""
	cp.SecretCipher = ""
	cp.PassphraseCipher = ""
	return &cp
}

func normalizeCredentialType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "password", "token", "ssh_key", "api_key":
		return strings.ToLower(strings.TrimSpace(t))
	default:
		return ""
	}
}

func errorsNew(msg string) error {
	return &validationError{msg}
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
