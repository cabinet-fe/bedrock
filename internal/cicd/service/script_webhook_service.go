package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"bedrock/internal/cicd/repository"
)

// ScriptWebhookService verifies URL secret, dedups deliveries, and enqueues script runs.
// No branch matching (script jobs have no repo).
type ScriptWebhookService struct {
	jobs       *repository.ScriptJobRepository
	deliveries *repository.ScriptWebhookDeliveryRepository
	runs       *ScriptRunService
}

func NewScriptWebhookService(
	jobs *repository.ScriptJobRepository,
	deliveries *repository.ScriptWebhookDeliveryRepository,
	runs *ScriptRunService,
) *ScriptWebhookService {
	return &ScriptWebhookService{jobs: jobs, deliveries: deliveries, runs: runs}
}

// Receive processes a script-job webhook. URL secret must match.
func (s *ScriptWebhookService) Receive(
	jobID uint,
	urlSecret string,
	headers map[string]string,
	body []byte,
) (*WebhookResult, error) {
	job, err := s.jobs.FindByID(jobID)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
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

	if hasSignatureHeaders(headers) {
		platform := detectScriptWebhookPlatform(headers, job.WebhookType)
		if err := verifyPlatformSignature(platform, headers, body, job.WebhookSecret); err != nil {
			return nil, errUnauthorized("签名校验失败")
		}
	}

	deliveryKey := headersDeliveryID(headers)
	if deliveryKey == "" {
		sum := sha256.Sum256(body)
		deliveryKey = "body:" + hex.EncodeToString(sum[:16])
	}

	ok, err := s.deliveries.TryInsert(jobID, deliveryKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &WebhookResult{Accepted: true, Duplicate: true, Message: "duplicate delivery"}, nil
	}

	run, err := s.runs.EnqueueInternal(job.ID, 0, "webhook")
	if err != nil {
		return &WebhookResult{
			Accepted:  true,
			Triggered: 0,
			Message:   "enqueue failed",
		}, nil
	}

	return &WebhookResult{
		Accepted:  true,
		Triggered: 1,
		RunIDs:    []uint{run.ID},
		JobIDs:    []uint{job.ID},
	}, nil
}

func detectScriptWebhookPlatform(headers map[string]string, configured string) string {
	switch {
	case header(headers, "X-Gitea-Event") != "":
		return "gitea"
	case header(headers, "X-Gitee-Event") != "":
		return "gitee"
	case header(headers, "X-GitHub-Event") != "":
		return "github"
	case header(headers, "X-Gitlab-Event") != "":
		return "gitlab"
	case header(headers, "X-Event-Key") != "":
		return "bitbucket"
	}
	configured = strings.ToLower(strings.TrimSpace(configured))
	if configured != "" && configured != "auto" && configured != "generic" {
		return configured
	}
	return "generic"
}
