package store

import (
	"database/sql"
	"math"
	"sort"

	"studyhub/internal/models"
)

// A family bill is derived, never stored: every Monthly invoice one parent can see
// for one period. Keying on students.contact matches exactly what the parent is
// shown, and deriving it means a reissued replacement, a contact change or a
// deleted invoice moves in and out of the bill with no membership to maintain.

// FamilyBillMember is one child's invoice inside a family bill.
type FamilyBillMember struct {
	InvoiceID     string  `json:"invoiceId"`
	InvoiceNo     string  `json:"invoiceNo"`
	StudentID     string  `json:"studentId"`
	StudentName   string  `json:"studentName"`
	Status        string  `json:"status"`
	Amount        float64 `json:"amount"`
	DueDate       string  `json:"dueDate"`
	PaymentMethod string  `json:"paymentMethod"`
	ReferenceNo   string  `json:"referenceNo"`
	ParentName    string  `json:"-"`
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

const familyBillSelect = `SELECT i.id, COALESCE(i.invoice_no,''), i.student_id,
	COALESCE(s.first_name,'')||' '||COALESCE(s.last_name,''), i.status, i.amount, COALESCE(i.due_date,''),
	COALESCE(i.payment_method,''), COALESCE(i.reference_no,''), COALESCE(s.parent_name,'')
	FROM invoices i JOIN students s ON s.id=i.student_id AND s.tenant_id=i.tenant_id
	WHERE i.tenant_id=? AND s.contact=? AND i.period=? AND i.type='Monthly'
	  AND i.deleted_at IS NULL AND s.deleted_at IS NULL` + ParentVisibleInvoiceSQL +
	` ORDER BY s.first_name, i.id`

// FamilyBillMembers lists a bill's invoices. Pass a transaction to lock them for a payment.
func FamilyBillMembers(q querier, tenantID int, contact, period string, forUpdate bool) ([]FamilyBillMember, error) {
	query := familyBillSelect
	if forUpdate {
		query += " FOR UPDATE OF i"
	}
	rows, err := q.Query(query, tenantID, contact, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FamilyBillMember{}
	for rows.Next() {
		var m FamilyBillMember
		if err := rows.Scan(&m.InvoiceID, &m.InvoiceNo, &m.StudentID, &m.StudentName, &m.Status, &m.Amount,
			&m.DueDate, &m.PaymentMethod, &m.ReferenceNo, &m.ParentName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FamilyBillTargets is the members a payment action moves: what the parent still
// owes for a claim or a confirmation, and only the claimed ones for a rejection.
func FamilyBillTargets(members []FamilyBillMember, action string) []FamilyBillMember {
	out := []FamilyBillMember{}
	for _, m := range members {
		if isFamilyBillTarget(m.Status, action) {
			out = append(out, m)
		}
	}
	return out
}

func isFamilyBillTarget(status, action string) bool {
	if action == models.InvoiceStatusUnpaid {
		return status == models.InvoiceStatusPendingVerification
	}
	return status != models.InvoiceStatusPaid
}

// SameFamilyBill reports whether the ids and total a client showed still describe targets.
func SameFamilyBill(targets []FamilyBillMember, shownIDs []string, shownTotal float64) bool {
	if len(targets) == 0 || len(targets) != len(shownIDs) {
		return false
	}
	ids := make([]string, len(targets))
	total := 0.0
	for i, m := range targets {
		ids[i] = m.InvoiceID
		total += m.Amount
	}
	shown := append([]string(nil), shownIDs...)
	sort.Strings(ids)
	sort.Strings(shown)
	for i := range ids {
		if ids[i] != shown[i] {
			return false
		}
	}
	return math.Round(total*100) == math.Round(shownTotal*100)
}
