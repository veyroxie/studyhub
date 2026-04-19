package main

import (
	"encoding/json"
	"net/http"
)

// ── Cancelled Classes ─────────────────────────────────────────────────────────

func listCancelledClasses(db *DB, c *Claims) []CancelledClass {
	tid := tenantID(c)
	rows, err := db.Query(`SELECT id,class_id,date,reason,cancelled_by,created_on FROM cancelled_classes WHERE (tenant_id=? OR ?=0) ORDER BY date DESC`, tid, tid)
	if err != nil {
		return []CancelledClass{}
	}
	defer rows.Close()
	out := []CancelledClass{}
	for rows.Next() {
		var cc CancelledClass
		if err := rows.Scan(&cc.ID, &cc.ClassID, &cc.Date, &cc.Reason, &cc.CancelledBy, &cc.CreatedOn); err != nil {
			continue
		}
		out = append(out, cc)
	}
	return out
}

func handleListCancelledClasses(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		respond(w, listCancelledClasses(db, c))
	}
}

func handleCreateCancelledClass(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || (c.Role != "admin" && c.Role != "superadmin") {
			respondError(w, "admin only", 403)
			return
		}
		var cc CancelledClass
		if err := json.NewDecoder(r.Body).Decode(&cc); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		if msg := validationError("classId", cc.ClassID, "date", cc.Date); msg != "" {
			respondError(w, msg, 400)
			return
		}
		if cc.ID == "" {
			cc.ID = generateID("CC")
		}
		if cc.CreatedOn == "" {
			cc.CreatedOn = today()
		}
		if cc.CancelledBy == "" && c != nil {
			cc.CancelledBy = c.Email
		}
		tid := tenantID(c)
		_, err := db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,reason,cancelled_by,created_on) VALUES(?,?,?,?,?,?,?)`,
			cc.ID, tid, cc.ClassID, cc.Date, cc.Reason, cc.CancelledBy, cc.CreatedOn)
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		if c != nil {
			logAudit(db, c.Email, "class_cancelled", "cancelled_class", cc.ID, "class="+cc.ClassID+" date="+cc.Date)
		}
		w.WriteHeader(http.StatusCreated)
		respond(w, cc)
	}
}
