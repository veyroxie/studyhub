package core

import (
	"net/http"
	"os"
	"strings"
)

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

// AllowedRecipient gates every outbound message against OUTBOUND_ALLOWLIST, a
// comma-separated list of addresses. When set, mail to anyone else is dropped
// rather than sent — the safety valve for working on a live system whose
// address book is 24 real families. Empty list means normal operation.
//
// Deliberately a DROP, not a redirect: rewriting the recipient would put one
// parent's invoice in another person's inbox, which is worse than sending
// nothing.
//
// It lives in core rather than mailer because mailer imports store, so the
// email queue could not otherwise ask the question before it sends.
func AllowedRecipient(to string) bool {
	list := envEmailList("OUTBOUND_ALLOWLIST")
	return len(list) == 0 || listHasEmail(list, to)
}

// OutboundRestrictedTo is how many addresses OUTBOUND_ALLOWLIST lets mail reach; 0 means everyone.
func OutboundRestrictedTo() int {
	return len(envEmailList("OUTBOUND_ALLOWLIST"))
}

// envEmailList reads a comma-separated address list from the environment, blanks dropped.
func envEmailList(key string) []string {
	out := []string{}
	for _, e := range strings.Split(os.Getenv(key), ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func listHasEmail(list []string, email string) bool {
	for _, e := range list {
		if strings.EqualFold(e, strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

// IsDeveloper reports whether email belongs to the technical owner (DEVELOPER_EMAILS,
// comma-separated). Admins run the centre; developer screens are for this list only.
func IsDeveloper(email string) bool {
	return listHasEmail(envEmailList("DEVELOPER_EMAILS"), email)
}

// SendEmail delivers a message via the registered mailer. When no mailer has
// been registered yet (e.g. a test that didn't wire one) it is a no-op that
// reports success, matching the previous dev-mode behaviour.
//
// Suppression returns nil, not an error: a dozen handlers treat a send failure
// as a reason to log loudly or roll back, and a deliberate testing mute is
// neither. Callers that need to record the distinction (the email queue) ask
// AllowedRecipient first.
func SendEmail(to, subject, htmlBody string) error {
	if activeMailer == nil {
		return nil
	}
	if !AllowedRecipient(to) {
		Logger.Warn("outbound suppressed — recipient not in OUTBOUND_ALLOWLIST", "to", to, "subject", subject)
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
