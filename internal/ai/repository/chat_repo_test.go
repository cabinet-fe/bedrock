package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func setupChatRepoTestDB(t *testing.T) (*gorm.DB, *repository.ChatRepository) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chat_repo_test.sqlite")
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration up: %v", err)
	}

	repo := repository.NewChatRepository(gdb)
	return gdb, repo
}

func TestChatRepository_CRUDAndCascade(t *testing.T) {
	gdb, repo := setupChatRepoTestDB(t)

	userID := uint(10)
	otherUser := uint(20)

	// Create session
	sess := &model.ChatSession{
		UserID:  userID,
		Title:   "测试会话",
		ModelID: "gpt-4o",
	}
	if err := repo.CreateSession(sess); err != nil {
		t.Fatalf("create session failed: %v", err)
	}
	if sess.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	// Find session
	found, err := repo.FindSession(sess.ID, userID)
	if err != nil {
		t.Fatalf("find session failed: %v", err)
	}
	if found.Title != "测试会话" {
		t.Fatalf("expected title '测试会话', got '%s'", found.Title)
	}

	// Other user cannot find
	if _, err := repo.FindSession(sess.ID, otherUser); err == nil {
		t.Fatal("expected error finding session with other user ID")
	}

	// Update session
	found.Title = "新标题"
	found.ModelID = "claude-3-5-sonnet"
	if err := repo.UpdateSession(found); err != nil {
		t.Fatalf("update session failed: %v", err)
	}

	// List sessions
	list, total, err := repo.ListSessions(userID, 1, 10)
	if err != nil {
		t.Fatalf("list sessions failed: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Title != "新标题" {
		t.Fatalf("unexpected list result: total=%d, items=%+v", total, list)
	}

	// Add messages
	msg1 := &model.ChatMessage{
		SessionID: sess.ID,
		UserID:    userID,
		Role:      model.RoleUser,
		Content:   "你好",
	}
	if err := repo.CreateMessage(msg1); err != nil {
		t.Fatalf("create message failed: %v", err)
	}

	msg2 := &model.ChatMessage{
		SessionID: sess.ID,
		UserID:    userID,
		Role:      model.RoleAssistant,
		Content:   "世界",
	}
	if err := repo.CreateMessage(msg2); err != nil {
		t.Fatalf("create message failed: %v", err)
	}

	// List messages
	msgs, err := repo.ListMessagesBySession(sess.ID, userID)
	if err != nil {
		t.Fatalf("list messages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Cascade delete
	if err := repo.DeleteSession(sess.ID, userID); err != nil {
		t.Fatalf("delete session failed: %v", err)
	}

	// Verify session deleted
	if _, err := repo.FindSession(sess.ID, userID); err == nil {
		t.Fatal("expected session to be deleted")
	}

	// Verify messages deleted from DB
	var remainingMsgs int64
	gdb.Model(&model.ChatMessage{}).Where("session_id = ?", sess.ID).Count(&remainingMsgs)
	if remainingMsgs != 0 {
		t.Fatalf("expected 0 messages remaining, got %d", remainingMsgs)
	}
}
