package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bedrock/internal/dashboard/model"
	"bedrock/internal/dashboard/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestSystemStatusReportsHostDiskAndDirectorySizes(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspaces")
	artifacts := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("bedrock-dashboard-disk")
	if err := os.WriteFile(filepath.Join(workspace, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "b.bin"), append(payload, payload...), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{workspace, artifacts})
	status, err := svc.SystemStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.DiskTotalBytes == 0 {
		t.Fatalf("expected host disk total, got %#v", status)
	}
	if status.DiskUsedBytes == 0 && status.DiskUsagePercent == 0 && status.DiskFreeBytes == 0 {
		t.Fatalf("expected host disk usage sample, got %#v", status)
	}
	if len(status.Directories) != 2 {
		t.Fatalf("directories = %#v", status.Directories)
	}
	if status.Directories[0].UsedBytes != uint64(len(payload)) {
		t.Fatalf("workspace used = %d, want %d", status.Directories[0].UsedBytes, len(payload))
	}
	if status.Directories[1].UsedBytes != uint64(len(payload)*2) {
		t.Fatalf("artifacts used = %d, want %d", status.Directories[1].UsedBytes, len(payload)*2)
	}
}

func TestDirectoryUsedBytesMissingRootIsZero(t *testing.T) {
	got, err := directoryUsedBytes(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestLayoutFiltersStaleBuildCardAndRejectsUnauthorizedAddition(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{"dashboard:system_info", "dashboard:system_status"}

	if err := repo.CreateLayout(&model.Layout{
		UserID:    42,
		CardsJSON: `[{"id":"build_summary","visible":true,"order":0},{"id":"system_info","visible":true,"order":1}]`,
	}); err != nil {
		t.Fatal(err)
	}
	layout, err := svc.GetLayout(42, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range layout.Cards {
		if card.ID == CardBuildSummary {
			t.Fatalf("stale build card must be filtered: %#v", layout.Cards)
		}
	}
	_, err = svc.PutLayout(42, false, permissions, []model.CardLayout{
		{ID: CardBuildSummary, Visible: true, Order: 0},
	})
	if !errors.Is(err, ErrUnauthorizedCard) {
		t.Fatalf("expected ErrUnauthorizedCard, got %v", err)
	}
}

func TestLayoutFiltersStaleAgentRunCardAndRejectsUnauthorizedAddition(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{"dashboard:system_info", "dashboard:system_status"}

	if err := repo.CreateLayout(&model.Layout{
		UserID:    43,
		CardsJSON: `[{"id":"agent_run_summary","visible":true,"order":0},{"id":"system_info","visible":true,"order":1}]`,
	}); err != nil {
		t.Fatal(err)
	}
	layout, err := svc.GetLayout(43, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range layout.Cards {
		if card.ID == CardAgentRunSummary {
			t.Fatalf("stale agent run card must be filtered: %#v", layout.Cards)
		}
	}
	_, err = svc.PutLayout(43, false, permissions, []model.CardLayout{
		{ID: CardAgentRunSummary, Visible: true, Order: 0},
	})
	if !errors.Is(err, ErrUnauthorizedCard) {
		t.Fatalf("expected ErrUnauthorizedCard, got %v", err)
	}
}

func TestLayoutPersistsAfterPut(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{
		"cicd_build_runs:view", "dashboard:system_info", "dashboard:system_status",
	}
	input := []model.CardLayout{
		{ID: CardSystemStatus, Visible: false, Order: 0, X: 0, Y: 0, W: 8, H: 3},
		{ID: CardBuildSummary, Visible: true, Order: 1, X: 0, Y: 3, W: 6, H: 4},
		{ID: CardSystemInfo, Visible: true, Order: 2, X: 6, Y: 3, W: 6, H: 3},
	}
	// order is normalized to y*12+x on save.
	want := []model.CardLayout{
		{ID: CardSystemStatus, Visible: false, Order: 0, X: 0, Y: 0, W: 8, H: 3},
		{ID: CardBuildSummary, Visible: true, Order: 36, X: 0, Y: 3, W: 6, H: 4},
		{ID: CardSystemInfo, Visible: true, Order: 42, X: 6, Y: 3, W: 6, H: 3},
	}
	if _, err := svc.PutLayout(7, false, permissions, input); err != nil {
		t.Fatal(err)
	}
	// A fresh service models a restart or a new request lifecycle.
	got, err := NewDashboardService(repo, "test", time.Now(), []string{"."}).GetLayout(7, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cards) != len(want) {
		t.Fatalf("got %d cards, want %d: %#v", len(got.Cards), len(want), got.Cards)
	}
	for index, card := range got.Cards {
		if card != want[index] {
			t.Fatalf("card %d = %#v, want %#v", index, card, want[index])
		}
	}
}

func TestLayoutUpgradesLegacyJSONWithoutGeometry(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{
		"cicd_build_runs:view", "ai_runs:view",
		"dashboard:system_info", "dashboard:system_status",
	}
	if err := repo.CreateLayout(&model.Layout{
		UserID: 8,
		CardsJSON: `[
			{"id":"system_info","visible":false,"order":0},
			{"id":"build_summary","visible":true,"order":1},
			{"id":"agent_run_summary","visible":true,"order":2},
			{"id":"system_status","visible":true,"order":3}
		]`,
	}); err != nil {
		t.Fatal(err)
	}
	layout, err := svc.GetLayout(8, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]model.CardLayout{
		CardBuildSummary:    {ID: CardBuildSummary, Visible: true, Order: 0, X: 0, Y: 0, W: 6, H: 2},
		CardAgentRunSummary: {ID: CardAgentRunSummary, Visible: true, Order: 24, X: 0, Y: 2, W: 6, H: 2},
		CardSystemInfo:      {ID: CardSystemInfo, Visible: false, Order: 72, X: 0, Y: 6, W: 6, H: 3},
		CardSystemStatus:    {ID: CardSystemStatus, Visible: true, Order: 78, X: 6, Y: 6, W: 6, H: 3},
	}
	if len(layout.Cards) != len(want) {
		t.Fatalf("got %d cards, want %d: %#v", len(layout.Cards), len(want), layout.Cards)
	}
	for _, card := range layout.Cards {
		expected, ok := want[card.ID]
		if !ok {
			t.Fatalf("unexpected card %#v", card)
		}
		if card != expected {
			t.Fatalf("card %s = %#v, want %#v", card.ID, card, expected)
		}
	}
}

func TestLayoutRejectsInvalidGeometry(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{"dashboard:system_info"}

	_, err := svc.PutLayout(9, false, permissions, []model.CardLayout{
		{ID: CardSystemInfo, Visible: true, Order: 0, X: 0, Y: 0, W: 1, H: 4},
	})
	if err == nil {
		t.Fatal("expected error for w < 2")
	}

	_, err = svc.PutLayout(9, false, permissions, []model.CardLayout{
		{ID: CardSystemInfo, Visible: true, Order: 0, X: 0, Y: 0, W: 6, H: 1},
	})
	if err == nil {
		t.Fatal("expected error for h < 2")
	}

	_, err = svc.PutLayout(9, false, permissions, []model.CardLayout{
		{ID: CardSystemInfo, Visible: true, Order: 0, X: 0, Y: 0, W: 13, H: 3},
	})
	if err == nil {
		t.Fatal("expected error for w > 12")
	}
}

func TestDefaultLayoutIncludesGeometry(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{
		"cicd_build_runs:view", "ai_runs:view",
		"dashboard:system_info", "dashboard:system_status",
	}
	layout, err := svc.GetLayout(10, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.CardLayout{
		{ID: CardBuildSummary, Visible: true, Order: 0, X: 0, Y: 0, W: 6, H: 2},
		{ID: CardAgentRunSummary, Visible: true, Order: 24, X: 0, Y: 2, W: 6, H: 2},
		{ID: CardSystemInfo, Visible: true, Order: 72, X: 0, Y: 6, W: 6, H: 3},
		{ID: CardSystemStatus, Visible: true, Order: 78, X: 6, Y: 6, W: 6, H: 3},
	}
	if len(layout.Cards) != len(want) {
		t.Fatalf("got %d cards, want %d: %#v", len(layout.Cards), len(want), layout.Cards)
	}
	for i, card := range layout.Cards {
		if card != want[i] {
			t.Fatalf("card %d = %#v, want %#v", i, card, want[i])
		}
	}
}

func TestLayoutIncludesTaskOverviewWithDashboardViewOnly(t *testing.T) {
	repo := newDashboardRepository(t)
	svc := NewDashboardService(repo, "test", time.Now(), []string{"."})
	permissions := []string{"dashboard:view"}

	layout, err := svc.GetLayout(11, false, permissions)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, card := range layout.Cards {
		if card.ID == CardCICDTaskOverview {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cicd_task_overview in layout: %#v", layout.Cards)
	}
}

func newDashboardRepository(t *testing.T) *repository.DashboardRepository {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "dashboard.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return repository.NewDashboardRepository(gdb)
}
