package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WSHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func newHub() *WSHub { return &WSHub{clients: make(map[*websocket.Conn]bool)} }

func (h *WSHub) broadcast(v any) {
	msg, err := json.Marshal(v)
	if err != nil { return }
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (h *WSHub) handleWS() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("ws upgrade:", err)
			return
		}
		h.mu.Lock()
		h.clients[conn] = true
		h.mu.Unlock()

		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
		}()

		// Keep alive — read until disconnect
		for {
			_, _, err := conn.ReadMessage()
			if err != nil { break }
		}
	}
}
