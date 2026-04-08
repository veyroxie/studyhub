package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ── User management (admin) ───────────────────────────────────────────────────

type userCreateReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

func handleUsers(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c := claimsFrom(r)
			tid := tenantID(c)
			rows, _ := db.Query(`SELECT id,email,role,name FROM users WHERE (tenant_id=? OR ?=0) ORDER BY role,name`, tid, tid)
			defer rows.Close()
			type userRow struct {
				ID    int    `json:"id"`
				Email string `json:"email"`
				Role  string `json:"role"`
				Name  string `json:"name"`
			}
			out := []userRow{}
			for rows.Next() {
				var u userRow
				rows.Scan(&u.ID, &u.Email, &u.Role, &u.Name)
				out = append(out, u)
			}
			respond(w, out)
		case http.MethodPost:
			var req userCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			req.Email = strings.ToLower(strings.TrimSpace(req.Email))
			if !validateEmail(req.Email) {
				respondError(w, "invalid email format", http.StatusBadRequest)
				return
			}
			if ok, msg := validatePassword(req.Password); !ok {
				respondError(w, msg, http.StatusBadRequest)
				return
			}
			if req.Role == "" {
				req.Role = "parent"
			}
			hash, err := hashPassword(req.Password)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			c := claimsFrom(r)
			tid := tenantID(c)
			_, err = db.Exec(`INSERT INTO users(tenant_id,email,password_hash,role,name) VALUES(?,?,?,?,?)`, tid, req.Email, hash, req.Role, req.Name)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
					respondError(w, "email already exists", 409)
					return
				}
				respondError(w, "server error", 500)
				return
			}
			w.WriteHeader(http.StatusCreated)
			respond(w, map[string]string{"email": req.Email, "role": req.Role})
		}
	}
}

func handleUserDelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		tid := tenantID(claimsFrom(r))
		if _, err := db.Exec(`DELETE FROM users WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
			respondError(w, "could not delete user", 500)
			return
		}
		if c := claimsFrom(r); c != nil {
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "user_deleted", "user", id, "hard deleted")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
