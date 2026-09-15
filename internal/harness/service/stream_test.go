package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

// fakeBusProvider extends the session fake with the stream surface the
// bridge consumes: a chan-backed bus and recorded answers.
type fakeBusProvider struct {
	*fakeProvider

	bus         chan *provider.Frame
	permCalls   chan permCall
	permErr     error
	questionIDs chan string
	rejectIDs   chan string
}

type permCall struct {
	sessionID string
	requestID string
	reply     provider.PermissionReply
}

func newFakeBusProvider() *fakeBusProvider {
	return &fakeBusProvider{
		fakeProvider: newFakeProvider(),
		bus:          make(chan *provider.Frame, 64),
		permCalls:    make(chan permCall, 64),
		questionIDs:  make(chan string, 64),
		rejectIDs:    make(chan string, 64),
	}
}

func (f *fakeBusProvider) BusStream(context.Context) (provider.Stream, error) {
	return chanStream{ch: f.bus}, nil
}

func (f *fakeBusProvider) ReplyPermission(_ context.Context, sessionID, requestID string, reply provider.PermissionReply) error {
	f.permCalls <- permCall{sessionID: sessionID, requestID: requestID, reply: reply}
	if f.permErr != nil {
		return f.permErr
	}
	return nil
}

func (f *fakeBusProvider) ReplyQuestion(_ context.Context, _, requestID string, _ provider.QuestionAnswers) error {
	f.questionIDs <- requestID
	return nil
}

func (f *fakeBusProvider) RejectQuestion(_ context.Context, _, requestID string) error {
	f.rejectIDs <- requestID
	return nil
}

// chanStream adapts a frame channel to provider.Stream.
type chanStream struct{ ch <-chan *provider.Frame }

func (c chanStream) Next(ctx context.Context) (*provider.Frame, error) {
	select {
	case f := <-c.ch:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (chanStream) Close() error { return nil }

func textFrame(sessionID string, seq int64, text string) *provider.Frame {
	return &provider.Frame{
		Seq: seq, SessionID: sessionID, Kind: provider.FrameMessageText,
		MessageText: &provider.MessageText{TextID: text, Text: text},
	}
}

func permFrame(sessionID, eventID, requestID string) *provider.Frame {
	return &provider.Frame{
		EventID: eventID, SessionID: sessionID, Kind: provider.FramePermission,
		Permission: &provider.PermissionFrame{RequestID: requestID, Action: "bash"},
	}
}

func questionFrame(sessionID, eventID, requestID string) *provider.Frame {
	return &provider.Frame{
		EventID: eventID, SessionID: sessionID, Kind: provider.FrameQuestion,
		Question: &provider.QuestionFrame{RequestID: requestID, Questions: []provider.Question{
			{Question: "proceed?", Options: []string{"yes", "no"}},
		}},
	}
}

// drainLive non-blockingly collects everything currently queued on live.
func drainLive(live <-chan *provider.Frame) []*provider.Frame {
	var out []*provider.Frame
	for {
		select {
		case f := <-live:
			out = append(out, f)
		default:
			return out
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func assertNoCall[T any](t *testing.T, ch chan T, what string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected %s: %v", what, v)
	case <-time.After(100 * time.Millisecond):
	}
}

func pendingIDs(svc *StreamService, sessionID string) []string {
	var ids []string
	for _, req := range svc.PendingRequests(sessionID) {
		ids = append(ids, req.RequestID)
	}
	return ids
}

func TestStreamDispatchDedupesAndFansOut(t *testing.T) {
	svc := NewStreamService(newFakeBusProvider(), StreamConfig{}, nil)
	replay, live, stop := svc.Subscribe("ses_1")
	defer stop()
	if len(replay) != 0 {
		t.Fatalf("initial replay = %d frames, want 0", len(replay))
	}

	svc.dispatch(textFrame("ses_1", 5, "a"))
	svc.dispatch(textFrame("ses_1", 5, "a"))   // duplicate durable seq
	svc.dispatch(textFrame("ses_1", 3, "old")) // seq regression
	svc.dispatch(textFrame("ses_1", 7, "b"))
	svc.dispatch(permFrame("ses_1", "evt_1", "req_1"))
	svc.dispatch(permFrame("ses_1", "evt_1", "req_2")) // duplicate transient event id
	svc.dispatch(permFrame("ses_1", "evt_2", "req_3"))
	svc.dispatch(textFrame("other", 9, "x")) // other session: not for this feed

	got := drainLive(live)
	if len(got) != 4 {
		t.Fatalf("live frames = %d (%v), want 4", len(got), got)
	}
	if got[0].Seq != 5 || got[1].Seq != 7 {
		t.Fatalf("durable frames seq = %d,%d, want 5,7", got[0].Seq, got[1].Seq)
	}
	if got[2].Permission.RequestID != "req_1" || got[3].Permission.RequestID != "req_3" {
		t.Fatalf("permission frames = %s,%s, want req_1,req_3",
			got[2].Permission.RequestID, got[3].Permission.RequestID)
	}

	// Duplicates were never registered as pending either.
	ids := pendingIDs(svc, "ses_1")
	if len(ids) != 2 || ids[0] != "req_1" || ids[1] != "req_3" {
		t.Fatalf("pending = %v, want [req_1 req_3]", ids)
	}

	// A late joiner replays exactly the accepted frames.
	replay2, _, stop2 := svc.Subscribe("ses_1")
	defer stop2()
	if len(replay2) != 4 {
		t.Fatalf("replay = %d frames, want 4", len(replay2))
	}
}

func TestStreamAutoApproveAllows(t *testing.T) {
	t.Run("config fallback auto", func(t *testing.T) {
		fake := newFakeBusProvider()
		svc := NewStreamService(fake, StreamConfig{ApprovalMode: ApprovalAuto}, nil)
		_, live, stop := svc.Subscribe("ses_1")
		defer stop()

		svc.dispatch(permFrame("ses_1", "evt_1", "req_1"))

		call := <-fake.permCalls
		if call.requestID != "req_1" || call.reply != provider.PermissionOnce {
			t.Fatalf("auto-approve call = %+v, want req_1/once", call)
		}
		waitFor(t, "pending to clear", func() bool { return len(pendingIDs(svc, "ses_1")) == 0 })
		if got := drainLive(live); len(got) != 1 || got[0].Kind != provider.FramePermission {
			t.Fatalf("permission frame not dispatched: %v", got)
		}
	})

	t.Run("session override beats config", func(t *testing.T) {
		fake := newFakeBusProvider()
		svc := NewStreamService(fake, StreamConfig{ApprovalMode: ApprovalManual}, nil)
		svc.SetApprovalMode("ses_1", ApprovalAuto)
		svc.dispatch(permFrame("ses_1", "evt_1", "req_1"))
		if call := <-fake.permCalls; call.reply != provider.PermissionOnce {
			t.Fatalf("auto-approve call = %+v, want once", call)
		}

		svc.SetApprovalMode("ses_2", "bogus") // normalized to manual
		svc.dispatch(permFrame("ses_2", "evt_2", "req_2"))
		assertNoCall(t, fake.permCalls, "auto-approve of a manual session")
	})

	t.Run("failed auto-approve keeps pending", func(t *testing.T) {
		fake := newFakeBusProvider()
		fake.permErr = errors.New("boom")
		svc := NewStreamService(fake, StreamConfig{ApprovalMode: ApprovalAuto}, nil)
		svc.dispatch(permFrame("ses_1", "evt_1", "req_1"))

		<-fake.permCalls
		waitFor(t, "pending to settle", func() bool {
			ids := pendingIDs(svc, "ses_1")
			return len(ids) == 1 && ids[0] == "req_1"
		})
	})
}

func TestStreamAutoModeRecordsQuestionOnly(t *testing.T) {
	fake := newFakeBusProvider()
	svc := NewStreamService(fake, StreamConfig{ApprovalMode: ApprovalAuto}, nil)
	_, live, stop := svc.Subscribe("ses_1")
	defer stop()

	svc.dispatch(questionFrame("ses_1", "evt_1", "req_q"))

	// Questions are recorded and forwarded, never auto-answered.
	assertNoCall(t, fake.permCalls, "auto-answer of a question")
	assertNoCall(t, fake.questionIDs, "auto-answer of a question")
	assertNoCall(t, fake.rejectIDs, "auto-reject of a question")
	ids := pendingIDs(svc, "ses_1")
	if len(ids) != 1 || ids[0] != "req_q" {
		t.Fatalf("pending = %v, want [req_q]", ids)
	}
	pending := svc.PendingRequests("ses_1")
	if pending[0].Kind != provider.FrameQuestion || pending[0].Frame.Question == nil {
		t.Fatalf("pending entry = %+v, want recorded question frame", pending[0])
	}
	if got := drainLive(live); len(got) != 1 || got[0].Kind != provider.FrameQuestion {
		t.Fatalf("question frame not dispatched: %v", got)
	}
}

func TestStreamPendingTTLRejects(t *testing.T) {
	fake := newFakeBusProvider()
	svc := NewStreamService(fake, StreamConfig{}, nil)
	svc.dispatch(permFrame("ses_1", "evt_1", "req_p"))
	svc.dispatch(questionFrame("ses_1", "evt_2", "req_q"))

	// Force both entries past their expiry, then sweep.
	svc.mu.Lock()
	for _, st := range svc.sessions {
		for id, req := range st.pending {
			req.ExpiresAt = time.Now().Add(-time.Second)
			st.pending[id] = req
		}
	}
	svc.mu.Unlock()
	svc.sweepExpired(context.Background())

	call := <-fake.permCalls
	if call.requestID != "req_p" || call.reply != provider.PermissionReject {
		t.Fatalf("TTL reject call = %+v, want req_p/reject", call)
	}
	if id := <-fake.rejectIDs; id != "req_q" {
		t.Fatalf("question reject = %q, want req_q", id)
	}
	waitFor(t, "pending to clear", func() bool { return len(pendingIDs(svc, "ses_1")) == 0 })
}

func TestStreamRingBufferEvictsOldest(t *testing.T) {
	svc := NewStreamService(newFakeBusProvider(), StreamConfig{RingSize: 5}, nil)
	for seq := int64(1); seq <= 8; seq++ {
		svc.dispatch(textFrame("ses_1", seq, "t"))
	}
	replay, _, stop := svc.Subscribe("ses_1")
	defer stop()
	if len(replay) != 5 {
		t.Fatalf("replay = %d frames, want 5", len(replay))
	}
	if replay[0].Seq != 4 || replay[4].Seq != 8 {
		t.Fatalf("replay seq span = %d..%d, want 4..8", replay[0].Seq, replay[4].Seq)
	}
}

func TestStreamReplies(t *testing.T) {
	fake := newFakeBusProvider()
	svc := NewStreamService(fake, StreamConfig{}, nil)
	ctx := context.Background()

	svc.dispatch(permFrame("ses_1", "evt_1", "req_p"))
	if err := svc.ReplyPermission(ctx, "ses_1", "req_p", provider.PermissionAlways); err != nil {
		t.Fatalf("reply permission: %v", err)
	}
	if call := <-fake.permCalls; call.reply != provider.PermissionAlways {
		t.Fatalf("reply call = %+v, want always", call)
	}
	if err := svc.ReplyPermission(ctx, "ses_1", "req_p", provider.PermissionOnce); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("second reply err = %v, want ErrRequestNotPending", err)
	}

	svc.dispatch(questionFrame("ses_1", "evt_2", "req_q"))
	if err := svc.ReplyQuestion(ctx, "ses_1", "req_q", provider.QuestionAnswers{Answers: [][]string{{"yes"}}}); err != nil {
		t.Fatalf("reply question: %v", err)
	}
	if id := <-fake.questionIDs; id != "req_q" {
		t.Fatalf("question reply = %q, want req_q", id)
	}

	// A failed forward restores the entry with its remaining TTL.
	svc.dispatch(permFrame("ses_1", "evt_3", "req_r"))
	fake.permErr = errors.New("boom")
	if err := svc.ReplyPermission(ctx, "ses_1", "req_r", provider.PermissionOnce); err == nil {
		t.Fatalf("reply permission must fail")
	}
	<-fake.permCalls
	waitFor(t, "entry to be restored", func() bool {
		ids := pendingIDs(svc, "ses_1")
		return len(ids) == 1 && ids[0] == "req_r"
	})
}
