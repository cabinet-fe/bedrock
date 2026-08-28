package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallEncodesEnvelopeAndDecodesValue(t *testing.T) {
	var got struct {
		Type    string          `json:"type"`
		RPCID   uint64          `json:"rpcId"`
		Method  string          `json:"method"`
		Payload json.RawMessage `json:"payload"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path != "/api/host.describe" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w, `{"type":"server-response","rpcId":1,"result":{"ok":true,"value":{"version":"0.1"}}}`)
	}))
	t.Cleanup(srv.Close)

	client := NewRpcClient(srv.URL, srv.Client())
	value, err := client.Call(context.Background(), MethodHostDescribe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "client-request" || got.RPCID != 1 || got.Method != MethodHostDescribe {
		t.Fatalf("envelope: %+v", got)
	}
	if string(got.Payload) != "{}" {
		t.Fatalf("payload %s", got.Payload)
	}
	m, ok := value.(map[string]any)
	if !ok || m["version"] != "0.1" {
		t.Fatalf("value %#v", value)
	}

	var out struct {
		Version string `json:"version"`
	}
	if err := client.CallRaw(context.Background(), MethodHostDescribe, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != "0.1" {
		t.Fatalf("CallRaw %+v", out)
	}
	if got.RPCID != 2 {
		t.Fatalf("rpcId want 2 got %d", got.RPCID)
	}
}

func TestCallReturnsRPCErrorWithoutDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"type":"server-response","rpcId":1,
			"result":{"ok":false,"error":{"code":"bad-request","message":"cwd required","details":{"stack":"secret"}}}
		}`)
	}))
	t.Cleanup(srv.Close)

	_, err := NewRpcClient(srv.URL, srv.Client()).Call(context.Background(), MethodSessionCreate, SessionCreatePayload{Cwd: "/tmp"})
	rpc, ok := errors.AsType[*RPCError](err)
	if !ok {
		t.Fatalf("got %T %v", err, err)
	}
	if rpc.Code != "bad-request" || rpc.Message != "cwd required" {
		t.Fatalf("rpc %+v", rpc)
	}
	if strings.Contains(rpc.Error(), "secret") {
		t.Fatal("details leaked")
	}
}

func TestCallRawSessionCancelValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session.cancel" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"type":"server-response","rpcId":1,"result":{"ok":true,"value":{"accepted":true}}}`)
	}))
	t.Cleanup(srv.Close)

	var out SessionCancelValue
	err := NewRpcClient(srv.URL, srv.Client()).CallRaw(
		context.Background(), MethodSessionCancel, SessionCancelPayload{SessionID: "s1"}, &out,
	)
	if err != nil || !out.Accepted {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestRespondEncodesClientResponse(t *testing.T) {
	var got clientResponse
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/respond" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = io.WriteString(w, `{"accepted":true}`)
	}))
	t.Cleanup(srv.Close)

	accepted, reason, err := NewRpcClient(srv.URL, srv.Client()).Respond(
		context.Background(), 9, map[string]any{"ok": true, "value": "allowed-once"},
	)
	if err != nil || !accepted || reason != "" {
		t.Fatalf("accepted=%v reason=%q err=%v", accepted, reason, err)
	}
	if got.Type != "client-response" || got.RPCID != 9 {
		t.Fatalf("envelope %+v", got)
	}
}

func TestCallConnectionFailureAndTimeout(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		client := NewRpcClient("http://127.0.0.1:1", &http.Client{Timeout: 200 * time.Millisecond})
		_, err := client.Call(context.Background(), MethodHostDescribe, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		mapped := MapError(err)
		if mapped.Status != http.StatusServiceUnavailable || mapped.Message != MsgUnavailable {
			t.Fatalf("mapped %+v", mapped)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
		}))
		t.Cleanup(srv.Close)
		client := NewRpcClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond})
		_, err := client.Call(context.Background(), MethodHostDescribe, nil)
		if err == nil {
			t.Fatal("expected timeout")
		}
		mapped := MapError(err)
		if mapped.Status != http.StatusServiceUnavailable || mapped.Message != MsgUnavailable {
			t.Fatalf("mapped %+v", mapped)
		}
	})
}

func TestMapError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want MappedError
	}{
		{
			name: "session-not-found",
			err:  &RPCError{Code: "session-not-found", Message: "gone"},
			want: MappedError{Status: 404, Message: MsgSessionNotFound},
		},
		{
			name: "session-conflict",
			err:  &RPCError{Code: "session-conflict", Message: "exists"},
			want: MappedError{Status: 409, Message: MsgSessionConflict},
		},
		{
			name: "model-unavailable",
			err:  &RPCError{Code: "model-unavailable", Message: "down"},
			want: MappedError{Status: 503, Message: MsgModelUnavailable},
		},
		{
			name: "cancelled",
			err:  &RPCError{Code: "cancelled", Message: "stopped"},
			want: MappedError{Status: 200, Cancelled: true},
		},
		{
			name: "bad-request keeps message drops details",
			err:  &RPCError{Code: "bad-request", Message: "cwd required"},
			want: MappedError{Status: 400, Message: "cwd required"},
		},
		{
			name: "dial",
			err:  errors.New("dial tcp 127.0.0.1:17800: connect: connection refused"),
			want: MappedError{Status: 503, Message: MsgUnavailable},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapError(tt.err)
			if got != tt.want {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestParseMuxStream(t *testing.T) {
	input := ": connected\n\n" +
		"data: {\"rpcId\":1,\"payload\":{\"type\":\"session/event\"}}\n\n" +
		"data: {\"rpcId\":2,\"payload\":{\"type\":\"ping\"}}\n\n"
	var frames []MuxFrame
	if err := ParseMuxStream(strings.NewReader(input), func(f MuxFrame) error {
		frames = append(frames, f)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].RPCID != 1 || frames[1].RPCID != 2 {
		t.Fatalf("frames %+v", frames)
	}
	if string(frames[0].Payload) != `{"type":"session/event"}` {
		t.Fatalf("payload %s", frames[0].Payload)
	}
}

func TestParseMuxStreamViaHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events.mux" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": connected\n\ndata: {\"rpcId\":7,\"payload\":{\"ok\":true}}\n\n")
	}))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/api/events.mux")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	var frames []MuxFrame
	if err := ParseMuxStream(resp.Body, func(f MuxFrame) error {
		frames = append(frames, f)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].RPCID != 7 {
		t.Fatalf("frames %+v", frames)
	}
}

func TestRPCMethodsAreSection43Subset(t *testing.T) {
	want := []string{
		"host.describe",
		"session.create",
		"session.list",
		"session.history",
		"session.prompt",
		"session.cancel",
		"session.models",
		"session.selectModel",
		"agentPresets.list",
		"agentPresets.select",
		"llm.providers",
		"llm.models",
	}
	if len(rpcMethods) != len(want) {
		t.Fatalf("len %d want %d", len(rpcMethods), len(want))
	}
	forbidden := []string{"goals.", "workspace.", "subagent.", "credentials.", "settings."}
	for i, m := range rpcMethods {
		if m != want[i] {
			t.Fatalf("rpcMethods[%d]=%s want %s", i, m, want[i])
		}
		for _, prefix := range forbidden {
			if strings.HasPrefix(m, prefix) {
				t.Fatalf("forbidden method %s", m)
			}
		}
	}
}

func TestPayloadJSONTags(t *testing.T) {
	raw, err := json.Marshal(SessionCreatePayload{
		Cwd:         "/ws/1/abcd1234",
		SessionID:   "s1",
		AgentPreset: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sessionId":"s1"`) || !strings.Contains(string(raw), `"agentPreset":"default"`) {
		t.Fatalf("create payload %s", raw)
	}
	before := int64(10)
	max := 50
	raw, err = json.Marshal(SessionPromptPayload{
		SessionID: "s1",
		Mode:      PromptModeQueue,
		Content:   []PromptContent{{Type: "text", Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode":"queue"`) {
		t.Fatalf("prompt %s", raw)
	}
	raw, err = json.Marshal(SessionPromptPayload{SessionID: "s1", Mode: PromptModeSteer, Content: nil})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode":"steer"`) {
		t.Fatalf("steer %s", raw)
	}
	raw, err = json.Marshal(SessionHistoryPayload{SessionID: "s1", BeforeSeq: &before, MaxMessages: &max})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"beforeSeq":10`) || !strings.Contains(string(raw), `"maxMessages":50`) {
		t.Fatalf("history %s", raw)
	}
	raw, err = json.Marshal(SessionCancelPayload{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"sessionId":"s1"}` {
		t.Fatalf("cancel %s", raw)
	}
}
