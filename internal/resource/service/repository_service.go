package service

import (
	"encoding/json"
	"strings"
	"time"

	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
)

// GitLister abstracts git ls-remote for tests.
type GitLister interface {
	ListBranches(repoURL, authType, username, password string) ([]string, error)
}

type defaultGitLister struct{}

func (defaultGitLister) ListBranches(repoURL, authType, username, password string) ([]string, error) {
	return engine.GitListBranches(repoURL, authType, username, password)
}

type RepositoryService struct {
	repo  *repository.RepositoryRepository
	creds *CredentialService
	git   GitLister
}

func NewRepositoryService(repo *repository.RepositoryRepository, creds *CredentialService, git ...GitLister) *RepositoryService {
	lister := GitLister(defaultGitLister{})
	if len(git) > 0 && git[0] != nil {
		lister = git[0]
	}
	return &RepositoryService{repo: repo, creds: creds, git: lister}
}

type CreateRepositoryInput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Tags         string `json:"tags"`
	RepoURL      string `json:"repo_url"`
	AuthType     string `json:"auth_type"`
	CredentialID *uint  `json:"credential_id"`
}

type UpdateRepositoryInput struct {
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	Tags            *string `json:"tags"`
	RepoURL         *string `json:"repo_url"`
	AuthType        *string `json:"auth_type"`
	CredentialID    *uint   `json:"credential_id"`
	ClearCredential bool    `json:"clear_credential"`
}

func (s *RepositoryService) Create(createdBy uint, in CreateRepositoryInput, canUseCredential bool) (*model.Repository, error) {
	name := strings.TrimSpace(in.Name)
	url := strings.TrimSpace(in.RepoURL)
	if name == "" || url == "" {
		return nil, errorsNew("名称与仓库 URL 不能为空")
	}
	authType := normalizeRepoAuth(in.AuthType)
	if authType == "credential" {
		if in.CredentialID == nil || *in.CredentialID == 0 {
			return nil, errorsNew("auth_type=credential 时必须提供 credential_id")
		}
		if !canUseCredential {
			return nil, NewForbidden("绑定凭证需要 resource_credentials:use 权限")
		}
		if _, err := s.creds.Get(*in.CredentialID); err != nil {
			return nil, errorsNew("凭证不存在")
		}
	} else {
		in.CredentialID = nil
	}
	repo := &model.Repository{
		Name:         name,
		Description:  strings.TrimSpace(in.Description),
		Tags:         strings.TrimSpace(in.Tags),
		RepoURL:      url,
		AuthType:     authType,
		CredentialID: in.CredentialID,
		CreatedBy:    createdBy,
	}
	if err := s.repo.Create(repo); err != nil {
		return nil, err
	}
	return repo, nil
}

func (s *RepositoryService) Update(id uint, in UpdateRepositoryInput, canUseCredential bool) (*model.Repository, error) {
	existing, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("仓库不存在")
	}
	prevCred := existing.CredentialID
	if in.Name != nil {
		existing.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		existing.Description = strings.TrimSpace(*in.Description)
	}
	if in.Tags != nil {
		existing.Tags = strings.TrimSpace(*in.Tags)
	}
	if in.RepoURL != nil {
		existing.RepoURL = strings.TrimSpace(*in.RepoURL)
	}
	if in.AuthType != nil {
		existing.AuthType = normalizeRepoAuth(*in.AuthType)
	}
	if in.ClearCredential {
		existing.CredentialID = nil
		existing.AuthType = "none"
	} else if in.CredentialID != nil {
		if !credentialIDEqual(prevCred, in.CredentialID) {
			if !canUseCredential {
				return nil, NewForbidden("绑定/修改凭证需要 resource_credentials:use 权限")
			}
		}
		if *in.CredentialID == 0 {
			existing.CredentialID = nil
		} else {
			if _, err := s.creds.Get(*in.CredentialID); err != nil {
				return nil, errorsNew("凭证不存在")
			}
			existing.CredentialID = in.CredentialID
			existing.AuthType = "credential"
		}
	}
	if existing.AuthType == "credential" && (existing.CredentialID == nil || *existing.CredentialID == 0) {
		return nil, errorsNew("auth_type=credential 时必须提供 credential_id")
	}
	if existing.Name == "" || existing.RepoURL == "" {
		return nil, errorsNew("名称与仓库 URL 不能为空")
	}
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *RepositoryService) Delete(id uint) error {
	if _, err := s.repo.FindByID(id); err != nil {
		return NewNotFound("仓库不存在")
	}
	n, err := s.repo.CountJobs(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return NewConflict("该仓库仍被构建任务引用，无法删除")
	}
	bindings, err := s.repo.CountAgentBindings(id)
	if err != nil {
		return err
	}
	if bindings > 0 {
		return NewConflict("该仓库仍被智能体绑定引用，无法删除")
	}
	return s.repo.Delete(id)
}

func (s *RepositoryService) Get(id uint) (*model.Repository, error) {
	repo, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("仓库不存在")
	}
	decodeRepoBranches(repo)
	return repo, nil
}

func (s *RepositoryService) List(q pkg.ListQuery, keyword string) ([]model.Repository, int64, error) {
	items, total, err := s.repo.List(q, keyword)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		decodeRepoBranches(&items[i])
	}
	return items, total, nil
}

// CachedBranches returns previously synced branch names (may be empty).
func (s *RepositoryService) CachedBranches(id uint) (items []string, syncedAt *time.Time, err error) {
	repo, err := s.repo.FindByID(id)
	if err != nil {
		return nil, nil, NewNotFound("仓库不存在")
	}
	decodeRepoBranches(repo)
	return repo.Branches, repo.BranchesSyncedAt, nil
}

// SyncBranches fetches remote branches and writes the cache.
func (s *RepositoryService) SyncBranches(id uint) (*model.Repository, error) {
	repo, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("仓库不存在")
	}
	branches, err := s.fetchRemoteBranches(repo)
	if err != nil {
		return nil, err
	}
	if err := s.writeBranchCache(repo, branches); err != nil {
		return nil, err
	}
	decodeRepoBranches(repo)
	return repo, nil
}

// BranchSyncResult is one item in a batch sync-branches response.
type BranchSyncResult struct {
	ID          uint       `json:"id"`
	OK          bool       `json:"ok"`
	BranchCount int        `json:"branch_count,omitempty"`
	Error       string     `json:"error,omitempty"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
}

// SyncBranchesBatch syncs the given repository IDs. Empty ids returns an empty list.
func (s *RepositoryService) SyncBranchesBatch(ids []uint) []BranchSyncResult {
	out := make([]BranchSyncResult, 0, len(ids))
	for _, id := range ids {
		repo, err := s.SyncBranches(id)
		if err != nil {
			out = append(out, BranchSyncResult{ID: id, OK: false, Error: err.Error()})
			continue
		}
		out = append(out, BranchSyncResult{
			ID: id, OK: true, BranchCount: len(repo.Branches), SyncedAt: repo.BranchesSyncedAt,
		})
	}
	return out
}

func (s *RepositoryService) TestFetch(id uint) (map[string]interface{}, error) {
	repo, err := s.SyncBranches(id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"ok":           true,
		"branch_count": len(repo.Branches),
		"branches":     repo.Branches,
		"synced_at":    repo.BranchesSyncedAt,
	}, nil
}

func (s *RepositoryService) fetchRemoteBranches(repo *model.Repository) ([]string, error) {
	authType := "none"
	username, password := "", ""
	if repo.AuthType == "credential" && repo.CredentialID != nil {
		cred, secret, _, err := s.creds.GetDecrypted(*repo.CredentialID)
		if err != nil {
			return nil, err
		}
		username = cred.Username
		password = secret
		authType = "password"
		if cred.Type == "ssh_key" {
			authType = "ssh"
		}
	}
	return s.git.ListBranches(repo.RepoURL, authType, username, password)
}

func (s *RepositoryService) writeBranchCache(repo *model.Repository, branches []string) error {
	if branches == nil {
		branches = []string{}
	}
	raw, err := json.Marshal(branches)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	repo.BranchesJSON = string(raw)
	repo.BranchesSyncedAt = &now
	repo.Branches = branches
	return s.repo.Update(repo)
}

func decodeRepoBranches(repo *model.Repository) {
	if repo == nil {
		return
	}
	if strings.TrimSpace(repo.BranchesJSON) == "" {
		repo.Branches = []string{}
		return
	}
	var items []string
	if err := json.Unmarshal([]byte(repo.BranchesJSON), &items); err != nil || items == nil {
		repo.Branches = []string{}
		return
	}
	repo.Branches = items
}

func normalizeRepoAuth(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "credential":
		return "credential"
	default:
		return "none"
	}
}

func credentialIDEqual(a, b *uint) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
