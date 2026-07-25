package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
	"bedrock/internal/resource/service"
)

func setupPAT(t *testing.T) *service.PATService {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "pat.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return service.NewPATService(repository.NewPATRepository(gdb))
}

func TestPATPlaintextOnceAndScopes(t *testing.T) {
	pats := setupPAT(t)
	created, err := pats.Create(1, service.CreatePATInput{
		Name: "t", Scopes: []string{model.ScopeSkillsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "br_") || strings.HasPrefix(created.Token, "br_pat_") {
		t.Fatalf("unexpected token %s", created.Token)
	}
	list, _, err := pats.List(1, pkg.ListQuery{Page: 1, PageSize: 20})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %#v", err, list)
	}
	// Metadata list must not include plaintext.
	if strings.Contains(list[0].TokenHash, created.Token) {
		t.Fatal("plaintext must not be stored")
	}
	if _, _, err := pats.ValidateBearer("br_deadbeef"); err == nil {
		t.Fatal("invalid PAT must fail")
	}
	if _, _, err := pats.ValidateBearer("br_pat_" + strings.Repeat("ab", 32)); err == nil {
		t.Fatal("legacy br_pat_ prefix must be rejected")
	}
	uid, scopes, err := pats.ValidateBearer(created.Token)
	if err != nil || uid != 1 {
		t.Fatalf("valid PAT: %v uid=%d", err, uid)
	}
	if err := pats.RequireScope(scopes, model.ScopeAgentsRun); err == nil {
		t.Fatal("wrong scope must 403")
	}
	if err := pats.RequireScope(scopes, model.ScopeSkillsRead); err != nil {
		t.Fatal(err)
	}
	if err := pats.Delete(1, created.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pats.ValidateBearer(created.Token); err == nil {
		t.Fatal("deleted PAT must be invalid")
	}
	list, _, err = pats.List(1, pkg.ListQuery{Page: 1, PageSize: 20})
	if err != nil || len(list) != 0 {
		t.Fatalf("deleted PAT must be removed from list: %v %#v", err, list)
	}
}

func TestPATDocsScopesAndExpiresAt(t *testing.T) {
	pats := setupPAT(t)
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := pats.Create(1, service.CreatePATInput{
		Name: "expired", Scopes: []string{model.ScopeDocsWrite}, ExpiresAt: &past,
	}); err == nil {
		t.Fatal("past expires_at must be rejected")
	}
	future := time.Now().UTC().Add(time.Hour)
	created, err := pats.Create(1, service.CreatePATInput{
		Name: "docs", Scopes: []string{model.ScopeDocsWrite, model.ScopeDocsPublish}, ExpiresAt: &future,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, scopes, err := pats.ValidateBearer(created.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := pats.RequireScope(scopes, model.ScopeDocsWrite); err != nil {
		t.Fatal(err)
	}
	if err := pats.RequireScope(scopes, model.ScopeDocsPublish); err != nil {
		t.Fatal(err)
	}
}

func TestPATExpiresInDays(t *testing.T) {
	pats := setupPAT(t)
	bad := 7
	if _, err := pats.Create(1, service.CreatePATInput{
		Name: "bad-days", Scopes: []string{model.ScopeSkillsRead}, ExpiresInDays: &bad,
	}); err == nil {
		t.Fatal("non-whitelist expires_in_days must be rejected")
	}
	days := 30
	future := time.Now().UTC().Add(time.Hour)
	if _, err := pats.Create(1, service.CreatePATInput{
		Name: "both", Scopes: []string{model.ScopeSkillsRead}, ExpiresAt: &future, ExpiresInDays: &days,
	}); err == nil {
		t.Fatal("expires_at and expires_in_days together must be rejected")
	}
	created, err := pats.Create(1, service.CreatePATInput{
		Name: "days", Scopes: []string{model.ScopeSkillsRead}, ExpiresInDays: &days,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Metadata.ExpiresAt == nil {
		t.Fatal("expires_in_days must persist expires_at")
	}
	wantMin := time.Now().UTC().Add(29 * 24 * time.Hour)
	wantMax := time.Now().UTC().Add(31 * 24 * time.Hour)
	if created.Metadata.ExpiresAt.Before(wantMin) || created.Metadata.ExpiresAt.After(wantMax) {
		t.Fatalf("expires_at out of range: %v", created.Metadata.ExpiresAt)
	}
	never, err := pats.Create(1, service.CreatePATInput{
		Name: "never", Scopes: []string{model.ScopeSkillsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if never.Metadata.ExpiresAt != nil {
		t.Fatal("omit expire fields must mean never expires")
	}
}

func TestPATUserScopedDelete(t *testing.T) {
	pats := setupPAT(t)
	created, err := pats.Create(1, service.CreatePATInput{
		Name: "mine", Scopes: []string{model.ScopeAgentsRun},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pats.Delete(2, created.Metadata.ID); err == nil {
		t.Fatal("other user must not delete someone else's PAT")
	}
	if err := pats.Delete(1, created.Metadata.ID); err != nil {
		t.Fatal(err)
	}
}
