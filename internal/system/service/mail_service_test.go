package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

const testSMTPPassword = "smtp-secret-pass"

func setupMailService(t *testing.T) (*service.MailService, *gorm.DB) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("init encryption: %v", err)
	}
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mail.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gdb.AutoMigrate(&model.MailSMTPConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return service.NewMailService(repository.NewMailRepository(gdb)), gdb
}

func TestMailService_SaveConfigEncryptsPassword(t *testing.T) {
	svc, gdb := setupMailService(t)

	view, err := svc.SaveConfig(service.MailSMTPConfigInput{
		Host:        "smtp.example.com",
		Port:        465,
		Username:    "noreply@example.com",
		Password:    testSMTPPassword,
		FromAddress: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if view.Password != "******" {
		t.Fatalf("expected masked password, got %q", view.Password)
	}

	// API response shape carries neither plaintext nor ciphertext.
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, testSMTPPassword) {
		t.Fatalf("view JSON leaks plaintext password: %s", body)
	}
	if strings.Contains(body, "password_cipher") {
		t.Fatalf("view JSON leaks password cipher field: %s", body)
	}

	// Database row stores ciphertext only.
	var row model.MailSMTPConfig
	if err := gdb.First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.PasswordCipher == "" || row.PasswordCipher == testSMTPPassword {
		t.Fatalf("expected ciphertext in DB, got %q", row.PasswordCipher)
	}
	plain, err := pkg.Decrypt(row.PasswordCipher)
	if err != nil {
		t.Fatalf("decrypt stored cipher: %v", err)
	}
	if plain != testSMTPPassword {
		t.Fatalf("decrypted mismatch: %q", plain)
	}
}

func TestMailService_SaveConfigKeepsPasswordWhenEmpty(t *testing.T) {
	svc, gdb := setupMailService(t)
	if _, err := svc.SaveConfig(service.MailSMTPConfigInput{
		Host: "smtp.example.com", Port: 587, Username: "noreply@example.com",
		Password: testSMTPPassword, FromAddress: "noreply@example.com",
	}); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	view, err := svc.SaveConfig(service.MailSMTPConfigInput{
		Host: "smtp2.example.com", Port: 25, Username: "other@example.com",
		Password: "", FromAddress: "other@example.com",
	})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if view.Host != "smtp2.example.com" || view.Password != "******" {
		t.Fatalf("unexpected view after update: %+v", view)
	}

	var row model.MailSMTPConfig
	if err := gdb.First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	plain, err := pkg.Decrypt(row.PasswordCipher)
	if err != nil || plain != testSMTPPassword {
		t.Fatalf("stored password should be preserved, plain=%q err=%v", plain, err)
	}
}

func TestMailService_SendTestUsesInjectedSender(t *testing.T) {
	svc, _ := setupMailService(t)
	if _, err := svc.SaveConfig(service.MailSMTPConfigInput{
		Host: "smtp.example.com", Port: 587, Username: "noreply@example.com",
		Password: testSMTPPassword, FromAddress: "noreply@example.com",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var (
		gotCfg    model.MailSMTPConfig
		gotPass   string
		gotTo     string
		gotSubj   string
		sendCalls int
	)
	svc.SetSender(func(cfg model.MailSMTPConfig, password, to, subject, body string) error {
		sendCalls++
		gotCfg, gotPass, gotTo, gotSubj = cfg, password, to, subject
		return nil
	})

	res, err := svc.SendTest("user@example.com")
	if err != nil {
		t.Fatalf("send test: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success result, got %+v", res)
	}
	if sendCalls != 1 {
		t.Fatalf("expected exactly one send, got %d", sendCalls)
	}
	if gotTo != "user@example.com" || gotPass != testSMTPPassword || gotSubj == "" {
		t.Fatalf("unexpected sender args: to=%q pass=%q subject=%q", gotTo, gotPass, gotSubj)
	}
	if gotCfg.Host != "smtp.example.com" || gotCfg.Port != 587 {
		t.Fatalf("unexpected config passed to sender: %+v", gotCfg)
	}

	// Sender failure is reported in the result, not as an error.
	svc.SetSender(func(model.MailSMTPConfig, string, string, string, string) error {
		return errors.New("dial failed")
	})
	res, err = svc.SendTest("user@example.com")
	if err != nil {
		t.Fatalf("send test with failing sender: %v", err)
	}
	if res.Success || !strings.Contains(res.Message, "dial failed") {
		t.Fatalf("expected failure result with reason, got %+v", res)
	}
}

func TestMailService_SendTestGuards(t *testing.T) {
	svc, _ := setupMailService(t)

	if _, err := svc.SendTest("user@example.com"); err == nil || !strings.Contains(err.Error(), "SMTP 未配置") {
		t.Fatalf("expected unconfigured error, got %v", err)
	}
	if _, err := svc.SaveConfig(service.MailSMTPConfigInput{
		Host: "smtp.example.com", Port: 25, FromAddress: "noreply@example.com",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := svc.SendTest("not-an-email"); err == nil || !strings.Contains(err.Error(), "收件邮箱") {
		t.Fatalf("expected invalid recipient error, got %v", err)
	}
}

// ---------- failure notice dispatch ----------

type sentMail struct {
	to, subject, body string
}

func setupMailDispatcher(t *testing.T) (*service.MailDispatcher, *service.MailService, *gorm.DB) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("init encryption: %v", err)
	}
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dispatch.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gdb.AutoMigrate(&model.MailSMTPConfig{}, &authmodel.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mailSvc := service.NewMailService(repository.NewMailRepository(gdb))
	dispatcher := service.NewMailDispatcher(mailSvc, authrepo.NewUserRepository(gdb), nil)
	dispatcher.SetSyncDispatch(true)
	return dispatcher, mailSvc, gdb
}

func createMailUser(t *testing.T, gdb *gorm.DB, id uint, email string) {
	t.Helper()
	user := &authmodel.User{ID: id, Username: fmt.Sprintf("user%d", id), PasswordHash: "x", Email: email}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create user %d: %v", id, err)
	}
}

func saveSMTPConfig(t *testing.T, mailSvc *service.MailService) {
	t.Helper()
	if _, err := mailSvc.SaveConfig(service.MailSMTPConfigInput{
		Host: "smtp.example.com", Port: 587, Username: "noreply@example.com",
		Password: testSMTPPassword, FromAddress: "noreply@example.com",
	}); err != nil {
		t.Fatalf("save smtp config: %v", err)
	}
}

func captureSender(t *testing.T, mailSvc *service.MailService) *[]sentMail {
	t.Helper()
	calls := &[]sentMail{}
	mailSvc.SetSender(func(cfg model.MailSMTPConfig, password, to, subject, body string) error {
		*calls = append(*calls, sentMail{to: to, subject: subject, body: body})
		return nil
	})
	return calls
}

func TestMailDispatcher_StatusFilter(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	saveSMTPConfig(t, mailSvc)
	createMailUser(t, gdb, 7, "alice@example.com")
	calls := captureSender(t, mailSvc)

	dispatcher.DispatchBuildRunFailure(7, 0, 42, 3, "success", "")
	dispatcher.DispatchBuildRunFailure(7, 0, 42, 3, "cancelled", "")
	if len(*calls) != 0 {
		t.Fatalf("success/cancelled must not mail, got %+v", *calls)
	}
	dispatcher.DispatchBuildRunFailure(7, 0, 42, 3, "failed", "boom")
	if len(*calls) != 1 {
		t.Fatalf("expected one mail for failed, got %d", len(*calls))
	}
	dispatcher.DispatchBuildRunFailure(7, 0, 42, 3, "interrupted", "")
	if len(*calls) != 2 {
		t.Fatalf("expected second mail for interrupted, got %d", len(*calls))
	}
	if got := (*calls)[0]; got.to != "alice@example.com" ||
		!strings.Contains(got.subject, "构建 #3 失败") || !strings.Contains(got.body, "boom") {
		t.Fatalf("unexpected mail: %+v", got)
	}
}

func TestMailDispatcher_RecipientResolution(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	saveSMTPConfig(t, mailSvc)
	createMailUser(t, gdb, 7, "alice@example.com")
	createMailUser(t, gdb, 9, "bob@example.com")
	calls := captureSender(t, mailSvc)

	// Manual/API trigger: the triggerer wins even when a creator fallback exists.
	dispatcher.DispatchAgentRunFailure(7, 9, 5, "failed", "boom")
	if len(*calls) != 1 || (*calls)[0].to != "alice@example.com" {
		t.Fatalf("manual trigger should mail only the triggerer, got %+v", *calls)
	}

	// Cron/webhook trigger (no triggerer): fall back to the creator.
	dispatcher.DispatchBuildRunFailure(0, 9, 42, 3, "failed", "boom")
	if len(*calls) != 2 || (*calls)[1].to != "bob@example.com" {
		t.Fatalf("auto trigger should mail the creator, got %+v", *calls)
	}

	// Triggerer == creator: exactly one mail.
	dispatcher.DispatchScriptRunFailure(9, 9, 8, 2, "failed", "boom")
	if len(*calls) != 3 {
		t.Fatalf("same person must produce one mail, got %d", len(*calls))
	}
}

func TestMailDispatcher_SkipsRecipientWithoutEmail(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	saveSMTPConfig(t, mailSvc)
	createMailUser(t, gdb, 11, "")
	calls := captureSender(t, mailSvc)

	dispatcher.DispatchBuildRunFailure(0, 11, 42, 3, "failed", "boom")
	dispatcher.DispatchBuildRunFailure(0, 0, 43, 4, "failed", "boom")
	if len(*calls) != 0 {
		t.Fatalf("recipients without email or fallback must be skipped, got %+v", *calls)
	}
}

func TestMailDispatcher_SkipsWhenSMTPUnconfigured(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	createMailUser(t, gdb, 7, "alice@example.com")
	calls := captureSender(t, mailSvc)

	dispatcher.DispatchBuildRunFailure(7, 0, 42, 3, "failed", "boom")
	if len(*calls) != 0 {
		t.Fatalf("unconfigured SMTP must skip sending, got %+v", *calls)
	}
}

func TestMailDispatcher_DeliveryFailureOnlyLogs(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	saveSMTPConfig(t, mailSvc)
	createMailUser(t, gdb, 7, "alice@example.com")
	calls := 0
	mailSvc.SetSender(func(model.MailSMTPConfig, string, string, string, string) error {
		calls++
		return errors.New("dial failed")
	})

	// Must not surface the error to the caller; nothing else to observe.
	dispatcher.DispatchPipelineRunFailure(7, 0, 9, 1, "failed", "boom")
	if calls != 1 {
		t.Fatalf("expected one attempted delivery, got %d", calls)
	}
}

func TestMailDispatcher_DispatchDoesNotWaitForSMTP(t *testing.T) {
	dispatcher, mailSvc, gdb := setupMailDispatcher(t)
	dispatcher.SetSyncDispatch(false) // exercise the real async path
	saveSMTPConfig(t, mailSvc)
	createMailUser(t, gdb, 7, "alice@example.com")
	release := make(chan struct{})
	sent := make(chan struct{})
	mailSvc.SetSender(func(model.MailSMTPConfig, string, string, string, string) error {
		<-release
		close(sent)
		return nil
	})

	returned := make(chan struct{})
	go func() {
		dispatcher.DispatchBuildRunFailure(7, 0, 1, 1, "failed", "boom")
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch blocked on SMTP delivery")
	}
	close(release)
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not complete")
	}
}
