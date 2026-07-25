package ws

import (
	"testing"
	"time"
)

func TestBroadcastRemovesClientWithFullQueue(t *testing.T) {
	hub := NewHub()
	t.Cleanup(hub.Shutdown)

	client := &Client{Send: make(chan []byte, 1), Channel: "runs"}
	hub.Register(client)
	client.Send <- []byte("queued")

	hub.BroadcastToChannel("runs", []byte("next"))

	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.RLock()
		_, registered := hub.clients[client]
		hub.mu.RUnlock()
		if !registered {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("满队列客户端未被移除")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUnregisterReturnsAfterShutdown(t *testing.T) {
	hub := NewHub()
	hub.Shutdown()

	done := make(chan struct{})
	go func() {
		hub.Unregister(&Client{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("关闭后注销客户端发生阻塞")
	}
}
