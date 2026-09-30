package handlers

import (
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// Reports are paused while a monthly invoice is unpaid. The PDF was gated but the
// snapshot still carried every report's text to the parent's browser.
func TestAParentWhoOwesGetsNoReportText(t *testing.T) {
	r, cleanup := setupTestApp(t)
	defer cleanup()
	db := store.InitDB(testDSN())
	defer db.Close()
	id := core.GenerateID("PR")
	db.Exec(`DELETE FROM progress_reports WHERE student_id='STU001'`)
	if _, err := db.Exec(`INSERT INTO progress_reports(id,tenant_id,student_id,term,subject,strengths,teacher_comment,published)
		VALUES(?,?,?,?,?,?,?,?)`, id, 1, "STU001", "2026-T3", "Maths", "Fractions", "Keep going", true); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM progress_reports WHERE id=?`, id) })

	parent := getParentToken(t, r)
	// The seeded family has unpaid March invoices (INV006, INV012).
	for _, pr := range parentSnapshot(t, r, parent).ProgressReports {
		if pr.ID != id {
			continue
		}
		if pr.Strengths != "" || pr.TeacherComment != "" {
			t.Fatalf("report text reached a parent who owes: %q / %q", pr.Strengths, pr.TeacherComment)
		}
		if pr.Term == "" {
			t.Fatal("the report should still be listed, so the page can say it is paused")
		}
		return
	}
	t.Fatal("the report is missing entirely; it should be listed as paused")
}
