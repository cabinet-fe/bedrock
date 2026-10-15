package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/ops/model"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestSiteLinkRepository_ListOrdersByUpdatedAtDesc(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "site_links.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(gdb) })
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up: %v", err)
	}

	repo := NewOpsRepository(gdb)
	older := &model.SiteLink{Name: "old", URL: "https://old.example", PasswordCipher: "cipher-old", Enabled: true}
	mid := &model.SiteLink{Name: "mid", URL: "https://mid.example", Enabled: false}
	newer := &model.SiteLink{
		Name: "new", URL: "https://new.example", Account: "user",
		PasswordCipher: "", Remark: "note", Enabled: true,
	}
	for _, item := range []*model.SiteLink{older, mid, newer} {
		if err := repo.CreateSiteLink(item); err != nil {
			t.Fatalf("create %s: %v", item.Name, err)
		}
	}

	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	stamps := []struct {
		id uint
		at time.Time
	}{
		{older.ID, base},
		{mid.ID, base.Add(time.Hour)},
		{newer.ID, base.Add(2 * time.Hour)},
	}
	for _, stamp := range stamps {
		err := gdb.Model(&model.SiteLink{}).Where("id = ?", stamp.id).UpdateColumn("updated_at", stamp.at).Error
		if err != nil {
			t.Fatalf("set updated_at: %v", err)
		}
	}

	items, err := repo.ListSiteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 links, got %d", len(items))
	}
	got := []uint{items[0].ID, items[1].ID, items[2].ID}
	want := []uint{newer.ID, mid.ID, older.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if items[0].PasswordCipher != "" {
		t.Fatalf("empty password must stay an empty string, got %q", items[0].PasswordCipher)
	}
	if items[2].PasswordCipher != "cipher-old" {
		t.Fatalf("ciphertext = %q", items[2].PasswordCipher)
	}
	if items[1].Enabled {
		t.Fatal("expected mid link to stay disabled")
	}

	older.Name = "old-edited"
	older.PasswordCipher = ""
	if err := repo.UpdateSiteLink(older); err != nil {
		t.Fatal(err)
	}
	items, err = repo.ListSiteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID != older.ID || items[0].Name != "old-edited" || items[0].PasswordCipher != "" {
		t.Fatalf("updated row not newest: %+v", items[0])
	}

	if err := repo.DeleteSiteLink(mid.ID); err != nil {
		t.Fatal(err)
	}
	items, err = repo.ListSiteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 links after delete, got %d", len(items))
	}
	for _, item := range items {
		if item.ID == mid.ID {
			t.Fatal("deleted link still listed")
		}
	}
}
