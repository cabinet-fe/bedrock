package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/ai/model"
	airepo "bedrock/internal/ai/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"
	"bedrock/internal/resource/service"
)

type stubGitLister struct {
	branches []string
	err      error
	calls    int
}

func (s *stubGitLister) ListBranches(repoURL, authType, username, password string) ([]string, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.branches...), nil
}

func TestRepositoryDeleteBlockedByAgentBinding(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "repo-del.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}

	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	credRepo := resourcerepo.NewCredentialRepository(gdb)
	repoSvc := service.NewRepositoryService(repoRepo, service.NewCredentialService(credRepo))
	aiRepo := airepo.NewAIRepository(gdb)

	repo := &resourcemodel.Repository{
		Name: "bound", RepoURL: "https://example.com/bound.git", AuthType: "none",
	}
	if err := repoRepo.Create(repo); err != nil {
		t.Fatal(err)
	}
	agent := &model.AiAgent{
		Name: "a", CliKey: "claude_code", Enabled: true, TimeoutSec: 60,
		SkillIDsJSON: "[]", OutputDir: "output",
	}
	if err := aiRepo.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := aiRepo.ReplaceAgentRepoBindings(agent.ID, []model.RepoBinding{
		{RepositoryID: repo.ID, Branch: "main"},
	}); err != nil {
		t.Fatal(err)
	}

	err = repoSvc.Delete(repo.ID)
	if err == nil || !service.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}

	if err := aiRepo.ReplaceAgentRepoBindings(agent.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := repoSvc.Delete(repo.ID); err != nil {
		t.Fatalf("delete after unbind: %v", err)
	}
}

func TestRepositoryBranchCacheSyncAndRead(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "repo-branches.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}

	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	credRepo := resourcerepo.NewCredentialRepository(gdb)
	git := &stubGitLister{branches: []string{"main", "develop"}}
	repoSvc := service.NewRepositoryService(repoRepo, service.NewCredentialService(credRepo), git)

	repo := &resourcemodel.Repository{
		Name: "cached", RepoURL: "https://example.com/cached.git", AuthType: "none",
	}
	if err := repoRepo.Create(repo); err != nil {
		t.Fatal(err)
	}

	items, syncedAt, err := repoSvc.CachedBranches(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || syncedAt != nil {
		t.Fatalf("empty cache expected, items=%v syncedAt=%v", items, syncedAt)
	}
	if git.calls != 0 {
		t.Fatalf("cache read must not hit remote, calls=%d", git.calls)
	}

	synced, err := repoSvc.SyncBranches(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(synced.Branches) != 2 || synced.BranchesSyncedAt == nil {
		t.Fatalf("synced=%#v", synced)
	}
	if git.calls != 1 {
		t.Fatalf("sync calls=%d", git.calls)
	}

	items, syncedAt, err = repoSvc.CachedBranches(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || syncedAt == nil {
		t.Fatalf("items=%v syncedAt=%v", items, syncedAt)
	}
	if git.calls != 1 {
		t.Fatalf("second cache read hit remote, calls=%d", git.calls)
	}

	got, err := repoSvc.Get(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Branches) != 2 {
		t.Fatalf("list decode branches=%v", got.Branches)
	}

	results := repoSvc.SyncBranchesBatch([]uint{repo.ID, 9999})
	if len(results) != 2 || !results[0].OK || results[1].OK {
		t.Fatalf("batch=%#v", results)
	}

	testRes, err := repoSvc.TestFetch(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if testRes["ok"] != true {
		t.Fatalf("test=%v", testRes)
	}
	if git.calls < 3 {
		t.Fatalf("test should refresh remote, calls=%d", git.calls)
	}
}
