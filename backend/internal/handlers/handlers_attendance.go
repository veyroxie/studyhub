package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

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
		core.Logger.Error("list query failed", "err", err, "type", "Attendance")
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
	// Teachers see attendance only for students in their own classes, and never
	// staff rows (which expose colleagues' work patterns). The query above is
	// tenant-wide for every non-parent role.
	if c != nil && c.Role == "teacher" {
		stuIDs := teacherStudentIDSet(db, c)
		myStaffID := staffIDForClaims(db, c)
		scoped := []models.Attendance{}
		for _, a := range out {
			// Their own students, plus their OWN staff row so the teacher
			// self check-in/out panel still works.
			if a.PersonType == "student" && stuIDs[a.PersonID] {
				scoped = append(scoped, a)
			} else if a.PersonType == "staff" && myStaffID != "" && a.PersonID == myStaffID {
				scoped = append(scoped, a)
			}
		}
		return scoped
	}
	return out
}

func listAttendancePaged(db *store.DB, c *core.Claims, p core.Pagination) ([]models.Attendance, int) {
	// Teachers: reuse the scoped non-paged list and page it in memory. Their
	// visibility depends on an app-computed student set, so scoping here in SQL
	// would duplicate that logic — and without this the paginated endpoint was a
	// clean bypass of the filter in listAttendance.
	if c != nil && c.Role == "teacher" {
		all := listAttendance(db, c)
		total := len(all)
		start := p.Offset
		if start > total {
			start = total
		}
		end := start + p.Limit
		if end > total {
			end = total
		}
		return all[start:end], total
	}
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
		core.Logger.Error("list query failed", "err", err, "type", "Attendance")
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

			// Upsert: update existing record for same person+class+date, or
			// insert. Runs inside a transaction guarded by an advisory lock on
			// the (tenant, person, date, class) key so two concurrent kiosk
			// scans can't both miss the existing row and insert duplicates —
			// duplicate attendance rows double-count part-time payroll hours.
			// Scoped to the caller's tenant so a teacher in tenant A cannot
			// overwrite an attendance row for a colliding person_id in tenant B.
			tid := store.TenantID(c)
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			defer tx.Rollback()

			classKey := ""
			if a.ClassID != nil {
				classKey = *a.ClassID
			}
			lockKey := core.AdvisoryLockKey(strconv.Itoa(tid) + "|att|" + a.PersonID + "|" + a.Date + "|" + classKey)
			if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, lockKey); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}

			var existingID string
			q := `SELECT id FROM attendance WHERE person_id=? AND date=?` + tw
			args := append([]any{a.PersonID, a.Date}, twArgs...)
			if a.ClassID != nil {
				q = `SELECT id FROM attendance WHERE person_id=? AND date=? AND class_id=?` + tw
				args = append([]any{a.PersonID, a.Date, *a.ClassID}, twArgs...)
			}
			tx.QueryRow(q, args...).Scan(&existingID)

			if existingID != "" {
				a.ID = existingID
				updArgs := append([]any{checkIn, checkOut, a.Status, existingID}, twArgs...)
				if _, err := tx.Exec(`UPDATE attendance SET check_in=?,check_out=?,status=? WHERE id=?`+tw, updArgs...); err != nil {
					core.RespondError(w, "could not update attendance", 500)
					return
				}
			} else {
				if _, err := tx.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?,?)`,
					a.ID, tid, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status); err != nil {
					core.RespondError(w, "could not create attendance", 500)
					return
				}
			}
			if err := tx.Commit(); err != nil {
				core.RespondError(w, "server error", 500)
				return
			}
			if existingID != "" {
				core.LogAudit(db, store.TenantID(c), c.Email, "attendance_updated", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			} else {
				core.LogAudit(db, store.TenantID(c), c.Email, "attendance_created", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			}

			// Notify the parent on check-in/out across every channel: in-app
			// toast (WebSocket broadcast below), web push and opt-in email
			// (notifyParentOnCheck). Status-only updates (e.g. marking absent)
			// carry no time, so they're skipped — the parent toast must never
			// read "checked in at " with no time.
			if a.PersonType == "student" && (a.CheckIn != nil || a.CheckOut != nil) {
				isCheckIn := a.CheckOut == nil
				if hub != nil {
					eventType := "CHECK_IN"
					if !isCheckIn {
						eventType = "CHECK_OUT"
					}
					// Look up the owning parent so the event reaches staff and
					// only that child's parent — not every family in the tenant.
					var ownerEmail string
					ownerArgs := append([]any{a.PersonID}, twArgs...)
					db.QueryRow(`SELECT COALESCE(contact,'') FROM students WHERE id=?`+tw, ownerArgs...).Scan(&ownerEmail)
					hub.broadcastCheckIn(tid, ownerEmail, map[string]any{
						"type":     eventType,
						"personId": a.PersonID,
						"checkIn":  a.CheckIn,
						"checkOut": a.CheckOut,
						"date":     a.Date,
					})
				}
				goSafe("notify_parent_on_check", func() { notify.NotifyParentOnCheck(db, tid, a, isCheckIn) })
			}
			core.Respond(w, a)
		}
	}
}

// HandleDeleteAttendance removes one attendance row — the undo for a
// mis-tapped check-in or absence. Hard delete: the table has no deleted_at,
// and an undone record should vanish from payroll hours and parent views
// alike. Credits granted alongside an absence are NOT clawed back here; that
// policy is still open with the centre, so the admin adjusts them from the
// student's profile when needed.
func HandleDeleteAttendance(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		res, err := db.Exec(`DELETE FROM attendance WHERE id=?`+tw, append([]any{id}, twArgs...)...)
		if err != nil {
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.RespondError(w, "not found", http.StatusNotFound)
			return
		}
		core.LogAudit(db, store.TenantID(c), c.Email, "attendance_undone", "attendance", id, "")
		w.WriteHeader(http.StatusNoContent)
	}
}
