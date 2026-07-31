package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
)

// PipelineCronScheduler schedules per-BuildPipeline cron with IANA timezone.
type PipelineCronScheduler struct {
	cron       *cron.Cron
	entries    map[uint]cron.EntryID
	mu         sync.Mutex
	pipelines  *repository.BuildPipelineRepository
	runs       *repository.PipelineRunRepository
	orchestrator *PipelineOrchestrator
	logger     *zap.Logger
}

func NewPipelineCronScheduler(
	pipelines *repository.BuildPipelineRepository,
	runs *repository.PipelineRunRepository,
	orchestrator *PipelineOrchestrator,
	logger *zap.Logger,
) *PipelineCronScheduler {
	return &PipelineCronScheduler{
		cron:         cron.New(),
		entries:      make(map[uint]cron.EntryID),
		pipelines:    pipelines,
		runs:         runs,
		orchestrator: orchestrator,
		logger:       logger,
	}
}

func (cs *PipelineCronScheduler) Start() error {
	list, err := cs.pipelines.ListCronEnabled()
	if err != nil {
		return fmt.Errorf("load pipeline cron: %w", err)
	}
	for _, p := range list {
		if err := cs.addEntry(p); err != nil {
			if cs.logger != nil {
				cs.logger.Warn("skip pipeline cron entry", zap.Uint("pipeline_id", p.ID), zap.Error(err))
			}
		}
	}
	cs.cron.Start()
	return nil
}

func (cs *PipelineCronScheduler) Stop() {
	cs.cron.Stop()
}

func (cs *PipelineCronScheduler) Add(p model.BuildPipeline) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if entryID, ok := cs.entries[p.ID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entries, p.ID)
	}
	if !p.TriggerCron || p.CronExpression == "" || !p.Enabled {
		return nil
	}
	return cs.addEntryLocked(p)
}

func (cs *PipelineCronScheduler) Remove(pipelineID uint) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if entryID, ok := cs.entries[pipelineID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entries, pipelineID)
	}
}

func (cs *PipelineCronScheduler) addEntry(p model.BuildPipeline) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.addEntryLocked(p)
}

func (cs *PipelineCronScheduler) addEntryLocked(p model.BuildPipeline) error {
	pipelineID := p.ID
	tzName := strings.TrimSpace(p.CronTimezone)
	if tzName == "" {
		tzName = "UTC"
	}
	if _, err := time.LoadLocation(tzName); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", tzName, err)
	}
	spec := "CRON_TZ=" + tzName + " " + p.CronExpression
	entryID, err := cs.cron.AddFunc(spec, func() {
		defer func() {
			if r := recover(); r != nil && cs.logger != nil {
				cs.logger.Error("pipeline cron panic", zap.Uint("pipeline_id", pipelineID), zap.Any("panic", r))
			}
		}()
		cs.trigger(pipelineID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q (tz=%s): %w", p.CronExpression, tzName, err)
	}
	cs.entries[pipelineID] = entryID
	return nil
}

func (cs *PipelineCronScheduler) trigger(pipelineID uint) {
	if cs.logger != nil {
		cs.logger.Info("pipeline cron triggered", zap.Uint("pipeline_id", pipelineID))
	}
	active, err := cs.runs.HasNonTerminal(pipelineID)
	if err != nil {
		if cs.logger != nil {
			cs.logger.Error("pipeline cron overlap check failed", zap.Error(err))
		}
		return
	}
	if active {
		if cs.logger != nil {
			cs.logger.Info("pipeline cron skipped: non-terminal run exists", zap.Uint("pipeline_id", pipelineID))
		}
		return
	}
	if _, err := cs.orchestrator.EnqueueInternal(pipelineID, 0, "cron"); err != nil {
		if cs.logger != nil {
			cs.logger.Error("pipeline cron enqueue failed", zap.Uint("pipeline_id", pipelineID), zap.Error(err))
		}
	}
}
