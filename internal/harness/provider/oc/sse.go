package oc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"bedrock/internal/harness/provider"
)

// rawEvent is one decoded SSE payload from the event bus: {id, created?,
// type, location?, data, durable?}. Durable events carry {aggregateID, seq,
// version}; transient ones (e.g. permission asks) only an id.
type rawEvent struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Durable  *durableRef     `json:"durable,omitempty"`
	Location *LocationRef    `json:"location,omitempty"`
	Data     json.RawMessage `json:"data"`
}

// durableRef positions an event inside the per-session durable log.
type durableRef struct {
	AggregateID string `json:"aggregateID"`
	Seq         int64  `json:"seq"`
	Version     int64  `json:"version"`
}

// frameStream turns an SSE response body into unified frames.
type frameStream struct {
	resp    *http.Response
	pending chan *provider.Frame
	errCh   chan error
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// BusStream opens the service-level event bus GET /api/event (opencode 2.x
// has no per-session event route). The bus carries every session's frames —
// durable and transient; consumers filter by SessionID and use location to
// route by workspace.
func (c *Client) BusStream(ctx context.Context) (provider.Stream, error) {
	return c.openStream(ctx, "/api/event")
}

func (c *Client) openStream(ctx context.Context, path string) (provider.Stream, error) {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("oc stream %s: %w", path, err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oc stream %s: %w: %w", path, provider.ErrUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		apiErr := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(raw, apiErr)
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("oc stream %s: %w: %w", path, provider.ErrNotFound, apiErr)
		}
		return nil, fmt.Errorf("oc stream %s: %w: %w", path, provider.ErrUnavailable, apiErr)
	}

	s := &frameStream{
		resp:    resp,
		pending: make(chan *provider.Frame),
		errCh:   make(chan error, 1),
		done:    make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// Next blocks until a frame arrives, the stream fails, or ctx is done.
func (s *frameStream) Next(ctx context.Context) (*provider.Frame, error) {
	select {
	case f := <-s.pending:
		return f, nil
	case err := <-s.errCh:
		return nil, err
	case <-s.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close terminates the SSE connection and the reader goroutine.
func (s *frameStream) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.resp.Body.Close()
	})
	return s.closeErr
}

func (s *frameStream) readLoop() {
	defer close(s.done)
	err := scanSSE(s.resp.Body, func(data []byte) error {
		var ev rawEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			// Skip malformed keepalives instead of killing the stream.
			return nil
		}
		frame := toFrame(ev)
		if frame == nil {
			return nil
		}
		select {
		case s.pending <- frame:
			return nil
		case <-s.done:
			return io.ErrClosedPipe
		}
	})
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		select {
		case s.errCh <- err:
		default:
		}
	}
}

// scanSSE reads text/event-stream frames, invoking emit for each complete
// data event. Comment lines (": connected" style keepalives) are skipped.
func scanSSE(r io.Reader, emit func([]byte) error) error {
	br := bufio.NewReader(r)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := []byte(data.String())
		data.Reset()
		return emit(payload)
	}
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if err := flush(); err != nil {
					return err
				}
			case strings.HasPrefix(line, ":"):
				// comment / keepalive
			case strings.HasPrefix(line, "data:"):
				payload := strings.TrimPrefix(line, "data:")
				payload = strings.TrimPrefix(payload, " ")
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(payload)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return flush()
			}
			return err
		}
	}
}

// Wire payload shapes (opencode 2.x). Every payload scopes itself with
// sessionID; it moves onto the returned Frame so the neutral payloads stay
// lean. Text/reasoning parts are identified by their ordinal within the
// assistant message (v1 used a textID string); the neutral TextID carries
// the stringified ordinal so downstream part keying keeps working.

type wirePartDelta struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	Ordinal            int    `json:"ordinal"`
	Delta              string `json:"delta"`
}

type wirePartEnded struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	Ordinal            int    `json:"ordinal"`
	Text               string `json:"text"`
}

type wireToolCalled struct {
	SessionID          string          `json:"sessionID"`
	AssistantMessageID string          `json:"assistantMessageID"`
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Input              json.RawMessage `json:"input"`
}

type wireToolResult struct {
	SessionID          string          `json:"sessionID"`
	AssistantMessageID string          `json:"assistantMessageID"`
	ID                 string          `json:"id"`
	Content            json.RawMessage `json:"content"`
	Error              struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireStatus struct {
	SessionID          string             `json:"sessionID"`
	MessageID          string             `json:"messageID"`
	InboxID            string             `json:"inboxID"`
	AssistantMessageID string             `json:"assistantMessageID"`
	Delivery           string             `json:"delivery"`
	Finish             string             `json:"finish"`
	Agent              string             `json:"agent"`
	Model              *provider.ModelRef `json:"model"`
	Error              struct {
		Message string `json:"message"`
	} `json:"error"`
	Item struct {
		Delivery string `json:"delivery"`
	} `json:"item"`
}

type wirePermissionAsked struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
	Save      []string `json:"save"`
}

// wireFormCreated is the form.created payload: a Form.Info (opencode 2.x
// replaced v1 questions with the form system).
type wireFormCreated struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionID"`
	Title     string          `json:"title"`
	Fields    []wireFormField `json:"fields"`
}

type wireFormField struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Options     []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
}

// toFrame converts a wire event into a unified frame, or nil for events the
// harness does not consume (registry updates, file watchers, pty, tui, ...).
func toFrame(ev rawEvent) *provider.Frame {
	f := &provider.Frame{EventID: ev.ID}
	if ev.Durable != nil {
		f.Seq = ev.Durable.Seq
	}
	switch ev.Type {
	case "session.inbox.enqueued":
		return statusFrame(f, provider.StatusPromptAdmitted, ev.Data)
	case "session.inbox.delivered":
		return statusFrame(f, provider.StatusPrompted, ev.Data)
	case "session.step.started":
		return statusFrame(f, provider.StatusStepStarted, ev.Data)
	case "session.step.ended":
		return statusFrame(f, provider.StatusStepEnded, ev.Data)
	case "session.step.failed":
		return statusFrame(f, provider.StatusStepFailed, ev.Data)
	case "session.execution.failed":
		return statusFrame(f, provider.StatusError, ev.Data)
	case "session.execution.succeeded", "session.execution.interrupted", "session.idle":
		return statusFrame(f, provider.StatusIdle, ev.Data)
	case "session.text.delta":
		w := decode[wirePartDelta](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameMessageDelta, w.SessionID
		f.MessageDelta = &provider.MessageDelta{
			AssistantMessageID: w.AssistantMessageID,
			TextID:             strconv.Itoa(w.Ordinal),
			Delta:              w.Delta,
		}
		return f
	case "session.text.ended":
		w := decode[wirePartEnded](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameMessageText, w.SessionID
		f.MessageText = &provider.MessageText{
			AssistantMessageID: w.AssistantMessageID,
			TextID:             strconv.Itoa(w.Ordinal),
			Text:               w.Text,
		}
		return f
	case "session.reasoning.delta":
		w := decode[wirePartDelta](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameReasoningDelta, w.SessionID
		f.ReasoningDelta = &provider.ReasoningDelta{
			AssistantMessageID: w.AssistantMessageID,
			Delta:              w.Delta,
		}
		return f
	case "session.tool.called":
		w := decode[wireToolCalled](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameToolCall, w.SessionID
		f.ToolCall = &provider.ToolCall{
			AssistantMessageID: w.AssistantMessageID,
			CallID:             w.ID,
			Tool:               w.Name,
			Input:              w.Input,
		}
		return f
	case "session.tool.success", "session.tool.failed":
		w := decode[wireToolResult](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameToolResult, w.SessionID
		f.ToolResult = &provider.ToolResult{
			AssistantMessageID: w.AssistantMessageID,
			CallID:             w.ID,
			Output:             w.Content,
			Error:              w.Error.Message,
		}
		return f
	case "permission.asked":
		w := decode[wirePermissionAsked](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FramePermission, w.SessionID
		f.Permission = &provider.PermissionFrame{
			RequestID: w.ID,
			Action:    w.Action,
			Resources: w.Resources,
			Save:      w.Save,
		}
		return f
	case "form.created":
		w := decode[wireFormCreated](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameQuestion, w.SessionID
		qf := &provider.QuestionFrame{RequestID: w.ID}
		for _, field := range w.Fields {
			labels := make([]string, 0, len(field.Options))
			for _, o := range field.Options {
				labels = append(labels, o.Label)
			}
			question := field.Title
			if question == "" {
				question = field.Description
			}
			if question == "" {
				question = field.Key
			}
			qf.Questions = append(qf.Questions, provider.Question{
				Question: question,
				Key:      field.Key,
				Options:  labels,
				Multiple: field.Type == "multiselect",
			})
		}
		f.Question = qf
		return f
	default:
		return nil
	}
}

// statusFrame decodes a status payload onto the frame. Prompt transitions
// carry the user message as inboxID; step transitions identify the assistant
// message. Delivery rides the enqueued inbox item.
func statusFrame(f *provider.Frame, name provider.StatusName, data json.RawMessage) *provider.Frame {
	f.Kind = provider.FrameStatus
	w := decode[wireStatus](data)
	if w == nil {
		return nil
	}
	f.SessionID = w.SessionID
	messageID := w.MessageID
	if messageID == "" {
		messageID = w.InboxID
	}
	delivery := w.Delivery
	if delivery == "" {
		delivery = w.Item.Delivery
	}
	f.Status = &provider.StatusFrame{
		Name:               name,
		MessageID:          messageID,
		AssistantMessageID: w.AssistantMessageID,
		Delivery:           provider.Delivery(delivery),
		Finish:             w.Finish,
		Agent:              w.Agent,
		Model:              w.Model,
		Error:              w.Error.Message,
	}
	return f
}

// decode unmarshals data into *T, returning nil on failure.
func decode[T any](data json.RawMessage) *T {
	if len(data) == 0 {
		return nil
	}
	out := new(T)
	if err := json.Unmarshal(data, out); err != nil {
		return nil
	}
	return out
}
