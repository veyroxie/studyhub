package jobs

import (
	"strings"
	"sync"
	"testing"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

type sentEmail struct{ to, subject, body string }

type recordingMailer struct {
	mu   sync.Mutex
	sent []sentEmail
}

func (m *recordingMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentEmail{to, subject, body})
	return nil
}

func (m *recordingMailer) to(addr string) []sentEmail {
	out := []sentEmail{}
	for _, e := range m.sent {
		if e.to == addr {
			out = append(out, e)
		}
	}
	return out
}

// The seeded parent has two children, each with an overdue March invoice.
const reminderParent = "seeduser27@example.com"

func TestAParentWithTwoOverdueChildrenGetsOneReminder(t *testing.T) {
	core.InitLogger()
	db := store.InitDB(testDSN())
	defer db.Close()
	t.Setenv("SEED_DEMO_DATA", "1")
	SeedIfEmpty(db)
	db.Exec(`UPDATE invoices SET reminder_sent_on=NULL WHERE id IN ('INV006','INV012')`)

	m := &recordingMailer{}
	core.SetMailer(m)
	defer core.SetMailer(nil)

	sendOverdueInvoiceReminders(db)
	sent := m.to(reminderParent)
	if len(sent) != 1 {
		t.Fatalf("%d reminders to the parent, want 1", len(sent))
	}
	if !strings.Contains(sent[0].subject, "invoices overdue") || strings.Count(sent[0].body, "Mar 2026 Tuition") < 2 {
		t.Fatalf("reminder %q does not cover both children", sent[0].subject)
	}
	var unstamped int
	db.QueryRow(`SELECT count(*) FROM invoices WHERE id IN ('INV006','INV012') AND reminder_sent_on IS NULL`).Scan(&unstamped)
	if unstamped != 0 {
		t.Fatalf("%d invoices left unstamped, so the next tick would remind again", unstamped)
	}

	sendOverdueInvoiceReminders(db)
	if again := m.to(reminderParent); len(again) != 1 {
		t.Fatalf("a second run sent %d more, want none within three days", len(again)-1)
	}
}
