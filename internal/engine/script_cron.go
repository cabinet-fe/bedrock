package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
)

// ScriptCronScheduler schedules per-ScriptJob cron with IANA timezone.
type ScriptCronScheduler struct {
	cron      *cron.Cron
	entries   map[uint]cron.EntryID
	mu        sync.Mutex
	jobs      ScriptJobStore
	runs      ScriptRunStore
	enqueuer  ScriptRunEnqueuer
	scheduler RunScheduler
	logger    *zap.Logger
	clock     Clock
}

func NewScriptCronScheduler(
	jobs ScriptJobStore,
	runs ScriptRunStore,
	enqueuer ScriptRunEnqueuer,
	scheduler RunScheduler,
	logger *zap.Logger,
) *ScriptCronScheduler {
	return &ScriptCronScheduler{
		cron:      cron.New(),
		entries:   make(map[uint]cron.EntryID),
		jobs:      jobs,
		runs:      runs,
		enqueuer:  enqueuer,
		scheduler: scheduler,
		logger:    logger,
		clock:     realClock{},
	}
}

func (cs *ScriptCronScheduler) Start() error {
	list, err := cs.jobs.ListCronEnabled()
	if err != nil {
		return fmt.Errorf("load script cron jobs: %w", err)
	}
	for _, job := range list {
		if err := cs.addEntry(job); err != nil {
			if cs.logger != nil {
				cs.logger.Warn("skip script cron entry", zap.Uint("job_id", job.ID), zap.Error(err))
			}
		}
	}
	cs.cron.Start()
	return nil
}

func (cs *ScriptCronScheduler) Stop() {
	cs.cron.Stop()
}

func (cs *ScriptCronScheduler) Add(job model.ScriptJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if entryID, ok := cs.entries[job.ID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entries, job.ID)
	}
	if !job.TriggerCron || job.CronExpression == "" || !job.Enabled {
		return nil
	}
	return cs.addEntryLocked(job)
}

func (cs *ScriptCronScheduler) Remove(jobID uint) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if entryID, ok := cs.entries[jobID]; ok {
		cs.cron.Remove(entryID)
		delete(cs.entries, jobID)
	}
}

func (cs *ScriptCronScheduler) addEntry(job model.ScriptJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.addEntryLocked(job)
}

func (cs *ScriptCronScheduler) addEntryLocked(job model.ScriptJob) error {
	jobID := job.ID
	tzName := strings.TrimSpace(job.CronTimezone)
	if tzName == "" {
		tzName = "UTC"
	}
	if _, err := time.LoadLocation(tzName); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", tzName, err)
	}

	spec := "CRON_TZ=" + tzName + " " + job.CronExpression
	entryID, err := cs.cron.AddFunc(spec, func() {
		defer func() {
			if r := recover(); r != nil && cs.logger != nil {
				cs.logger.Error("script cron callback panic", zap.Uint("job_id", jobID), zap.Any("panic", r))
			}
		}()
		cs.trigger(jobID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q (tz=%s): %w", job.CronExpression, tzName, err)
	}
	cs.entries[jobID] = entryID
	return nil
}

func (cs *ScriptCronScheduler) trigger(jobID uint) {
	if cs.logger != nil {
		cs.logger.Info("script cron triggered", zap.Uint("job_id", jobID), zap.Time("now", cs.clock.Now()))
	}
	active, err := cs.runs.HasNonTerminal(jobID)
	if err != nil {
		if cs.logger != nil {
			cs.logger.Error("script cron overlap check failed", zap.Error(err))
		}
		return
	}
	if active {
		if cs.logger != nil {
			cs.logger.Info("script cron skipped: non-terminal run exists", zap.Uint("job_id", jobID))
		}
		return
	}
	run, err := cs.enqueuer.EnqueueInternal(jobID, 0, "cron")
	if err != nil {
		if cs.logger != nil {
			cs.logger.Error("script cron enqueue failed", zap.Uint("job_id", jobID), zap.Error(err))
		}
		return
	}
	if cs.scheduler != nil {
		_ = cs.scheduler.Submit(run.ID)
	}
}
