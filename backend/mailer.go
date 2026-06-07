package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// safeName runs an untrusted display name (parent name, teacher name, etc.)
// through the stdlib HTML escaper before it's interpolated into an email
// template. Without this, a parent who registers as "<script>alert(1)</script>"
// would inject script tags into every email we send to them — most clients
// strip them, but it's still a leaked attack vector and ugly raw HTML.
//
// Stdlib escaping (not bluemonday) because the templates only ever take
// plain-text fields — there's no use case for allowing rich HTML from users.
func safeName(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
}

// mailer is the package-level email sender. In production it talks to Resend's
// HTTP API; in dev (no RESEND_API_KEY set) it logs the message to stdout so
// developers can copy verification links straight from the terminal.
//
// Resend's API is a single POST endpoint, so we don't pull in a third-party
// SDK — keeps the dependency tree small and the failure modes obvious.
var mailer Mailer

// Mailer is the interface used throughout the codebase. Tests can swap in a
// stub by assigning to the package-level `mailer` variable.
type Mailer interface {
	Send(to, subject, htmlBody string) error
}

// initMailer wires up the mailer at startup. Called from main().
func initMailer() {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("EMAIL_FROM")
	if from == "" {
		from = "The Study Hub <hello@studyhub.fit>"
	}

	if apiKey == "" {
		logger.Warn("mailer in dev mode — RESEND_API_KEY not set, emails will be logged instead of sent")
		mailer = &devMailer{from: from}
		return
	}
	mailer = &resendMailer{apiKey: apiKey, from: from, client: &http.Client{Timeout: 10 * time.Second}}
	logger.Info("mailer initialised", "provider", "resend", "from", from)
}

// appURL returns the public-facing base URL for building links inside emails.
// Defaults to https://studyhub.fit but can be overridden via APP_URL for staging.
func appURL() string {
	if v := os.Getenv("APP_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://studyhub.fit"
}

// ── Resend implementation ────────────────────────────────────────────────────

type resendMailer struct {
	apiKey string
	from   string
	client *http.Client
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

func (m *resendMailer) Send(to, subject, htmlBody string) error {
	body, err := json.Marshal(resendRequest{
		From:    m.from,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend %d: %s", resp.StatusCode, string(respBody))
	}
	logger.Info("email sent", "to", to, "subject", subject)
	return nil
}

// ── Dev fallback ─────────────────────────────────────────────────────────────

type devMailer struct {
	from string
}

func (m *devMailer) Send(to, subject, htmlBody string) error {
	// In dev mode we want the full HTML in the log so verification links are
	// click-able from the terminal. Use slog's structured shape but include
	// the body as a single block field.
	logger.Info("MAILER[DEV] would send email",
		"from", m.from,
		"to", to,
		"subject", subject,
		"body", htmlBody,
	)
	return nil
}

// ── Templates ────────────────────────────────────────────────────────────────

// All templates are plain string constants — there's only a handful and inline
// HTML keeps everything in one place. The %s placeholders are filled by the
// caller via fmt.Sprintf.

// emailBrand was the startup-once snapshot of tenant 1 settings. Replaced
// by brand() which reads live from the cached loadTenantSettings — that
// way an admin changing the brand via /api/admin/settings is reflected in
// the very next email/PDF without waiting for a restart. The 10-minute
// cache in tenant_settings.go means this still costs ~0 per render.
//
// Kept as a fallback when initEmailBrand hasn't been called yet (tests).
var emailBrand = defaultTenantSettings
var emailBrandDB *DB

// initEmailBrand wires the DB handle so brand() can fetch live values.
// Still pre-loads tenant 1 at boot so the very first email render after
// startup is warm.
func initEmailBrand(db *DB) {
	emailBrandDB = db
	emailBrand = loadTenantSettings(db, 1)
}

// brand returns the current tenant settings for use in mailer + PDF
// rendering. Reads through the 10-min cache, picks up admin PUTs at the
// next request (which already invalidates the cache via
// invalidateTenantSettings).
func brand() *TenantSettings {
	if emailBrandDB == nil {
		return emailBrand
	}
	return loadTenantSettings(emailBrandDB, 1)
}

func emailLayoutOpen() string {
	return `<!DOCTYPE html>
<html><head><meta charset="utf-8"></head>
<body style="margin:0;padding:0;background:#f5f5f0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#1a1a1a">
<table width="100%" cellpadding="0" cellspacing="0" style="background:#f5f5f0;padding:32px 16px">
<tr><td align="center">
<table width="560" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:14px;border:1px solid rgba(0,0,0,0.07);box-shadow:0 2px 8px rgba(0,0,0,0.04);overflow:hidden">
<tr><td style="padding:32px 40px 0">
  <div style="font-family:Georgia,'Times New Roman',serif;font-size:1.75rem;font-weight:700;color:#0a0a0a;letter-spacing:-0.02em">` + safeName(brand().BrandName) + `</div>
  <div style="height:2px;width:48px;background:` + brand().PrimaryColor + `;margin:12px 0 24px"></div>
</td></tr>
<tr><td style="padding:0 40px 32px;font-size:15px;line-height:1.6;color:#374151">`
}

func emailLayoutClose() string {
	return `
</td></tr>
<tr><td style="padding:24px 40px;background:#fafaf8;border-top:1px solid #f0eee8;font-size:12px;color:#94a3b8;line-height:1.5">
  This email was sent by ` + safeName(brand().BrandName) + `. If you weren't expecting it, you can safely ignore it.
</td></tr>
</table>
</td></tr>
</table>
</body></html>`
}

// renderVerifyParentEmail builds the welcome + verification email sent when
// a parent registers via the public form. The single CTA verifies the email
// and immediately logs them in — no separate "first login" step.
func renderVerifyParentEmail(name, verifyURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + safeName(name)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">Welcome to The Study Hub! Click the button below to verify your email and finish setting up your parent account. The link is valid for the next 24 hours.</p>
<p style="margin:24px 0;text-align:center">
  <a href="` + verifyURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">Verify email &amp; sign in</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#64748b">If the button doesn't work, paste this link into your browser:</p>
<p style="margin:0 0 16px;font-size:12px;color:#94a3b8;word-break:break-all">` + verifyURL + `</p>
<p style="margin:24px 0 8px;font-size:13px;color:#64748b">After you verify, our team will link your child's profile to your account. You'll be able to see schedules, billing, and feedback in real time as soon as that's done.</p>
<p style="margin:0;font-size:13px;color:#64748b">Didn't sign up? You can safely ignore this email — no account will be activated.</p>` +
		emailLayoutClose()
}

// renderVerifyTeacherEmail is sent when a teacher applies via the public
// form. Unlike the parent flow this does NOT auto-log them in — verification
// only confirms the email is real, after which the application sits in the
// admin queue for review.
func renderVerifyTeacherEmail(name, verifyURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + safeName(name)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">Thanks for applying to teach at The Study Hub. Click the button below to confirm your email address — this lets us know to take your application seriously and avoid lost-in-spam mishaps.</p>
<p style="margin:24px 0;text-align:center">
  <a href="` + verifyURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">Confirm email</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#64748b">If the button doesn't work, paste this link into your browser:</p>
<p style="margin:0 0 16px;font-size:12px;color:#94a3b8;word-break:break-all">` + verifyURL + `</p>
<p style="margin:24px 0 0;font-size:13px;color:#64748b">After you confirm, our team will review your application. We aim to respond within 3-5 business days. If you're approved we'll send a follow-up email with a link to set your password and access your teacher account.</p>` +
		emailLayoutClose()
}

// renderTeacherWelcomeEmail is sent when admin approves a teacher application.
// The CTA takes them to /set-password.html where they choose their first
// password and are immediately logged into their teacher account.
func renderTeacherWelcomeEmail(name, setPasswordURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + safeName(name)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">Welcome aboard — your application has been approved. Click below to set your password and sign in to your teacher account for the first time. The link is valid for the next 24 hours.</p>
<p style="margin:24px 0;text-align:center">
  <a href="` + setPasswordURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">Set password &amp; sign in</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#64748b">If the button doesn't work, paste this link into your browser:</p>
<p style="margin:0 0 16px;font-size:12px;color:#94a3b8;word-break:break-all">` + setPasswordURL + `</p>
<p style="margin:24px 0 8px;font-size:13px;color:#64748b">Once you're in, you'll see your assigned classes, attendance check-in, and student feedback tools. If anything looks off or you have questions, just reply to this email.</p>` +
		emailLayoutClose()
}

// renderParentWelcomeEmail is sent when admin manually adds a student whose
// parent doesn't already have a login account. The link lets the parent
// pick their own password — no temp credentials are shared by hand.
func renderParentWelcomeEmail(name, setPasswordURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + safeName(name)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">A Study Hub account has been set up for you. Click below to choose your password and sign in for the first time. The link is valid for 24 hours.</p>
<p style="margin:24px 0;text-align:center">
  <a href="` + setPasswordURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">Set password &amp; sign in</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#64748b">If the button doesn't work, paste this link into your browser:</p>
<p style="margin:0 0 16px;font-size:12px;color:#94a3b8;word-break:break-all">` + setPasswordURL + `</p>
<p style="margin:24px 0 8px;font-size:13px;color:#64748b">Once you're in, you'll see your child's classes, attendance and billing. Reply to this email if anything looks off.</p>` +
		emailLayoutClose()
}

// renderEnrollmentApprovedEmail is sent when admin approves a child enrollment
// request. It tells the parent the outcome and optionally lists assigned classes.
func renderEnrollmentApprovedEmail(parentName, studentName, classListHTML string) string {
	greeting := "Hi"
	if strings.TrimSpace(parentName) != "" {
		greeting = "Hi " + safeName(parentName)
	}
	classSection := ""
	if classListHTML != "" {
		classSection = `<p style="margin:16px 0 8px;font-weight:600;color:#0a0a0a">Assigned classes:</p>` + classListHTML
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">Great news — <strong>` + safeName(studentName) + `</strong> has been enrolled at The Study Hub.</p>` +
		classSection +
		`<p style="margin:16px 0 0;font-size:13px;color:#64748b">Log in to your parent portal to see the full schedule, billing, and teacher feedback. If anything looks off, just reply to this email.</p>` +
		emailLayoutClose()
}

// renderPaymentReceivedEmail confirms to the parent that their "I've Paid"
// submission was received and is now awaiting admin verification. This closes
// the feedback loop so parents stop WhatsApping "did you get my payment?".
func renderPaymentReceivedEmail(parentName, description, amountRM, method string) string {
	greeting := "Hi"
	if strings.TrimSpace(parentName) != "" {
		greeting = "Hi " + safeName(parentName)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">We've received your payment submission. Here are the details:</p>
<table cellpadding="0" cellspacing="0" style="margin:18px 0;background:#fafaf8;border:1px solid #f0eee8;border-radius:10px;width:100%">
<tr><td style="padding:14px 18px;font-size:13px;color:#64748b">
  <div style="margin-bottom:6px"><strong style="color:#0a0a0a">` + html.EscapeString(description) + `</strong></div>
  <div>Amount: <strong style="color:#0a0a0a">RM ` + html.EscapeString(amountRM) + `</strong></div>` +
		func() string {
			if method != "" {
				return `<div>Method: ` + html.EscapeString(method) + `</div>`
			}
			return ""
		}() + `
</td></tr>
</table>
<p style="margin:0;font-size:13px;color:#64748b">Our team will verify your payment within 1-2 business days. You'll see the status update to "Paid" on your billing page once it's confirmed. No further action needed from you.</p>` +
		emailLayoutClose()
}

// renderInvoiceReminderEmail is sent automatically by the background job
// when an unpaid invoice goes overdue. The deduplication logic in the job
// itself prevents the same invoice from generating reminders more often
// than every 3 days.
func renderInvoiceReminderEmail(parentName, studentName, description, amountRM, dueDate, daysOverdueLabel, billingURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(parentName) != "" {
		greeting = "Hi " + safeName(parentName)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">This is a friendly reminder that ` + safeName(studentName) + `'s invoice is currently <strong>` + html.EscapeString(daysOverdueLabel) + `</strong>.</p>
<table cellpadding="0" cellspacing="0" style="margin:18px 0;background:#fafaf8;border:1px solid #f0eee8;border-radius:10px;width:100%">
<tr><td style="padding:14px 18px;font-size:13px;color:#64748b">
  <div style="margin-bottom:6px"><strong style="color:#0a0a0a">` + html.EscapeString(description) + `</strong></div>
  <div>Amount: <strong style="color:#0a0a0a">RM ` + html.EscapeString(amountRM) + `</strong></div>
  <div>Due date: ` + html.EscapeString(dueDate) + `</div>
</td></tr>
</table>
<p style="margin:18px 0;text-align:center">
  <a href="` + billingURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">View &amp; pay</a>
</p>
<p style="margin:0 0 12px;font-size:13px;color:#64748b">Already paid? Just ignore this email — once we mark it received, you'll stop hearing from us. If something's wrong with the invoice, reply to this email and we'll sort it out.</p>` +
		emailLayoutClose()
}

// renderInvoiceIssuedEmail is sent when the monthly cron generates a new
// invoice, so parents are notified the moment it's issued (not only when it
// goes overdue). earlyBirdNote is the optional "pay by X to keep the discount"
// line — empty when no early-bird applies.
func renderInvoiceIssuedEmail(parentName, studentName, description, amountRM, dueDate, earlyBirdNote string) string {
	greeting := "Hi"
	if strings.TrimSpace(parentName) != "" {
		greeting = "Hi " + safeName(parentName)
	}
	noteHTML := ""
	if strings.TrimSpace(earlyBirdNote) != "" {
		noteHTML = `<p style="margin:0 0 16px;font-size:13px;color:#92400e"><strong>` + html.EscapeString(earlyBirdNote) + `</strong></p>`
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">A new invoice has been issued for ` + safeName(studentName) + `.</p>
<table cellpadding="0" cellspacing="0" style="margin:18px 0;background:#fafaf8;border:1px solid #f0eee8;border-radius:10px;width:100%">
<tr><td style="padding:14px 18px;font-size:13px;color:#64748b">
  <div style="margin-bottom:6px"><strong style="color:#0a0a0a">` + html.EscapeString(description) + `</strong></div>
  <div>Amount: <strong style="color:#0a0a0a">RM ` + html.EscapeString(amountRM) + `</strong></div>
  <div>Due date: ` + html.EscapeString(dueDate) + `</div>
</td></tr>
</table>` + noteHTML + `
<p style="margin:18px 0;text-align:center">
  <a href="` + appURL() + `/#billing" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">View &amp; pay</a>
</p>
<p style="margin:0 0 12px;font-size:13px;color:#64748b">Already paid? You can ignore this email. Questions about the invoice? Just reply and we'll sort it out.</p>` +
		emailLayoutClose()
}

// renderResetPasswordEmail builds the HTML body for a password reset email.
func renderResetPasswordEmail(name, resetURL string) string {
	greeting := "Hi"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + safeName(name)
	}
	return emailLayoutOpen() +
		`<p style="margin:0 0 16px;font-size:16px;color:#0a0a0a">` + greeting + `,</p>
<p style="margin:0 0 16px">We received a request to reset your password for The Study Hub. Click the button below to choose a new one — the link is valid for the next 60 minutes.</p>
<p style="margin:24px 0;text-align:center">
  <a href="` + resetURL + `" style="display:inline-block;padding:12px 28px;background:#C9A227;color:#0a0a0a;font-weight:700;text-decoration:none;border-radius:8px;font-size:15px">Reset password</a>
</p>
<p style="margin:0 0 8px;font-size:13px;color:#64748b">If the button doesn't work, paste this link into your browser:</p>
<p style="margin:0 0 16px;font-size:12px;color:#94a3b8;word-break:break-all">` + resetURL + `</p>
<p style="margin:24px 0 0;font-size:13px;color:#64748b">Didn't request a reset? You can safely ignore this email — your password won't change.</p>` +
		emailLayoutClose()
}
