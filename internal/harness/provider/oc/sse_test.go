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
// shapes captured from opencode v1.18.29.
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
			wire: `{"id":"evt_a","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_1","seq":1,"version":1},"data":{"sessionID":"ses_1","messageID":"msg_1","prompt":{"text":"hi"},"delivery":"queue"}}`,
			kind: provider.FrameStatus,
			seq:  1,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusPromptAdmitted || f.Status.MessageID != "msg_1" || f.Status.Delivery != provider.DeliveryQueue {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "step ended with finish",
			wire: `{"id":"evt_b","type":"session.next.step.ended","durable":{"aggregateID":"ses_1","seq":8,"version":2},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","finish":"stop","cost":0,"tokens":{"input":1,"output":2,"reasoning":0,"cache":{"read":0,"write":0}}}}`,
			kind: provider.FrameStatus,
			seq:  8,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusStepEnded || f.Status.Finish != "stop" || f.Status.AssistantMessageID != "msg_2" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "text delta is transient",
			wire: `{"id":"evt_c","type":"session.next.text.delta","location":{"directory":"/w"},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","textID":"t_1","delta":"po"}}`,
			kind: provider.FrameMessageDelta,
			seq:  0,
			check: func(t *testing.T, f *provider.Frame) {
				if f.MessageDelta.Delta != "po" || f.MessageDelta.TextID != "t_1" {
					t.Fatalf("delta = %+v", f.MessageDelta)
				}
			},
		},
		{
			name: "text ended carries full text",
			wire: `{"id":"evt_d","type":"session.next.text.ended","durable":{"aggregateID":"ses_1","seq":7,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","textID":"t_1","text":"pong"}}`,
			kind: provider.FrameMessageText,
			seq:  7,
			check: func(t *testing.T, f *provider.Frame) {
				if f.MessageText.Text != "pong" {
					t.Fatalf("text = %+v", f.MessageText)
				}
			},
		},
		{
			name: "tool called",
			wire: `{"id":"evt_e","type":"session.next.tool.called","durable":{"aggregateID":"ses_1","seq":10,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","callID":"call_1","tool":"bash","input":{"command":"echo hi"},"provider":{"executed":false}}}`,
			kind: provider.FrameToolCall,
			seq:  10,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ToolCall.Tool != "bash" || string(f.ToolCall.Input) == "" {
					t.Fatalf("toolCall = %+v", f.ToolCall)
				}
			},
		},
		{
			name: "tool failed",
			wire: `{"id":"evt_f","type":"session.next.tool.failed","durable":{"aggregateID":"ses_1","seq":12,"version":1},"data":{"sessionID":"ses_1","assistantMessageID":"msg_2","callID":"call_1","error":{"message":"denied"},"provider":{"executed":false}}}`,
			kind: provider.FrameToolResult,
			seq:  12,
			check: func(t *testing.T, f *provider.Frame) {
				if f.ToolResult.Error != "denied" {
					t.Fatalf("toolResult = %+v", f.ToolResult)
				}
			},
		},
		{
			name: "permission asked (bus only)",
			wire: `{"id":"evt_g","type":"permission.v2.asked","location":{"directory":"/w"},"data":{"id":"per_1","sessionID":"ses_1","action":"bash","resources":["echo hi"],"save":["echo hi"],"source":{"type":"tool"}}}`,
			kind: provider.FramePermission,
			seq:  0,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Permission.RequestID != "per_1" || f.Permission.Action != "bash" || len(f.Permission.Resources) != 1 {
					t.Fatalf("permission = %+v", f.Permission)
				}
			},
		},
		{
			name: "question asked (bus only)",
			wire: `{"id":"evt_h","type":"question.v2.asked","location":{"directory":"/w"},"data":{"id":"que_1","sessionID":"ses_1","questions":[{"question":"color?","header":"Color","options":[{"label":"red","description":"r"},{"label":"blue","description":"b"}],"multiple":false}]}}`,
			kind: provider.FrameQuestion,
			seq:  0,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Question.RequestID != "que_1" || len(f.Question.Questions) != 1 {
					t.Fatalf("question = %+v", f.Question)
				}
				q := f.Question.Questions[0]
				if q.Header != "Color" || len(q.Options) != 2 || q.Options[0] != "red" {
					t.Fatalf("question detail = %+v", q)
				}
			},
		},
		{
			name: "session error",
			wire: `{"id":"evt_i","type":"session.error","location":{"directory":"/w"},"data":{"sessionID":"ses_1","error":{"name":"APIError","message":"boom"}}}`,
			kind: provider.FrameStatus,
			check: func(t *testing.T, f *provider.Frame) {
				if f.Status.Name != provider.StatusError || f.Status.Error != "boom" {
					t.Fatalf("status = %+v", f.Status)
				}
			},
		},
		{
			name: "irrelevant event dropped",
			wire: `{"id":"evt_j","type":"file.watcher.updated","data":{"path":"/x"}}`,
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

// TestSessionStreamReplayAndLive drives a fake SSE server that first replies
// the recorded prompt.ended payload matching after=3, then streams a live
// frame.
func TestSessionStreamReplayAndLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/ses_1/event" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("after"); got != "3" {
			t.Errorf("after = %q, want 3", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":4,\"version\":1},\"data\":{\"sessionID\":\"ses_1\",\"messageID\":\"msg_9\",\"delivery\":\"queue\"}}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: {\"id\":\"evt_2\",\"type\":\"session.next.prompted\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":5,\"version\":1},\"data\":{\"sessionID\":\"ses_1\",\"messageID\":\"msg_9\",\"delivery\":\"queue\"}}\n\n"))
		fl.Flush()
		// keep the stream open so Next must honor context cancellation
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	ctx := context.Background()
	stream, err := client.SessionStream(ctx, "ses_1", 3)
	if err != nil {
		t.Fatalf("session stream: %v", err)
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

// TestBusStreamAuthAndFrames checks the service bus stream: Basic Auth and
// unified frame delivery including transient delta frames.
func TestBusStreamAuthAndFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "opencode" || pass != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(": connected\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"evt_1\",\"type\":\"session.next.text.delta\",\"location\":{\"directory\":\"/w\"},\"data\":{\"sessionID\":\"ses_1\",\"assistantMessageID\":\"msg_2\",\"textID\":\"t_1\",\"delta\":\"po\"}}\n\n"))
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	ctx := context.Background()
	stream, err := client.BusStream(ctx)
	if err != nil {
		t.Fatalf("bus stream: %v", err)
	}
	defer stream.Close()

	f, err := stream.Next(ctx)
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
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"_tag":"NotFoundError","message":"session not found"}`))
	}))
	t.Cleanup(srv.Close)
	client := NewClient(Config{BaseURL: srv.URL, Password: "pw"})
	_, err := client.SessionStream(context.Background(), "ses_missing", 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want 404 APIError", err)
	}
}
