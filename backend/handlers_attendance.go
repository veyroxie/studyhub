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
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? ORDER BY a.date DESC`, c.Email, tid, tid)
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
		db.QueryRow(`SELECT COUNT(*) FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=?`, c.Email, tid, tid).Scan(&total)
		rows, err = db.Query(`SELECT a.id,a.person_id,a.person_type,a.date,a.class_id,a.check_in,a.check_out,a.status FROM attendance a JOIN students s ON s.id=a.person_id WHERE a.person_type='student' AND s.contact=? AND s.tenant_id=? AND a.tenant_id=? ORDER BY a.date DESC LIMIT ? OFFSET ?`, c.Email, tid, tid, p.Limit, p.Offset)
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

			// Upsert: update existing record for same person+class+date, or insert
			var existingID string
			q := `SELECT id FROM attendance WHERE person_id=? AND date=?`
			args := []any{a.PersonID, a.Date}
			if a.ClassID != nil {
				q = `SELECT id FROM attendance WHERE person_id=? AND date=? AND class_id=?`
				args = append(args, *a.ClassID)
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
				if _, err := db.Exec(`UPDATE attendance SET check_in=?,check_out=?,status=? WHERE id=?`, checkIn, checkOut, a.Status, existingID); err != nil {
					respondError(w, "could not update attendance", 500)
					return
				}
			} else {
				tid := tenantID(c)
				if _, err := db.Exec(`INSERT INTO attendance(id,tenant_id,person_id,person_type,date,class_id,check_in,check_out,status) VALUES(?,?,?,?,?,?,?,?,?)`,
					a.ID, tid, a.PersonID, a.PersonType, a.Date, classID, checkIn, checkOut, a.Status); err != nil {
					respondError(w, "could not create attendance", 500)
					return
				}
			}

			// Broadcast check-in/out event to WebSocket clients. Only fire
			// when there's an actual time to communicate — status-only
			// updates (e.g. marking absent) would otherwise produce a
			// "checked in at " toast with no time on the parent's screen.
			if hub != nil && a.PersonType == "student" && (a.CheckIn != nil || a.CheckOut != nil) {
				eventType := "CHECK_IN"
				if a.CheckOut != nil {
					eventType = "CHECK_OUT"
				}
				hub.broadcast(map[string]any{
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
