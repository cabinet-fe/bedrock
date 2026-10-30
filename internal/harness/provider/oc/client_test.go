package oc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bedrock/internal/harness/provider"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Password: "pw"})
}

func TestBasicAuthAndLocationQuery(t *testing.T) {
	var gotUser, gotPass, gotQuery string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	if _, err := client.ListAgents(context.Background(), "/tmp/ws"); err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if gotUser != "opencode" || gotPass != "pw" {
		t.Fatalf("basic auth = %q/%q, want opencode/pw", gotUser, gotPass)
	}
	// The location param must be the bracketed object form; dotted and
	// JSON-encoded forms are rejected by serve (InvalidRequestError).
	if want := "location%5Bdirectory%5D=%2Ftmp%2Fws"; gotQuery != want {
		t.Fatalf("query = %q, want %q", gotQuery, want)
	}
}

func TestEnvelopeUnwrap(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Agent    string      `json:"agent"`
			Model    *struct{}   `json:"model"`
			Location LocationRef `json:"location"`
		}
		_ = json.Unmarshal(body, &in)
		if in.Location.Directory != "/tmp/ws" {
			t.Errorf("location.directory = %q", in.Location.Directory)
		}
		// Real wire shape: the directory is nested under location.
		_, _ = w.Write([]byte(`{"data":{"id":"ses_x","title":"t","agent":"build","location":{"directory":"/tmp/ws"},"time":{"created":1,"updated":1}}}`))
	})
	session, err := client.CreateSession(context.Background(), provider.CreateSessionInput{
		Directory: "/tmp/ws",
		Agent:     "build",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ID != "ses_x" || session.Directory != "/tmp/ws" || session.Agent != "build" {
		t.Fatalf("session = %+v", session)
	}
}

func TestCreateSessionRequiresDirectory(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler must not be called")
	})
	if _, err := client.CreateSession(context.Background(), provider.CreateSessionInput{}); err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestPromptDefaultsToQueue(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Text     string `json:"text"`
			Delivery string `json:"delivery"`
		}
		_ = json.Unmarshal(body, &in)
		if in.Delivery != "queue" {
			t.Errorf("delivery = %q, want queue", in.Delivery)
		}
		if in.Text != "hi" {
			t.Errorf("text = %q", in.Text)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"msg_1","sessionID":"ses_x","type":"user","payload":{"text":"hi"},"delivery":"queue"}}`))
	})
	ack, err := client.Prompt(context.Background(), "ses_x", provider.PromptInput{Text: "hi"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if ack.MessageID != "msg_1" {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestAPIError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"_tag":"UnauthorizedError","message":"nope"}`))
	})
	_, err := client.History(context.Background(), "ses_x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T (%v), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusUnauthorized || apiErr.Tag != "UnauthorizedError" || apiErr.Message != "nope" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestListSessionsPaginatesAndParses(t *testing.T) {
	page := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session" {
			t.Errorf("path = %q, want /api/session", r.URL.Path)
		}
		if dir := r.URL.Query().Get("directory"); dir != "/tmp/ws" {
			t.Errorf("directory = %q, want /tmp/ws", dir)
		}
		if limit := r.URL.Query().Get("limit"); limit != "100" {
			t.Errorf("limit = %q, want 100", limit)
		}
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 0:
			_, _ = w.Write([]byte(`{"data":[{"id":"ses_new","title":"t2","agent":"build","location":{"directory":"/tmp/ws"},"time":{"created":2000,"updated":2001,"archived":1990}}],"cursor":{"next":"cur-2"}}`))
		case 1:
			if r.URL.Query().Get("cursor") != "cur-2" {
				t.Errorf("cursor = %q, want cur-2", r.URL.Query().Get("cursor"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"ses_old","title":"t1","location":{"directory":"/tmp/ws"},"time":{"created":1000,"updated":1001}}],"cursor":{"next":""}}`))
		default:
			t.Errorf("unexpected page %d", page)
		}
		page++
	})
	sessions, err := client.ListSessions(context.Background(), "/tmp/ws")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	newest, oldest := sessions[0], sessions[1]
	if newest.ID != "ses_new" || newest.Title != "t2" || newest.Agent != "build" {
		t.Fatalf("newest = %+v", newest)
	}
	if newest.ArchivedAt == nil || newest.ArchivedAt.UnixMilli() != 1990 {
		t.Fatalf("newest archivedAt = %v, want 1990ms", newest.ArchivedAt)
	}
	if newest.CreatedAt.UnixMilli() != 2000 || newest.UpdatedAt.UnixMilli() != 2001 {
		t.Fatalf("newest times = %+v", newest)
	}
	if oldest.ArchivedAt != nil {
		t.Fatalf("oldest archivedAt = %v, want nil", oldest.ArchivedAt)
	}
}

func TestGetSession(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/ses_x" {
			t.Errorf("path = %q, want /api/session/ses_x", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ses_x","title":"t","agent":"bedrock-agent-7","model":{"id":"m","providerID":"p"},"location":{"directory":"/tmp/ws"},"time":{"created":1000,"updated":1001}}}`))
	})
	info, err := client.GetSession(context.Background(), "ses_x")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if info.Directory != "/tmp/ws" || info.Agent != "bedrock-agent-7" || info.Model == nil || info.Model.ProviderID != "p" {
		t.Fatalf("info = %+v", info)
	}
}

func TestArchiveSession(t *testing.T) {
	var gotMethod, gotPath string
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := client.ArchiveSession(context.Background(), "ses_x"); err != nil {
		t.Fatalf("archive session: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/api/session/ses_x" {
		t.Fatalf("path = %q, want /api/session/ses_x", gotPath)
	}
	if archived, _ := body["archived"].(bool); !archived {
		t.Fatalf("body = %v, want {\"archived\": true}", body)
	}
}

func TestWaitUnavailable(t *testing.T) {
	var gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"_tag":"ServiceUnavailableError","message":"Session wait is not available yet"}`))
	})
	err := client.Wait(context.Background(), "ses_x")
	if err == nil {
		t.Fatal("wait unexpectedly succeeded")
	}
	if !errors.Is(err, provider.ErrWaitUnavailable) {
		t.Fatalf("wait error = %v, want ErrWaitUnavailable", err)
	}
	if want := "/api/experimental/session/ses_x/wait"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
}

// TestWaitNotBoundedByDefaultClientTimeout pins the long-poll contract: the
// wait route holds the request until the session is idle, which can outlast
// the default client's 60s total timeout, so Wait must ride the dedicated
// no-timeout client (only the caller's context bounds it).
func TestWaitNotBoundedByDefaultClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Longer than the default client timeout would tolerate.
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	client := NewClient(Config{
		BaseURL: srv.URL,
		Password: "pw",
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Wait(ctx, "ses_x"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if client.longPollHTTP.Timeout != 0 {
		t.Fatalf("long-poll client timeout = %v, want 0", client.longPollHTTP.Timeout)
	}
}

func TestReplyPermissionSendsDecision(t *testing.T) {
	var gotPath string
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
	})
	err := client.ReplyPermission(context.Background(), "ses_x", "per_1", provider.PermissionAlways, "go ahead")
	if err != nil {
		t.Fatalf("reply permission: %v", err)
	}
	if want := "/api/session/ses_x/permission/per_1/reply"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if body["decision"] != "always" || body["message"] != "go ahead" {
		t.Fatalf("body = %v, want decision=always message=go ahead", body)
	}
}

func TestReplyQuestionKeysFormAnswers(t *testing.T) {
	var gotPath string
	var replyBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_x/form/frm_1":
			_, _ = w.Write([]byte(`{"data":{"id":"frm_1","sessionID":"ses_x","title":"Pick","fields":[` +
				`{"key":"color","type":"string","title":"color?","options":[{"value":"red","label":"red"}]},` +
				`{"key":"tags","type":"multiselect"},` +
				`{"key":"dry","type":"boolean"}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_x/form/frm_1/reply":
			gotPath = r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &replyBody)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	err := client.ReplyQuestion(context.Background(), "ses_x", "frm_1", provider.QuestionAnswers{
		Answers: [][]string{{"red"}, {"a", "b"}, {"yes"}},
	})
	if err != nil {
		t.Fatalf("reply question: %v", err)
	}
	if gotPath != "/api/session/ses_x/form/frm_1/reply" {
		t.Fatalf("path = %q", gotPath)
	}
	answer, _ := replyBody["answer"].(map[string]any)
	if answer["color"] != "red" {
		t.Errorf("color = %v, want red", answer["color"])
	}
	if tags, ok := answer["tags"].([]any); !ok || len(tags) != 2 || tags[0] != "a" {
		t.Errorf("tags = %v, want [a b]", answer["tags"])
	}
	if dry, ok := answer["dry"].(bool); !ok || !dry {
		t.Errorf("dry = %v, want true", answer["dry"])
	}
}

func TestRejectQuestionCancelsForm(t *testing.T) {
	var gotMethod, gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if err := client.RejectQuestion(context.Background(), "ses_x", "frm_1"); err != nil {
		t.Fatalf("reject question: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/session/ses_x/form/frm_1" {
		t.Fatalf("%s %q, want DELETE /api/session/ses_x/form/frm_1", gotMethod, gotPath)
	}
}
