package oc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

// TestScanSSE feeds a recorded wire stream (including the ": connected"
// keepalive and a multi-line data event) through the parser.
func TestScanSSE(t *testing.T) {
	input := ": connected\n\n" +
		"data: {\"id\":\"evt_1\",\"type\":\"session.idle\",\"data\":{\"sessionID\":\"ses_1\"}}\n\n" +
		"data: line one\n" +
		"data: line two\n\n"
	var events []string
	if err := scanSSE(strings.NewReader(input), func(data []byte) error {
		events = append(events, string(data))
		return nil
	}); err != nil {
		t.Fatalf("scanSSE: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	if events[0] != `{"id":"evt_1","type":"session.idle","data":{"sessionID":"ses_1"}}` {
		t.Fatalf("event[0] = %q", events[0])
	}
	if events[1] != "line one\nline two" {
		t.Fatalf("multi-line event = %q", events[1])
	}
}

// TestScanSSEEmitError propagates handler errors.
func TestScanSSEEmitError(t *testing.T) {
	want := errors.New("boom")
	err := scanSSE(strings.NewReader("data: x\n\n"), func([]byte) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestToFrame maps every wire type the adapter consumes; payloads are the
// shapes captured from opencode 2.2.0.
func TestToFrame(t *testing.T) {
	tests := []struct {
		name  string
		wire  string
		kind  provider.FrameKind
		seq   int64
		check func(*testing.T, *provider.Frame)
	}{
		{
			name: "prompt admitted",
			wire: `{"id":"evt_a","type":"session.inbox.enqueued","location":{"directory":"/w"},"durable":{"aggregateID":"ses_1","seq":1,"version":1},"data":{"sessionID":"ses_1","inboxID":"msg_1","item":{"type":"user","payload":{"text":"hi"},"delivery":"queue"}}}`,
			kind: provider.FrameStatus,
			seq:  1,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusPromptAdmitted || f.Status.MessageID != "msg_1" || f.Status.Delivery != provider.DeliveryQueue {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "prompt delivered",
			wire: `{"id":"evt_a2","type":"session.inbox.delivered","durable":{"aggregateID":"ses_1","seq":2,"version":1},"data":{"sessionID":"ses_1","inboxID":"msg_1"}}`,
			kind: provider.FrameStatus,
			seq:  2,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusPrompted || f.Status.MessageID != "msg_1" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "step started with agent and model",
			wire: `{"id":"evt_b","type":"session.step.started","durable":{"aggregateID":"ses_1","seq":5,"version":1},"data":{"sessionID":"ses_1","agent":"build","model":{"id":"m","providerID":"p","variant":"default"},"assistantMessageID":"msg_2","started":1793182899724}}`,
			kind: provider.FrameStatus,
			seq:  5,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusStepStarted || f.Status.Agent != "build" || f.Status.Model == nil || f.Status.Model.ProviderID != "p" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "step ended with finish",
			wire: `{"id":"evt_b2","type":"session.step.ended","durable":{"aggregateID":"ses_1","seq":8,"version":2},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","finish":"stop","cost":0,"tokens":{"input":1,"output":2,"reasoning":0,"cache":{"read":0,"write":0}}}}`,
			kind: provider.FrameStatus,
			seq:  8,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusStepEnded || f.Status.Finish != "stop" || f.Status.AssistantMessageID != "msg_2" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "step failed with error",
			wire: `{"id":"evt_b3","type":"session.step.failed","durable":{"aggregateID":"ses_1","seq":9,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","error":{"type":"provider.transport","message":"ECONNREFUSED"}}}`,
			kind: provider.FrameStatus,
			seq:  9,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusStepFailed || f.Status.Error != "ECONNREFUSED" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "execution failed maps to terminal error",
			wire: `{"id":"evt_b4","type":"session.execution.failed","durable":{"aggregateID":"ses_1","seq":10,"version":1},"data":{"sessionID":"ses_1","error":{"type":"provider.transport","message":"boom"}}}`,
			kind: provider.FrameStatus,
			seq:  10,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusError || f.Status.Error != "boom" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "execution succeeded maps to idle",
			wire: `{"id":"evt_b5","type":"session.execution.succeeded","durable":{"aggregateID":"ses_1","seq":11,"version":1},"data":{"sessionID":"ses_1"}}`,
			kind: provider.FrameStatus,
			seq:  11,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusIdle {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "text delta carries ordinal as textId",
			wire: `{"id":"evt_c","type":"session.text.delta","location":{"directory":"/w"},"durable":{"aggregateID":"ses_1","seq":12,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","ordinal":0,"delta":"po"}}`,
			kind: provider.FrameMessageDelta,
			seq:  12,
			check: func(t *testing.T, f *provider.Frame) {
				if f.MessageDelta.Delta != "po" || f.MessageDelta.TextID != "0" {
					t.Fatalf("delta = %+v", f.MessageDelta)
				}
			},
		},
		{
			name: "text ended carries full text",
			wire: `{"id":"evt_d","type":"session.text.ended","durable":{"aggregateID":"ses_1","seq":7,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","ordinal":0,"text":"pong"}}`,
			kind: provider.FrameMessageText,
			seq:  7,
			check: func(t *testing.T, f *provider.Frame) {
				if f.MessageText.Text != "pong" || f.MessageText.TextID != "0" {
					t.Fatalf("text = %+v", f.MessageText)
				}
			},
		},
		{
			name: "reasoning delta",
			wire: `{"id":"evt_d2","type":"session.reasoning.delta","durable":{"aggregateID":"ses_1","seq":13,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","ordinal":0,"delta":"thinking"}}`,
			kind: provider.FrameReasoningDelta,
			seq:  13,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ReasoningDelta.Delta != "thinking" {
					t.Fatalf("reasoning = %+v", f.ReasoningDelta)
				}
			},
		},
		{
			name: "tool called",
			wire: `{"id":"evt_e","type":"session.tool.called","durable":{"aggregateID":"ses_1","seq":10,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","id":"call_1","name":"shell","input":{"command":"echo hi"},"executed":false}}`,
			kind: provider.FrameToolCall,
			seq:  10,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ToolCall.Tool != "shell" || f.ToolCall.CallID != "call_1" || string(f.ToolCall.Input) == "" {
					t.Fatalf("toolCall = %+v", f.ToolCall)
				}
			},
		},
		{
			name: "tool success",
			wire: `{"id":"evt_e2","type":"session.tool.success","durable":{"aggregateID":"ses_1","seq":11,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","id":"call_1","content":[{"type":"text","text":"hi\n"}],"metadata":{"exit":0},"executed":false}}`,
			kind: provider.FrameToolResult,
			seq:  11,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ToolResult.CallID != "call_1" || string(f.ToolResult.Output) == "" || f.ToolResult.Error != "" {
					t.Fatalf("toolResult = %+v", f.ToolResult)
				}
			},
		},
		{
			name: "tool failed",
			wire: `{"id":"evt_f","type":"session.tool.failed","durable":{"aggregateID":"ses_1","seq":12,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","id":"call_1","error":{"type":"tool.execution","message":"denied"},"executed":false}}`,
			kind: provider.FrameToolResult,
			seq:  12,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ToolResult.Error != "denied" {
					t.Fatalf("toolResult = %+v", f.ToolResult)
				}
			},
		},
		{
			name: "permission asked",
			wire: `{"id":"evt_g","type":"permission.asked","location":{"directory":"/w"},"data":{"id":"per_1","sessionID":"ses_1","action":"shell","resources":["echo hi"],"save":["echo hi"],"source":{"type":"tool","messageID":"msg_2","id":"call_1"}}}`,
			kind: provider.FramePermission,
			seq:  0,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Permission.RequestID != "per_1" || f.Permission.Action != "shell" || len(f.Permission.Resources) != 1 {
					t.Fatalf("permission = %+v", f.Permission)
				}
			},
		},
		{
			name: "form created maps to question",
			wire: `{"id":"evt_h","type":"form.created","location":{"directory":"/w"},"data":{"id":"frm_1","sessionID":"ses_1","title":"Pick a color","fields":[{"key":"color","type":"string","title":"color?","options":[{"value":"red","label":"red"},{"value":"blue","label":"blue"}]},{"key":"notes","type":"multiselect","description":"extra notes"}]}}`,
			kind: provider.FrameQuestion,
			seq:  0,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Question.RequestID != "frm_1" || len(f.Question.Questions) != 2 {
					t.Fatalf("question = %+v", f.Question)
				}
				q := f.Question.Questions[0]
				if q.Question != "color?" || q.Key != "color" || len(q.Options) != 2 || q.Options[0] != "red" {
					t.Fatalf("question detail = %+v", q)
				}
				if !f.Question.Questions[1].Multiple || f.Question.Questions[1].Question != "extra notes" {
					t.Fatalf("second question = %+v", f.Question.Questions[1])
				}
			},
		},
		{
			name: "irrelevant event dropped",
			wire: `{"id":"evt_j","type":"filesystem.changed","data":{"path":"/x"}}`,
			kind: "",
			check: func(t *testing.T, f *provider.Frame) {
				if f != nil {
					t.Fatalf("expected nil frame, got %+v", f)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ev rawEvent
			if err := json.Unmarshal([]byte(tt.wire), &ev); err != nil {
				t.Fatalf("unmarshal wire: %v", err)
			}
			f := toFrame(ev)
			if tt.kind == "" {
				tt.check(t, f)
				return
			}
			if f == nil {
				t.Fatal("toFrame returned nil")
			}
			if f.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", f.Kind, tt.kind)
			}
			if f.Seq != tt.seq {
				t.Fatalf("seq = %d, want %d", f.Seq, tt.seq)
			}
			if f.SessionID != "ses_1" {
				t.Fatalf("sessionID = %q", f.SessionID)
			}
			tt.check(t, f)
		})
	}
}

// TestBusStreamFramesAndCancellation checks the event bus stream: Basic
// Auth, unified frame delivery of durable events and context cancellation on
// a drained stream.
func TestBusStreamFramesAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/event" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if user, pass, _ := r.BasicAuth(); user != "opencode" || pass != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(": connected\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"session.inbox.enqueued\",\"location\":{\"directory\":\"/w\"},\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":4,\"version\":1},\"data\":{\"sessionID\":\"ses_1\",\"inboxID\":\"msg_9\",\"item\":{\"type\":\"user\",\"payload\":{\"text\":\"hi\"},\"delivery\":\"queue\"}}}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: {\"id\":\"evt_2\",\"type\":\"session.inbox.delivered\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":5,\"version\":1},\"data\":{\"sessionID\":\"ses_1\",\"inboxID\":\"msg_9\"}}\n\n"))
		fl.Flush()
		// keep the stream open so Next must honor context cancellation
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	ctx := context.Background()
	stream, err := client.BusStream(ctx)
	if err != nil {
		t.Fatalf("bus stream: %v", err)
	}
	defer stream.Close()

	first, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if first.Seq != 4 || first.Kind != provider.FrameStatus || first.Status.Name != provider.StatusPromptAdmitted {
		t.Fatalf("first = %+v", first)
	}
	second, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("second frame: %v", err)
	}
	if second.Seq != 5 || second.Status.Name != provider.StatusPrompted {
		t.Fatalf("second = %+v", second)
	}

	// no more frames: Next honors context cancellation
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := stream.Next(cancelCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("next on drained stream = %v, want deadline exceeded", err)
	}
}

// TestBusStreamTransientDelta checks transient delta frame delivery.
func TestBusStreamTransientDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(": connected\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"session.text.delta\",\"location\":{\"directory\":\"/w\"},\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":6,\"version\":1},\"data\":{\"sessionID\":\"ses_1\",\"assistantMessageID\":\"msg_2\",\"ordinal\":0,\"delta\":\"po\"}}\n\n"))
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	stream, err := client.BusStream(context.Background())
	if err != nil {
		t.Fatalf("bus stream: %v", err)
	}
	defer stream.Close()

	f, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("bus frame: %v", err)
	}
	if f.Kind != provider.FrameMessageDelta || f.MessageDelta.Delta != "po" {
		t.Fatalf("frame = %+v", f)
	}
}

// TestStreamHTTPError verifies non-2xx stream handshake surfaces APIError.
func TestStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"_tag":"ServiceUnavailableError","message":"overloaded"}`))
	}))
	t.Cleanup(srv.Close)
	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	_, err := client.BusStream(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want 503 APIError", err)
	}
}
