package oc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
		_, _ = w.Write([]byte(`{"data":{"id":"ses_x","directory":"/tmp/ws","title":"t"}}`))
	})
	session, err := client.CreateSession(context.Background(), provider.CreateSessionInput{
		Directory: "/tmp/ws",
		Agent:     "build",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ID != "ses_x" || session.Directory != "/tmp/ws" {
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
			Prompt struct {
				Text string `json:"text"`
			} `json:"prompt"`
			Delivery string `json:"delivery"`
		}
		_ = json.Unmarshal(body, &in)
		if in.Delivery != "queue" {
			t.Errorf("delivery = %q, want queue", in.Delivery)
		}
		if in.Prompt.Text != "hi" {
			t.Errorf("text = %q", in.Prompt.Text)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"msg_1","admittedSeq":3}}`))
	})
	ack, err := client.Prompt(context.Background(), "ses_x", provider.PromptInput{Text: "hi"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if ack.MessageID != "msg_1" || ack.AdmittedSeq != 3 {
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

func TestWaitUnavailable(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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
}
