package handlers

import (
	"testing"
	"time"

	"studyhub/internal/core"
	"studyhub/internal/jobs"
)

// TestSessionBillingPreview locks the F5 dry run: session totals from the
// expander x rate resolver beside the live monthly fees, holiday sessions
// unbilled, cancelled sessions billed, per-student bands honored, package
// override untouched, pricing holes flagged, and a nothing-to-bill student
// skipped with the no-referral-consumed guarantee in the reason.
func TestSessionBillingPreview(t *testing.T) {
	_, db, cleanup := setupFeatureTestApp(t)
	defer cleanup()

	// Two months out: clear of this month's real dates and any seeded rows.
	month := time.Now().AddDate(0, 2, 0)
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	weekdayDates := func(wd time.Weekday) []string {
		var out []string
		for d := monthStart; d.Month() == monthStart.Month(); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == wd {
				out = append(out, d.Format("2006-01-02"))
			}
		}
		return out
	}
	mondays, tuesdays := weekdayDates(time.Monday), weekdayDates(time.Tuesday)

	classA := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band) VALUES(?,?,?,?,?,?,?,?)`,
		classA, 1, "Preview Group 1-3", "Monday", "10:00", "11:00", "Group", "1-3")
	classHole := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band) VALUES(?,?,?,?,?,?,?,?)`,
		classHole, 1, "Preview Untiered", "Monday", "10:00", "11:00", "Workshop", "1-3")
	classAllHoliday := core.GenerateID("CLS")
	db.Exec(`INSERT INTO classes(id,tenant_id,name,day,time,end_time,class_type,level_band) VALUES(?,?,?,?,?,?,?,?)`,
		classAllHoliday, 1, "Preview Tuesdays", "Tuesday", "10:00", "11:00", "Group", "1-3")

	// One Monday is a holiday (unbilled), another is cancelled (still billed).
	db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date) VALUES(?,?,?,?,?)`,
		core.GenerateID("HOL"), 1, "Preview Holiday", mondays[0], "")
	db.Exec(`INSERT INTO cancelled_classes(id,tenant_id,class_id,date,created_on) VALUES(?,?,?,?,?)`,
		core.GenerateID("CC"), 1, classA, mondays[1], core.Today())
	// Every Tuesday is a holiday, so classAllHoliday has zero billable sessions.
	for _, d := range tuesdays {
		db.Exec(`INSERT INTO holidays(id,tenant_id,name,date,end_date) VALUES(?,?,?,?,?)`,
			core.GenerateID("HOL"), 1, "Tue Holiday "+d, d, "")
	}

	mkStudent := func(name, band string, pkg float64, classes string) string {
		id := core.GenerateID("STU")
		db.Exec(`INSERT INTO students(id,tenant_id,first_name,last_name,contact,level_band,package_amount,enrolled_classes) VALUES(?,?,?,?,?,?,?,?)`,
			id, 1, name, "Preview", name+"@example.com", band, pkg, classes)
		return id
	}
	stuA := mkStudent("Alpha", "", 0, `["`+classA+`"]`)
	stuB := mkStudent("Beta", "4-6", 0, `["`+classA+`"]`)
	stuPkg := mkStudent("Gamma", "", 500, `["`+classA+`"]`)
	stuHole := mkStudent("Delta", "", 0, `["`+classHole+`"]`)
	stuNone := mkStudent("Epsilon", "", 0, `["`+classAllHoliday+`"]`)

	claims := &core.Claims{TenantID: 1, Role: "admin", Email: "admin@studyhub.com"}
	preview := jobs.SessionBillingPreview(db, claims, month)
	byID := map[string]jobs.PreviewStudent{}
	for _, ps := range preview.Students {
		byID[ps.StudentID] = ps
	}

	billable := len(mondays) - 1 // holiday Monday unbilled; cancelled one billed

	a := byID[stuA]
	if len(a.Lines) != 1 || a.Lines[0].Billable != billable || a.Lines[0].Holiday != 1 || a.Lines[0].Cancelled != 1 {
		t.Fatalf("student A line wrong: %+v", a.Lines)
	}
	if a.Lines[0].Rate != 60 || a.SessionTotal != float64(billable)*60 {
		t.Errorf("student A: want %d x RM60, got %+v", billable, a.Lines[0])
	}
	if a.MonthlyTotal != 240 {
		t.Errorf("student A monthly baseline: want 240 (tier), got %v", a.MonthlyTotal)
	}
	if a.Delta != a.SessionTotal-a.MonthlyTotal {
		t.Errorf("student A delta wrong: %+v", a)
	}

	if b := byID[stuB]; b.SessionTotal != float64(billable)*65 {
		t.Errorf("student B band 4-6: want %d x RM65, got %v", billable, b.SessionTotal)
	}

	if p := byID[stuPkg]; p.SessionTotal != 500 || p.MonthlyTotal != 500 || p.Delta != 0 || len(p.Lines) != 0 {
		t.Errorf("package student must pass through untouched: %+v", p)
	}

	if h := byID[stuHole]; !h.Flagged || len(h.Lines) != 1 || !h.Lines[0].Flagged || h.SessionTotal != 0 {
		t.Errorf("untiered class must flag, not price at 0: %+v", h)
	}

	n := byID[stuNone]
	if !n.Skipped || n.SessionTotal != 0 {
		t.Fatalf("all-holiday student must be skipped: %+v", n)
	}
	if len(n.Lines) != 1 || !n.Lines[0].Skipped || n.Lines[0].Billable != 0 {
		t.Errorf("all-holiday line must be skipped with 0 billable: %+v", n.Lines)
	}
	if n.Reason == "" {
		t.Error("skipped student must carry the no-referral-consumed reason")
	}
}
