package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/google/uuid"
)

const (
	lockKeyTTL = 24 * time.Hour
)

type Hub struct {
	redis        *redis.Client
	upgrader     websocket.Upgrader
	rooms        map[string]map[*Client]bool
	register     chan *Client
	unregister   chan *Client
	broadcast    chan *BroadcastMessage
	clientsMutex sync.RWMutex
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	roomID   string
	clientID string
	locked   bool
}

type BroadcastMessage struct {
	roomID  string
	message []byte
}

type WSMessage struct {
	Type string `json:"type"`
}

type LockMessage struct {
	Type   string `json:"type"`
	Holder string `json:"holder,omitempty"`
}

type RequestLockMessage struct {
	Type string `json:"type"`
}

type CodeUpdateMessage struct {
	Type string `json:"type"`
	Code string `json:"code,omitempty"`
}

type RoomStateMessage struct {
	Type   string `json:"type"`
	Holder string `json:"holder,omitempty"`
	Code   string `json:"code,omitempty"`
}

func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		redis:      redisClient,
		upgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *BroadcastMessage),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMutex.Lock()
			room := h.rooms[client.roomID]
			if room == nil {
				room = make(map[*Client]bool)
				h.rooms[client.roomID] = room
			}
			room[client] = true
			h.clientsMutex.Unlock()
			client.sendInitialState()

	case client := <-h.unregister:
		h.clientsMutex.Lock()
		if room, ok := h.rooms[client.roomID]; ok {
			if _, exists := room[client]; exists {
				delete(room, client)
				close(client.send)
			}
			if len(room) == 0 {
				delete(h.rooms, client.roomID)
			}
		}
		h.clientsMutex.Unlock()
		if client.locked {
			h.releaseLock(context.Background(), client)
		}

	case broadcast := <-h.broadcast:
		h.clientsMutex.RLock()
		room := h.rooms[broadcast.roomID]
		for client := range room {
			select {
			case client.send <- broadcast.message:
			default:
				close(client.send)
				delete(room, client)
			}
		}
		h.clientsMutex.RUnlock()
	}
	}
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, roomID string) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	clientID := newClientID()
	if candidate := r.URL.Query().Get("client_id"); isValidClientID(candidate) {
		clientID = candidate
	}

	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 16),
		roomID:   roomID,
		clientID: clientID,
	}

	client.conn.SetCloseHandler(func(code int, text string) error {
		h.unregister <- client
		return nil
	})

	h.register <- client

	go client.writePump()
	client.readPump()
}

func isValidClientID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func newClientID() string {
	id := uuid.New().String()
	return strings.ReplaceAll(id, "-", "")
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		msg := struct {
			Type string `json:"type"`
			Code string `json:"code,omitempty"`
		}{ }
		if err := c.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "request_lock":
			c.hub.handleLockRequest(context.Background(), c)
		case "code_update":
			c.hub.handleCodeUpdate(context.Background(), c, msg.Code)
		default:
			continue
		}
	}
}

func (c *Client) writePump() {
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
	c.conn.Close()
}

func (c *Client) sendInitialState() {
	lockHolder, err := c.hub.currentLockHolder(context.Background(), c.roomID)
	if err != nil {
		log.Printf("failed to read lock state: %v", err)
	}
	code, err := currentRoomCode(context.Background(), c.roomID)
	if err != nil {
		log.Printf("failed to read room code: %v", err)
	}
	message := RoomStateMessage{Type: "room_state", Holder: lockHolder, Code: code}
	c.sendJSON(message)
}

func (h *Hub) handleCodeUpdate(ctx context.Context, c *Client, code string) {
	lockHolder, err := h.currentLockHolder(ctx, c.roomID)
	if err != nil {
		log.Printf("code update failed: %v", err)
		return
	}
	if lockHolder != c.clientID {
		return
	}
	if err := updateRoomCode(ctx, c.roomID, code); err != nil {
		log.Printf("failed to save room code: %v", err)
		return
	}
	h.broadcastToRoom(c.roomID, CodeUpdateMessage{Type: "code_update", Code: code})
}

func (h *Hub) handleLockRequest(ctx context.Context, c *Client) {
	lockHolder, err := h.currentLockHolder(ctx, c.roomID)
	if err != nil {
		log.Printf("lock request failed: %v", err)
		return
	}
	if lockHolder != "" && lockHolder != c.clientID {
		log.Printf("lock takeover: %s is taking control in room %s from %s", c.clientID, c.roomID, lockHolder)
	}

	if err := h.grantLock(ctx, c); err != nil {
		log.Printf("failed to grant lock: %v", err)
		return
	}
}

func (h *Hub) currentLockHolder(ctx context.Context, roomID string) (string, error) {
	holder, err := h.redis.HGet(ctx, sessionKey(roomID), "lock_holder").Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return holder, nil
}

func (h *Hub) grantLock(ctx context.Context, c *Client) error {
	pipe := h.redis.TxPipeline()
	pipe.HSet(ctx, sessionKey(c.roomID), "lock_holder", c.clientID)
	pipe.Expire(ctx, sessionKey(c.roomID), lockKeyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	h.clientsMutex.RLock()
	if room, ok := h.rooms[c.roomID]; ok {
		for client := range room {
			client.locked = client.clientID == c.clientID
		}
	}
	h.clientsMutex.RUnlock()

	message := LockMessage{Type: "lock_granted", Holder: c.clientID}
	h.broadcastToRoom(c.roomID, message)
	return nil
}

func (h *Hub) releaseLock(ctx context.Context, c *Client) {
	current, err := h.currentLockHolder(ctx, c.roomID)
	if err != nil {
		log.Printf("release lock failed: %v", err)
		return
	}
	if current != c.clientID {
		return
	}
	if err := h.redis.HSet(ctx, sessionKey(c.roomID), "lock_holder", "").Err(); err != nil {
		log.Printf("failed to clear lock holder: %v", err)
	}
	c.locked = false
	message := LockMessage{Type: "lock_released"}
	h.broadcastToRoom(c.roomID, message)
}

func (h *Hub) broadcastToRoom(roomID string, payload interface{}) {
	message, err := json.Marshal(payload)
	if err != nil {
		log.Printf("broadcast marshal failed: %v", err)
		return
	}
	h.broadcast <- &BroadcastMessage{roomID: roomID, message: message}
}

func (c *Client) sendJSON(payload interface{}) {
	message, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case c.send <- message:
	default:
	}
}
