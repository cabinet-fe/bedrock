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

func TestSiteLinkAccountsCipherAndEmpty(t *testing.T) {
	svc, gdb := newSiteLinkService(t)
	const plain1 = "plain-secret-1"
	const plain2 = "plain-secret-2"
	view, err := svc.Create(SiteLinkInput{
		Name: "secret", URL: "https://secret.example", Remark: "note",
		Accounts: []model.SiteLinkAccount{
			{Account: "admin", Password: plain1, Remark: "管理员"},
			{Account: "tester", Password: plain2, Remark: "测试员"},
			{Account: "   ", Password: "", Remark: "  "}, // completely blank row ignored
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Accounts) != 2 || view.Accounts[0].Password != plain1 || view.Accounts[1].Password != plain2 || view.AccountsJSON != "" {
		t.Fatalf("read model = %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "password_cipher") || strings.Contains(string(raw), "accounts_json") {
		t.Fatalf("response exposes internal ciphertext field: %s", raw)
	}

	var row model.SiteLink
	if err := gdb.First(&row, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AccountsJSON == "" || strings.Contains(row.AccountsJSON, plain1) || strings.Contains(row.AccountsJSON, plain2) {
		t.Fatalf("stored accounts_json contains plaintext or is empty: %q", row.AccountsJSON)
	}
	var stored []storedSiteLinkAccount
	if err := json.Unmarshal([]byte(row.AccountsJSON), &stored); err != nil || len(stored) != 2 {
		t.Fatalf("unmarshal stored accounts: %v err=%v", stored, err)
	}
	got1, err := pkg.Decrypt(stored[0].PasswordCipher)
	if err != nil || got1 != plain1 {
		t.Fatalf("decrypt stored[0] = %q err=%v", got1, err)
	}
	if strings.Contains(string(raw), stored[0].PasswordCipher) {
		t.Fatalf("response contains ciphertext: %s", raw)
	}

	cleared, err := svc.Update(view.ID, SiteLinkInput{
		Name: "secret", URL: "https://secret.example",
		Accounts: []model.SiteLinkAccount{{Account: "admin", Password: "", Remark: "无密码"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Accounts) != 1 || cleared.Accounts[0].Password != "" {
		t.Fatalf("cleared accounts = %+v", cleared.Accounts)
	}
	if err := gdb.First(&row, view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(row.AccountsJSON), &stored); err != nil || len(stored) != 1 || stored[0].PasswordCipher != "" {
		t.Fatalf("empty password stored as %+v", stored)
	}

	empty, err := svc.Create(SiteLinkInput{Name: "none", URL: "https://none.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Accounts) != 0 {
		t.Fatalf("empty create accounts = %+v", empty.Accounts)
	}
	var emptyRow model.SiteLink
	if err := gdb.First(&emptyRow, empty.ID).Error; err != nil {
		t.Fatal(err)
	}
	if emptyRow.AccountsJSON != "[]" {
		t.Fatalf("empty create stored %q", emptyRow.AccountsJSON)
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
	older, err := svc.Create(SiteLinkInput{
		Name: "old", URL: "https://old.example",
		Accounts: []model.SiteLinkAccount{{Account: "u1", Password: "old-secret"}},
	})
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
	if len(items[1].Accounts) != 1 || items[1].Accounts[0].Password != "old-secret" || len(items[0].Accounts) != 0 {
		t.Fatalf("list accounts = %+v / %+v", items[0].Accounts, items[1].Accounts)
	}
}
