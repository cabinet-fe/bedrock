package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func setupChatTestDB(t *testing.T) (*gorm.DB, *service.ChatService) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "chat_test.sqlite")
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
	svc := service.NewChatService(repo)
	return gdb, svc
}

func TestChatService_SessionLifecycleAndIsolation(t *testing.T) {
	gdb, svc := setupChatTestDB(t)

	userA := uint(101)
	userB := uint(102)

	// 1. User A creates a session with default title
	sessA1, err := svc.CreateSession(userA, model.ChatSessionInput{
		ModelID: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sessA1.Title != "新对话" {
		t.Fatalf("expected title '新对话', got '%s'", sessA1.Title)
	}

	// 2. User A creates another session with custom title
	sessA2, err := svc.CreateSession(userA, model.ChatSessionInput{
		Title:   "代码审查助手",
		ModelID: "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sessA2.Title != "代码审查助手" {
		t.Fatalf("expected title '代码审查助手', got '%s'", sessA2.Title)
	}

	// 3. User A lists sessions -> should see 2 sessions
	itemsA, totalA, err := svc.ListSessions(userA, 1, 20)
	if err != nil {
		t.Fatalf("list sessions error: %v", err)
	}
	if totalA != 2 || len(itemsA) != 2 {
		t.Fatalf("expected 2 sessions for User A, got %d", totalA)
	}

	// 4. User B lists sessions -> should see 0 sessions (isolation)
	itemsB, totalB, err := svc.ListSessions(userB, 1, 20)
	if err != nil {
		t.Fatalf("user B list sessions error: %v", err)
	}
	if totalB != 0 || len(itemsB) != 0 {
		t.Fatalf("expected 0 sessions for User B, got %d", totalB)
	}

	// 5. User B tries to get User A's session -> must fail
	if _, err := svc.GetSession(sessA1.ID, userB); err == nil {
		t.Fatalf("expected error when User B accesses User A's session, got nil")
	}

	// 6. User B tries to update User A's session -> must fail
	if _, err := svc.UpdateSession(sessA1.ID, userB, model.ChatSessionInput{Title: "Hacked"}); err == nil {
		t.Fatalf("expected error when User B updates User A's session, got nil")
	}

	// 7. User A updates own session
	updated, err := svc.UpdateSession(sessA1.ID, userA, model.ChatSessionInput{Title: "前端优化探讨", ModelID: "deepseek-chat"})
	if err != nil {
		t.Fatalf("user A update session failed: %v", err)
	}
	if updated.Title != "前端优化探讨" || updated.ModelID != "deepseek-chat" {
		t.Fatalf("updated fields mismatch: title=%s, model=%s", updated.Title, updated.ModelID)
	}

	// 8. User A adds messages to session
	msg1, err := svc.CreateMessage(sessA1.ID, userA, model.ChatMessageInput{
		Role:    model.RoleUser,
		Content: "如何优化 Vue3 渲染性能？",
	})
	if err != nil {
		t.Fatalf("failed to create user message: %v", err)
	}
	if msg1.ID == 0 {
		t.Fatalf("expected non-zero message ID")
	}

	_, err = svc.CreateMessage(sessA1.ID, userA, model.ChatMessageInput{
		Role:             model.RoleAssistant,
		Content:          "可以使用 v-once、shallowRef 以及虚拟滚动...",
		ReasoningContent: "思考过程：分析 Vue 3 核心渲染机制",
	})
	if err != nil {
		t.Fatalf("failed to create assistant message: %v", err)
	}

	// 9. User B tries to read messages of sessA1 -> must fail
	if _, err := svc.ListMessages(sessA1.ID, userB); err == nil {
		t.Fatalf("expected error when User B reads User A's messages, got nil")
	}

	// 10. User B tries to add message into sessA1 -> must fail
	if _, err := svc.CreateMessage(sessA1.ID, userB, model.ChatMessageInput{
		Role:    model.RoleUser,
		Content: "B tries to inject",
	}); err == nil {
		t.Fatalf("expected error when User B creates message in User A's session, got nil")
	}

	// 11. User A lists messages -> should see 2 messages in order
	msgs, err := svc.ListMessages(sessA1.ID, userA)
	if err != nil {
		t.Fatalf("user A list messages error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleUser || msgs[1].Role != model.RoleAssistant {
		t.Fatalf("messages role order incorrect: %s, %s", msgs[0].Role, msgs[1].Role)
	}
	if msgs[1].ReasoningContent != "思考过程：分析 Vue 3 核心渲染机制" {
		t.Fatalf("reasoning content mismatch: %s", msgs[1].ReasoningContent)
	}

	// 12. Verify cascade deletion: delete session A1
	// Check count in DB before deletion
	var msgCountBefore int64
	gdb.Model(&model.ChatMessage{}).Where("session_id = ?", sessA1.ID).Count(&msgCountBefore)
	if msgCountBefore != 2 {
		t.Fatalf("expected 2 messages in DB before delete, got %d", msgCountBefore)
	}

	// User B tries to delete User A's session -> must fail
	if err := svc.DeleteSession(sessA1.ID, userB); err == nil {
		t.Fatalf("expected error when User B deletes User A's session, got nil")
	}

	// User A deletes own session
	if err := svc.DeleteSession(sessA1.ID, userA); err != nil {
		t.Fatalf("user A delete session error: %v", err)
	}

	// Verify session is gone
	if _, err := svc.GetSession(sessA1.ID, userA); err == nil {
		t.Fatalf("expected session to be deleted, but still found")
	}

	// Verify messages are cascaded deleted from DB
	var msgCountAfter int64
	gdb.Model(&model.ChatMessage{}).Where("session_id = ?", sessA1.ID).Count(&msgCountAfter)
	if msgCountAfter != 0 {
		t.Fatalf("expected 0 messages after cascade deletion, got %d", msgCountAfter)
	}
}

func TestChatService_SaveExchangeAndAutoTitle(t *testing.T) {
	_, svc := setupChatTestDB(t)
	userID := uint(201)

	// Create session with default title
	sess, err := svc.CreateSession(userID, model.ChatSessionInput{
		Title: "新对话",
	})
	if err != nil {
		t.Fatal(err)
	}

	userQ := "请介绍一下 Go 1.26 的新特性与并发优势"
	assistantA := "Go 1.26 进一步提升了垃圾回收和调度效率..."
	reasoning := "深入检索 Go 官方发版说明"

	err = svc.SaveExchange(sess.ID, userID, userQ, assistantA, reasoning)
	if err != nil {
		t.Fatalf("save exchange failed: %v", err)
	}

	// Verify auto title updated
	reloaded, err := svc.GetSession(sess.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title == "新对话" || !strings.Contains(reloaded.Title, "Go 1.26") {
		t.Fatalf("expected session title to be updated from '新对话', got '%s'", reloaded.Title)
	}

	// Verify messages saved
	msgs, err := svc.ListMessages(sess.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != userQ || msgs[1].Content != assistantA || msgs[1].ReasoningContent != reasoning {
		t.Fatalf("persisted message contents do not match")
	}
}
