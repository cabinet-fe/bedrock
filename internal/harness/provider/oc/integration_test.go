package oc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

// integrationSetup returns the adapter and the model used for real prompts.
// Prompts cost tokens, so these tests are opt-in like the contract suite.
func integrationSetup(t *testing.T) (*Adapter, provider.ModelRef) {
	t.Helper()
	cfg := serveEnv(t)
	modelID := os.Getenv("HARNESS_OC_TEST_MODEL")
	if modelID == "" {
		modelID = "opencode/muse-spark-1.3-contributor-free"
	}
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("HARNESS_OC_TEST_MODEL must be provider/model, got %q", modelID)
	}
	adapter := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := adapter.Client().Health(ctx); err != nil {
		t.Skipf("serve at %s unhealthy: %v", cfg.BaseURL, err)
	}
	return adapter, provider.ModelRef{ProviderID: parts[0], ID: parts[1]}
}

// readUntil collects frames until pred holds or the deadline expires.
func readUntil(ctx context.Context, t *testing.T, stream provider.Stream, pred func(*provider.Frame) bool, what string) *provider.Frame {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		frameCtx, cancel := context.WithDeadline(ctx, deadline)
		f, err := stream.Next(frameCtx)
		cancel()
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if pred(f) {
			return f
		}
	}
}

// TestIntegrationSessionPromptReplay covers the M0 acceptance against a real
// serve: create a session with location.directory, prompt with queue and
// steer delivery, and resume the per-session durable stream via after=<seq>.
func TestIntegrationSessionPromptReplay(t *testing.T) {
	adapter, model := integrationSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	session, err := adapter.CreateSession(ctx, provider.CreateSessionInput{
		Directory: t.TempDir(),
		Model:     &model,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if !strings.HasPrefix(session.ID, "ses") {
		t.Fatalf("unexpected session id %q", session.ID)
	}

	// 1. queue delivery. opencode holds SSE response headers until the first
	// event, so the stream opens after the prompt; the after=0 durable
	// replay redelivers everything from seq 1.
	ack, err := adapter.Prompt(ctx, session.ID, provider.PromptInput{
		Text:     "Reply with exactly the word: pong",
		Delivery: provider.DeliveryQueue,
	})
	if err != nil {
		t.Fatalf("prompt queue: %v", err)
	}
	if ack.AdmittedSeq == 0 || ack.MessageID == "" {
		t.Fatalf("prompt ack missing admittedSeq/messageID: %+v", ack)
	}

	live, err := adapter.EventStream(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("open live stream: %v", err)
	}
	defer live.Close()

	lastSeq := int64(0)
	sawAdmitted, sawPrompted, sawStepEnded, sawText := false, false, false, false
	for deadline := time.Now().Add(90 * time.Second); !sawStepEnded && time.Now().Before(deadline); {
		f, err := live.Next(ctx)
		if err != nil {
			t.Fatalf("live frame: %v", err)
		}
		if f.Seq > lastSeq {
			lastSeq = f.Seq
		}
		switch {
		case f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusPromptAdmitted:
			sawAdmitted = true
			if f.Status.MessageID != ack.MessageID {
				t.Errorf("admitted frame messageID %q != ack %q", f.Status.MessageID, ack.MessageID)
			}
			if f.Status.Delivery != provider.DeliveryQueue {
				t.Errorf("admitted delivery = %q, want queue", f.Status.Delivery)
			}
		case f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusPrompted:
			sawPrompted = true
		case f.Kind == provider.FrameMessageText:
			sawText = true
		case f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusStepEnded:
			sawStepEnded = true
		}
	}
	if !sawAdmitted || !sawPrompted || !sawText || !sawStepEnded {
		t.Fatalf("live stream incomplete: admitted=%v prompted=%v text=%v stepEnded=%v", sawAdmitted, sawPrompted, sawText, sawStepEnded)
	}

	// 2. steer delivery: admitted immediately, prompted after the idle turn.
	steerAck, err := adapter.Prompt(ctx, session.ID, provider.PromptInput{
		Text:     "Now reply with exactly the word: ping",
		Delivery: provider.DeliverySteer,
	})
	if err != nil {
		t.Fatalf("prompt steer: %v", err)
	}
	readUntil(ctx, t, live, func(f *provider.Frame) bool {
		return f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusPromptAdmitted &&
			f.Status.MessageID == steerAck.MessageID && f.Status.Delivery == provider.DeliverySteer
	}, "steer admission")
	steerDone := readUntil(ctx, t, live, func(f *provider.Frame) bool {
		return f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusPrompted &&
			f.Status.MessageID == steerAck.MessageID
	}, "steer prompted")
	if steerDone.Seq <= lastSeq {
		t.Errorf("steer prompted seq %d not after prior events %d", steerDone.Seq, lastSeq)
	}

	// drain the second turn
	readUntil(ctx, t, live, func(f *provider.Frame) bool {
		return f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusStepEnded
	}, "steer step ended")

	// 3. replay from the last durable seq: the reconnect must only deliver
	// events after the baseline, then continue live for a fresh prompt.
	after := lastSeq
	replay, err := adapter.EventStream(ctx, session.ID, after)
	if err != nil {
		t.Fatalf("open replay stream: %v", err)
	}
	defer replay.Close()
	third, err := adapter.Prompt(ctx, session.ID, provider.PromptInput{
		Text:     "Reply with exactly the word: last",
		Delivery: provider.DeliveryQueue,
	})
	if err != nil {
		t.Fatalf("prompt replay-turn: %v", err)
	}
	var sawReplayAdmit, sawReplayEnd bool
	for deadline := time.Now().Add(90 * time.Second); !sawReplayEnd && time.Now().Before(deadline); {
		f, err := replay.Next(ctx)
		if err != nil {
			t.Fatalf("replay frame: %v", err)
		}
		if f.Seq <= after {
			t.Errorf("replay delivered seq %d <= after %d (duplicate replay)", f.Seq, after)
		}
		if f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusPromptAdmitted && f.Status.MessageID == third.MessageID {
			sawReplayAdmit = true
		}
		if f.Kind == provider.FrameStatus && f.Status.Name == provider.StatusStepEnded && sawReplayAdmit {
			sawReplayEnd = true
		}
	}
	if !sawReplayEnd {
		t.Fatal("replayed stream never reached the new turn's step end")
	}

	// 4. history reflects the prompts
	messages, err := adapter.History(ctx, session.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(messages) < 4 {
		t.Fatalf("history has %d messages, want at least 4 (2 user + 2 assistant)", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("first history role = %q, want user", messages[0].Role)
	}
}

// TestIntegrationSelectModelAndInterrupt verifies model switching and a
// graceful interrupt of an idle session.
func TestIntegrationSelectModelAndInterrupt(t *testing.T) {
	adapter, model := integrationSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	session, err := adapter.CreateSession(ctx, provider.CreateSessionInput{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := adapter.SelectModel(ctx, session.ID, model); err != nil {
		t.Fatalf("select model: %v", err)
	}
	if err := adapter.Interrupt(ctx, session.ID); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 500 {
			t.Fatalf("interrupt: %v", err)
		}
		t.Logf("interrupt on idle session: %v", err)
	}
}

// TestIntegrationCatalogs verifies the directory catalog endpoints on a real
// serve, including the bracketed location query encoding.
func TestIntegrationCatalogs(t *testing.T) {
	adapter, _ := integrationSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()

	models, err := adapter.ListModels(ctx, dir)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	// A freshly queried directory can briefly return an empty catalog while
	// the project loads (harness-integration-plan.md §2.2); retry briefly.
	for deadline := time.Now().Add(30 * time.Second); len(models) == 0 && time.Now().Before(deadline); {
		time.Sleep(2 * time.Second)
		models, err = adapter.ListModels(ctx, dir)
		if err != nil {
			t.Fatalf("list models retry: %v", err)
		}
	}
	if len(models) == 0 {
		t.Fatal("model catalog empty after retries")
	}
	for _, m := range models {
		if m.ID == "" || m.ProviderID == "" {
			t.Fatalf("model entry missing id/providerID: %+v", m)
		}
	}

	providers, err := adapter.Client().ListProviders(ctx, dir)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("provider catalog empty")
	}

	agents, err := adapter.ListAgents(ctx, dir)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	// A freshly queried directory can briefly return an empty list while
	// the project loads (harness-integration-plan.md §2.2); retry briefly.
	for deadline := time.Now().Add(30 * time.Second); len(agents) == 0 && time.Now().Before(deadline); {
		time.Sleep(2 * time.Second)
		agents, err = adapter.ListAgents(ctx, dir)
		if err != nil {
			t.Fatalf("list agents retry: %v", err)
		}
	}
	found := map[string]bool{}
	for _, a := range agents {
		found[a.Name] = true
	}
	if !found["build"] {
		t.Fatalf("builtin build agent missing from catalog: %v", found)
	}
}
