package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/mailer"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

// Payment gateway integration. Two providers are wired:
//
//   Billplz (Malaysian preferred — FPX, e-wallets, cards via 3-party)
//     env: BILLPLZ_API_KEY, BILLPLZ_COLLECTION_ID, BILLPLZ_X_SIGNATURE,
//          BILLPLZ_API_BASE (default https://www.billplz.com/api)
//
//   Stripe (international cards)
//     env: STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET,
//          STRIPE_API_BASE (default https://api.stripe.com)
//
// Provider selection per checkout request via ?provider=billplz|stripe;
// defaults to PAYMENT_PROVIDER env var or "billplz". Webhooks are mounted
// at /api/payments/webhook/billplz and /api/payments/webhook/stripe.
//
// On webhook success the matching invoice is marked Paid via the existing
// handler logic — reference_no is the gateway's transaction id, so the
// non-cash ref enforcement is automatically satisfied.

func defaultPaymentProvider() string {
	if v := os.Getenv("PAYMENT_PROVIDER"); v != "" {
		return v
	}
	return "billplz"
}

// handlePaymentCheckout creates a hosted checkout for the given invoice.
// Returns {url} which the frontend assigns to window.location.
//
// POST /api/invoices/{id}/checkout?provider=billplz
func HandlePaymentCheckout(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		id := chi.URLParam(r, "id")
		tw, twArgs := store.ScopeTenant(c, "")
		var studentID, description string
		var amount float64
		readArgs := append([]any{id}, twArgs...)
		if err := db.QueryRow(`SELECT student_id, description, amount FROM invoices WHERE id=? AND deleted_at IS NULL AND status<>'Paid'`+tw, readArgs...).Scan(&studentID, &description, &amount); err != nil {
			core.RespondError(w, "invoice not found or already paid", http.StatusNotFound)
			return
		}
		if c.Role == "parent" {
			var ownerEmail string
			stuArgs := append([]any{studentID}, twArgs...)
			db.QueryRow(`SELECT contact FROM students WHERE id=?`+tw, stuArgs...).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				core.RespondError(w, "not your invoice", http.StatusForbidden)
				return
			}
		}

		provider := r.URL.Query().Get("provider")
		if provider == "" {
			provider = defaultPaymentProvider()
		}

		var checkoutURL string
		var err error
		switch provider {
		case "billplz":
			checkoutURL, err = createBillplzBill(id, c.Email, c.Name, description, amount)
		case "stripe":
			checkoutURL, err = createStripeCheckout(id, c.Email, description, amount)
		default:
			core.RespondError(w, "unknown payment provider", http.StatusBadRequest)
			return
		}
		if err != nil {
			core.Logger.Error("checkout create failed", "err", err, "provider", provider, "invoice_id", id)
			// Surface the actual reason to admin/superadmin so the first-deploy
			// "I set the keys but it still doesn't work" loop is debuggable.
			// Parents get the generic 502 — they can't act on config errors.
			msg := "payment gateway error"
			if core.IsAdminRole(c) {
				msg = err.Error()
			}
			core.RespondError(w, msg, http.StatusBadGateway)
			return
		}
		core.LogAudit(db, c.Email, "payment_checkout_created", "invoice", id, provider)
		core.Respond(w, map[string]string{"url": checkoutURL, "provider": provider})
	}
}

// ── Billplz ─────────────────────────────────────────────────────────────────

func billplzBase() string {
	if v := os.Getenv("BILLPLZ_API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://www.billplz.com/api"
}

func createBillplzBill(invoiceID, email, name, description string, amount float64) (string, error) {
	apiKey := os.Getenv("BILLPLZ_API_KEY")
	collection := os.Getenv("BILLPLZ_COLLECTION_ID")
	if apiKey == "" || collection == "" {
		return "", errors.New("billplz not configured: set BILLPLZ_API_KEY + BILLPLZ_COLLECTION_ID")
	}
	form := url.Values{}
	form.Set("collection_id", collection)
	form.Set("email", email)
	if name != "" {
		form.Set("name", name)
	}
	form.Set("amount", strconv.FormatFloat(amount*100, 'f', 0, 64)) // sen
	form.Set("description", description)
	form.Set("reference_1_label", "InvoiceID")
	form.Set("reference_1", invoiceID)
	form.Set("redirect_url", mailer.AppURL()+"/#billing?paid="+invoiceID)
	form.Set("callback_url", mailer.AppURL()+"/api/payments/webhook/billplz")

	req, err := http.NewRequest("POST", billplzBase()+"/v3/bills", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(apiKey, "")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("billplz %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// handleBillplzWebhook processes Billplz payment callbacks. Body is form-encoded.
// x_signature is HMAC-SHA256 over the alphabetically-sorted source string.
func HandleBillplzWebhook(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			core.RespondError(w, "bad body", 400)
			return
		}
		// Verify signature.
		secret := os.Getenv("BILLPLZ_X_SIGNATURE")
		if secret != "" {
			given := r.Form.Get("x_signature")
			if !verifyBillplzSignature(r.Form, secret, given) {
				core.RespondError(w, "invalid signature", http.StatusForbidden)
				return
			}
		}
		invoiceID := r.Form.Get("reference_1")
		paid := r.Form.Get("paid") == "true"
		billID := r.Form.Get("id")
		if invoiceID == "" || !paid {
			w.WriteHeader(http.StatusOK)
			return // not our concern or not paid yet
		}
		// Mark invoice paid. We don't have a Claims context here — system action.
		now := core.Today()
		res, err := db.Exec(
			`UPDATE invoices SET status='Paid', paid_on=?, payment_method='Billplz', reference_no=? WHERE id=? AND deleted_at IS NULL AND status<>'Paid'`,
			now, billID, invoiceID,
		)
		if err != nil {
			core.Logger.Error("billplz webhook update failed", "err", err, "invoice_id", invoiceID)
			core.RespondError(w, "db error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			core.Logger.Info("billplz webhook: invoice marked paid", "invoice_id", invoiceID, "bill_id", billID)
			core.LogAudit(db, "billplz", "invoice_paid", "invoice", invoiceID, "via webhook")
			var studentID string
			db.QueryRow(`SELECT student_id FROM invoices WHERE id=?`, invoiceID).Scan(&studentID)
			if studentID != "" {
				store.ReferralCheckMilestoneOnPay(db, studentID, nil)
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

// verifyBillplzSignature implements Billplz's HMAC scheme: sort the
// non-signature keys alphabetically, join "key|value" with the pipe,
// HMAC-SHA256 with the X-Signature secret, compare to x_signature.
func verifyBillplzSignature(form url.Values, secret, given string) bool {
	if given == "" {
		return false
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		if k == "x_signature" {
			continue
		}
		keys = append(keys, k)
	}
	// Lexicographic sort.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(k)
		b.WriteString(form.Get(k))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(b.String()))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(given))
}

// ── Stripe ──────────────────────────────────────────────────────────────────

func stripeBase() string {
	if v := os.Getenv("STRIPE_API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.stripe.com"
}

func createStripeCheckout(invoiceID, email, description string, amount float64) (string, error) {
	apiKey := os.Getenv("STRIPE_SECRET_KEY")
	if apiKey == "" {
		return "", errors.New("stripe not configured: set STRIPE_SECRET_KEY")
	}
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", mailer.AppURL()+"/#billing?paid="+invoiceID)
	form.Set("cancel_url", mailer.AppURL()+"/#billing")
	form.Set("customer_email", email)
	form.Set("client_reference_id", invoiceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", "myr")
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatFloat(amount*100, 'f', 0, 64))
	form.Set("line_items[0][price_data][product_data][name]", description)
	form.Set("metadata[invoice_id]", invoiceID)

	req, err := http.NewRequest("POST", stripeBase()+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("stripe %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// handleStripeWebhook processes Stripe checkout.session.completed events.
// Verifies the signature via Stripe-Signature header against
// STRIPE_WEBHOOK_SECRET.
func HandleStripeWebhook(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read body raw — signature is computed over the exact bytes.
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			core.RespondError(w, "read failed", 400)
			return
		}
		secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
		if secret != "" {
			if !verifyStripeSignature(body, r.Header.Get("Stripe-Signature"), secret) {
				core.RespondError(w, "invalid signature", http.StatusForbidden)
				return
			}
		}
		// Parse the parts we care about.
		var evt struct {
			Type string `json:"type"`
			Data struct {
				Object struct {
					ID            string `json:"id"`
					ClientRefID   string `json:"client_reference_id"`
					PaymentStatus string `json:"payment_status"`
					Metadata      struct {
						InvoiceID string `json:"invoice_id"`
					} `json:"metadata"`
				} `json:"object"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &evt); err != nil {
			core.RespondError(w, "bad event", 400)
			return
		}
		if evt.Type != "checkout.session.completed" {
			w.WriteHeader(http.StatusOK)
			return
		}
		invoiceID := evt.Data.Object.Metadata.InvoiceID
		if invoiceID == "" {
			invoiceID = evt.Data.Object.ClientRefID
		}
		if invoiceID == "" || evt.Data.Object.PaymentStatus != "paid" {
			w.WriteHeader(http.StatusOK)
			return
		}
		now := core.Today()
		res, err := db.Exec(
			`UPDATE invoices SET status='Paid', paid_on=?, payment_method='Stripe', reference_no=? WHERE id=? AND deleted_at IS NULL AND status<>'Paid'`,
			now, evt.Data.Object.ID, invoiceID,
		)
		if err != nil {
			core.Logger.Error("stripe webhook update failed", "err", err, "invoice_id", invoiceID)
			core.RespondError(w, "db error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			core.Logger.Info("stripe webhook: invoice marked paid", "invoice_id", invoiceID, "session_id", evt.Data.Object.ID)
			core.LogAudit(db, "stripe", "invoice_paid", "invoice", invoiceID, "via webhook")
			var studentID string
			db.QueryRow(`SELECT student_id FROM invoices WHERE id=?`, invoiceID).Scan(&studentID)
			if studentID != "" {
				store.ReferralCheckMilestoneOnPay(db, studentID, nil)
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

// verifyStripeSignature checks the t= timestamp and v1= hex signature
// embedded in the Stripe-Signature header.
func verifyStripeSignature(body []byte, header, secret string) bool {
	if header == "" {
		return false
	}
	var t, sig string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			t = kv[1]
		case "v1":
			sig = kv[1]
		}
	}
	if t == "" || sig == "" {
		return false
	}
	payload := t + "." + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// jsonReader returns a bytes.Reader for JSON-encoded v. Used by tests.
func jsonReader(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}
