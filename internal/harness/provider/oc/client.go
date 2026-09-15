// Package oc implements the harness Provider interface against a local
// `opencode serve` instance (OpenAPI 3.1, Basic Auth). Wire contract verified
// against opencode v1.18.29; see testdata golden snapshot for the full spec.
package oc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bedrock/internal/harness/provider"
)

// DefaultUsername is the Basic Auth user opencode expects.
const DefaultUsername = "opencode"

// Config configures a Client.
type Config struct {
	// BaseURL points at the serve root, e.g. http://127.0.0.1:4096.
	BaseURL string
	// Password is OPENCODE_SERVER_PASSWORD of the serve process.
	Password string
	// Username defaults to DefaultUsername.
	Username string
	// HTTPClient defaults to a client with a 60s total timeout. Event
	// streams always use a dedicated client without a total timeout.
	HTTPClient *http.Client
}

// Client is the opencode REST client.
type Client struct {
	baseURL    string
	username   string
	password   string
	http       *http.Client
	streamHTTP *http.Client
}

// NewClient builds a REST client.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	username := cfg.Username
	if username == "" {
		username = DefaultUsername
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		username:   username,
		password:   cfg.Password,
		http:       httpClient,
		streamHTTP: &http.Client{},
	}
}

// APIError is a non-2xx response from serve.
type APIError struct {
	Status  int    `json:"-"`
	Tag     string `json:"_tag"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("oc api: http %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("oc api: http %d", e.Status)
}

// Health reports serve liveness.
type Health struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// Health probes GET /global/health.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var body Health
	if err := c.get(ctx, "/global/health", nil, &body); err != nil {
		return nil, err
	}
	return &body, nil
}

// Spec fetches the OpenAPI 3.1 document from GET /doc.
func (c *Client) Spec(ctx context.Context) (json.RawMessage, error) {
	raw, err := c.getRaw(ctx, "/doc")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// locationQuery encodes a directory as the bracketed location object opencode
// expects: ?location[directory]=/path (not dotted, not JSON-encoded).
func locationQuery(directory string) url.Values {
	if directory == "" {
		return url.Values{}
	}
	q := url.Values{}
	q.Set("location[directory]", directory)
	return q
}

// LocationRef names the workspace of a session.
type LocationRef struct {
	Directory string `json:"directory"`
}

// CreateSession creates a session in a workspace directory.
func (c *Client) CreateSession(ctx context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	if input.Directory == "" {
		return nil, errors.New("oc: create session: directory is required")
	}
	body := struct {
		Agent    string             `json:"agent,omitempty"`
		Model    *provider.ModelRef `json:"model,omitempty"`
		Location LocationRef        `json:"location"`
	}{
		Agent:    input.Agent,
		Model:    input.Model,
		Location: LocationRef{Directory: input.Directory},
	}
	// The response echoes the session-inventory shape: the directory is
	// nested under location, not a top-level field.
	var resp envelope[sessionV2Info]
	if err := c.post(ctx, "/api/session", body, &resp); err != nil {
		return nil, err
	}
	session := &provider.Session{
		ID:    resp.Data.ID,
		Title: resp.Data.Title,
		Agent: resp.Data.Agent,
		Model: resp.Data.Model,
	}
	if resp.Data.Location != nil {
		session.Directory = resp.Data.Location.Directory
	}
	return session, nil
}

// sessionV2Info is the session-inventory entry shape (GET /api/session and
// GET /api/session/{id}); time values are epoch milliseconds.
type sessionV2Info struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Agent    string             `json:"agent,omitempty"`
	Model    *provider.ModelRef `json:"model,omitempty"`
	Location *LocationRef       `json:"location,omitempty"`
	Time     sessionV2Times     `json:"time"`
}

type sessionV2Times struct {
	Created  float64  `json:"created"`
	Updated  float64  `json:"updated"`
	Archived *float64 `json:"archived"`
}

func epochMilli(ms float64) time.Time { return time.UnixMilli(int64(ms)).UTC() }

func sessionInfoFromV2(v sessionV2Info) provider.SessionInfo {
	info := provider.SessionInfo{
		ID:        v.ID,
		Title:     v.Title,
		Agent:     v.Agent,
		Model:     v.Model,
		CreatedAt: epochMilli(v.Time.Created),
		UpdatedAt: epochMilli(v.Time.Updated),
	}
	if v.Location != nil {
		info.Directory = v.Location.Directory
	}
	if v.Time.Archived != nil {
		info.ArchivedAt = new(epochMilli(*v.Time.Archived))
	}
	return info
}

const (
	// sessionListPageSize is the page size requested per /api/session call.
	sessionListPageSize = 100
	// sessionListMaxPages bounds the cursor walk so a misbehaving serve
	// cannot loop the listing forever.
	sessionListMaxPages = 100
)

// ListSessions lists the persisted sessions of a directory, newest first
// (the serve default order); archived entries are included.
func (c *Client) ListSessions(ctx context.Context, directory string) ([]provider.SessionInfo, error) {
	var out []provider.SessionInfo
	cursor := ""
	for page := 0; page < sessionListMaxPages; page++ {
		q := url.Values{}
		q.Set("directory", directory)
		q.Set("limit", strconv.Itoa(sessionListPageSize))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp struct {
			Data   []sessionV2Info `json:"data"`
			Cursor struct {
				Next string `json:"next"`
			} `json:"cursor"`
		}
		if err := c.get(ctx, "/api/session", q, &resp); err != nil {
			return nil, err
		}
		for _, v := range resp.Data {
			out = append(out, sessionInfoFromV2(v))
		}
		if cursor = resp.Cursor.Next; cursor == "" || len(resp.Data) == 0 {
			return out, nil
		}
	}
	return out, nil
}

// GetSession fetches one session-inventory entry by id.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*provider.SessionInfo, error) {
	var resp envelope[sessionV2Info]
	if err := c.get(ctx, "/api/session/"+sessionID, nil, &resp); err != nil {
		return nil, err
	}
	info := sessionInfoFromV2(resp.Data)
	return &info, nil
}

// sessionPatch is the PATCH /session/{id} body. The archive flag lives only
// on the unprefixed route group (not under /api) per the 1.18.29 spec
// snapshot; it is not an /experimental endpoint.
type sessionPatch struct {
	Time sessionPatchTime `json:"time"`
}

type sessionPatchTime struct {
	Archived float64 `json:"archived"`
}

// ArchiveSession marks a session archived at the current time.
func (c *Client) ArchiveSession(ctx context.Context, sessionID string) error {
	body := sessionPatch{Time: sessionPatchTime{Archived: float64(time.Now().UnixMilli())}}
	return c.patch(ctx, "/session/"+sessionID, body, nil)
}

// Prompt sends one user message. Empty delivery defaults to queue.
func (c *Client) Prompt(ctx context.Context, sessionID string, input provider.PromptInput) (*provider.PromptAck, error) {
	delivery := string(input.Delivery)
	if delivery == "" {
		delivery = string(provider.DeliveryQueue)
	}
	body := struct {
		Prompt struct {
			Text string `json:"text"`
		} `json:"prompt"`
		Delivery string `json:"delivery"`
	}{
		Prompt: struct {
			Text string `json:"text"`
		}{Text: input.Text},
		Delivery: delivery,
	}
	var resp envelope[provider.PromptAck]
	if err := c.post(ctx, "/api/session/"+sessionID+"/prompt", body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// SelectModel switches the session model via POST /api/session/{id}/model.
func (c *Client) SelectModel(ctx context.Context, sessionID string, model provider.ModelRef) error {
	body := struct {
		Model provider.ModelRef `json:"model"`
	}{Model: model}
	return c.post(ctx, "/api/session/"+sessionID+"/model", body, nil)
}

// Wait blocks until the session loop is idle (POST /api/session/{id}/wait).
// opencode 1.18.29 answers 503 "Session wait is not available yet" for every
// call; callers must fall back to event-driven idle detection.
func (c *Client) Wait(ctx context.Context, sessionID string) error {
	err := c.post(ctx, "/api/session/"+sessionID+"/wait", nil, nil)
	if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.Status == http.StatusServiceUnavailable {
		return fmt.Errorf("%w: %s", provider.ErrWaitUnavailable, apiErr.Message)
	}
	return err
}

// Interrupt cancels the running loop (POST /api/session/{id}/interrupt).
func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/api/session/"+sessionID+"/interrupt", nil, nil)
}

// ReplyPermission answers a permission request with once/always/reject and
// an optional message.
func (c *Client) ReplyPermission(ctx context.Context, sessionID, requestID string, reply provider.PermissionReply, message string) error {
	body := struct {
		Reply   string `json:"reply"`
		Message string `json:"message,omitempty"`
	}{Reply: string(reply), Message: message}
	return c.post(ctx, "/api/session/"+sessionID+"/permission/"+requestID+"/reply", body, nil)
}

// ReplyQuestion answers a question request.
func (c *Client) ReplyQuestion(ctx context.Context, sessionID, requestID string, answers provider.QuestionAnswers) error {
	return c.post(ctx, "/api/session/"+sessionID+"/question/"+requestID+"/reply", answers, nil)
}

// RejectQuestion dismisses a question request without answering.
func (c *Client) RejectQuestion(ctx context.Context, sessionID, requestID string) error {
	return c.post(ctx, "/api/session/"+sessionID+"/question/"+requestID+"/reject", nil, nil)
}

// PendingPermission lists pending permission requests of a directory.
func (c *Client) PendingPermission(ctx context.Context, directory string) ([]provider.PermissionFrame, error) {
	var resp envelope[[]provider.PermissionFrame]
	if err := c.get(ctx, "/api/permission/request", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// PendingQuestion lists pending question requests of a directory.
func (c *Client) PendingQuestion(ctx context.Context, directory string) ([]provider.QuestionFrame, error) {
	var resp envelope[[]provider.QuestionFrame]
	if err := c.get(ctx, "/api/question/request", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// message is the transcript entry shape returned by GET /api/session/{id}/message.
type message struct {
	ID      string             `json:"id"`
	Role    string             `json:"type"`
	Agent   string             `json:"agent,omitempty"`
	Model   *provider.ModelRef `json:"model,omitempty"`
	Text    string             `json:"text,omitempty"`
	Content json.RawMessage    `json:"content,omitempty"`
}

// userTextContent wraps the top-level text of a v2 user message as a single
// text part: projected user messages carry no content array, only "text".
func userTextContent(text string) json.RawMessage {
	if text == "" {
		return nil
	}
	raw, err := json.Marshal([]map[string]string{{"type": "text", "text": text}})
	if err != nil {
		return nil
	}
	return raw
}

// History returns the transcript messages, oldest first (serve answers the
// message list newest first).
func (c *Client) History(ctx context.Context, sessionID string) ([]provider.Message, error) {
	var resp envelope[[]message]
	if err := c.get(ctx, "/api/session/"+sessionID+"/message", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]provider.Message, 0, len(resp.Data))
	for i := len(resp.Data) - 1; i >= 0; i-- {
		m := resp.Data[i]
		content := m.Content
		if m.Role == "user" && len(content) == 0 {
			content = userTextContent(m.Text)
		}
		out = append(out, provider.Message{
			ID:      m.ID,
			Role:    m.Role,
			Agent:   m.Agent,
			Model:   m.Model,
			Content: content,
		})
	}
	return out, nil
}

// Export assembles the transcript as JSONL, one message per line.
func (c *Client) Export(ctx context.Context, sessionID string) ([]byte, error) {
	messages, err := c.History(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for _, m := range messages {
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("oc export: %w", err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// ListModels lists models visible in a directory (GET /api/model). The
// catalog can be briefly empty right after serve boot.
func (c *Client) ListModels(ctx context.Context, directory string) ([]provider.ModelInfo, error) {
	var resp envelope[[]provider.ModelInfo]
	if err := c.get(ctx, "/api/model", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ProviderInfo describes one model provider (GET /api/provider).
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ListProviders lists model providers visible in a directory.
func (c *Client) ListProviders(ctx context.Context, directory string) ([]ProviderInfo, error) {
	var resp envelope[[]ProviderInfo]
	if err := c.get(ctx, "/api/provider", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// agent is the agent-catalog entry shape (GET /api/agent); the identifier
// lives in "id" (e.g. build, plan, bedrock-<key>).
type agent struct {
	ID          string                `json:"id"`
	Description string                `json:"description,omitempty"`
	Mode        string                `json:"mode,omitempty"`
	Hidden      bool                  `json:"hidden"`
	Permissions []AgentPermissionRule `json:"permissions,omitempty"`
}

// AgentPermissionRule is one resolved per-tool permission rule of an agent
// definition (action=tool, resource=pattern, effect=allow|ask|deny).
type AgentPermissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// AgentCatalogEntry is one agent definition plus its resolved permission
// rules; the provider-neutral AgentInfo drops the rules.
type AgentCatalogEntry struct {
	provider.AgentInfo
	Permissions []AgentPermissionRule
}

// ListAgents lists agent definitions visible in a directory. A freshly
// queried directory may briefly return an empty or incomplete list while the
// project instance loads.
func (c *Client) ListAgents(ctx context.Context, directory string) ([]provider.AgentInfo, error) {
	entries, err := c.ListAgentCatalog(ctx, directory)
	if err != nil {
		return nil, err
	}
	out := make([]provider.AgentInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.AgentInfo)
	}
	return out, nil
}

// ListAgentCatalog lists agent definitions with their resolved permission
// rules (e.g. the compiled bedrock-* skill allow rules).
func (c *Client) ListAgentCatalog(ctx context.Context, directory string) ([]AgentCatalogEntry, error) {
	var resp envelope[[]agent]
	if err := c.get(ctx, "/api/agent", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	out := make([]AgentCatalogEntry, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, AgentCatalogEntry{
			AgentInfo: provider.AgentInfo{
				Name:        a.ID,
				Description: a.Description,
				Mode:        a.Mode,
				Native:      !strings.HasPrefix(a.ID, "bedrock-"),
				Hidden:      a.Hidden,
			},
			Permissions: a.Permissions,
		})
	}
	return out, nil
}

// skill is the skill-catalog entry shape (GET /api/skill).
type skill struct {
	Name        string `json:"name"`
	Location    string `json:"location"`
	Description string `json:"description,omitempty"`
	Slash       bool   `json:"slash,omitempty"`
}

// SkillInfo describes one registered skill in a workspace directory.
type SkillInfo struct {
	Name        string
	Location    string
	Description string
	Slash       bool
}

// ListSkills lists the skills opencode discovered in a directory (GET
// /api/skill). Like the agent catalog, a fresh directory may briefly return
// an incomplete list while the project instance loads.
func (c *Client) ListSkills(ctx context.Context, directory string) ([]SkillInfo, error) {
	var resp envelope[[]skill]
	if err := c.get(ctx, "/api/skill", locationQuery(directory), &resp); err != nil {
		return nil, err
	}
	out := make([]SkillInfo, 0, len(resp.Data))
	for _, s := range resp.Data {
		out = append(out, SkillInfo{
			Name:        s.Name,
			Location:    s.Location,
			Description: s.Description,
			Slash:       s.Slash,
		})
	}
	return out, nil
}

// envelope unwraps the {location?, data} wrapper used by directory-scoped
// list endpoints.
type envelope[T any] struct {
	Location *LocationRef `json:"location,omitempty"`
	Data     T            `json:"data"`
}

// get performs a GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.request(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("oc decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) getRaw(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, nil, nil)
}

// post performs a POST with a JSON body and decodes the JSON response into
// out (nil out ignores the body).
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.send(ctx, http.MethodPost, path, body, out)
}

// patch performs a PATCH with a JSON body (nil out ignores the response).
func (c *Client) patch(ctx context.Context, path string, body, out any) error {
	return c.send(ctx, http.MethodPatch, path, body, out)
}

// send marshals body to JSON, performs the request and decodes the JSON
// response into out (nil out ignores the body).
func (c *Client) send(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("oc encode %s: %w", path, err)
		}
	}
	raw, err := c.request(ctx, method, path, nil, payload)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("oc decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, fmt.Errorf("oc %s %s: %w", method, path, err)
	}
	req.SetBasicAuth(c.username, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oc %s %s: %w: %w", method, path, provider.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oc %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(raw, apiErr)
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("oc %s %s: %w: %w", method, path, provider.ErrNotFound, apiErr)
		}
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, fmt.Errorf("oc %s %s: %w: %w", method, path, provider.ErrUnavailable, apiErr)
		}
		return nil, apiErr
	}
	return raw, nil
}

// compile-time interface check: the adapter below implements Provider.
var _ provider.Provider = (*Adapter)(nil)

// Adapter adapts Client + the SSE layer to provider.Provider.
type Adapter struct {
	client *Client
}

// New returns the opencode Provider implementation.
func New(cfg Config) *Adapter {
	return &Adapter{client: NewClient(cfg)}
}

// Client exposes the underlying REST client for endpoints outside the
// Provider contract (health, spec, pending requests).
func (a *Adapter) Client() *Client { return a.client }

// CreateSession implements provider.Provider.
func (a *Adapter) CreateSession(ctx context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	return a.client.CreateSession(ctx, input)
}

// ListSessions implements provider.Provider.
func (a *Adapter) ListSessions(ctx context.Context, directory string) ([]provider.SessionInfo, error) {
	return a.client.ListSessions(ctx, directory)
}

// GetSession implements provider.Provider.
func (a *Adapter) GetSession(ctx context.Context, sessionID string) (*provider.SessionInfo, error) {
	return a.client.GetSession(ctx, sessionID)
}

// ArchiveSession implements provider.Provider.
func (a *Adapter) ArchiveSession(ctx context.Context, sessionID string) error {
	return a.client.ArchiveSession(ctx, sessionID)
}

// Prompt implements provider.Provider.
func (a *Adapter) Prompt(ctx context.Context, sessionID string, input provider.PromptInput) (*provider.PromptAck, error) {
	return a.client.Prompt(ctx, sessionID, input)
}

// SelectModel implements provider.Provider.
func (a *Adapter) SelectModel(ctx context.Context, sessionID string, model provider.ModelRef) error {
	return a.client.SelectModel(ctx, sessionID, model)
}

// History implements provider.Provider.
func (a *Adapter) History(ctx context.Context, sessionID string) ([]provider.Message, error) {
	return a.client.History(ctx, sessionID)
}

// EventStream implements provider.Provider: durable replay after the given
// sequence, then live unified frames.
func (a *Adapter) EventStream(ctx context.Context, sessionID string, after int64) (provider.Stream, error) {
	return a.client.SessionStream(ctx, sessionID, after)
}

// BusStream implements provider.Provider: the backend-wide live event bus
// (transient frames of every session, no replay).
func (a *Adapter) BusStream(ctx context.Context) (provider.Stream, error) {
	return a.client.BusStream(ctx)
}

// Wait implements provider.Provider.
func (a *Adapter) Wait(ctx context.Context, sessionID string) error {
	return a.client.Wait(ctx, sessionID)
}

// Interrupt implements provider.Provider.
func (a *Adapter) Interrupt(ctx context.Context, sessionID string) error {
	return a.client.Interrupt(ctx, sessionID)
}

// ReplyPermission implements provider.Provider.
func (a *Adapter) ReplyPermission(ctx context.Context, sessionID, requestID string, reply provider.PermissionReply) error {
	return a.client.ReplyPermission(ctx, sessionID, requestID, reply, "")
}

// ReplyQuestion implements provider.Provider.
func (a *Adapter) ReplyQuestion(ctx context.Context, sessionID, requestID string, answers provider.QuestionAnswers) error {
	return a.client.ReplyQuestion(ctx, sessionID, requestID, answers)
}

// RejectQuestion implements provider.Provider.
func (a *Adapter) RejectQuestion(ctx context.Context, sessionID, requestID string) error {
	return a.client.RejectQuestion(ctx, sessionID, requestID)
}

// ListModels implements provider.Provider.
func (a *Adapter) ListModels(ctx context.Context, directory string) ([]provider.ModelInfo, error) {
	return a.client.ListModels(ctx, directory)
}

// ListAgents implements provider.Provider.
func (a *Adapter) ListAgents(ctx context.Context, directory string) ([]provider.AgentInfo, error) {
	return a.client.ListAgents(ctx, directory)
}

// Export implements provider.Provider.
func (a *Adapter) Export(ctx context.Context, sessionID string) ([]byte, error) {
	return a.client.Export(ctx, sessionID)
}
