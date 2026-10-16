package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/rbac"
)

// SiteLinkService stores website links with zero or more test accounts.
// Each non-empty password is AES-GCM ciphertext inside accounts_json in the database;
// reads return the plaintext accounts.
type SiteLinkService struct {
	repo *repository.OpsRepository
}

func NewSiteLinkService(repo *repository.OpsRepository) *SiteLinkService {
	return &SiteLinkService{repo: repo}
}

type storedSiteLinkAccount struct {
	Account        string `json:"account"`
	PasswordCipher string `json:"password_cipher"`
	Remark         string `json:"remark"`
}

// SiteLinkInput is the create and update body. Omitted enabled defaults to true.
type SiteLinkInput struct {
	Name       string                  `json:"name"`
	URL        string                  `json:"url"`
	IconBase64 string                  `json:"icon_base64"`
	IconMime   string                  `json:"icon_mime"`
	Accounts   []model.SiteLinkAccount `json:"accounts"`
	Remark     string                  `json:"remark"`
	Enabled    *bool                   `json:"enabled"`
}

// SiteLinkView is the read model: plaintext accounts, no ciphertext.
type SiteLinkView struct {
	model.SiteLink
	Accounts []model.SiteLinkAccount `json:"accounts"`
}

func (s *SiteLinkService) List() ([]SiteLinkView, error) {
	rows, err := s.repo.ListSiteLinks()
	if err != nil {
		return nil, err
	}
	out := make([]SiteLinkView, 0, len(rows))
	for i := range rows {
		view, err := toSiteLinkView(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *SiteLinkService) Create(in SiteLinkInput) (*SiteLinkView, error) {
	item := &model.SiteLink{}
	if err := applySiteLink(item, in); err != nil {
		return nil, err
	}
	if err := s.repo.CreateSiteLink(item); err != nil {
		return nil, err
	}
	view, err := toSiteLinkView(*item)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *SiteLinkService) Update(id uint, in SiteLinkInput) (*SiteLinkView, error) {
	item, err := s.repo.FindSiteLink(id)
	if err != nil {
		return nil, err
	}
	if err := applySiteLink(item, in); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateSiteLink(item); err != nil {
		return nil, err
	}
	view, err := toSiteLinkView(*item)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *SiteLinkService) Delete(id uint) error {
	if _, err := s.repo.FindSiteLink(id); err != nil {
		return err
	}
	return s.repo.DeleteSiteLink(id)
}

func applySiteLink(item *model.SiteLink, in SiteLinkInput) error {
	name := strings.TrimSpace(in.Name)
	url := strings.TrimSpace(in.URL)
	if name == "" {
		return errors.New("名称不能为空")
	}
	if url == "" {
		return errors.New("地址不能为空")
	}
	icon, mime, err := normalizeSiteLinkIcon(in.IconBase64, in.IconMime)
	if err != nil {
		return err
	}
	stored := make([]storedSiteLinkAccount, 0, len(in.Accounts))
	for _, acc := range in.Accounts {
		if strings.TrimSpace(acc.Account) == "" && acc.Password == "" && strings.TrimSpace(acc.Remark) == "" {
			continue
		}
		cipher, err := pkg.Encrypt(acc.Password)
		if err != nil {
			return err
		}
		stored = append(stored, storedSiteLinkAccount{
			Account:        acc.Account,
			PasswordCipher: cipher,
			Remark:         acc.Remark,
		})
	}
	accountsBytes, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	item.Name = name
	item.URL = url
	item.IconBase64 = icon
	item.IconMime = mime
	item.AccountsJSON = string(accountsBytes)
	item.Remark = in.Remark
	item.Enabled = enabled
	return nil
}

func toSiteLinkView(item model.SiteLink) (SiteLinkView, error) {
	accounts := []model.SiteLinkAccount{}
	raw := strings.TrimSpace(item.AccountsJSON)
	if raw != "" && raw != "[]" {
		var stored []storedSiteLinkAccount
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return SiteLinkView{}, err
		}
		accounts = make([]model.SiteLinkAccount, 0, len(stored))
		for _, s := range stored {
			plain, err := pkg.Decrypt(s.PasswordCipher)
			if err != nil {
				return SiteLinkView{}, err
			}
			accounts = append(accounts, model.SiteLinkAccount{
				Account:  s.Account,
				Password: plain,
				Remark:   s.Remark,
			})
		}
	}
	item.AccountsJSON = ""
	return SiteLinkView{SiteLink: item, Accounts: accounts}, nil
}

// normalizeSiteLinkIcon accepts raw Base64 or a data URL. Empty clears the icon.
// Decoded size uses the same 32KB cap as menu icons.
func normalizeSiteLinkIcon(iconBase64, iconMime string) (string, string, error) {
	payload := strings.TrimSpace(iconBase64)
	mime := strings.TrimSpace(iconMime)
	if payload == "" {
		return "", "", nil
	}
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 {
			return "", "", errors.New("无效的 data URL")
		}
		header := strings.TrimPrefix(parts[0], "data:")
		header = strings.TrimSuffix(header, ";base64")
		payload = parts[1]
		if mime == "" {
			mime = header
		}
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return "", "", errors.New("图标 Base64 解码失败")
		}
	}
	if len(raw) > rbac.MaxMenuIconBytes {
		return "", "", fmt.Errorf("图标原始体积不得超过 32KB（当前 %d 字节）", len(raw))
	}
	if len(raw) == 0 {
		return "", "", nil
	}
	return base64.StdEncoding.EncodeToString(raw), mime, nil
}
