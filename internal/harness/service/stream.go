package service

// This file hosts the stream bridge: one backend-wide event bus
// subscription fanned out to per-session consumers. See StreamService for
// the policy implemented here (dedupe, ring buffer, pending
// approval/question lifecycle).

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/harness/provider"
)

// Session approval modes.
const (
	// ApprovalManual requires a human to answer every permission request.
	ApprovalManual = "manual"
	// ApprovalAuto lets the bridge allow permission requests itself.
	ApprovalAuto = "auto"
)

// Bridge defaults and bounds.
const (
	// DefaultPendingTTL bounds how long an unanswered permission or question
	// request stays pending before the bridge rejects it as a fallback.
	DefaultPendingTTL = 10 * time.Minute
	// DefaultRingSize is the per-session replay buffer size.
	DefaultRingSize = 200
	// subscriberBuffer bounds one subscriber's live-frame queue before
	// frames are dropped (the ring buffer still holds them for catch-up).
	subscriberBuffer = 256
	// recentEventIDs bounds the transient-frame dedupe window.
	recentEventIDs = 1024
	// maxTrackedSessions bounds per-session bridge state; beyond it the
	// least recently active sessions without subscribers or pending
	// requests are evicted.
	maxTrackedSessions = 1024
	// pendingSweepInterval is the pending-TTL sweep cadence.
	pendingSweepInterval = 5 * time.Second
	reconnectMinBackoff  = time.Second
	reconnectMaxBackoff  = 30 * time.Second
	// reconnectResetAfter resets the backoff once a subscription stayed
	// healthy this long.
	reconnectResetAfter = time.Minute
	answerTimeout       = 30 * time.Second
	// DefaultSettleDelay is the quiet window after the last step-ended-ish
	// frame before the bridge asks the backend whether the session still
	// runs; DefaultSettlePoll is the re-check cadence while it does.
	DefaultSettleDelay = 2 * time.Second
	DefaultSettlePoll  = 2 * time.Second
)

// ErrRequestNotPending reports a reply to a request the bridge does not hold
// (already answered, auto-approved, or TTL-rejected).
var ErrRequestNotPending = errors.New("harness: request is not pending")

// StreamConfig configures the StreamService.
type StreamConfig struct {
	// ApprovalMode is the fallback for sessions without a registered mode
	// (harness.approval_mode); anything but "auto" behaves as manual.
	ApprovalMode string
	// PendingTTL overrides DefaultPendingTTL (0 = default).
	PendingTTL time.Duration
	// SweepInterval overrides the pending-TTL sweep cadence (0 = default;
	// tests with short PendingTTL tighten it for determinism).
	SweepInterval time.Duration
	// RingSize overrides DefaultRingSize (0 = default).
	RingSize int
	// SettleDelay overrides DefaultSettleDelay (0 = default).
	SettleDelay time.Duration
	// SettlePoll overrides DefaultSettlePoll (0 = default).
	SettlePoll time.Duration
}

// PendingRequest is one registered permission or question request awaiting
// an answer.
type PendingRequest struct {
	SessionID string             `json:"sessionId"`
	Kind      provider.FrameKind `json:"kind"`
	RequestID string             `json:"requestId"`
	// ExpiresAt is when the bridge rejects the request as a fallback.
	ExpiresAt time.Time `json:"expiresAt"`
	// Frame is the asking frame, kept for consumers that replay the ask.
	Frame provider.Frame `json:"frame"`
}

// sessionState is the per-session bridge state, guarded by StreamService.mu.
type sessionState struct {
	lastSeq  int64
	ring     []provider.Frame
	pending  map[string]PendingRequest
	subs     map[int]chan *provider.Frame
	nextSub  int
	approval string // "" = configured fallback
	activeAt time.Time
	// busy tracks whether the backend agent loop is believed to run. It
	// flips true on prompt/step-start frames and false only when the settle
	// check confirms idleness; the synthesized idle frame closes the gap
	// left by backends that never broadcast session.idle (opencode 1.18.x).
	frames int64
	busy   bool
	settle *time.Timer
}

// StreamService is the stream bridge: it keeps exactly one backend-wide
// event-bus subscription open, deduplicates frames per session (durable by
// seq, transient by event id), fans them out to subscribers, and retains a
// per-session ring buffer as the replay baseline for late-joining
// connections. Permission and question requests are registered as pending:
// auto-mode sessions get permissions allowed immediately (questions are only
// recorded), and unanswered requests are rejected when their TTL elapses, so
// no session blocks forever on a missing answer.
type StreamService struct {
	provider provider.Provider
	cfg      StreamConfig
	log      *zap.Logger

	mu        sync.Mutex
	sessions  map[string]*sessionState
	recentIDs []string // transient event ids, oldest first
	seenIDs   map[string]struct{}
}

// NewStreamService builds a stream bridge. A nil logger is replaced by a
// no-op logger.
func NewStreamService(prov provider.Provider, cfg StreamConfig, log *zap.Logger) *StreamService {
	if log == nil {
		log = zap.NewNop()
	}
	return &StreamService{
		provider: prov,
		cfg:      cfg,
		log:      log,
		sessions: map[string]*sessionState{},
		seenIDs:  map[string]struct{}{},
	}
}

// Run drives the bridge until ctx is cancelled: it holds one bus
// subscription open (reconnecting with backoff) and sweeps expired pending
// requests. Run blocks; start it in a goroutine at wiring time.
func (s *StreamService) Run(ctx context.Context) {
	go s.sweepLoop(ctx)
	backoff := reconnectMinBackoff
	for ctx.Err() == nil {
		started := time.Now()
		err := s.pumpBus(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= reconnectResetAfter {
			backoff = reconnectMinBackoff
		}
		s.log.Warn("harness event bus ended; reconnecting",
			zap.Error(err), zap.Duration("backoff", backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < reconnectMaxBackoff {
			backoff *= 2
			if backoff > reconnectMaxBackoff {
				backoff = reconnectMaxBackoff
			}
		}
	}
}

// pumpBus consumes one bus subscription until it fails or ctx ends.
func (s *StreamService) pumpBus(ctx context.Context) error {
	stream, err := s.provider.BusStream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		frame, err := stream.Next(ctx)
		if err != nil {
			return err
		}
		s.dispatch(frame)
	}
}

// SetApprovalMode registers the approval mode of a session (agent-level
// approval_mode; unattended triggers register auto). Sessions without a
// registered mode use the configured fallback. Only "auto" enables
// auto-approval; every other value behaves as manual.
func (s *StreamService) SetApprovalMode(sessionID, mode string) {
	if mode != ApprovalAuto {
		mode = ApprovalManual
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateLocked(sessionID).approval = mode
}

// Subscribe registers a live frame feed for a session and returns the
// current ring buffer (oldest first) as the replay baseline. Snapshot and
// registration are atomic, so replay and live frames neither overlap nor
// leave a gap. stop must be called when the consumer goes away.
func (s *StreamService) Subscribe(sessionID string) (replay []provider.Frame, live <-chan *provider.Frame, stop func()) {
	ch := make(chan *provider.Frame, subscriberBuffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stateLocked(sessionID)
	id := st.nextSub
	st.nextSub++
	st.subs[id] = ch
	var once sync.Once
	return append([]provider.Frame(nil), st.ring...), ch, func() {
		once.Do(func() { s.unsubscribe(sessionID, id) })
	}
}

func (s *StreamService) unsubscribe(sessionID string, id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.sessions[sessionID]; st != nil {
		delete(st.subs, id)
	}
}

// PendingRequests snapshots the pending permission and question requests of
// a session, earliest expiry first.
func (s *StreamService) PendingRequests(sessionID string) []PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[sessionID]
	if st == nil {
		return nil
	}
	out := make([]PendingRequest, 0, len(st.pending))
	for _, req := range st.pending {
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].RequestID < out[j].RequestID
	})
	return out
}

// ReplyPermission answers a pending permission request.
func (s *StreamService) ReplyPermission(ctx context.Context, sessionID, requestID string, reply provider.PermissionReply) error {
	return s.answer(sessionID, requestID, provider.FramePermission, func() error {
		return s.provider.ReplyPermission(ctx, sessionID, requestID, reply)
	})
}

// ReplyQuestion answers a pending question request.
func (s *StreamService) ReplyQuestion(ctx context.Context, sessionID, requestID string, answers provider.QuestionAnswers) error {
	return s.answer(sessionID, requestID, provider.FrameQuestion, func() error {
		return s.provider.ReplyQuestion(ctx, sessionID, requestID, answers)
	})
}

// dispatch deduplicates, records and fans out one bus frame.
func (s *StreamService) dispatch(frame *provider.Frame) {
	if frame == nil || frame.SessionID == "" {
		return
	}
	var auto *PendingRequest
	s.mu.Lock()
	st := s.stateLocked(frame.SessionID)
	if !s.acceptLocked(st, frame) {
		s.mu.Unlock()
		return
	}
	st.activeAt = time.Now()
	st.frames++
	s.appendRingLocked(st, frame)
	if frame.Kind == provider.FramePermission || frame.Kind == provider.FrameQuestion {
		if req, ok := s.putPendingLocked(st, frame); ok &&
			frame.Kind == provider.FramePermission && s.modeLocked(st) == ApprovalAuto {
			auto = &req
		}
	}
	s.trackBusyLocked(frame.SessionID, st, frame)
	s.fanoutLocked(st, frame)
	s.evictLocked()
	s.mu.Unlock()

	if auto != nil {
		go s.autoApprove(*auto)
	}
}

// trackBusyLocked folds status frames into the busy state and (dis)arms the
// settle check: prompt/step-start frames assert business, step-end-ish frames
// open the settle window that confirms idleness against the backend.
func (s *StreamService) trackBusyLocked(sessionID string, st *sessionState, frame *provider.Frame) {
	if frame.Kind != provider.FrameStatus || frame.Status == nil {
		return
	}
	switch frame.Status.Name {
	case provider.StatusPromptAdmitted, provider.StatusPrompted, provider.StatusStepStarted:
		st.busy = true
		s.stopSettleLocked(st)
	case provider.StatusStepEnded, provider.StatusStepFailed, provider.StatusError:
		s.armSettleLocked(sessionID, st, s.settleDelay())
	}
}

// armSettleLocked schedules the settle check for a session.
func (s *StreamService) armSettleLocked(sessionID string, st *sessionState, delay time.Duration) {
	s.stopSettleLocked(st)
	st.settle = time.AfterFunc(delay, func() { s.settleCheck(sessionID) })
}

func (s *StreamService) stopSettleLocked(st *sessionState) {
	if st.settle != nil {
		st.settle.Stop()
		st.settle = nil
	}
}

// settleCheck asks the backend whether the session still runs an agent loop
// and, once it does not and no ask awaits an answer, synthesizes the idle
// status frame through the ring + fanout path. A still busy backend re-arms
// the check; a failed probe retries.
func (s *StreamService) settleCheck(sessionID string) {
	s.mu.Lock()
	st := s.sessions[sessionID]
	if st == nil || !st.busy || st.settle == nil || len(st.pending) > 0 {
		// Gone, a prompt frame arrived meanwhile, already checking, or an
		// approval/question waits for an answer (the backend resumes the
		// loop once it is answered and the next frames re-arm the check).
		s.mu.Unlock()
		return
	}
	st.settle = nil
	seen := st.frames
	s.mu.Unlock()

	active, err := s.provider.ActiveSessions(context.Background())
	s.mu.Lock()
	defer s.mu.Unlock()
	if st = s.sessions[sessionID]; st == nil || st.frames != seen {
		// Evicted or a frame arrived while probing: that frame already
		// re-tracked business, so this round is obsolete.
		return
	}
	switch {
	case err != nil:
		// Backend unreachable: probe again later.
	case active[sessionID]:
		// Turn still open (inter-step gap or queued prompt): keep polling.
	default:
		st.busy = false
		st.activeAt = time.Now()
		idle := &provider.Frame{
			SessionID: sessionID,
			Kind:      provider.FrameStatus,
			Status:    &provider.StatusFrame{Name: provider.StatusIdle},
		}
		s.appendRingLocked(st, idle)
		st.frames++
		s.fanoutLocked(st, idle)
		return
	}
	st.settle = time.AfterFunc(s.settlePoll(), func() { s.settleCheck(sessionID) })
}

// SessionBusy reports whether the bridge believes the session's agent loop
// still runs. Unknown sessions are idle.
func (s *StreamService) SessionBusy(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[sessionID]
	return st != nil && st.busy
}

// SettleSession opens a settle window right away, skipping the quiet delay.
// Interrupt is the main caller: an aborted loop may produce no further
// frames, so only the active probe can close the business.
func (s *StreamService) SettleSession(sessionID string) {
	s.mu.Lock()
	st := s.sessions[sessionID]
	if st == nil || !st.busy || st.settle != nil {
		s.mu.Unlock()
		return
	}
	st.settle = time.AfterFunc(0, func() { s.settleCheck(sessionID) })
	s.mu.Unlock()
}

// acceptLocked reports whether the frame is new for its session, updating
// the dedupe cursors: durable frames by per-session seq, transient frames by
// event id.
func (s *StreamService) acceptLocked(st *sessionState, frame *provider.Frame) bool {
	if frame.Seq > 0 {
		if frame.Seq <= st.lastSeq {
			return false
		}
		st.lastSeq = frame.Seq
		return true
	}
	if frame.EventID == "" {
		return true
	}
	if _, dup := s.seenIDs[frame.EventID]; dup {
		return false
	}
	s.seenIDs[frame.EventID] = struct{}{}
	s.recentIDs = append(s.recentIDs, frame.EventID)
	if len(s.recentIDs) > recentEventIDs {
		delete(s.seenIDs, s.recentIDs[0])
		s.recentIDs = s.recentIDs[1:]
	}
	return true
}

// appendRingLocked keeps the last RingSize dispatched frames per session as
// the replay baseline for late-joining subscribers.
func (s *StreamService) appendRingLocked(st *sessionState, frame *provider.Frame) {
	st.ring = append(st.ring, *frame)
	if size := s.ringSize(); len(st.ring) > size {
		st.ring = st.ring[len(st.ring)-size:]
	}
}

// putPendingLocked registers a permission or question request. It reports
// false for frames without a request id.
func (s *StreamService) putPendingLocked(st *sessionState, frame *provider.Frame) (PendingRequest, bool) {
	req := PendingRequest{
		SessionID: frame.SessionID,
		Kind:      frame.Kind,
		ExpiresAt: time.Now().Add(s.pendingTTL()),
		Frame:     *frame,
	}
	switch {
	case frame.Permission != nil:
		req.RequestID = frame.Permission.RequestID
	case frame.Question != nil:
		req.RequestID = frame.Question.RequestID
	}
	if req.RequestID == "" {
		return req, false
	}
	st.pending[req.RequestID] = req
	return req, true
}

// fanoutLocked delivers the frame to every live subscriber without blocking
// the bus pump; an overflowing subscriber drops frames (the ring buffer
// still holds them for catch-up).
func (s *StreamService) fanoutLocked(st *sessionState, frame *provider.Frame) {
	for _, ch := range st.subs {
		select {
		case ch <- frame:
		default:
			s.log.Warn("harness subscriber overflow; frame dropped",
				zap.String("session_id", frame.SessionID),
				zap.String("kind", string(frame.Kind)))
		}
	}
}

// evictLocked bounds the tracked-session state.
func (s *StreamService) evictLocked() {
	if len(s.sessions) <= maxTrackedSessions {
		return
	}
	type candidate struct {
		id string
		at time.Time
	}
	var candidates []candidate
	for id, st := range s.sessions {
		if len(st.subs) == 0 && len(st.pending) == 0 {
			candidates = append(candidates, candidate{id: id, at: st.activeAt})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	drop := len(s.sessions) - maxTrackedSessions
	if drop > len(candidates) {
		drop = len(candidates)
	}
	for _, c := range candidates[:drop] {
		delete(s.sessions, c.id)
	}
}

func (s *StreamService) stateLocked(sessionID string) *sessionState {
	st := s.sessions[sessionID]
	if st == nil {
		st = &sessionState{
			pending:  map[string]PendingRequest{},
			subs:     map[int]chan *provider.Frame{},
			activeAt: time.Now(),
		}
		s.sessions[sessionID] = st
	}
	return st
}

func (s *StreamService) pendingTTL() time.Duration {
	if s.cfg.PendingTTL > 0 {
		return s.cfg.PendingTTL
	}
	return DefaultPendingTTL
}

func (s *StreamService) ringSize() int {
	if s.cfg.RingSize > 0 {
		return s.cfg.RingSize
	}
	return DefaultRingSize
}

func (s *StreamService) settleDelay() time.Duration {
	if s.cfg.SettleDelay > 0 {
		return s.cfg.SettleDelay
	}
	return DefaultSettleDelay
}

func (s *StreamService) settlePoll() time.Duration {
	if s.cfg.SettlePoll > 0 {
		return s.cfg.SettlePoll
	}
	return DefaultSettlePoll
}

// modeLocked returns the effective approval mode of the session.
func (s *StreamService) modeLocked(st *sessionState) string {
	if st.approval != "" {
		return st.approval
	}
	return s.cfg.ApprovalMode
}

// answer takes the pending entry, forwards the reply to the provider and
// restores the entry with its remaining TTL when the provider call fails.
func (s *StreamService) answer(sessionID, requestID string, kind provider.FrameKind, forward func() error) error {
	req, ok := s.takePending(sessionID, requestID, kind)
	if !ok {
		return ErrRequestNotPending
	}
	if err := forward(); err != nil {
		s.requeue(req)
		return err
	}
	return nil
}

// takePending removes and returns the pending entry, if held.
func (s *StreamService) takePending(sessionID, requestID string, kind provider.FrameKind) (PendingRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[sessionID]
	if st == nil {
		return PendingRequest{}, false
	}
	req, ok := st.pending[requestID]
	if !ok || req.Kind != kind {
		return PendingRequest{}, false
	}
	delete(st.pending, requestID)
	return req, true
}

// requeue restores a pending entry unless a newer ask already re-registered
// the same request id.
func (s *StreamService) requeue(req PendingRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stateLocked(req.SessionID)
	if _, held := st.pending[req.RequestID]; !held {
		st.pending[req.RequestID] = req
	}
}

// autoApprove allows a permission request of an auto-mode session. On
// failure the request stays pending so the TTL sweep can reject it.
func (s *StreamService) autoApprove(req PendingRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), answerTimeout)
	defer cancel()
	if err := s.provider.ReplyPermission(ctx, req.SessionID, req.RequestID, provider.PermissionOnce); err != nil {
		s.log.Warn("harness auto-approve failed; pending TTL will reject",
			zap.String("session_id", req.SessionID),
			zap.String("request_id", req.RequestID),
			zap.Error(err))
		return
	}
	s.takePending(req.SessionID, req.RequestID, provider.FramePermission)
	s.log.Info("harness permission auto-approved",
		zap.String("session_id", req.SessionID),
		zap.String("request_id", req.RequestID))
}

func (s *StreamService) sweepLoop(ctx context.Context) {
	interval := s.cfg.SweepInterval
	if interval <= 0 {
		interval = pendingSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepExpired(ctx)
		}
	}
}

// sweepExpired rejects pending requests whose TTL elapsed: permissions with
// a reject reply, questions with a dismiss. A failed reject is requeued with
// a fresh TTL so the next sweep retries.
func (s *StreamService) sweepExpired(ctx context.Context) {
	now := time.Now()
	var expired []PendingRequest
	s.mu.Lock()
	for _, st := range s.sessions {
		for id, req := range st.pending {
			if !req.ExpiresAt.After(now) {
				delete(st.pending, id)
				expired = append(expired, req)
			}
		}
	}
	s.mu.Unlock()
	for _, req := range expired {
		var err error
		if req.Kind == provider.FramePermission {
			err = s.provider.ReplyPermission(ctx, req.SessionID, req.RequestID, provider.PermissionReject)
		} else {
			err = s.provider.RejectQuestion(ctx, req.SessionID, req.RequestID)
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("harness pending TTL reject failed; will retry",
				zap.String("session_id", req.SessionID),
				zap.String("request_id", req.RequestID),
				zap.Error(err))
			req.ExpiresAt = time.Now().Add(s.pendingTTL())
			s.requeue(req)
			continue
		}
		s.log.Warn("harness pending request expired; rejected",
			zap.String("session_id", req.SessionID),
			zap.String("request_id", req.RequestID))
	}
}
