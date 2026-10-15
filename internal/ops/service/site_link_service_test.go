package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/rbac"
)

func newSiteLinkService(t *testing.T) (*SiteLinkService, *gorm.DB) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "site_links.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(gdb) })
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	return NewSiteLinkService(repository.NewOpsRepository(gdb)), gdb
}

func countSiteLinks(t *testing.T, gdb *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := gdb.Model(&model.SiteLink{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSiteLinkRejectsBlankAndOversizedIcon(t *testing.T) {
	svc, gdb := newSiteLinkService(t)
	huge := make([]byte, rbac.MaxMenuIconBytes+1)
	for i := range huge {
		huge[i] = 1
	}
	hugeB64 := base64.StdEncoding.EncodeToString(huge)
	created, err := svc.Create(SiteLinkInput{Name: "ok", URL: " https://ok.example "})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "ok" || created.URL != "https://ok.example" || !created.Enabled {
		t.Fatalf("stored trimmed fields = %+v", created)
	}

	cases := []SiteLinkInput{
		{Name: "  ", URL: "https://x.example"},
		{Name: "x", URL: " \t "},
		{Name: "x", URL: "https://x.example", IconBase64: hugeB64},
		{Name: "x", URL: "https://x.example", IconBase64: "not-base64!!!"},
	}
	for _, in := range cases {
		if _, err := svc.Create(in); err == nil {
			t.Fatalf("create accepted %#v", in)
		}
		if _, err := svc.Update(created.ID, in); err == nil {
			t.Fatalf("update accepted %#v", in)
		}
	}
	if n := countSiteLinks(t, gdb); n != 1 {
		t.Fatalf("rejected writes persisted, count=%d", n)
	}
	var row model.SiteLink
	if err := gdb.First(&row, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "ok" || row.URL != "https://ok.example" {
		t.Fatalf("rejected update changed the row: %+v", row)
	}

	exact := make([]byte, rbac.MaxMenuIconBytes)
	for i := range exact {
		exact[i] = 2
	}
	view, err := svc.Update(created.ID, SiteLinkInput{
		Name: "ok", URL: "https://ok.example", IconBase64: base64.StdEncoding.EncodeToString(exact), IconMime: "image/png",
	})
	if err != nil {
		t.Fatalf("32KB icon should pass: %v", err)
	}
	if view.IconMime != "image/png" || len(view.IconBase64) == 0 {
		t.Fatalf("icon not stored: %+v", view)
	}
}

func TestSiteLinkPasswordCipherAndEmpty(t *testing.T) {
	svc, gdb := newSiteLinkService(t)
	const plain = "plain-secret"
	view, err := svc.Create(SiteLinkInput{
		Name: "secret", URL: "https://secret.example", Account: "user", Password: plain, Remark: "note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Password != plain || view.PasswordCipher != "" {
		t.Fatalf("read model = password %q cipher %q", view.Password, view.PasswordCipher)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "password_cipher") {
		t.Fatalf("response exposes ciphertext field: %s", raw)
	}

	var row model.SiteLink
	if err := gdb.First(&row, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.PasswordCipher == "" || row.PasswordCipher == plain || strings.Contains(row.PasswordCipher, plain) {
		t.Fatalf("stored password is not ciphertext: %q", row.PasswordCipher)
	}
	got, err := pkg.Decrypt(row.PasswordCipher)
	if err != nil || got != plain {
		t.Fatalf("decrypt stored password = %q err=%v", got, err)
	}
	if strings.Contains(string(raw), row.PasswordCipher) {
		t.Fatalf("response contains ciphertext: %s", raw)
	}

	cleared, err := svc.Update(view.ID, SiteLinkInput{Name: "secret", URL: "https://secret.example", Password: ""})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Password != "" {
		t.Fatalf("cleared password = %q", cleared.Password)
	}
	if err := gdb.First(&row, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.PasswordCipher != "" {
		t.Fatalf("empty password stored as %q", row.PasswordCipher)
	}

	empty, err := svc.Create(SiteLinkInput{Name: "none", URL: "https://none.example", Password: ""})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Password != "" {
		t.Fatalf("empty create password = %q", empty.Password)
	}
	var emptyRow model.SiteLink
	if err := gdb.First(&emptyRow, empty.ID).Error; err != nil {
		t.Fatal(err)
	}
	if emptyRow.PasswordCipher != "" {
		t.Fatalf("empty create stored %q", emptyRow.PasswordCipher)
	}

	off := false
	disabled, err := svc.Create(SiteLinkInput{Name: "off", URL: "https://off.example", Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("expected disabled link")
	}
	var offRow model.SiteLink
	if err := gdb.First(&offRow, disabled.ID).Error; err != nil {
		t.Fatal(err)
	}
	if offRow.Enabled {
		t.Fatal("disabled flag stored as true")
	}
}

func TestSiteLinkListOrdersByUpdatedAtDesc(t *testing.T) {
	svc, gdb := newSiteLinkService(t)
	older, err := svc.Create(SiteLinkInput{Name: "old", URL: "https://old.example", Password: "old-secret"})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := svc.Create(SiteLinkInput{Name: "new", URL: "https://new.example"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, stamp := range []struct {
		id uint
		at time.Time
	}{
		{older.ID, base},
		{newer.ID, base.Add(time.Hour)},
	} {
		err := gdb.Model(&model.SiteLink{}).Where("id = ?", stamp.id).UpdateColumn("updated_at", stamp.at).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Fatalf("order = %#v", items)
	}
	if items[1].Password != "old-secret" || items[0].Password != "" {
		t.Fatalf("list passwords = %q %q", items[0].Password, items[1].Password)
	}
}
