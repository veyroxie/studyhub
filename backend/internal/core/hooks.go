package core

import "net/http"

// This file holds the small set of behaviour hooks that let lower layers
// (store) invoke functionality owned by higher layers (mailer, auth) without
// creating an import cycle. The owning package registers its implementation at
// startup; callers use the package-level accessor.

// Mailer is the interface used throughout the codebase to send transactional
// email. The concrete implementation lives in the mailer package and registers
// itself via SetMailer at startup. Tests can swap in a stub the same way.
type Mailer interface {
	Send(to, subject, htmlBody string) error
}

var activeMailer Mailer

// SetMailer registers the process-wide mailer. Called by mailer.Init().
func SetMailer(m Mailer) { activeMailer = m }

// SendEmail delivers a message via the registered mailer. When no mailer has
// been registered yet (e.g. a test that didn't wire one) it is a no-op that
// reports success, matching the previous dev-mode behaviour.
func SendEmail(to, subject, htmlBody string) error {
	if activeMailer == nil {
		return nil
	}
	return activeMailer.Send(to, subject, htmlBody)
}

// IssueAuthCookieFunc writes the access-token cookie for a freshly
// authenticated user. The implementation lives in the auth package (it owns
// JWT signing) and registers itself via SetIssueAuthCookie. store.handleRefresh
// calls it to mint a new access cookie during token rotation.
type IssueAuthCookieFunc func(w http.ResponseWriter, r *http.Request, userID, tenantID int, email, role, name string, rememberMe bool)

var issueAuthCookie IssueAuthCookieFunc

// SetIssueAuthCookie registers the auth-cookie issuer. Called by auth at init.
func SetIssueAuthCookie(fn IssueAuthCookieFunc) { issueAuthCookie = fn }

// IssueAuthCookie writes the access-token cookie via the registered issuer.
// No-op when unregistered.
func IssueAuthCookie(w http.ResponseWriter, r *http.Request, userID, tenantID int, email, role, name string, rememberMe bool) {
	if issueAuthCookie == nil {
		return
	}
	issueAuthCookie(w, r, userID, tenantID, email, role, name, rememberMe)
}
