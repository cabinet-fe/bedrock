package service

import (
	"time"

	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

type AuditService struct {
	logs *repository.OperationLogRepository
}

func NewAuditService(logs *repository.OperationLogRepository) *AuditService {
	return &AuditService{logs: logs}
}

func (s *AuditService) Write(userID uint, username, action, resourceType, resourceID, details, ip string) error {
	row := &model.OperationLog{
		UserID:       userID,
		Username:     username,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ip,
		CreatedAt:    time.Now().UTC(),
	}
	return s.logs.Create(row)
}

func (s *AuditService) List(f repository.OperationLogFilters) ([]model.OperationLog, int64, error) {
	return s.logs.List(f)
}

func (s *AuditService) Clear() error {
	return s.logs.DeleteAll()
}
