package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
)

// ScriptScheduler is an in-memory worker pool for ScriptRuns.
type ScriptScheduler struct {
	maxConcurrent int
	semaphore     chan struct{}
	jobs          chan uint
	cancelMap     map[uint]context.CancelFunc
	mu            sync.RWMutex
	pipeline      *ScriptPipeline
	runs          ScriptRunStore
	logger        *zap.Logger
	wg            sync.WaitGroup
	closed        atomic.Bool
}

func NewScriptScheduler(maxConcurrent int, pipeline *ScriptPipeline, runs ScriptRunStore, logger *zap.Logger) *ScriptScheduler {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &ScriptScheduler{
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
		jobs:          make(chan uint, 256),
		cancelMap:     make(map[uint]context.CancelFunc),
		pipeline:      pipeline,
		runs:          runs,
		logger:        logger,
	}
}

func (s *ScriptScheduler) Start() {
	go s.run()
}

// RecoverOnStartup marks running→interrupted and re-submits queued script runs.
func (s *ScriptScheduler) RecoverOnStartup() error {
	if s.runs == nil {
		return nil
	}
	n, err := s.runs.MarkRunningInterrupted()
	if err != nil {
		return err
	}
	if n > 0 && s.logger != nil {
		s.logger.Info("marked interrupted script runs", zap.Int64("count", n))
	}
	queued, err := s.runs.ListByStatuses("queued")
	if err != nil {
		return err
	}
	for _, r := range queued {
		if err := s.Submit(r.ID); err != nil && s.logger != nil {
			s.logger.Warn("re-queue script run failed", zap.Uint("run_id", r.ID), zap.Error(err))
		}
	}
	if s.logger != nil {
		s.logger.Info("script scheduler recovery complete", zap.Int("queued_restored", len(queued)))
	}
	return nil
}

func (s *ScriptScheduler) run() {
	for runID := range s.jobs {
		s.semaphore <- struct{}{}
		s.wg.Add(1)
		go func(id uint) {
			defer func() {
				<-s.semaphore
				s.wg.Done()
			}()
			defer func() {
				if r := recover(); r != nil {
					if s.logger != nil {
						s.logger.Error("script worker panic recovered", zap.Uint("run_id", id), zap.Any("panic", r))
					}
					run, err := s.pipeline.runs.FindByID(id)
					if err == nil && run != nil && run.Status == "success" {
						return
					}
					if run == nil {
						run = &model.ScriptRun{ID: id}
					}
					s.pipeline.failRun(run, fmt.Sprintf("internal panic: %v", r))
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			s.mu.Lock()
			s.cancelMap[id] = cancel
			s.mu.Unlock()
			defer func() {
				s.mu.Lock()
				delete(s.cancelMap, id)
				s.mu.Unlock()
				cancel()
			}()
			s.pipeline.Execute(ctx, id)
		}(runID)
	}
}

func (s *ScriptScheduler) Submit(runID uint) error {
	if s.closed.Load() {
		return fmt.Errorf("script scheduler is shut down")
	}
	select {
	case s.jobs <- runID:
		return nil
	default:
		s.jobs <- runID
		return nil
	}
}

func (s *ScriptScheduler) Cancel(runID uint) bool {
	s.mu.RLock()
	cancel, ok := s.cancelMap[runID]
	s.mu.RUnlock()
	if ok {
		cancel()
		return true
	}
	return false
}

func (s *ScriptScheduler) Shutdown() {
	s.closed.Store(true)
	close(s.jobs)
	s.wg.Wait()
}

// Ensure ScriptScheduler satisfies RunScheduler (Submit/Cancel).
var _ RunScheduler = (*ScriptScheduler)(nil)
