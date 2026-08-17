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

	deadline := time.Now().Add(5 * time.Second)
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
	case <-time.After(5 * time.Second):
		t.Fatal("关闭后注销客户端发生阻塞")
	}
}

func TestSubscribeMultipleChannels(t *testing.T) {
	hub := NewHub()
	t.Cleanup(hub.Shutdown)

	client := &Client{Send: make(chan []byte, 256), Channel: "alpha"}
	hub.Register(client)
	hub.Subscribe(client, "beta")

	deadline := time.Now().Add(5 * time.Second)
	for hub.ChannelSubscriberCount("beta") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("beta 频道订阅未生效")
		}
		time.Sleep(time.Millisecond)
	}

	hub.BroadcastToChannel("beta", []byte("ping"))
	select {
	case msg := <-client.Send:
		if string(msg) != "ping" {
			t.Fatalf("got %q, want ping", string(msg))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("未收到 beta 频道消息")
	}
}

func TestChannelSubscriberCount(t *testing.T) {
	hub := NewHub()
	t.Cleanup(hub.Shutdown)

	if hub.ChannelSubscriberCount("empty") != 0 {
		t.Fatal("空频道订阅数应为 0")
	}

	client := &Client{Send: make(chan []byte, 256), Channel: "runs"}
	hub.Register(client)

	deadline := time.Now().Add(5 * time.Second)
	for hub.ChannelSubscriberCount("runs") != 1 {
		if time.Now().After(deadline) {
			t.Fatal("runs 频道订阅数未更新为 1")
		}
		time.Sleep(time.Millisecond)
	}
}
