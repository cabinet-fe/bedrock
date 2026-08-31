package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
)

var (
	ErrPATInvalid     = errors.New("invalid or expired token")
	ErrPATWrongScope  = errors.New("token scope insufficient")
	ErrPATBadScope    = errors.New("scope 仅允许 skills:read、agents:run、docs:read、docs:write、dev_docs:read、dev_docs:write、builds:run、pipelines:run、scripts:run")
	ErrPATNotCopyable = errors.New("该令牌创建于加密存储启用前，无法复制明文，请删除后重建")
)

// Allowed PAT expires_in_days presets (UI / API whitelist).
var allowedPATExpireDays = map[int]struct{}{30: {}, 90: {}, 180: {}, 365: {}}

type PATService struct {
	repo  *repository.PATRepository
	audit AuditWriter
}

func NewPATService(repo *repository.PATRepository, audit ...AuditWriter) *PATService {
	svc := &PATService{repo: repo}
	if len(audit) > 0 {
		svc.audit = audit[0]
	}
	return svc
}

type CreatePATInput struct {
	Name          string     `json:"name"`
	Scopes        []string   `json:"scopes"`
	ExpiresAt     *time.Time `json:"expires_at"`
	ExpiresInDays *int       `json:"expires_in_days"`
}

type CreatePATResult struct {
	Token    string                    `json:"token"`
	Metadata model.PersonalAccessToken `json:"metadata"`
}

type RevealPATResult struct {
	TokenCipher string `json:"token_cipher"`
}

func resolvePATExpiresAt(in CreatePATInput) (*time.Time, error) {
	if in.ExpiresAt != nil && in.ExpiresInDays != nil {
		return nil, errors.New("expires_at 与 expires_in_days 不可同时传")
	}
	if in.ExpiresInDays != nil {
		days := *in.ExpiresInDays
		if _, ok := allowedPATExpireDays[days]; !ok {
			return nil, errors.New("expires_in_days 仅允许 30、90、180、365")
		}
		return new(time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)), nil
	}
	if in.ExpiresAt != nil {
		if !in.ExpiresAt.After(time.Now().UTC()) {
			return nil, errors.New("expires_at 必须晚于当前时间")
		}
		return in.ExpiresAt, nil
	}
	return nil, nil
}

func (s *PATService) Create(userID uint, in CreatePATInput) (*CreatePATResult, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("名称不能为空")
	}
	scopes, err := normalizeScopes(in.Scopes)
	if err != nil {
		return nil, err
	}
	expiresAt, err := resolvePATExpiresAt(in)
	if err != nil {
		return nil, err
	}
	plain, err := generatePATPlaintext()
	if err != nil {
		return nil, err
	}
	cipher, err := pkg.Encrypt(plain)
	if err != nil {
		return nil, err
	}
	hash := hashToken(plain)
	scopesJSON, _ := json.Marshal(scopes)
	item := &model.PersonalAccessToken{
		UserID:      userID,
		Name:        name,
		TokenPrefix: plain[:12],
		TokenHash:   hash,
		TokenCipher: cipher,
		ScopesJSON:  string(scopesJSON),
		Scopes:      scopes,
		Copyable:    true,
		ExpiresAt:   expiresAt,
	}
	if err := s.repo.Create(item); err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "pat_create", "personal_access_token", fmt.Sprintf("%d", item.ID),
			fmt.Sprintf("name=%s scopes=%v", name, scopes), "")
	}
	return &CreatePATResult{Token: plain, Metadata: *item}, nil
}

func (s *PATService) List(userID uint, q pkg.ListQuery) ([]model.PersonalAccessToken, int64, error) {
	items, total, err := s.repo.ListByUser(userID, q)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		decodeScopes(&items[i])
		items[i].Copyable = items[i].TokenCipher != ""
		items[i].TokenCipher = "" // never leak ciphertext via list JSON
	}
	return items, total, nil
}

// Reveal returns the stored AES-GCM ciphertext for the owner (client decrypts).
// Legacy hash-only rows return ErrPATNotCopyable.
func (s *PATService) Reveal(userID uint, id uint) (*RevealPATResult, error) {
	token, err := s.repo.Find(id)
	if err != nil {
		return nil, err
	}
	if token.UserID != userID {
		return nil, ErrPATInvalid
	}
	if token.TokenCipher == "" {
		return nil, ErrPATNotCopyable
	}
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "pat_reveal", "personal_access_token", fmt.Sprintf("%d", id),
			fmt.Sprintf("name=%s", token.Name), "")
	}
	return &RevealPATResult{TokenCipher: token.TokenCipher}, nil
}

func (s *PATService) Delete(userID uint, id uint) error {
	token, err := s.repo.Find(id)
	if err != nil {
		return err
	}
	if token.UserID != userID {
		return ErrPATInvalid
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "pat_delete", "personal_access_token", fmt.Sprintf("%d", id),
			fmt.Sprintf("name=%s", token.Name), "")
	}
	return nil
}

// ValidateBearer returns userID and scopes for a valid PAT; otherwise ErrPATInvalid.
// Only accepts prefix "br_" + hex; legacy "br_pat_" tokens are rejected.
func (s *PATService) ValidateBearer(raw string) (userID uint, scopes []string, err error) {
	raw = strings.TrimSpace(raw)
	if !isPATPlaintext(raw) {
		return 0, nil, ErrPATInvalid
	}
	token, err := s.repo.FindByHash(hashToken(raw))
	if err != nil {
		return 0, nil, ErrPATInvalid
	}
	if token.RevokedAt != nil {
		return 0, nil, ErrPATInvalid
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now().UTC()) {
		return 0, nil, ErrPATInvalid
	}
	decodeScopes(token)
	now := time.Now().UTC()
	token.LastUsedAt = &now
	_ = s.repo.Update(token)
	return token.UserID, token.Scopes, nil
}

func (s *PATService) RequireScope(scopes []string, required string) error {
	if slices.Contains(scopes, required) {
		return nil
	}
	return ErrPATWrongScope
}

func normalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, ErrPATBadScope
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		sc = strings.TrimSpace(sc)
		switch sc {
		case model.ScopeSkillsRead, model.ScopeAgentsRun, model.ScopeDocsRead, model.ScopeDocsWrite,
			model.ScopeDevDocsRead, model.ScopeDevDocsWrite,
			model.ScopeBuildsRun, model.ScopePipelinesRun, model.ScopeScriptsRun:
		default:
			return nil, ErrPATBadScope
		}
		if !seen[sc] {
			seen[sc] = true
			out = append(out, sc)
		}
	}
	return out, nil
}

func decodeScopes(token *model.PersonalAccessToken) {
	_ = json.Unmarshal([]byte(token.ScopesJSON), &token.Scopes)
	seen := map[string]bool{}
	out := make([]string, 0, len(token.Scopes))
	for _, sc := range token.Scopes {
		// Legacy docs:publish → docs:write (publish concept removed).
		if sc == "docs:publish" {
			sc = model.ScopeDocsWrite
		}
		if sc == "" || seen[sc] {
			continue
		}
		seen[sc] = true
		out = append(out, sc)
	}
	token.Scopes = out
}

func generatePATPlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "br_" + hex.EncodeToString(buf), nil
}

// isPATPlaintext accepts only "br_" + hex (rejects legacy "br_pat_...").
func isPATPlaintext(raw string) bool {
	if !strings.HasPrefix(raw, "br_") {
		return false
	}
	rest := raw[len("br_"):]
	if len(rest) == 0 {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
