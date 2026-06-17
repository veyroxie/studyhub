package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"studyhub/internal/core"
	"studyhub/internal/models"
	"studyhub/internal/notify"
	"studyhub/internal/store"
)

// ── Attendance ────────────────────────────────────────────────────────────────

func listAttendance(db *store.DB, c *core.Claims) []models.Attendance {
	var rows *sql.Rows
	var err error
	tid := store.TenantID(c)
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the (tenant_id=? OR ?=0)
		// pattern so Postgres can use idx_attendance_tenant_date.
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL ORDER BY a.date DESC`, c.Email, tid, tid)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE 1=1`+tw+` ORDER BY date DESC`, twArgs...)
	}
	if err != nil {
		return []models.Attendance{}
	}
	defer rows.Close()
	out := []models.Attendance{}
	for rows.Next() {
		var a models.Attendance
		var classID, checkIn, checkOut sql.NullString
		if err := rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status); err != nil {
			continue
		}
		if classID.Valid {
			a.ClassID = &classID.String
		}
		if checkIn.Valid {
			a.CheckIn = &checkIn.String
		}
		if checkOut.Valid {
			a.CheckOut = &checkOut.String
		}
		out = append(out, a)
	}
	return out
}

func listAttendancePaged(db *store.DB, c *core.Claims, p core.Pagination) ([]models.Attendance, int) {
	tid := store.TenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL ORDER BY a.date DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := store.ScopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE 1=1`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE 1=1`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		return []models.Attendance{}, total
	}
	defer rows.Close()
	out := []models.Attendance{}
	for rows.Next() {
		var a models.Attendance
		var classID, checkIn, checkOut sql.NullString
		if err := rows.Scan(&a.ID, &a.PersonID, &a.PersonType, &a.Date, &classID, &checkIn, &checkOut, &a.Status); err != nil {
			continue
		}
		if classID.Valid {
			a.ClassID = &classID.String
		}
		if checkIn.Valid {
			a.CheckIn = &checkIn.String
		}
		if checkOut.Valid {
			a.CheckOut = &checkOut.String
		}
		out = append(out, a)
	}
	return out, total
}

func HandleAttendance(db *store.DB, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := core.ParsePagination(r)
			if !p.Active {
				core.Respond(w, listAttendance(db, c))
				return
			}
			data, total := listAttendancePaged(db, c, p)
			core.Respond(w, core.PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			// Only staff (admin/teacher) can submit attendance. Previously
			// any authenticated user — parents included — could fabricate
			// attendance for any person_id they knew.
			if !core.IsStaffRole(c) {
				core.RespondError(w, "staff only", http.StatusForbidden)
				return
			}
			var a models.Attendance
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			if a.ID == "" {
				a.ID = core.GenerateID("ATT")
			}
			if a.Date == "" {
				a.Date = core.Today()
			}

			tw, twArgs := store.ScopeTenant(c, "")

			// Verify person_id belongs to the caller's tenant before inserting
			// anything. Without this, a teacher in tenant A could submit
			// attendance for a foreign student/staff id and create a junk row
			// scoped to tenant A pointing at a foreign entity. The check uses
			// the same tenant-scope clause so superadmin (cross-tenant) still
			// works.
			if a.PersonID == "" {
				core.RespondError(w, "personId is required", http.StatusBadRequest)
				return
			}
			personTable := "students"
			if a.PersonType == "teacher" || a.PersonType == "staff" {
				personTable = "staff"
			}
			var personExists int
			personArgs := append([]any{a.PersonID}, twArgs...)
			db.QueryRow(`SELECT 1 FROM `+personTable+` WHERE id=? AND deleted_at IS NULL`+tw, personArgs...).Scan(&personExists)
			if personExists != 1 {
				core.RespondError(w, "person not found in tenant", http.StatusBadRequest)
				return
			}
			if a.ClassID != nil && *a.ClassID != "" {
				var classExists int
				classArgs := append([]any{*a.ClassID}, twArgs...)
				db.QueryRow(`SELECT 1 FROM classes WHERE id=? AND deleted_at IS NULL`+tw, classArgs...).Scan(&classExists)
				if classExists != 1 {
					core.RespondError(w, "class not found in tenant", http.StatusBadRequest)
					return
				}
			}

			// Upsert: update existing record for same person+class+date, or insert.
			// Scoped to the caller's tenant so a teacher in tenant A cannot
			// overwrite an attendance row for a colliding person_id in tenant B.
			var existingID string
			q := `SELECT id FROM attendance WHERE person_id=? AND date=?` + tw
			args := append([]any{a.PersonID, a.Date}, twArgs...)
			if a.ClassID != nil {
				q = `SELECT id FROM attendance WHERE person_id=? AND date=? AND class_id=?` + tw
				args = append([]any{a.PersonID, a.Date, *a.ClassID}, twArgs...)
			}
			db.QueryRow(q, args...).Scan(&existingID)

			var classID, checkIn, checkOut any
			if a.ClassID != nil {
				classID = *a.ClassID
			}
			if a.CheckIn != nil {
				checkIn = *a.CheckIn
			}
			if a.CheckOut != nil {
				checkOut = *a.CheckOut
			}

			if existingID != "" {
				a.ID = existingID
				updArgs := append([]any{checkIn, checkOut, a.Status, existingID}, twArgs...)
				if _, err := db.Exec(`UPDATE attendance SET check_in=?,check_out=?,status=? WHERE id=?`+tw, updArgs...); err != nil {
					core.RespondError(w, "could not update attendance", 500)
					return
				}
				core.LogAudit(db, c.Email, "attendance_updated", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			} else {
				tid := store.TenantID(c)
				if _, err := db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?,?)`,
					a.ID, tid, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status); err != nil {
					core.RespondError(w, "could not create attendance", 500)
					return
				}
				core.LogAudit(db, c.Email, "attendance_created", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			}

			// Notify the parent on check-in/out across every channel: in-app
			// toast (WebSocket broadcast below), web push and opt-in email
			// (notifyParentOnCheck). Status-only updates (e.g. marking absent)
			// carry no time, so they're skipped — the parent toast must never
			// read "checked in at " with no time.
			if a.PersonType == "student" && (a.CheckIn != nil || a.CheckOut != nil) {
				isCheckIn := a.CheckOut == nil
				tid := store.TenantID(c)
				if hub != nil {
					eventType := "CHECK_IN"
					if !isCheckIn {
						eventType = "CHECK_OUT"
					}
					hub.broadcastTenant(tid, map[string]any{
						"type":     eventType,
						"personId": a.PersonID,
						"checkIn":  a.CheckIn,
						"checkOut": a.CheckOut,
						"date":     a.Date,
					})
				}
				go notify.NotifyParentOnCheck(db, tid, a, isCheckIn)
			}
			core.Respond(w, a)
		}
	}
}
