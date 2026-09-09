package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/store"

	"github.com/go-chi/chi/v5"
)

// ── Families ─────────────────────────────────────────────────────────────────

func listFamilies(db *store.DB, c *core.Claims) []models.Family {
	tid := store.TenantID(c)
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the OR pattern.
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,''),COALESCE(referral_credits_remaining,0) FROM families WHERE contact=? AND tenant_id=? AND deleted_at IS NULL ORDER BY name LIMIT 5000`, c.Email, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`SELECT id,name,contact,phone,parent_name,COALESCE(address,''),COALESCE(notes,''),COALESCE(referral_code,''),COALESCE(referral_credits_remaining,0) FROM families WHERE deleted_at IS NULL`+tw+` ORDER BY name LIMIT 5000`, twArgs...)
	}
	if err != nil {
		core.Logger.Error("list query failed", "err", err, "type", "Family")
		return []models.Family{}
	}
	defer rows.Close()
	out := []models.Family{}
	for rows.Next() {
		var f models.Family
		if err := rows.Scan(&f.ID, &f.Name, &f.Contact, &f.Phone, &f.ParentName, &f.Address, &f.Notes, &f.ReferralCode, &f.ReferralCreditsRemaining); err != nil {
			continue
		}
		out = append(out, f)
	}
	// Teachers must not see parent contact details. listStudents already blanks
	// them on the student record (redactContactForTeacher), but students carry
	// familyId, so leaving families intact made that redaction cosmetic — a
	// one-line join in devtools recovered every parent's email and phone.
	// Referral code / credit balance is billing-adjacent, so it goes too.
	if c != nil && c.Role == "teacher" {
		for i := range out {
			out[i].Contact = ""
			out[i].Phone = ""
			out[i].ParentName = ""
			out[i].Address = ""
			out[i].Notes = ""
			out[i].ReferralCode = ""
			out[i].ReferralCreditsRemaining = 0
		}
	}
	return out
}

func HandleFamilies(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			core.Respond(w, listFamilies(db, c))
		case http.MethodPost:
			if !core.IsAdminRole(c) {
				core.RespondError(w, "admin only", 403)
				return
			}
			var f models.Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if msg := validationError("name", f.Name, "contact", f.Contact); msg != "" {
				core.RespondError(w, msg, 400)
				return
			}
			if f.ID == "" {
				f.ID = core.GenerateID("FAM")
			}
			if f.ReferralCode == "" {
				f.ReferralCode = core.NewReferralCode()
			}
			tid, tOK := writeTenant(w, c)
			if !tOK {
				return
			}
			_, err := db.Exec(`INSERT INTO families(id,tenant_id,name,contact,phone,parent_name,address,notes,referral_code) VALUES(?,?,?,?,?,?,?,?,?)`,
				f.ID, tid, f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, f.ReferralCode)
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "family_created", "family", f.ID, f.Name)
			w.WriteHeader(http.StatusCreated)
			core.Respond(w, f)
		}
	}
}

// handleFamilyPDPADelete is the admin-only PDPA account deletion endpoint.
// Soft-deletes the family, parent user and all linked students, and clears the
// subject from everything else keyed on their email: the original registration,
// push subscriptions, queued and sent mail, outstanding email tokens, and
// feedback replies. PII is overwritten with "[deleted]" so invoices and audit
// logs can be retained without containing personal data.
//
// Anything added later that stores a parent's email, name or phone belongs in
// this transaction too. The response tells the centre the person has been
// erased, so a table missed here is a false compliance claim, not a bug report.
//
// DELETE /api/families/{id}/pdpa
func HandleFamilyPDPADelete(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		famID := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")

		var contact string
		qArgs := append([]any{famID}, twArgs...)
		if err := db.QueryRow(`SELECT contact FROM families WHERE id=? AND deleted_at IS NULL`+tw, qArgs...).Scan(&contact); err != nil {
			core.RespondError(w, "family not found", 404)
			return
		}

		tx, err := db.BeginTx(r.Context())
		if err != nil {
			core.RespondError(w, "server error", 500)
			return
		}
		defer tx.Rollback()

		// Anonymise + soft-delete family. Tenant scope added so a
		// superadmin's tx targeting one tenant cannot accidentally touch
		// a family with the same id in another tenant. Each statement's error
		// is checked: this is a compliance endpoint, so a partial failure must
		// roll back (via the deferred Rollback) rather than report success
		// while PII survives.
		famArgs := append([]any{famID}, twArgs...)
		if _, err := tx.Exec(`UPDATE families SET deleted_at=NOW(), name='[deleted]', contact='deleted-'||id||'@redacted', phone='', parent_name='[deleted]', address='', notes='' WHERE id=?`+tw, famArgs...); err != nil {
			core.Logger.Error("pdpa delete: family anonymise failed", "err", err, "family_id", famID)
			core.RespondError(w, "could not anonymise account", 500)
			return
		}

		// Anonymise + soft-delete all students in this family.
		stuArgs := append([]any{famID}, twArgs...)
		if _, err := tx.Exec(`UPDATE students SET deleted_at=NOW(), first_name='[deleted]', last_name='[deleted]', parent_name='[deleted]', contact='deleted-'||id||'@redacted', phone='', notes='', medical_info='', allergies='', emergency2_name='', emergency2_phone='' WHERE family_id=? AND deleted_at IS NULL`+tw, stuArgs...); err != nil {
			core.Logger.Error("pdpa delete: students anonymise failed", "err", err, "family_id", famID)
			core.RespondError(w, "could not anonymise account", 500)
			return
		}

		// Anonymise the parent user account only if this contact email is
		// unique to a single tenant. users.email is globally unique by
		// schema, but historical rows may have duplicates from cross-tenant
		// imports — guard with a tenant scope so an admin in tenant A
		// cannot delete a user owned by tenant B that shares an email.
		if contact != "" {
			userArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`UPDATE users SET password_hash='DELETED', email='deleted-'||id||'@redacted', name='[deleted]', status='deleted' WHERE email=?`+tw, userArgs...); err != nil {
				core.Logger.Error("pdpa delete: user anonymise failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
		}

		// Everything else keyed on the parent's email address. Redacting only
		// families/students/users left the subject fully identifiable: their
		// name, phone, child's date of birth and school all survived in
		// registrations, a queued email still reached them, and an outstanding
		// reset token still worked. Same transaction as the rest, so a failure
		// here rolls the whole erasure back rather than reporting success.
		if contact != "" {
			// The original enrolment record: redact, don't delete. The row is
			// still needed for intake reporting; the person in it is not.
			regArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`UPDATE registrations SET parent_name='[deleted]', email='deleted-'||id||'@redacted', phone='', emergency_name='', emergency_phone='', student_first_name='[deleted]', student_last_name='[deleted]', student_dob='', school_name='' WHERE email=?`+tw, regArgs...); err != nil {
				core.Logger.Error("pdpa delete: registrations anonymise failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
			// A live delivery channel to the erased person's device.
			pushArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE parent_email=?`+tw, pushArgs...); err != nil {
				core.Logger.Error("pdpa delete: push subscription removal failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
			// Unsent mail goes; the sender dispatches on status alone and would
			// otherwise still deliver to an address just certified as erased.
			// Already-sent rows keep the fact of the send, without the address.
			qDelArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`DELETE FROM email_queue WHERE to_email=? AND status IN ('pending','sending')`+tw, qDelArgs...); err != nil {
				core.Logger.Error("pdpa delete: queued mail removal failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
			qRedArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`UPDATE email_queue SET to_email='deleted-'||id||'@redacted' WHERE to_email=?`+tw, qRedArgs...); err != nil {
				core.Logger.Error("pdpa delete: sent mail redaction failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
			// Outstanding reset and verification links must die with the account.
			tokArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`DELETE FROM email_tokens WHERE email=?`+tw, tokArgs...); err != nil {
				core.Logger.Error("pdpa delete: token removal failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
			repArgs := append([]any{contact}, twArgs...)
			if _, err := tx.Exec(`UPDATE feedback_replies SET author_email='deleted-'||id||'@redacted', author_name='[deleted]' WHERE author_email=?`+tw, repArgs...); err != nil {
				core.Logger.Error("pdpa delete: feedback reply anonymise failed", "err", err, "family_id", famID)
				core.RespondError(w, "could not anonymise account", 500)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			core.RespondError(w, "server error", 500)
			return
		}

		// The erasure needs an audit trail, but the erased identifier must not
		// BE that trail: this used to write the real email into audit_logs.detail
		// and the application log, re-introducing the address the transaction
		// above had just removed. The family id identifies the record; the
		// admin's own email identifies who acted.
		core.LogAudit(db, store.TenantID(c), c.Email, "pdpa_account_deleted", "family", famID, "identifiers redacted across 8 tables")
		core.Logger.Info("PDPA account deleted", "family_id", famID, "admin", c.Email)

		core.Respond(w, map[string]string{"message": "Account and associated data have been anonymised."})
	}
}

func HandleFamilyByID(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", 403)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		switch r.Method {
		case http.MethodPut:
			var f models.Family
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			f.ID = id
			args := append([]any{f.Name, f.Contact, f.Phone, f.ParentName, f.Address, f.Notes, id}, twArgs...)
			if _, err := db.Exec(`UPDATE families SET name=?,contact=?,phone=?,parent_name=?,address=?,notes=? WHERE id=?`+tw+` AND deleted_at IS NULL`, args...); err != nil {
				core.RespondError(w, "could not update family", 500)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "family_updated", "family", id, f.Name)
			core.Respond(w, f)
		case http.MethodDelete:
			args := append([]any{id}, twArgs...)
			res, err := db.Exec(`UPDATE families SET deleted_at=NOW() WHERE id=?`+tw+` AND deleted_at IS NULL`, args...)
			if err != nil {
				core.RespondError(w, "could not delete family", 500)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				core.RespondError(w, "family not found", http.StatusNotFound)
				return
			}
			core.LogAudit(db, store.TenantID(c), c.Email, "family_deleted", "family", id, "soft deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
