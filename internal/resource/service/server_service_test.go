package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/resource/repository"
	"bedrock/internal/resource/service"
)

func setupServerSvc(t *testing.T) *service.ServerService {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "server.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return service.NewServerService(
		repository.NewServerRepository(gdb),
		service.NewCredentialService(repository.NewCredentialRepository(gdb)),
	)
}

func TestServerAuthNormalizeAndPasswordCipher(t *testing.T) {
	svc := setupServerSvc(t)

	created, err := svc.Create(1, service.CreateServerInput{
		Name:     "s1",
		Host:     "127.0.0.1",
		AuthType: "key",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthType != "ssh_key" {
		t.Fatalf("auth_type=%q want ssh_key", created.AuthType)
	}
	if created.HasPassword {
		t.Fatal("ssh_key create must not set has_password")
	}
	if created.PasswordCipher != "" {
		t.Fatal("response must not expose password_cipher")
	}

	agentSSH, err := svc.Create(1, service.CreateServerInput{
		Name:     "s2",
		Host:     "10.0.0.1",
		AuthType: "ssh_agent",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if agentSSH.AuthType != "ssh_key" {
		t.Fatalf("ssh_agent normalize: %q", agentSSH.AuthType)
	}

	pw, err := svc.Create(1, service.CreateServerInput{
		Name:     "s3",
		Host:     "10.0.0.2",
		AuthType: "password",
		Password: "secret-pw",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pw.AuthType != "password" || !pw.HasPassword {
		t.Fatalf("password server: auth=%q has=%v", pw.AuthType, pw.HasPassword)
	}

	empty := ""
	updated, err := svc.Update(pw.ID, service.UpdateServerInput{Password: &empty}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HasPassword {
		t.Fatal("empty password must keep existing cipher")
	}

	got, err := svc.Get(pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasPassword || got.PasswordCipher != "" {
		t.Fatalf("get mask: has=%v cipher=%q", got.HasPassword, got.PasswordCipher)
	}

	cleared, err := svc.Update(pw.ID, service.UpdateServerInput{ClearPassword: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.HasPassword {
		t.Fatal("clear_password must clear has_password")
	}
}

func TestServerAgentCredentialRequiresUse(t *testing.T) {
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "server-agent.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	credSvc := service.NewCredentialService(repository.NewCredentialRepository(gdb))
	srvSvc := service.NewServerService(repository.NewServerRepository(gdb), credSvc)

	cred, err := credSvc.Create(1, service.CreateCredentialInput{
		Name: "tok", Type: "token", Secret: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = srvSvc.Create(1, service.CreateServerInput{
		Name:              "agent-forbid",
		AuthType:          "agent",
		AgentURL:          "http://127.0.0.1:9090",
		AgentCredentialID: &cred.ID,
	}, false)
	if err == nil || !service.IsForbidden(err) {
		t.Fatalf("expected forbidden without credentials:use, got %v", err)
	}

	ok, err := srvSvc.Create(1, service.CreateServerInput{
		Name:              "agent-ok",
		AuthType:          "agent",
		AgentURL:          "http://127.0.0.1:9090",
		AgentCredentialID: &cred.ID,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok.AgentCredentialID == nil || *ok.AgentCredentialID != cred.ID {
		t.Fatalf("agent_credential_id=%v", ok.AgentCredentialID)
	}
	if ok.CredentialID != nil {
		t.Fatal("general credential_id must not appear on API model")
	}
}
