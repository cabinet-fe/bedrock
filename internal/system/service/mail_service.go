package service

import (
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/wneessen/go-mail"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

// maskedPassword is what the API returns instead of the stored password.
const maskedPassword = "******"

// MailService manages the system-level SMTP sending config and test sends.
type MailService struct {
	repo *repository.MailRepository
	// sender performs one mail delivery; swapped out in tests.
	sender func(cfg model.MailSMTPConfig, password, to, subject, body string) error
}

func NewMailService(repo *repository.MailRepository) *MailService {
	return &MailService{repo: repo, sender: sendViaSMTP}
}

// SetSender overrides the mail transport (tests inject a fake here).
func (s *MailService) SetSender(sender func(cfg model.MailSMTPConfig, password, to, subject, body string) error) {
	s.sender = sender
}

// MailSMTPConfigInput is a save request for the system SMTP config.
type MailSMTPConfigInput struct {
	Host        string
	Port        int
	Username    string
	Password    string // plaintext from the API; empty keeps the stored password
	FromAddress string
}

// MailSMTPConfigView is the API view: password masked, cipher never exposed.
type MailSMTPConfigView struct {
	ID          uint      `json:"id"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	Password    string    `json:"password"`
	FromAddress string    `json:"from_address"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MailTestResult reports the outcome of a test send.
type MailTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// GetConfig returns the masked SMTP config view, or nil when not configured.
func (s *MailService) GetConfig() (*MailSMTPConfigView, error) {
	cfg, err := s.repo.Find()
	if err != nil {
		return nil, err
	}
	return toConfigView(cfg), nil
}

// SaveConfig creates or updates the single-row config; the password is
// encrypted with pkg.Encrypt before it touches the database.
func (s *MailService) SaveConfig(in MailSMTPConfigInput) (*MailSMTPConfigView, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	in.FromAddress = strings.TrimSpace(in.FromAddress)
	if in.Host == "" {
		return nil, errors.New("SMTP 服务器地址不能为空")
	}
	if in.Port < 1 || in.Port > 65535 {
		return nil, errors.New("端口需在 1-65535 之间")
	}
	if _, err := netmail.ParseAddress(in.FromAddress); err != nil {
		return nil, errors.New("发件人邮箱格式不正确")
	}

	cfg, err := s.repo.Find()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &model.MailSMTPConfig{}
	}
	cfg.Host = in.Host
	cfg.Port = in.Port
	cfg.Username = in.Username
	cfg.FromAddress = in.FromAddress
	if in.Password != "" {
		cipher, err := pkg.Encrypt(in.Password)
		if err != nil {
			return nil, fmt.Errorf("加密 SMTP 密码失败: %w", err)
		}
		cfg.PasswordCipher = cipher
	}
	if cfg.ID == 0 {
		if err := s.repo.Create(cfg); err != nil {
			return nil, fmt.Errorf("保存 SMTP 配置失败: %w", err)
		}
	} else if err := s.repo.Update(cfg); err != nil {
		return nil, fmt.Errorf("保存 SMTP 配置失败: %w", err)
	}
	return toConfigView(cfg), nil
}

// SendTest delivers a test mail to the given address with the stored config.
// A delivery failure is reported in the result, not as an error.
func (s *MailService) SendTest(to string) (*MailTestResult, error) {
	addr, err := netmail.ParseAddress(strings.TrimSpace(to))
	if err != nil {
		return nil, errors.New("收件邮箱格式不正确")
	}
	cfg, err := s.repo.Find()
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.Host == "" || cfg.FromAddress == "" {
		return nil, errors.New("SMTP 未配置")
	}
	password, err := pkg.Decrypt(cfg.PasswordCipher)
	if err != nil {
		return nil, fmt.Errorf("读取 SMTP 密码失败: %w", err)
	}
	if err := s.sender(*cfg, password, addr.Address, "Bedrock 测试邮件",
		"这是一封来自 Bedrock 的测试邮件，收到即说明 SMTP 配置可用。"); err != nil {
		return &MailTestResult{Success: false, Message: err.Error()}, nil
	}
	return &MailTestResult{Success: true, Message: "测试邮件已发送"}, nil
}

func toConfigView(cfg *model.MailSMTPConfig) *MailSMTPConfigView {
	if cfg == nil {
		return nil
	}
	masked := ""
	if cfg.PasswordCipher != "" {
		masked = maskedPassword
	}
	return &MailSMTPConfigView{
		ID:          cfg.ID,
		Host:        cfg.Host,
		Port:        cfg.Port,
		Username:    cfg.Username,
		Password:    masked,
		FromAddress: cfg.FromAddress,
		CreatedAt:   cfg.CreatedAt,
		UpdatedAt:   cfg.UpdatedAt,
	}
}

// sendViaSMTP builds a go-mail client from cfg and sends synchronously.
func sendViaSMTP(cfg model.MailSMTPConfig, password, to, subject, body string) error {
	msg := mail.NewMsg()
	if err := msg.From(cfg.FromAddress); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, body)
	opts := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithTimeout(15 * time.Second),
	}
	if cfg.Port == 465 {
		opts = append(opts, mail.WithSSLPort(false))
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}
	if cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(password),
		)
	}
	cl, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return err
	}
	return cl.DialAndSend(msg)
}
