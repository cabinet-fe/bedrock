package service_test

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
	"bedrock/internal/ws"
)

func setupNotifDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Notification{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestNotificationService_PushBroadcastsChannel(t *testing.T) {
	db := setupNotifDB(t)
	hub := ws.NewHub()
	defer hub.Shutdown()
	svc := service.NewNotificationService(repository.NewNotificationRepository(db), hub)

	recv := make(chan []byte, 1)
	client := &ws.Client{
		Send:    recv,
		Channel: "notifications:7",
		UserID:  7,
	}
	hub.Register(client)
	time.Sleep(20 * time.Millisecond)

	runID := uint(42)
	n, err := svc.Push(service.PushInput{
		UserID: 7, Type: "build_run_failed", Title: "构建 #1 失败", Message: "boom", BuildRunID: &runID,
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if n.ID == 0 || n.IsRead {
		t.Fatalf("unexpected notification: %+v", n)
	}

	select {
	case raw := <-recv:
		var got model.Notification
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ID != n.ID || got.Type != "build_run_failed" || got.BuildRunID == nil || *got.BuildRunID != 42 {
			t.Fatalf("payload mismatch: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WS broadcast")
	}

	items, total, err := svc.ListByUser(7, nil, pkg.ListQuery{Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list: items=%d total=%d err=%v", len(items), total, err)
	}
	if err := svc.MarkRead(n.ID, 7); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	unread, err := svc.CountUnread(7)
	if err != nil || unread != 0 {
		t.Fatalf("unread=%d err=%v", unread, err)
	}
}

func TestNotificationService_ListByUserIsReadFilter(t *testing.T) {
	db := setupNotifDB(t)
	hub := ws.NewHub()
	defer hub.Shutdown()
	svc := service.NewNotificationService(repository.NewNotificationRepository(db), hub)

	read, err := svc.Push(service.PushInput{UserID: 9, Type: "build_run_success", Title: "构建 #1 成功"})
	if err != nil {
		t.Fatalf("push read candidate: %v", err)
	}
	if _, err := svc.Push(service.PushInput{UserID: 9, Type: "build_run_failed", Title: "构建 #2 失败"}); err != nil {
		t.Fatalf("push unread: %v", err)
	}
	if err := svc.MarkRead(read.ID, 9); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	q := pkg.ListQuery{Page: 1, PageSize: 20}
	isRead := false
	unreadItems, unreadTotal, err := svc.ListByUser(9, &isRead, q)
	if err != nil || unreadTotal != 1 || len(unreadItems) != 1 {
		t.Fatalf("unread list: items=%d total=%d err=%v", len(unreadItems), unreadTotal, err)
	}
	if unreadItems[0].IsRead {
		t.Fatalf("expected unread item, got %+v", unreadItems[0])
	}

	isRead = true
	readItems, readTotal, err := svc.ListByUser(9, &isRead, q)
	if err != nil || readTotal != 1 || len(readItems) != 1 {
		t.Fatalf("read list: items=%d total=%d err=%v", len(readItems), readTotal, err)
	}
	if !readItems[0].IsRead {
		t.Fatalf("expected read item, got %+v", readItems[0])
	}

	_, allTotal, err := svc.ListByUser(9, nil, q)
	if err != nil || allTotal != 2 {
		t.Fatalf("unfiltered list: total=%d err=%v", allTotal, err)
	}
}
