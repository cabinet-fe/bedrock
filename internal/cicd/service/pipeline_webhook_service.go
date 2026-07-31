package service

import (
	"crypto/sha256"
	"encoding/hex"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
)

// PipelineWebhookService handles unauthenticated pipeline webhook triggers.
type PipelineWebhookService struct {
	pipelines    *repository.BuildPipelineRepository
	deliveries   *repository.PipelineWebhookDeliveryRepository
	orchestrator *PipelineOrchestrator
}

func NewPipelineWebhookService(
	pipelines *repository.BuildPipelineRepository,
	deliveries *repository.PipelineWebhookDeliveryRepository,
	orchestrator *PipelineOrchestrator,
) *PipelineWebhookService {
	return &PipelineWebhookService{
		pipelines:    pipelines,
		deliveries:   deliveries,
		orchestrator: orchestrator,
	}
}

// Receive processes a pipeline webhook. No branch matching in v1 (pipeline has no single branch).
func (s *PipelineWebhookService) Receive(
	pipelineID uint,
	urlSecret string,
	headers map[string]string,
	body []byte,
) (*WebhookResult, error) {
	p, err := s.pipelines.FindByID(pipelineID)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if p.WebhookSecret == "" || !secureEqual(p.WebhookSecret, urlSecret) {
		return nil, errUnauthorized("无效的 webhook secret")
	}
	if !p.Enabled || !p.TriggerWebhook {
		return &WebhookResult{Accepted: true, Triggered: 0, Message: "webhook trigger disabled"}, nil
	}

	asJob := &model.BuildJob{WebhookType: p.WebhookType}
	platform := detectWebhookPlatform(headers, asJob)
	if hasSignatureHeaders(headers) {
		if err := verifyPlatformSignature(platform, headers, body, p.WebhookSecret); err != nil {
			return nil, errUnauthorized("签名校验失败")
		}
	}

	deliveryKey := headersDeliveryID(headers)
	if deliveryKey == "" {
		sum := sha256.Sum256(body)
		deliveryKey = "body:" + hex.EncodeToString(sum[:16])
	}
	ok, err := s.deliveries.TryInsert(pipelineID, deliveryKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &WebhookResult{Accepted: true, Duplicate: true, Message: "duplicate delivery"}, nil
	}

	run, err := s.orchestrator.EnqueueInternal(pipelineID, 0, "webhook")
	if err != nil {
		return &WebhookResult{Accepted: true, Triggered: 0, Message: "enqueue failed"}, nil
	}
	return &WebhookResult{
		Accepted:  true,
		Triggered: 1,
		RunIDs:    []uint{run.ID},
	}, nil
}
