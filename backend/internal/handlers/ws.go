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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// wsAllowedOrigins is the static portion of the WS origin allowlist. The
// ALLOWED_ORIGIN env var (same one CORS reads in main.go) is appended at
// upgrade time so custom-domain deploys don't have to rebuild the binary.
var wsAllowedOrigins = []string{
	"https://studyhub.fit",
	"https://www.studyhub.fit",
	// http://studyhub.fit intentionally omitted — production is HTTPS-only.
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

// wsClient tracks a single WebSocket connection plus the tenant/role/email of
// the authenticated user. tenantID scopes broadcasts across tenants; role +
// email let check-in events reach only staff and the affected child's parent.
type wsClient struct {
	conn     *websocket.Conn
	tenantID int
	role     string
	email    string
	writeMu  sync.Mutex // gorilla panics on concurrent writes to one conn
}

// send writes one message to this client under its own lock and a write
// deadline, so a stalled peer can't block the whole broadcast. Returns the
// write error (caller may drop the client).
func (c *wsClient) send(msg []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}

const wsWriteTimeout = 10 * time.Second

type WSHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*wsClient
}

func NewHub() *WSHub { return &WSHub{clients: make(map[*websocket.Conn]*wsClient)} }

// broadcastTenant delivers the message to every client whose JWT tenant_id
// matches tid. Superadmin connections (tenantID==0) receive everything. Use
// for tenant-wide events; for per-student check-ins use broadcastCheckIn.
func (h *WSHub) broadcastTenant(tid int, v any) {
	h.deliver(tid, v, func(*wsClient) bool { return true })
}

// broadcastCheckIn delivers a student check-in/out event only to staff
// (admin/teacher/superadmin) in the tenant and to the parent whose child it
// is. Without this, every connected parent in the tenant received real-time
// attendance timing for every other family's children.
func (h *WSHub) broadcastCheckIn(tid int, ownerEmail string, v any) {
	owner := strings.ToLower(strings.TrimSpace(ownerEmail))
	h.deliver(tid, v, func(c *wsClient) bool {
		if c.role != "parent" {
			return true // admin / teacher / superadmin see all check-ins
		}
		return owner != "" && strings.ToLower(c.email) == owner
	})
}

// deliver marshals v once and sends it to each tenant-matched client for which
// keep returns true, dropping clients whose write fails.
func (h *WSHub) deliver(tid int, v any, keep func(*wsClient) bool) {
	msg, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*wsClient, 0, len(h.clients))
	for _, c := range h.clients {
		if c.tenantID != 0 && c.tenantID != tid {
			continue
		}
		if keep(c) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	// Write outside the hub lock so a slow client can't stall other senders.
	for _, c := range targets {
		if err := c.send(msg); err != nil {
			core.Logger.Debug("ws send failed, dropping client", "err", err)
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
		client := &wsClient{conn: conn, tenantID: claims.TenantID, role: claims.Role, email: claims.Email}
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
