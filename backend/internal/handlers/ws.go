package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"studyhub/internal/auth"
	"studyhub/internal/core"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// wsAllowedOrigins is the static portion of the WS origin allowlist. The
// ALLOWED_ORIGIN env var (same one CORS reads in main.go) is appended at
// upgrade time so custom-domain deploys don't have to rebuild the binary.
var wsAllowedOrigins = []string{
	"https://studyhub.fit",
	"https://www.studyhub.fit",
	"http://studyhub.fit",
	"http://localhost:8080",
	"http://127.0.0.1:8080",
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		for _, a := range wsAllowedOrigins {
			if origin == a {
				return true
			}
		}
		if env := os.Getenv("ALLOWED_ORIGIN"); env != "" && origin == env {
			return true
		}
		return false
	},
}

// wsClient tracks a single WebSocket connection plus the tenant the
// authenticated user belongs to. broadcastTenant uses this so a parent in
// tenant B never sees attendance events from tenant A.
type wsClient struct {
	conn     *websocket.Conn
	tenantID int
}

type WSHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*wsClient
}

func NewHub() *WSHub { return &WSHub{clients: make(map[*websocket.Conn]*wsClient)} }

// broadcastTenant delivers the message only to clients whose JWT tenant_id
// matches tid. Superadmin connections (tenantID==0) receive everything.
func (h *WSHub) broadcastTenant(tid int, v any) {
	msg, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.tenantID == 0 || c.tenantID == tid {
			c.conn.WriteMessage(websocket.TextMessage, msg)
		}
	}
}

func (h *WSHub) HandleWS() http.HandlerFunc {
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
		claims := &core.Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return auth.JWTSecret(), nil
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
		client := &wsClient{conn: conn, tenantID: claims.TenantID}
		h.mu.Lock()
		h.clients[conn] = client
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
			if err != nil {
				break
			}
		}
	}
}
