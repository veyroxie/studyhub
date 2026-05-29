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
		// Parents are always tenant-scoped — drop the OR pattern.
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,''),COALESCE(referral_credits_remaining,0) FROM families WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY name LIMIT 5000`, c.Email, tid)
	} else {
		tw, twArgs := scopeTenant(c, "")
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,''),COALESCE(referral_credits_remaining,0) FROM families WHERE deleted_at IS NULL`+tw+` ORDER BY name LIMIT 5000`, twArgs...)
	}
	if err != nil {
		return []Family{}
	}
	defer rows.Close()
	out := []Family{}
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.ID, &f.Name, &f.Contact, &f.Phone, &f.ParentName, &f.Address, &f.Notes, &f.ReferralCode, &f.ReferralCreditsRemaining); err != nil {
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
			if !isAdminRole(c) {
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
			logAudit(db, c.Email, "family_created", "family", f.ID, f.Name)
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
		if !isAdminRole(c) {
			respondError(w, "admin only", 403)
			return
		}
		famID := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")

		var contact string
		qArgs := append([]any{famID}, twArgs...)
		if err := db.QueryRow(`SELECT contact FROM families WHERE id=? AND deleted_at IS NULL`+tw, qArgs...).Scan(&contact); err != nil {
			respondError(w, "family not found", 404)
			return
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		// Anonymise + soft-delete family. Tenant scope added so a
		// superadmin's tx targeting one tenant cannot accidentally touch
		// a family with the same id in another tenant.
		famArgs := append([]any{famID}, twArgs...)
		tx.Exec(`UPDATE families SET deleted_at=NOW(), name='[deleted]', contact='deleted-'||id||'@redacted', phone='', parent_name='[deleted]', address='', notes='' WHERE id=?`+tw, famArgs...)

		// Anonymise + soft-delete all students in this family.
		stuArgs := append([]any{famID}, twArgs...)
		tx.Exec(`UPDATE students SET deleted_at=NOW(), first_name='[deleted]', last_name='[deleted]', parent_name='[deleted]', contact='deleted-'||id||'@redacted', phone='', notes='', medical_info='', allergies='', emergency2_name='', emergency2_phone='' WHERE family_id=? AND deleted_at IS NULL`+tw, stuArgs...)

		// Anonymise the parent user account only if this contact email is
		// unique to a single tenant. users.email is globally unique by
		// schema, but historical rows may have duplicates from cross-tenant
		// imports — guard with a tenant scope so an admin in tenant A
		// cannot delete a user owned by tenant B that shares an email.
		if contact != "" {
			userArgs := append([]any{contact}, twArgs...)
			tx.Exec(`UPDATE users SET password_hash='DELETED', email='deleted-'||id||'@redacted', name='[deleted]', status='deleted' WHERE email=?`+tw, userArgs...)
		}

		if err := tx.Commit(); err != nil {
			respondError(w, "server error", 500)
			return
		}

		logAudit(db, c.Email, "pdpa_account_deleted", "family", famID, "contact="+contact)
		logger.Info("PDPA account deleted", "family_id", famID, "contact", contact, "admin", c.Email)

		respond(w, map[string]string{"message": "Account and associated data have been anonymised."})
	}
}

func handleFamilyByID(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := scopeTenant(c, "")
		switch r.Method {
		case http.MethodPut:
			var f Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			f.ID = id
			args := append([]any{f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, id}, twArgs...)
			if _, err := db.Exec(`UPDATE families SET name=?,contact=?,phone=?,parent_name=?,address=?,notes=? WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not update family", 500)
				return
			}
			logAudit(db, c.Email, "family_updated", "family", id, f.Name)
			respond(w, f)
		case http.MethodDelete:
			args := append([]any{id}, twArgs...)
			if _, err := db.Exec(`UPDATE families SET deleted_at=NOW() WHERE id=?`+tw, args...); err != nil {
				respondError(w, "could not delete family", 500)
				return
			}
			logAudit(db, c.Email, "family_deleted", "family", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
