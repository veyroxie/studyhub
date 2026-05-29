package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// ── Attendance ────────────────────────────────────────────────────────────────

func listAttendance(db *DB, c *Claims) []Attendance {
	var rows *sql.Rows
	var err error
	tid := tenantID(c)
	if c != nil && c.Role == "parent" {
		// Parents are always tenant-scoped — drop the (tenant_id=? OR ?=0)
		// pattern so Postgres can use idx_attendance_tenant_date.
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL ORDER BY a.date DESC`, c.Email, tid, tid)
	} else {
		tw, twArgs := scopeTenant(c, "")
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE 1=1`+tw+` ORDER BY date DESC`, twArgs...)
	}
	if err != nil {
		return []Attendance{}
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
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

func listAttendancePaged(db *DB, c *Claims, p Pagination) ([]Attendance, int) {
	tid := tenantID(c)
	var total int
	var rows *sql.Rows
	var err error
	if c != nil && c.Role == "parent" {
		db.QueryRow(`SELECT COUNT(*) FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? AND s.deleted_at IS NULL ORDER BY a.date DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
	} else {
		tw, twArgs := scopeTenant(c, "")
		db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE 1=1`+tw, twArgs...).Scan(&total)
		pageArgs := append(append([]any{}, twArgs...), p.Limit, p.Offset)
		rows, err = db.Query(`SELECT id,person_id,person_type,date,class_id,check_in,check_out,status FROM attendance WHERE 1=1`+tw+` ORDER BY date DESC LIMIT ? OFFSET ?`, pageArgs...)
	}
	if err != nil {
		return []Attendance{}, total
	}
	defer rows.Close()
	out := []Attendance{}
	for rows.Next() {
		var a Attendance
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

func handleAttendance(db *DB, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		switch r.Method {
		case http.MethodGet:
			p := parsePagination(r)
			if !p.Active {
				respond(w, listAttendance(db, c))
				return
			}
			data, total := listAttendancePaged(db, c, p)
			respond(w, PaginatedResponse{Data: data, Total: total, Limit: p.Limit, Offset: p.Offset})
		case http.MethodPost:
			// Only staff (admin/teacher) can submit attendance. Previously
			// any authenticated user — parents included — could fabricate
			// attendance for any person_id they knew.
			if !isStaffRole(c) {
				respondError(w, "staff only", http.StatusForbidden)
				return
			}
			var a Attendance
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				respondError(w, "bad body", 400)
				return
			}
			if a.ID == "" {
				a.ID = generateID("ATT")
			}
			if a.Date == "" {
				a.Date = today()
			}

			tw, twArgs := scopeTenant(c, "")

			// Verify person_id belongs to the caller's tenant before inserting
			// anything. Without this, a teacher in tenant A could submit
			// attendance for a foreign student/staff id and create a junk row
			// scoped to tenant A pointing at a foreign entity. The check uses
			// the same tenant-scope clause so superadmin (cross-tenant) still
			// works.
			if a.PersonID == "" {
				respondError(w, "personId is required", http.StatusBadRequest)
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
				respondError(w, "person not found in tenant", http.StatusBadRequest)
				return
			}
			if a.ClassID != nil && *a.ClassID != "" {
				var classExists int
				classArgs := append([]any{*a.ClassID}, twArgs...)
				db.QueryRow(`SELECT 1 FROM classes WHERE id=? AND deleted_at IS NULL`+tw, classArgs...).Scan(&classExists)
				if classExists != 1 {
					respondError(w, "class not found in tenant", http.StatusBadRequest)
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
					respondError(w, "could not update attendance", 500)
					return
				}
				logAudit(db, c.Email, "attendance_updated", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			} else {
				tid := tenantID(c)
				if _, err := db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?,?)`,
					a.ID, tid, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status); err != nil {
					respondError(w, "could not create attendance", 500)
					return
				}
				logAudit(db, c.Email, "attendance_created", "attendance", a.ID, a.PersonID+" "+a.Date+" "+a.Status)
			}

			// Broadcast check-in/out event only to clients in the same tenant.
			// Status-only updates (e.g. marking absent) are skipped so the
			// parent's toast never reads "checked in at " with no time.
			if hub != nil && a.PersonType == "student" && (a.CheckIn != nil || a.CheckOut != nil) {
				eventType := "CHECK_IN"
				if a.CheckOut != nil {
					eventType = "CHECK_OUT"
				}
				hub.broadcastTenant(tenantID(c), map[string]any{
					"type":     eventType,
					"personId": a.PersonID,
					"checkIn":  a.CheckIn,
					"checkOut": a.CheckOut,
					"date":     a.Date,
				})
			}
			respond(w, a)
		}
	}
}
