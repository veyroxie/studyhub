package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrEmailTaken: users.email is unique across every tenant.
var ErrEmailTaken = errors.New("another account already uses that email")

// MoveAccountEmail re-points everything keyed on a person's email, in the caller's
// transaction. Email is the only link from a login to a teacher's staff row and to
// a parent's children, so moving the login alone signs them in to an empty account.
//
// Moved: the login, the staff row, the parent's students, family records and
// enrolment requests. Discarded: outstanding invite/reset links and half-finished
// MFA sign-ins for the old address. Kept as written: audit, sent mail and authorship,
// which are history. Every session ends, because the old email is in its token.
func MoveAccountEmail(tx *Tx, userID, tenantID int, oldEmail, newEmail string) error {
	if _, err := tx.Exec(`UPDATE users SET email=?, sessions_invalid_before=NOW() WHERE id=?`, newEmail, userID); err != nil {
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
		return err
	}
	moves := []string{
		`UPDATE staff SET email=? WHERE email=? AND tenant_id=?`,
		`UPDATE students SET contact=? WHERE contact=? AND tenant_id=?`,
		`UPDATE families SET contact=? WHERE contact=? AND tenant_id=?`,
		`UPDATE registrations SET email=? WHERE email=? AND tenant_id=?`,
	}
	for _, q := range moves {
		if _, err := tx.Exec(q, newEmail, oldEmail, tenantID); err != nil {
			return err
		}
	}
	for _, q := range []string{`DELETE FROM email_tokens WHERE email=?`, `DELETE FROM mfa_intermediate WHERE email=?`} {
		if _, err := tx.Exec(q, oldEmail); err != nil {
			return err
		}
	}
	return nil
}

// 23505 is Postgres's unique_violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
