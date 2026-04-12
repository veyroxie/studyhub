package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ── Families ─────────────────────────────────────────────────────────────────

func listFamilies(db *DB, c *Claims) []Family {
	tid := tenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,'') FROM families WHERE contact=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY name`, c.Email, tid, tid)
	} else {
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,'') FROM families WHERE (tenant_id=? OR ?=0) AND deleted_at IS NULL ORDER BY name`, tid, tid)
	}
	if err != nil {
		return []Family{}
	}
	defer rows.Close()
	out := []Family{}
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.ID, &f.Name, &f.Contact, &f.Phone, &f.ParentName, &f.Address, &f.Notes, &f.ReferralCode); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}

func handleFamilies(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			respond(w, listFamilies(db, c))
		case http.MethodPost:
			if c.Role != "admin" {
				respondError(w, "admin only", 403)
				return
			}
			var f Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", f.Name, "contact", f.Contact); msg != "" {
				respondError(w, msg, 400)
				return
			}
			if f.ID == "" {
				f.ID = generateID("FAM")
			}
			if f.ReferralCode == "" {
				f.ReferralCode = newReferralCode()
			}
			tid := tenantID(c)
			_, err := db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,address,notes,referral_code) VALUES(?,?,?,?,?,?,?,?,?)`,
				f.ID, tid, f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, f.ReferralCode)
			if err != nil {
				respondError(w, "server error", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_created", "family", f.ID, f.Name)
			w.WriteHeader(http.StatusCreated)
			respond(w, f)
		}
	}
}

// handleFamilyPDPADelete is the admin-only PDPA account deletion endpoint.
// Soft-deletes the family, parent user, and all linked students. PII is
// overwritten with "[deleted]" so invoices / audit logs can be retained
// without containing personal data.
//
// DELETE /api/families/{id}/pdpa
func handleFamilyPDPADelete(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		famID := chi.URLParam(r, "id")
		tid := tenantID(c)

		var contact string
		if err := db.QueryRow(`SELECT contact FROM families WHERE id=? AND (tenant_id=? OR ?=0) AND deleted_at IS NULL`, famID, tid, tid).Scan(&contact); err != nil {
			respondError(w, "family not found", 404)
			return
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		// Anonymise + soft-delete family.
		tx.Exec(`UPDATE families SET deleted_at=NOW(), name='[deleted]', contact='deleted-'||id||'@redacted', phone='', parent_name='[deleted]', address='', notes='' WHERE id=?`, famID)

		// Anonymise + soft-delete all students in this family.
		tx.Exec(`UPDATE students SET deleted_at=NOW(), first_name='[deleted]', last_name='[deleted]', parent_name='[deleted]', contact='deleted-'||id||'@redacted', phone='', notes='', medical_info='', allergies='', emergency2_name='', emergency2_phone='' WHERE family_id=? AND deleted_at IS NULL`, famID)

		// Delete the parent user account.
		if contact != "" {
			tx.Exec(`UPDATE users SET password_hash='DELETED', email='deleted-'||id||'@redacted', name='[deleted]', status='deleted' WHERE email=?`, contact)
		}

		if err := tx.Commit(); err != nil {
			respondError(w, "server error", 500)
			return
		}

		db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
			c.Email, "pdpa_account_deleted", "family", famID, "contact="+contact)
		logger.Info("PDPA account deleted", "family_id", famID, "contact", contact, "admin", c.Email)

		respond(w, map[string]string{"message": "Account and associated data have been anonymised."})
	}
}

func handleFamilyByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if c == nil || c.Role != "admin" {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tid := tenantID(c)
		switch r.Method {
		case http.MethodPut:
			var f Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			f.ID = id
			if _, err := db.Exec(`UPDATE families SET name=?,contact=?,phone=?,parent_name=?,address=?,notes=? WHERE id=? AND (tenant_id=? OR ?=0)`,
				f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, id, tid, tid); err != nil {
				respondError(w, "could not update family", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_updated", "family", id, f.Name)
			respond(w, f)
		case http.MethodDelete:
			if _, err := db.Exec(`UPDATE families SET deleted_at=NOW() WHERE id=? AND (tenant_id=? OR ?=0)`, id, tid, tid); err != nil {
				respondError(w, "could not delete family", 500)
				return
			}
			db.Exec(`INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,detail) VALUES(?,?,?,?,?)`,
				c.Email, "family_deleted", "family", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
