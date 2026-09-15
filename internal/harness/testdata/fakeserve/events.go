package main

// events.go carries the streaming half of the fake: the backend-wide SSE bus
// (GET /api/event) and the deterministic prompt turn. Every event shape
// mirrors internal/harness/provider/oc/sse.go — durable events carry
// {aggregateID, seq, version}, transient ones only an id; the bedrock bridge
// dedupes durable frames per session by seq and transient ones by event id.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	// busBuffer bounds one SSE subscriber's event queue; a stalled subscriber
	// drops events instead of blocking the pump.
	busBuffer = 256
	// permissionWait bounds how long the scripted permission ask blocks for
	// the REST reply before the turn fails.
	permissionWait = 60 * time.Second
	fakeTool       = "exec"
)

// durableRef positions an event inside the per-session durable log.
type durableRef struct {
	AggregateID string `json:"aggregateID"`
	Seq         int64  `json:"seq"`
	Version     int64  `json:"version"`
}

// busEvent is one SSE payload of /api/event.
type busEvent struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Durable *durableRef `json:"durable,omitempty"`
	Data    any         `json:"data"`
}

// emitDurable broadcasts one durable event of a session and returns its seq.
func (b *backend) emitDurable(sessionID, typ string, data any) int64 {
	b.mu.Lock()
	b.seq[sessionID]++
	seq := b.seq[sessionID]
	b.events++
	ev := busEvent{
		ID:      fmt.Sprintf("evt_%d", b.events),
		Type:    typ,
		Durable: &durableRef{AggregateID: sessionID, Seq: seq, Version: 1},
		Data:    data,
	}
	b.mu.Unlock()
	b.emit(ev)
	return seq
}

// emitTransient broadcasts one transient (non-replayable) event.
func (b *backend) emitTransient(typ string, data any) {
	b.mu.Lock()
	b.events++
	ev := busEvent{ID: fmt.Sprintf("evt_%d", b.events), Type: typ, Data: data}
	b.mu.Unlock()
	b.emit(ev)
}

// emit marshals and fans one event out to every bus subscriber.
func (b *backend) emit(ev busEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeserve: marshal event: %v\n", err)
		return
	}
	b.mu.Lock()
	subs := make([]chan []byte, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- payload:
		default:
			fmt.Fprintln(os.Stderr, "fakeserve: bus subscriber overflow; event dropped")
		}
	}
}

// handleBus streams GET /api/event: the live event bus every connection
// subscribes to (keepalive comments keep proxies from buffering).
func (b *backend) handleBus(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, busBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// runScript drives the deterministic turn of one prompt: a transient text
// delta, a permission ask that blocks until the REST reply lands, then a tool
// call and result, the final text and idle. A reject (or reply timeout) ends
// the turn with a session error instead.
func (b *backend) runScript(sessionID, msgID string) {
	assistant := "am_" + msgID
	textID := "t_" + msgID
	b.emitTransient("session.next.text.delta", map[string]any{
		"sessionID": sessionID, "assistantMessageID": assistant, "textID": textID,
		"delta": "smoke-delta-" + msgID,
	})

	reqID := "req_" + msgID
	ch := make(chan string, 1)
	b.mu.Lock()
	b.pending[reqID] = ch
	b.mu.Unlock()
	b.emitTransient("permission.v2.asked", map[string]any{
		"id": reqID, "sessionID": sessionID, "action": "bash",
		"resources": []string{"echo smoke"},
	})
	var reply string
	select {
	case reply = <-ch:
	case <-time.After(permissionWait):
		reply = "reject"
	}
	b.mu.Lock()
	delete(b.pending, reqID)
	b.mu.Unlock()
	if reply == "reject" {
		b.emitDurable(sessionID, "session.error", map[string]any{
			"sessionID": sessionID, "error": map[string]string{"message": "permission rejected"},
		})
		b.emitDurable(sessionID, "session.idle", map[string]any{"sessionID": sessionID})
		return
	}

	call := "call_" + msgID
	b.emitDurable(sessionID, "session.next.tool.called", map[string]any{
		"sessionID": sessionID, "assistantMessageID": assistant, "callID": call,
		"tool": fakeTool, "input": map[string]string{"command": "echo smoke"},
	})
	b.emitDurable(sessionID, "session.next.tool.success", map[string]any{
		"sessionID": sessionID, "assistantMessageID": assistant, "callID": call,
		"tool": fakeTool, "content": []map[string]string{{"type": "text", "text": "smoke"}},
	})
	text := "smoke-done-" + msgID
	b.emitDurable(sessionID, "session.next.text.ended", map[string]any{
		"sessionID": sessionID, "assistantMessageID": assistant, "textID": textID, "text": text,
	})
	b.appendHistory(sessionID, fakeMessage{
		ID: "a_" + msgID, Role: "assistant", Content: textParts(text),
	})
	b.emitDurable(sessionID, "session.idle", map[string]any{"sessionID": sessionID})
}
