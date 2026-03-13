package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowed := []string{
			"https://studyhub.fit",
			"https://www.studyhub.fit",
			"http://localhost:8080",
			"http://127.0.0.1:8080",
		}
		for _, a := range allowed {
			if origin == a {
				return true
			}
		}
		return false
	},
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
		// Authenticate before upgrading — extract JWT from cookie or Authorization header
		tokenStr := ""
		if cookie, err := r.Cookie("sh_token"); err == nil {
			tokenStr = cookie.Value
		}
		if tokenStr == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if tokenStr == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

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
