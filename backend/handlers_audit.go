package main

import (
	"net/http"
)

func handleAuditLogs(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type LogEntry struct {
			ID         int    `json:"id"`
			ActorEmail string `json:"actorEmail"`
			Action     string `json:"action"`
			EntityType string `json:"entityType"`
			EntityID   string `json:"entityId"`
			Detail     string `json:"detail"`
			CreatedAt  string `json:"createdAt"`
		}

		p := parsePagination(r)
		if !p.Active {
			rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs ORDER BY created_at DESC LIMIT 200`)
			if err != nil {
				respond(w, []any{})
				return
			}
			defer rows.Close()
			out := []LogEntry{}
			for rows.Next() {
				var e LogEntry
				rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt)
				out = append(out, e)
			}
			respond(w, out)
			return
		}

		var total int
		db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total)
		rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, p.Limit, p.Offset)
		if err != nil {
			respond(w, PaginatedResponse{Data: []LogEntry{}, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}
		defer rows.Close()
		out := []LogEntry{}
		for rows.Next() {
			var e LogEntry
			rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt)
			out = append(out, e)
		}
		respond(w, PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
	}
}
