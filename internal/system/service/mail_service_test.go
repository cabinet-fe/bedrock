package service_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
