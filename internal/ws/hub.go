package ws

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn    *websocket.Conn
	Send    chan []byte
	Channel string
	UserID  uint
}

type subscription struct {
	client  *Client
	channel string
}

type Hub struct {
	clients    map[*Client]bool
	channels   map[string]map[*Client]bool
	clientSubs map[*Client]map[string]bool
	register   chan *Client
	unregister chan *Client
	subscribe  chan subscription
	quit       chan struct{}
	mu         sync.RWMutex
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		channels:   make(map[string]map[*Client]bool),
		clientSubs: make(map[*Client]map[string]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		subscribe:  make(chan subscription),
		quit:       make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	defer func() {
		if r := recover(); r != nil {
			go h.run()
		}
	}()

	for {
		select {
		case <-h.quit:
			h.mu.Lock()
			for client := range h.clients {
				close(client.Send)
				delete(h.clients, client)
			}
			h.channels = make(map[string]map[*Client]bool)
			h.clientSubs = make(map[*Client]map[string]bool)
			h.mu.Unlock()
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			if client.Channel != "" {
				h.addSubscription(client, client.Channel)
			}
			h.mu.Unlock()
		case sub := <-h.subscribe:
			h.mu.Lock()
			if _, ok := h.clients[sub.client]; ok {
				h.addSubscription(sub.client, sub.channel)
			}
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				h.removeClient(client)
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) addSubscription(client *Client, channel string) {
	if channel == "" {
		return
	}
	if _, ok := h.channels[channel]; !ok {
		h.channels[channel] = make(map[*Client]bool)
	}
	h.channels[channel][client] = true
	if _, ok := h.clientSubs[client]; !ok {
		h.clientSubs[client] = make(map[string]bool)
	}
	h.clientSubs[client][channel] = true
}

func (h *Hub) removeClient(client *Client) {
	delete(h.clients, client)
	for channel := range h.clientSubs[client] {
		if ch, ok := h.channels[channel]; ok {
			delete(ch, client)
			if len(ch) == 0 {
				delete(h.channels, channel)
			}
		}
	}
	delete(h.clientSubs, client)
	close(client.Send)
}

func (h *Hub) Shutdown() {
	close(h.quit)
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Subscribe 将已注册客户端加入额外频道（支持多频道订阅）。
func (h *Hub) Subscribe(client *Client, channel string) {
	if channel == "" {
		return
	}
	select {
	case h.subscribe <- subscription{client: client, channel: channel}:
	case <-h.quit:
	}
}

func (h *Hub) Unregister(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.quit:
	}
}

// ChannelSubscriberCount 返回频道当前订阅客户端数。
func (h *Hub) ChannelSubscriberCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel])
}

func (h *Hub) BroadcastToChannel(channel string, message []byte) {
	h.mu.RLock()
	full := sendMessage(h.channels[channel], message)
	h.mu.RUnlock()
	h.unregisterClients(full)
}

func (h *Hub) BroadcastToUser(userID uint, message []byte) {
	h.mu.RLock()
	var full []*Client
	for client := range h.clients {
		if client.UserID != userID {
			continue
		}
		select {
		case client.Send <- message:
		default:
			full = append(full, client)
		}
	}
	h.mu.RUnlock()
	h.unregisterClients(full)
}

func sendMessage(clients map[*Client]bool, message []byte) []*Client {
	if len(clients) == 0 {
		return nil
	}
	var full []*Client
	for client := range clients {
		select {
		case client.Send <- message:
		default:
			full = append(full, client)
		}
	}
	return full
}

func (h *Hub) unregisterClients(clients []*Client) {
	for _, client := range clients {
		h.Unregister(client)
	}
}

// WritePump sends messages from the Send channel to the WebSocket connection
func WritePump(client *Client, hub *Hub) {
	defer func() {
		hub.Unregister(client)
		client.Conn.Close()
	}()
	for message := range client.Send {
		if err := client.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}
