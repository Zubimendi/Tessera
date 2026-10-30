package ws

import (
	"fmt"
	"sync"
	"time"
)

type Message struct {
	Channel string         `json:"channel"`
	Payload map[string]any `json:"payload"`
	SentAt  time.Time      `json:"sent_at"`
}

type Client struct {
	ID   string
	Role string
	Send chan Message
}

func NewClient(id, role string) *Client {
	return &Client{
		ID:   id,
		Role: role,
		Send: make(chan Message, 16),
	}
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
	rooms   map[string]map[string]*Client
}

func NewHub() *Hub {
	return &Hub{
		clients: map[string]*Client{},
		rooms:   map[string]map[string]*Client{},
	}
}

func (h *Hub) RegisterClient(id, role string) *Client {
	hub := h
	hub.mu.Lock()
	defer hub.mu.Unlock()
	client := NewClient(id, role)
	hub.clients[id] = client
	return client
}

func (h *Hub) UnregisterClient(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, id)
	for room, members := range h.rooms {
		delete(members, id)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
}

func (h *Hub) JoinRoom(room string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rooms[room]; !ok {
		h.rooms[room] = map[string]*Client{}
	}
	h.rooms[room][client.ID] = client
}

func (h *Hub) LeaveRoom(room string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if members, ok := h.rooms[room]; ok {
		delete(members, client.ID)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
}

func (h *Hub) Publish(room string, payload map[string]any) {
	msg := Message{
		Channel: room,
		Payload: payload,
		SentAt:  time.Now(),
	}

	h.mu.RLock()
	members := make([]*Client, 0, len(h.rooms[room]))
	for _, client := range h.rooms[room] {
		members = append(members, client)
	}
	h.mu.RUnlock()

	for _, client := range members {
		select {
		case client.Send <- msg:
		default:
			select {
			case <-client.Send:
			default:
			}
			select {
			case client.Send <- msg:
			default:
			}
		}
	}
}

func (h *Hub) PublishToClient(clientID string, payload map[string]any) error {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}
	msg := Message{Channel: "direct", Payload: payload, SentAt: time.Now()}
	select {
	case client.Send <- msg:
		return nil
	default:
		select {
		case <-client.Send:
		default:
		}
		select {
		case client.Send <- msg:
			return nil
		default:
			return fmt.Errorf("client %s buffer full", clientID)
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
