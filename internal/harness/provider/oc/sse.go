package oc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"bedrock/internal/harness/provider"
)

// rawEvent is one decoded SSE payload from either stream. Per-session
// durable events carry {id, type, durable, data}; service-bus events add a
// location field.
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

// SessionStream opens GET /api/session/{id}/event?after=<seq>: serve replays
// durable events after the sequence, then continues live. Deltas, permission
// and question frames only appear on the BusStream; the durable stream
// carries finished text via message_text frames.
func (c *Client) SessionStream(ctx context.Context, sessionID string, after int64) (provider.Stream, error) {
	q := url.Values{}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	return c.openStream(ctx, "/api/session/"+sessionID+"/event", q)
}

// BusStream opens the service-level event bus GET /api/event. Unlike the
// per-session durable stream it also emits transient frames (text deltas,
// permission and question requests) for every session; consumers filter by
// SessionID and use location to route by workspace.
func (c *Client) BusStream(ctx context.Context) (provider.Stream, error) {
	return c.openStream(ctx, "/api/event", nil)
}

func (c *Client) openStream(ctx context.Context, path string, query url.Values) (provider.Stream, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
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

// Wire payload shapes. Every payload scopes itself with sessionID; it moves
// onto the returned Frame so the neutral payloads stay lean.

type wireTextDelta struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Delta              string `json:"delta"`
}

type wireTextEnded struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Text               string `json:"text"`
}

type wireReasoningDelta struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	Delta              string `json:"delta"`
}

type wireToolCalled struct {
	SessionID          string          `json:"sessionID"`
	AssistantMessageID string          `json:"assistantMessageID"`
	CallID             string          `json:"callID"`
	Tool               string          `json:"tool"`
	Input              json.RawMessage `json:"input"`
}

type wireToolResult struct {
	SessionID          string          `json:"sessionID"`
	AssistantMessageID string          `json:"assistantMessageID"`
	CallID             string          `json:"callID"`
	Tool               string          `json:"tool"`
	Content            json.RawMessage `json:"content"`
	Error              struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireStatus struct {
	SessionID          string             `json:"sessionID"`
	MessageID          string             `json:"messageID"`
	AssistantMessageID string             `json:"assistantMessageID"`
	Delivery           string             `json:"delivery"`
	Finish             string             `json:"finish"`
	Agent              string             `json:"agent"`
	Model              *provider.ModelRef `json:"model"`
	Error              struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wirePermissionAsked struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
	Save      []string `json:"save"`
}

type wireQuestionAsked struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		Multiple bool `json:"multiple"`
	} `json:"questions"`
}

// toFrame converts a wire event into a unified frame, or nil for events the
// harness does not consume (file watchers, pty, tui, ...).
func toFrame(ev rawEvent) *provider.Frame {
	f := &provider.Frame{EventID: ev.ID}
	if ev.Durable != nil {
		f.Seq = ev.Durable.Seq
	}
	switch ev.Type {
	case "session.next.prompt.admitted":
		return statusFrame(f, provider.StatusPromptAdmitted, ev.Data)
	case "session.next.prompted":
		return statusFrame(f, provider.StatusPrompted, ev.Data)
	case "session.next.step.started":
		return statusFrame(f, provider.StatusStepStarted, ev.Data)
	case "session.next.step.ended":
		return statusFrame(f, provider.StatusStepEnded, ev.Data)
	case "session.next.step.failed":
		return statusFrame(f, provider.StatusStepFailed, ev.Data)
	case "session.error":
		return statusFrame(f, provider.StatusError, ev.Data)
	case "session.idle":
		return statusFrame(f, provider.StatusIdle, ev.Data)
	case "session.next.text.delta":
		w := decode[wireTextDelta](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameMessageDelta, w.SessionID
		f.MessageDelta = &provider.MessageDelta{
			AssistantMessageID: w.AssistantMessageID,
			TextID:             w.TextID,
			Delta:              w.Delta,
		}
		return f
	case "session.next.text.ended":
		w := decode[wireTextEnded](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameMessageText, w.SessionID
		f.MessageText = &provider.MessageText{
			AssistantMessageID: w.AssistantMessageID,
			TextID:             w.TextID,
			Text:               w.Text,
		}
		return f
	case "session.next.reasoning.delta":
		w := decode[wireReasoningDelta](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameReasoningDelta, w.SessionID
		f.ReasoningDelta = &provider.ReasoningDelta{
			AssistantMessageID: w.AssistantMessageID,
			Delta:              w.Delta,
		}
		return f
	case "session.next.tool.called":
		w := decode[wireToolCalled](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameToolCall, w.SessionID
		f.ToolCall = &provider.ToolCall{
			AssistantMessageID: w.AssistantMessageID,
			CallID:             w.CallID,
			Tool:               w.Tool,
			Input:              w.Input,
		}
		return f
	case "session.next.tool.success", "session.next.tool.failed":
		w := decode[wireToolResult](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameToolResult, w.SessionID
		f.ToolResult = &provider.ToolResult{
			AssistantMessageID: w.AssistantMessageID,
			CallID:             w.CallID,
			Tool:               w.Tool,
			Output:             w.Content,
			Error:              w.Error.Message,
		}
		return f
	case "permission.v2.asked":
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
	case "question.v2.asked":
		w := decode[wireQuestionAsked](ev.Data)
		if w == nil {
			return nil
		}
		f.Kind, f.SessionID = provider.FrameQuestion, w.SessionID
		qf := &provider.QuestionFrame{RequestID: w.ID}
		for _, q := range w.Questions {
			labels := make([]string, 0, len(q.Options))
			for _, o := range q.Options {
				labels = append(labels, o.Label)
			}
			qf.Questions = append(qf.Questions, provider.Question{
				Question: q.Question,
				Header:   q.Header,
				Options:  labels,
				Multiple: q.Multiple,
			})
		}
		f.Question = qf
		return f
	default:
		return nil
	}
}

// statusFrame decodes a status payload onto the frame.
func statusFrame(f *provider.Frame, name provider.StatusName, data json.RawMessage) *provider.Frame {
	f.Kind = provider.FrameStatus
	w := decode[wireStatus](data)
	if w == nil {
		return nil
	}
	f.SessionID = w.SessionID
	f.Status = &provider.StatusFrame{
		Name:               name,
		MessageID:          w.MessageID,
		AssistantMessageID: w.AssistantMessageID,
		Delivery:           provider.Delivery(w.Delivery),
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
