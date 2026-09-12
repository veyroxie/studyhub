package store

import (
	"studyhub/internal/core"
	"studyhub/internal/models"
)

// DisplayStatus is what an invoice's status READS AS, which is not always what
// is stored. An unpaid invoice past its due date is overdue by arithmetic; the
// row does not need rewriting to say so.
//
// Nothing writes 'Overdue' any more. The early-bird expiry job used to, as a
// side effect of clawing a discount back, and it now reissues instead -- so
// without this an invoice simply stayed 'Unpaid' forever, however late.
// Deriving it also removes the stale window a nightly job leaves behind: an
// invoice becomes overdue at midnight, not whenever a job next happens to run.
//
// Stored 'Overdue' rows from before this are left alone and already read as
// overdue, so there is nothing to migrate.
func DisplayStatus(status, dueDate, today string) string {
	if status != models.InvoiceStatusUnpaid {
		return status
	}
	if dueDate == "" || dueDate >= today {
		return status
	}
	return models.InvoiceStatusOverdue
}

// DisplayStatusLocal is DisplayStatus against today in the centre's timezone,
// which is the only "today" that matters here: the due date is a local date and
// the container TZ is Asia/Kuala_Lumpur.
func DisplayStatusLocal(status, dueDate string) string {
	return DisplayStatus(status, dueDate, core.Today())
}
