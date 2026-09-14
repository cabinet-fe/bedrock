package service

import (
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/wneessen/go-mail"
	"go.uber.org/zap"

	authmodel "bedrock/internal/auth/model"
	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

// maskedPassword is what the API returns instead of the stored password.
const maskedPassword = "******"

// errSMTPNotConfigured marks a skip (not a delivery failure): nothing is sent
// until an admin saves the system SMTP config.
var errSMTPNotConfigured = errors.New("SMTP 未配置")

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
		return nil, errSMTPNotConfigured
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

// sendNotification delivers one notice over the stored SMTP config.
func (s *MailService) sendNotification(to, subject, body string) error {
	cfg, err := s.repo.Find()
	if err != nil {
		return err
	}
	if cfg == nil || cfg.Host == "" || cfg.FromAddress == "" {
		return errSMTPNotConfigured
	}
	password, err := pkg.Decrypt(cfg.PasswordCipher)
	if err != nil {
		return fmt.Errorf("读取 SMTP 密码失败: %w", err)
	}
	return s.sender(*cfg, password, to, subject, body)
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

// ---------- Failure notice dispatch ----------

// RunFailureNotice describes one run terminal event to fan out. The dispatcher
// drops everything but failed/interrupted (spec: failure notice only).
type RunFailureNotice struct {
	Kind        string // build_run|script_run|pipeline_run|agent_run
	Status      string // failed or interrupted
	RunID       uint
	RunNumber   int // 0 for runs addressed by ID (agent runs)
	TriggeredBy uint
	FallbackTo  uint   // job/pipeline/agent creator used when TriggeredBy is 0
	Detail      string // error message shown in the mail body
}

// NotificationChannel is one outbound failure-notice delivery channel
// (mail today; room for IM channels later).
type NotificationChannel interface {
	Name() string
	SendRunFailure(n RunFailureNotice, userIDs []uint) error
}

// UserEmailFinder loads users for recipient resolution.
type UserEmailFinder interface {
	FindByID(id uint) (*authmodel.User, error)
}

// MailChannel delivers failure notices through the system SMTP config,
// skipping recipients without an email address.
type MailChannel struct {
	mail  *MailService
	users UserEmailFinder
}

func NewMailChannel(mail *MailService, users UserEmailFinder) *MailChannel {
	return &MailChannel{mail: mail, users: users}
}

func (c *MailChannel) Name() string { return "mail" }

// SendRunFailure sends one mail per recipient and returns the first delivery
// error; with no usable address it is a no-op.
func (c *MailChannel) SendRunFailure(n RunFailureNotice, userIDs []uint) error {
	addrs := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		user, err := c.users.FindByID(id)
		if err != nil || user == nil || strings.TrimSpace(user.Email) == "" {
			continue // recipient without an email: skip
		}
		addrs = append(addrs, strings.TrimSpace(user.Email))
	}
	if len(addrs) == 0 {
		return nil
	}
	subject, body := failureMailContent(n)
	for _, addr := range addrs {
		if err := c.mail.sendNotification(addr, subject, body); err != nil {
			return err
		}
	}
	return nil
}

// MailDispatcher resolves failure-notice recipients and fans the notice out
// over every channel asynchronously. Delivery failures are only logged:
// no retry, no send records, no error to the caller.
type MailDispatcher struct {
	channels     []NotificationChannel
	logger       *zap.Logger
	syncDispatch bool // deliver inline instead of in a goroutine (tests)
}

func NewMailDispatcher(mail *MailService, users UserEmailFinder, logger *zap.Logger) *MailDispatcher {
	return &MailDispatcher{
		channels: []NotificationChannel{NewMailChannel(mail, users)},
		logger:   logger,
	}
}

// SetSyncDispatch delivers inline instead of in a goroutine (tests).
func (d *MailDispatcher) SetSyncDispatch(v bool) { d.syncDispatch = v }

// DispatchBuildRunFailure fans a BuildRun terminal notice out.
func (d *MailDispatcher) DispatchBuildRunFailure(triggeredBy, jobCreatedBy, runID uint, runNumber int, status, message string) {
	d.dispatch(RunFailureNotice{
		Kind: "build_run", Status: status, RunID: runID, RunNumber: runNumber,
		TriggeredBy: triggeredBy, FallbackTo: jobCreatedBy, Detail: message,
	})
}

// DispatchScriptRunFailure fans a ScriptRun terminal notice out.
func (d *MailDispatcher) DispatchScriptRunFailure(triggeredBy, jobCreatedBy, runID uint, runNumber int, status, message string) {
	d.dispatch(RunFailureNotice{
		Kind: "script_run", Status: status, RunID: runID, RunNumber: runNumber,
		TriggeredBy: triggeredBy, FallbackTo: jobCreatedBy, Detail: message,
	})
}

// DispatchPipelineRunFailure fans a PipelineRun terminal notice out.
func (d *MailDispatcher) DispatchPipelineRunFailure(triggeredBy, pipelineCreatedBy, runID uint, runNumber int, status, message string) {
	d.dispatch(RunFailureNotice{
		Kind: "pipeline_run", Status: status, RunID: runID, RunNumber: runNumber,
		TriggeredBy: triggeredBy, FallbackTo: pipelineCreatedBy, Detail: message,
	})
}

// DispatchAgentRunFailure fans an AgentRun terminal notice out.
func (d *MailDispatcher) DispatchAgentRunFailure(triggeredBy, agentCreatedBy, runID uint, status, message string) {
	d.dispatch(RunFailureNotice{
		Kind: "agent_run", Status: status, RunID: runID,
		TriggeredBy: triggeredBy, FallbackTo: agentCreatedBy, Detail: message,
	})
}

func (d *MailDispatcher) dispatch(n RunFailureNotice) {
	switch n.Status {
	case "failed", "interrupted":
	default:
		return // success/cancelled never mail
	}
	ids := recipientIDs(n)
	if len(ids) == 0 {
		return
	}
	if d.syncDispatch {
		d.deliver(n, ids)
		return
	}
	go d.deliver(n, ids)
}

// recipientIDs prefers the triggering user and falls back to the creator, so
// the same person always resolves to a single recipient.
func recipientIDs(n RunFailureNotice) []uint {
	if n.TriggeredBy != 0 {
		return []uint{n.TriggeredBy}
	}
	if n.FallbackTo != 0 {
		return []uint{n.FallbackTo}
	}
	return nil
}

func (d *MailDispatcher) deliver(n RunFailureNotice, userIDs []uint) {
	for _, ch := range d.channels {
		err := ch.SendRunFailure(n, userIDs)
		if err == nil || d.logger == nil {
			continue
		}
		if errors.Is(err, errSMTPNotConfigured) {
			d.logger.Info("failure mail skipped: SMTP not configured",
				zap.String("channel", ch.Name()), zap.String("kind", n.Kind), zap.Uint("run_id", n.RunID))
			continue
		}
		d.logger.Warn("failure mail delivery failed",
			zap.String("channel", ch.Name()), zap.String("kind", n.Kind), zap.Uint("run_id", n.RunID), zap.Error(err))
	}
}

// failureMailContent renders the subject and body for a failure notice.
func failureMailContent(n RunFailureNotice) (string, string) {
	number := n.RunNumber
	if number == 0 {
		number = int(n.RunID)
	}
	subject := fmt.Sprintf("Bedrock %s #%d %s", kindLabel(n.Kind), number, statusLabel(n.Status))
	body := subject
	if n.Detail != "" {
		body += "\n\n" + n.Detail
	}
	return subject, body
}

func kindLabel(kind string) string {
	switch kind {
	case "build_run":
		return "构建"
	case "script_run":
		return "脚本任务"
	case "pipeline_run":
		return "流水线"
	case "agent_run":
		return "智能体运行"
	default:
		return "运行"
	}
}
