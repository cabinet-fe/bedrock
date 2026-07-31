package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
)

func TestUpServerAuthInline_MigratesAuthAndPasswordCipher(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m039.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Apply migrations up to but not including 000039 by registering path via full Up after seed.
	// Seed tables via earlier migrations first.
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("full up (idempotent): %v", err)
	}

	// Re-run data logic against synthetic rows: insert legacy-shaped servers/credentials then invoke up again.
	// password_cipher column exists after full Up; clear and re-seed legacy state.
	if err := gdb.Exec(`DELETE FROM servers`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`DELETE FROM credentials`).Error; err != nil {
		t.Fatal(err)
	}

	if err := gdb.Exec(`
		INSERT INTO credentials (id, name, type, username, secret_cipher, created_by, created_at, updated_at)
		VALUES
			(1, 'pw', 'password', 'u', 'cipher-pw', 1, datetime('now'), datetime('now')),
			(2, 'key', 'ssh_key', 'u', 'cipher-pem', 1, datetime('now'), datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO servers (id, name, host, port, os_type, username, auth_type, credential_id, password_cipher, status, created_by, created_at, updated_at)
		VALUES
			(1, 'a', 'h1', 22, 'linux', 'u', 'key', 2, '', 'unknown', 1, datetime('now'), datetime('now')),
			(2, 'b', 'h2', 22, 'linux', 'u', 'ssh_agent', NULL, '', 'unknown', 1, datetime('now'), datetime('now')),
			(3, 'c', 'h3', 22, 'linux', 'u', 'password', 1, '', 'unknown', 1, datetime('now'), datetime('now')),
			(4, 'd', 'h4', 22, 'linux', 'u', 'password', 2, '', 'unknown', 1, datetime('now'), datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}

	if err := upServerAuthInline(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("upServerAuthInline: %v", err)
	}

	type row struct {
		ID             uint
		AuthType       string
		CredentialID   *uint
		PasswordCipher string
	}
	var rows []row
	if err := gdb.Table("servers").Select("id", "auth_type", "credential_id", "password_cipher").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows=%d", len(rows))
	}
	for _, r := range rows {
		if r.CredentialID != nil {
			t.Fatalf("server %d still has credential_id=%v", r.ID, *r.CredentialID)
		}
	}
	if rows[0].AuthType != "ssh_key" || rows[1].AuthType != "ssh_key" {
		t.Fatalf("key/ssh_agent → ssh_key: %#v %#v", rows[0], rows[1])
	}
	if rows[2].AuthType != "password" || rows[2].PasswordCipher != "cipher-pw" {
		t.Fatalf("password copy: %#v", rows[2])
	}
	if rows[3].AuthType != "ssh_key" {
		t.Fatalf("ssh_key credential binding → ssh_key: %#v", rows[3])
	}
}
