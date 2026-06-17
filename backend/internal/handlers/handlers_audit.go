package handlers

import (
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/store"
)

func HandleAuditLogs(db *store.DB) http.HandlerFunc {
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

		c := core.ClaimsFrom(r)
		tw, twArgs := store.ScopeTenant(c, "")

		p := core.ParsePagination(r)
		if !p.Active {
			rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs WHERE 1=1`+tw+` ORDER BY created_at DESC LIMIT 200`, twArgs...)
			if err != nil {
				core.Respond(w, []any{})
				return
			}
			defer rows.Close()
			out := []LogEntry{}
			for rows.Next() {
				var e LogEntry
				if err := rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt); err != nil {
					continue
				}
				out = append(out, e)
			}
			core.Respond(w, out)
			return
		}

		var total int
		db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE 1=1`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err := db.Query(`SELECT id,actor_email,action,entity_type,entity_id,detail,created_at FROM audit_logs WHERE 1=1`+tw+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, pageArgs...)
		if err != nil {
			core.Respond(w, core.PaginatedResponse{Data: []LogEntry{}, Total: total, Limit: p.Limit, Offset: p.Offset})
			return
		}
		defer rows.Close()
		out := []LogEntry{}
		for rows.Next() {
			var e LogEntry
			if err := rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.CreatedAt); err != nil {
				continue
			}
			out = append(out, e)
		}
		core.Respond(w, core.PaginatedResponse{Data: out, Total: total, Limit: p.Limit, Offset: p.Offset})
	}
}
