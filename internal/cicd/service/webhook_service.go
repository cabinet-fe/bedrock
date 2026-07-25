package service

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
)

// WebhookService verifies signatures, dedups deliveries, matches branch policy, enqueues runs.
type WebhookService struct {
	jobs       *repository.BuildJobRepository
	deliveries *repository.WebhookDeliveryRepository
	runs       *BuildRunService
}

func NewWebhookService(
	jobs *repository.BuildJobRepository,
	deliveries *repository.WebhookDeliveryRepository,
	runs *BuildRunService,
) *WebhookService {
	return &WebhookService{jobs: jobs, deliveries: deliveries, runs: runs}
}

type WebhookResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Triggered int    `json:"triggered"`
	RunIDs    []uint `json:"run_ids,omitempty"`
	JobIDs    []uint `json:"job_ids,omitempty"`
	Message   string `json:"message,omitempty"`
}

type webhookEvent struct {
	Ref           string
	CommitHash    string
	CommitMessage string
	DeliveryKey   string
}

// Receive processes a build-job webhook. URL secret must match. Platform signature preferred when present.
// Logs/errors must never include the secret (caller redacts).
func (s *WebhookService) Receive(
	jobID uint,
	urlSecret string,
	headers map[string]string,
	body []byte,
) (*WebhookResult, error) {
	job, err := s.jobs.FindByID(jobID)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if job.WebhookSecret == "" || !secureEqual(job.WebhookSecret, urlSecret) {
		return nil, errUnauthorized("无效的 webhook secret")
	}
	if !job.Enabled || !job.TriggerWebhook {
		return &WebhookResult{
			Accepted:  true,
			Triggered: 0,
			Message:   "webhook trigger disabled",
		}, nil
	}

	platform := detectWebhookPlatform(headers, job)
	if hasSignatureHeaders(headers) {
		if err := verifyPlatformSignature(platform, headers, body, job.WebhookSecret); err != nil {
			return nil, errUnauthorized("签名校验失败")
		}
	}

	event, err := parseWebhookPayload(platform, job, headers, body)
	if err != nil {
		return nil, errorsNew(err.Error())
	}

	deliveryKey := event.DeliveryKey
	if deliveryKey == "" {
		deliveryKey = headersDeliveryID(headers)
	}
	if deliveryKey == "" {
		sum := sha256.Sum256(append([]byte(event.Ref+"|"), body...))
		deliveryKey = "body:" + hex.EncodeToString(sum[:16])
	}

	ok, err := s.deliveries.TryInsert(jobID, deliveryKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &WebhookResult{Accepted: true, Duplicate: true, Message: "duplicate delivery"}, nil
	}

	branch := extractBranchFromRef(event.Ref)
	if !jobMatchesBranch(*job, branch) {
		return &WebhookResult{
			Accepted:  true,
			Branch:    branch,
			Triggered: 0,
			Message:   "branch not matched",
		}, nil
	}

	run, err := s.runs.EnqueueInternal(job.ID, 0, engine.EnqueueParams{
		Branch:        branch,
		TriggerType:   "webhook",
		CommitHash:    event.CommitHash,
		CommitMessage: event.CommitMessage,
	})
	if err != nil {
		return &WebhookResult{
			Accepted:  true,
			Branch:    branch,
			Triggered: 0,
			Message:   "enqueue failed",
		}, nil
	}

	return &WebhookResult{
		Accepted:  true,
		Branch:    branch,
		Triggered: 1,
		RunIDs:    []uint{run.ID},
		JobIDs:    []uint{job.ID},
	}, nil
}

func jobMatchesBranch(job model.BuildJob, branch string) bool {
	return job.Branch == branch
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func hasSignatureHeaders(h map[string]string) bool {
	return header(h, "X-Hub-Signature-256") != "" ||
		header(h, "X-Hub-Signature") != "" ||
		header(h, "X-Gitea-Signature") != "" ||
		header(h, "X-Gitee-Token") != "" ||
		header(h, "X-Gitlab-Token") != ""
}

func verifyPlatformSignature(platform string, h map[string]string, body []byte, secret string) error {
	switch platform {
	case "github", "gitea":
		sig256 := header(h, "X-Hub-Signature-256")
		if sig256 == "" {
			sig256 = header(h, "X-Gitea-Signature")
		}
		if sig256 != "" {
			if !verifyHMACSHA256(sig256, body, secret) {
				return fmt.Errorf("bad signature")
			}
			return nil
		}
		sig1 := header(h, "X-Hub-Signature")
		if sig1 != "" {
			if !verifyHMACSHA1(sig1, body, secret) {
				return fmt.Errorf("bad signature")
			}
			return nil
		}
		return fmt.Errorf("missing signature")
	case "gitlab":
		token := header(h, "X-Gitlab-Token")
		if token == "" || !secureEqual(token, secret) {
			return fmt.Errorf("bad token")
		}
		return nil
	case "gitee":
		token := header(h, "X-Gitee-Token")
		if token == "" || !secureEqual(token, secret) {
			return fmt.Errorf("bad token")
		}
		return nil
	default:
		return nil
	}
}

func verifyHMACSHA256(headerVal string, body []byte, secret string) bool {
	headerVal = strings.TrimSpace(headerVal)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return secureEqual(strings.ToLower(headerVal), strings.ToLower(expected))
}

func verifyHMACSHA1(headerVal string, body []byte, secret string) bool {
	headerVal = strings.TrimSpace(headerVal)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	expected := "sha1=" + hex.EncodeToString(mac.Sum(nil))
	return secureEqual(strings.ToLower(headerVal), strings.ToLower(expected))
}

func header(h map[string]string, key string) string {
	if v, ok := h[key]; ok {
		return v
	}
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func headersDeliveryID(h map[string]string) string {
	if v := header(h, "X-GitHub-Delivery"); v != "" {
		return "github:" + v
	}
	if v := header(h, "X-Gitea-Delivery"); v != "" {
		return "gitea:" + v
	}
	if v := header(h, "X-Gitlab-Event-UUID"); v != "" {
		return "gitlab:" + v
	}
	if v := header(h, "X-Request-Id"); v != "" {
		return "req:" + v
	}
	return ""
}

func detectWebhookPlatform(h map[string]string, job *model.BuildJob) string {
	switch {
	case header(h, "X-Gitea-Event") != "":
		return "gitea"
	case header(h, "X-Gitee-Event") != "":
		return "gitee"
	case header(h, "X-GitHub-Event") != "":
		return "github"
	case header(h, "X-Gitlab-Event") != "":
		return "gitlab"
	case header(h, "X-Event-Key") != "":
		return "bitbucket"
	case job.WebhookType != "" && job.WebhookType != "auto":
		return job.WebhookType
	case job.WebhookRefPath != "":
		return "generic"
	default:
		return "generic"
	}
}

func parseWebhookPayload(platform string, job *model.BuildJob, h map[string]string, body []byte) (*webhookEvent, error) {
	switch platform {
	case "github", "gitea", "gitee":
		return parseGitHubLike(h, body, platform)
	case "gitlab":
		return parseGitLab(h, body)
	case "bitbucket":
		return parseBitbucket(h, body)
	default:
		return parseGeneric(job, body)
	}
}

type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

func parseGitHubLike(h map[string]string, body []byte, platform string) (*webhookEvent, error) {
	eventHdr := header(h, "X-GitHub-Event")
	if eventHdr == "" {
		eventHdr = header(h, "X-Gitea-Event")
	}
	if eventHdr == "" {
		eventHdr = header(h, "X-Gitee-Event")
	}
	if eventHdr != "" {
		et := strings.ToLower(strings.TrimSpace(eventHdr))
		if platform == "gitee" {
			if et != "push hook" && et != "tag push hook" {
				return nil, fmt.Errorf("仅支持 Push Hook / Tag Push Hook 事件")
			}
		} else if et != "push" {
			return nil, fmt.Errorf("仅支持 push 事件")
		}
	}
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Ref == "" {
		return nil, fmt.Errorf("无法解析 push payload")
	}
	commitHash := payload.HeadCommit.ID
	if commitHash == "" {
		commitHash = payload.After
	}
	return &webhookEvent{
		Ref:           payload.Ref,
		CommitHash:    commitHash,
		CommitMessage: payload.HeadCommit.Message,
	}, nil
}

func parseGitLab(h map[string]string, body []byte) (*webhookEvent, error) {
	if et := header(h, "X-Gitlab-Event"); et != "" && !strings.EqualFold(et, "Push Hook") {
		return nil, fmt.Errorf("仅支持 push 事件")
	}
	var payload struct {
		Ref         string `json:"ref"`
		CheckoutSha string `json:"checkout_sha"`
		Commit      *struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Ref == "" {
		return nil, fmt.Errorf("无法解析 GitLab push payload")
	}
	msg := ""
	if payload.Commit != nil {
		msg = payload.Commit.Message
	}
	return &webhookEvent{Ref: payload.Ref, CommitHash: payload.CheckoutSha, CommitMessage: msg}, nil
}

func parseBitbucket(h map[string]string, body []byte) (*webhookEvent, error) {
	if et := header(h, "X-Event-Key"); et != "" && et != "repo:push" {
		return nil, fmt.Errorf("仅支持 repo:push 事件")
	}
	var payload struct {
		Push struct {
			Changes []struct {
				New *struct {
					Name   string `json:"name"`
					Target struct {
						Hash    string `json:"hash"`
						Message string `json:"message"`
					} `json:"target"`
				} `json:"new"`
			} `json:"changes"`
		} `json:"push"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Push.Changes) == 0 {
		return nil, fmt.Errorf("无法解析 Bitbucket push payload")
	}
	change := payload.Push.Changes[0]
	if change.New == nil {
		return nil, fmt.Errorf("bitbucket payload 缺少分支信息")
	}
	return &webhookEvent{
		Ref:           "refs/heads/" + change.New.Name,
		CommitHash:    change.New.Target.Hash,
		CommitMessage: change.New.Target.Message,
	}, nil
}

func parseGeneric(job *model.BuildJob, body []byte) (*webhookEvent, error) {
	if job.WebhookRefPath != "" {
		var payload any
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("无法解析 JSON payload")
		}
		ref, err := extractJSONString(payload, job.WebhookRefPath)
		if err != nil {
			return nil, fmt.Errorf("读取 ref 失败")
		}
		commitHash := ""
		if job.WebhookCommitPath != "" {
			commitHash, _ = extractJSONString(payload, job.WebhookCommitPath)
		}
		msg := ""
		if job.WebhookMessagePath != "" {
			msg, _ = extractJSONString(payload, job.WebhookMessagePath)
		}
		return &webhookEvent{Ref: ref, CommitHash: commitHash, CommitMessage: msg}, nil
	}
	var payload struct {
		Ref     string `json:"ref"`
		Branch  string `json:"branch"`
		After   string `json:"after"`
		Commit  string `json:"commit"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("无法解析 generic payload")
	}
	ref := payload.Ref
	if ref == "" && payload.Branch != "" {
		ref = "refs/heads/" + payload.Branch
	}
	if ref == "" {
		return nil, fmt.Errorf("generic payload 缺少 ref/branch")
	}
	hash := payload.After
	if hash == "" {
		hash = payload.Commit
	}
	return &webhookEvent{Ref: ref, CommitHash: hash, CommitMessage: payload.Message}, nil
}

func extractBranchFromRef(ref string) string {
	if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return after
	}
	return ref
}

func extractJSONString(payload any, path string) (string, error) {
	value, err := extractJSONValue(payload, path)
	if err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		return fmt.Sprintf("%.0f", typed), nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	default:
		return "", fmt.Errorf("JSONPath 值不是字符串")
	}
}

func extractJSONValue(payload any, path string) (any, error) {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.TrimPrefix(trimmed, "$.")
	trimmed = strings.TrimPrefix(trimmed, "$")
	if trimmed == "" {
		return nil, fmt.Errorf("JSONPath 不能为空")
	}
	current := payload
	for token := range strings.SplitSeq(trimmed, ".") {
		if token == "" {
			continue
		}
		name := token
		index := -1
		if open := strings.Index(token, "["); open >= 0 && strings.HasSuffix(token, "]") {
			name = token[:open]
			var parsed int
			if _, err := fmt.Sscanf(token[open:], "[%d]", &parsed); err != nil {
				return nil, fmt.Errorf("不支持的 JSONPath 片段")
			}
			index = parsed
		}
		if name != "" {
			obj, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("JSONPath 片段不是对象")
			}
			value, exists := obj[name]
			if !exists {
				return nil, fmt.Errorf("JSONPath 片段不存在")
			}
			current = value
		}
		if index >= 0 {
			items, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("JSONPath 片段不是数组")
			}
			if index >= len(items) {
				return nil, fmt.Errorf("JSONPath 数组索引越界")
			}
			current = items[index]
		}
	}
	return current, nil
}

type unauthorizedError struct{ msg string }

func (e *unauthorizedError) Error() string { return e.msg }

func errUnauthorized(msg string) error { return &unauthorizedError{msg: msg} }

func IsUnauthorized(err error) bool {
	_, ok := err.(*unauthorizedError)
	return ok
}

// RedactSecret replaces secret occurrences in text for safe logging.
func RedactSecret(text, secret string) string {
	if secret == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}
