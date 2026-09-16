package main

// backend.go implements the in-memory subset of the opencode serve REST
// surface bedrock consumes: session inventory, prompting (which runs the
// scripted event turn in events.go), message history and the model/agent
// catalogs. Wire shapes mirror internal/harness/provider/oc (opencode
// 1.18.29): {location?, data} envelopes, epoch-millisecond times and the
// unprefixed PATCH /session/{id} archive route.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Fixed catalog entries the smoke suite asserts on.
const (
	fakeProviderID = "bedrock-p-smoke"
	fakeModelID    = "smoke-model"
	fakeBuildAgent = "build"
)

// ocUsername is the Basic Auth user opencode serve expects.
const ocUsername = "opencode"

type modelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

type locationRef struct {
	Directory string `json:"directory"`
}

// fakeSession is the session-inventory entry shape (GET/POST /api/session).
type fakeSession struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Agent    string         `json:"agent,omitempty"`
	Model    *modelRef      `json:"model,omitempty"`
	Location *locationRef   `json:"location,omitempty"`
	Time     fakeSessionTms `json:"time"`
}

type fakeSessionTms struct {
	Created  float64  `json:"created"`
	Updated  float64  `json:"updated"`
	Archived *float64 `json:"archived"`
}

// fakeMessage is the transcript entry shape (GET /api/session/{id}/message).
type fakeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"type"`
	Agent   string          `json:"agent,omitempty"`
	Model   *modelRef       `json:"model,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

// backend is the scripted fake serve state. The mutex guards every field.
type backend struct {
	password string

	mu       sync.Mutex
	sessions []*fakeSession // creation order
	nextSes  int
	nextMsg  int
	history  map[string][]fakeMessage
	pending  map[string]chan string // permission request id -> reply
	subs     map[chan []byte]struct{}
	seq      map[string]int64 // per-session durable event sequence
	events   int64            // transient event id counter
}

func newBackend(password string) *backend {
	return &backend{
		password: password,
		history:  map[string][]fakeMessage{},
		pending:  map[string]chan string{},
		subs:     map[chan []byte]struct{}{},
		seq:      map[string]int64{},
	}
}

// register mounts the opencode surface on the mux.
func (b *backend) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/session", b.auth(b.handleCreateSession))
	mux.HandleFunc("GET /api/session", b.auth(b.handleListSessions))
	mux.HandleFunc("GET /api/session/{id}", b.auth(b.handleGetSession))
	mux.HandleFunc("GET /api/session/{id}/message", b.auth(b.handleMessages))
	mux.HandleFunc("POST /api/session/{id}/prompt", b.auth(b.handlePrompt))
	mux.HandleFunc("POST /api/session/{id}/interrupt", b.auth(b.handleInterrupt))
	mux.HandleFunc("POST /api/session/{id}/wait", b.auth(b.handleWait))
	mux.HandleFunc("POST /api/session/{id}/permission/{reqId}/reply", b.auth(b.handlePermissionReply))
	mux.HandleFunc("GET /api/model", b.auth(b.handleModelCatalog))
	mux.HandleFunc("GET /api/agent", b.auth(b.handleAgentCatalog))
	mux.HandleFunc("GET /api/event", b.auth(b.handleBus))
}

// auth enforces the Basic Auth opencode serve expects.
func (b *backend) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != ocUsername || pass != b.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="opencode"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (b *backend) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"healthy":true,"version":"fake-1.0.0"}`))
}

func (b *backend) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent    string       `json:"agent"`
		Model    *modelRef    `json:"model"`
		Location *locationRef `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Location == nil || body.Location.Directory == "" {
		writeError(w, http.StatusBadRequest, "location.directory is required")
		return
	}
	b.mu.Lock()
	b.nextSes++
	now := float64(time.Now().UnixMilli())
	sess := &fakeSession{
		ID:       fmt.Sprintf("ses_%d", b.nextSes),
		Title:    "Smoke Session",
		Agent:    body.Agent,
		Model:    body.Model,
		Location: body.Location,
	}
	sess.Time.Created = now
	sess.Time.Updated = now
	b.sessions = append(b.sessions, sess)
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": sess})
}

func (b *backend) handleListSessions(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("directory")
	b.mu.Lock()
	out := make([]*fakeSession, 0, len(b.sessions))
	for i := len(b.sessions) - 1; i >= 0; i-- { // serve order: newest first
		if s := b.sessions[i]; dir == "" || s.Location.Directory == dir {
			out = append(out, s)
		}
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "cursor": map[string]string{"next": ""}})
}

func (b *backend) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if sess := b.lookupSession(r.PathValue("id")); sess != nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": sess})
		return
	}
	writeError(w, http.StatusNotFound, "session not found")
}

func (b *backend) handleMessages(w http.ResponseWriter, r *http.Request) {
	sess := b.lookupSession(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	b.mu.Lock()
	msgs := b.history[sess.ID]
	out := make([]fakeMessage, len(msgs))
	for i, m := range msgs {
		out[len(msgs)-1-i] = m // serve answers newest first
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (b *backend) handlePrompt(w http.ResponseWriter, r *http.Request) {
	sess := b.lookupSession(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var body struct {
		Prompt struct {
			Text string `json:"text"`
		} `json:"prompt"`
		Delivery string `json:"delivery"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Prompt.Text == "" {
		writeError(w, http.StatusBadRequest, "prompt.text is required")
		return
	}
	delivery := body.Delivery
	if delivery == "" {
		delivery = "queue"
	}
	b.mu.Lock()
	b.nextMsg++
	n := b.nextMsg
	msgID := fmt.Sprintf("msg_%d", n)
	b.history[sess.ID] = append(b.history[sess.ID],
		fakeMessage{ID: msgID, Role: "user", Content: textParts(body.Prompt.Text)})
	sess.Time.Updated = float64(time.Now().UnixMilli())
	b.mu.Unlock()

	// Admission is synchronous (the ack carries the admitted durable seq);
	// the scripted turn continues on its own goroutine.
	admitted := b.emitDurable(sess.ID, "session.next.prompt.admitted", map[string]any{
		"sessionID": sess.ID, "messageID": msgID, "delivery": delivery,
	})
	b.emitDurable(sess.ID, "session.next.prompted", map[string]any{
		"sessionID": sess.ID, "messageID": msgID,
	})
	go b.runScript(sess.ID, msgID)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": msgID, "admittedSeq": admitted}})
}

func (b *backend) handleInterrupt(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

// handleWait mimics opencode 1.18.29, which answers 503 for every wait call.
func (b *backend) handleWait(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusServiceUnavailable, "Session wait is not available yet")
}

func (b *backend) handlePermissionReply(w http.ResponseWriter, r *http.Request) {
	if b.lookupSession(r.PathValue("id")) == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var body struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Reply == "" {
		writeError(w, http.StatusBadRequest, "reply is required")
		return
	}
	b.mu.Lock()
	ch, ok := b.pending[r.PathValue("reqId")]
	b.mu.Unlock()
	if !ok {
		writeError(w, http.StatusConflict, "permission request is not pending")
		return
	}
	select {
	case ch <- body.Reply:
	default:
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

func (b *backend) handleModelCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, locationEnvelope(r, []map[string]any{
		{"id": fakeModelID, "providerID": fakeProviderID, "name": "Smoke Model", "family": "smoke"},
	}))
}

func (b *backend) handleAgentCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, locationEnvelope(r, []map[string]any{
		{"id": fakeBuildAgent, "description": "Smoke build agent", "mode": "primary", "hidden": false},
	}))
}

func (b *backend) lookupSession(id string) *fakeSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (b *backend) appendHistory(sessionID string, m fakeMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history[sessionID] = append(b.history[sessionID], m)
}

// locationEnvelope wraps directory-scoped catalog data with the echoed
// bracketed location query (?location[directory]=/path).
func locationEnvelope(r *http.Request, data any) map[string]any {
	if dir := r.URL.Query().Get("location[directory]"); dir != "" {
		return map[string]any{"location": locationRef{Directory: dir}, "data": data}
	}
	return map[string]any{"data": data}
}

// textParts builds one text message part.
func textParts(text string) json.RawMessage {
	raw, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
	return raw
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}
