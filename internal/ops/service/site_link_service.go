package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/rbac"
)

// SiteLinkService stores one test-site account. A non-empty password is
// AES-GCM ciphertext in the database; reads return the plaintext.
type SiteLinkService struct {
	repo *repository.OpsRepository
}

func NewSiteLinkService(repo *repository.OpsRepository) *SiteLinkService {
	return &SiteLinkService{repo: repo}
}

// SiteLinkInput is the create and update body. Omitted enabled defaults to true.
// An empty password is stored as an empty string.
type SiteLinkInput struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	IconBase64 string `json:"icon_base64"`
	IconMime   string `json:"icon_mime"`
	Account    string `json:"account"`
	Password   string `json:"password"`
	Remark     string `json:"remark"`
	Enabled    *bool  `json:"enabled"`
}

// SiteLinkView is the read model: plaintext password, no ciphertext.
type SiteLinkView struct {
	model.SiteLink
	Password string `json:"password"`
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
	cipher, err := pkg.Encrypt(in.Password)
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
	item.Account = in.Account
	item.PasswordCipher = cipher
	item.Remark = in.Remark
	item.Enabled = enabled
	return nil
}

func toSiteLinkView(item model.SiteLink) (SiteLinkView, error) {
	plain, err := pkg.Decrypt(item.PasswordCipher)
	if err != nil {
		return SiteLinkView{}, err
	}
	item.PasswordCipher = ""
	return SiteLinkView{SiteLink: item, Password: plain}, nil
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
