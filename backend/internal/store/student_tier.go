package store

import (
	"fmt"

	"studyhub/internal/core"
)

// ApplyStudentTier sets a student's pricing tier on the enrolments it can
// apply to, and clears it on the rest.
//
// A student has a level; the price lives in the catalogue against
// (category, tier, sessions per week). Before this the student form offered a
// hardcoded "Pricing level band" of 1-3 or 4-6 -- the bands of the retired
// pricing_tiers table -- and the rating engine does not read that column at
// all. So choosing a band changed nothing, while looking like it should, and
// the difference had to be made up with a discount typed by hand.
//
// "No tier" is the empty string, not NULL: enrollments.tier_name is NOT NULL,
// and the resolver already reads an empty tier as "fall back to the class default".
//
// Only enrolments whose CATEGORY actually prices that tier are touched. A
// student taking Group and Mandarin has one level but two categories, and
// "Level 3" means nothing in Mandarin -- writing it there would make that
// enrolment unpriceable. The rest fall back to the class default, which is the
// behaviour they had already.
func ApplyStudentTier(tx *Tx, tenantID int, studentID, tier string) error {
	if tier == "" {
		if _, err := tx.Exec(`UPDATE enrollments SET tier_name=''
			WHERE tenant_id=? AND student_id=? AND ended_on IS NULL`, tenantID, studentID); err != nil {
			return fmt.Errorf("clear tier for %s: %w", studentID, err)
		}
		return nil
	}
	// Set where the tier exists for that class's category...
	if _, err := tx.Exec(`UPDATE enrollments e SET tier_name=?
		FROM classes c
		WHERE e.class_id = c.id AND c.tenant_id = e.tenant_id AND c.deleted_at IS NULL
		  AND e.tenant_id = ? AND e.student_id = ? AND e.ended_on IS NULL
		  AND EXISTS (SELECT 1 FROM pricing_plans p
		               WHERE p.tenant_id = e.tenant_id AND p.category_id = c.pricing_category_id
		                 AND p.tier_name = ? AND p.deleted_at IS NULL)`,
		tier, tenantID, studentID, tier); err != nil {
		return fmt.Errorf("apply tier %q to %s: %w", tier, studentID, err)
	}
	// ...and clear it where it does not, so a stale tier cannot linger on an
	// enrolment in a category that never priced it.
	if _, err := tx.Exec(`UPDATE enrollments e SET tier_name=''
		FROM classes c
		WHERE e.class_id = c.id AND c.tenant_id = e.tenant_id AND c.deleted_at IS NULL
		  AND e.tenant_id = ? AND e.student_id = ? AND e.ended_on IS NULL
		  AND NOT EXISTS (SELECT 1 FROM pricing_plans p
		                   WHERE p.tenant_id = e.tenant_id AND p.category_id = c.pricing_category_id
		                     AND p.tier_name = ? AND p.deleted_at IS NULL)`,
		tenantID, studentID, tier); err != nil {
		return fmt.Errorf("clear inapplicable tier for %s: %w", studentID, err)
	}
	core.Logger.Info("student tier applied", "student_id", studentID, "tier", tier)
	return nil
}
