package handlers

import (
	"net/http"
	"os"

	"studyhub/internal/core"
	"studyhub/internal/store"
)

// onlinePaymentReady is true only when checkout AND its webhook can both work. A
// gateway that can take money but cannot confirm it (no signing secret, so the
// webhook refuses) would leave parents paid and still marked unpaid.
func onlinePaymentReady() bool {
	set := func(keys ...string) bool {
		for _, k := range keys {
			if os.Getenv(k) == "" {
				return false
			}
		}
		return true
	}
	switch defaultPaymentProvider() {
	case "stripe":
		return set("STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET")
	case "billplz":
		return set("BILLPLZ_API_KEY", "BILLPLZ_COLLECTION_ID", "BILLPLZ_X_SIGNATURE")
	}
	return false
}

// HandlePaymentDetails tells the pay screen where to send money and whether online
// payment exists, so it shows bank details beside the amount and never offers a
// Pay Online button that can only fail.
//
// GET /api/payments/details
func HandlePaymentDetails(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil || (c.Role != "parent" && !core.IsAdminRole(c)) {
			core.RespondError(w, "forbidden", http.StatusForbidden)
			return
		}
		s := store.LoadTenantSettings(db, store.TenantID(c))
		core.Respond(w, map[string]any{
			"bankName":            s.BankName,
			"bankAccountNo":       s.BankAccountNo,
			"bankAccountHolder":   s.BankAccountHolder,
			"paymentInstructions": s.PaymentInstructions,
			"onlinePayment":       onlinePaymentReady(),
		})
	}
}
